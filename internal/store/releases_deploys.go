package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func releaseDeployProjects(ctx context.Context, tx pgx.Tx, access ManagementAccess, id int64) ([]ManagementProject, error) {
	rows, err := tx.Query(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at FROM release_deploy_projects dp
		JOIN projects p ON p.id=dp.project_id WHERE dp.deploy_id=$1 AND p.organization_id=$2::bigint
		AND ($3 OR p.id=ANY($4::bigint[])) ORDER BY p.id`, id, access.OrganizationID, access.AllProjects, access.ProjectIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ManagementProject{}
	for rows.Next() {
		var p ManagementProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Platform, &p.DateCreated); err != nil {
			return nil, err
		}
		p.DateCreated = p.DateCreated.UTC()
		result = append(result, p)
	}
	return result, rows.Err()
}

func releaseDeployRows(ctx context.Context, tx pgx.Tx, access ManagementAccess, id string, offset, limit int) ([]ReleaseDeploy, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,d.environment,d.name,d.url,d.created_at,d.date_started,d.date_finished
		FROM release_deploys d WHERE d.release_id=$1::bigint AND EXISTS(SELECT 1 FROM release_deploy_projects dp
		WHERE dp.deploy_id=d.id AND ($2 OR dp.project_id=ANY($3::bigint[])))
		ORDER BY d.created_at DESC,d.id DESC LIMIT $4 OFFSET $5`, id, access.AllProjects, access.ProjectIDs, limit+1, offset)
	if err != nil {
		return nil, err
	}
	result := []ReleaseDeploy{}
	for rows.Next() {
		var d ReleaseDeploy
		if err := rows.Scan(&d.ID, &d.Environment, &d.Name, &d.URL, &d.DateCreated, &d.DateStarted, &d.DateFinished); err != nil {
			rows.Close()
			return nil, err
		}
		d.DateCreated = d.DateCreated.UTC()
		d.DateStarted = releaseTimeUTC(d.DateStarted)
		d.DateFinished = d.DateFinished.UTC()
		result = append(result, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Projects, err = releaseDeployProjects(ctx, tx, access, result[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) CreateReleaseDeploy(ctx context.Context, access ManagementAccess, version ReleaseVersion, input ReleaseDeployInput) (ReleaseDeploy, error) {
	tx, _, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return ReleaseDeploy{}, err
	}
	defer tx.Rollback(ctx)
	id, _, err := releaseIdentity(ctx, tx, access.OrganizationID, version, false)
	if err != nil {
		return ReleaseDeploy{}, err
	}
	if err := releaseVisible(ctx, tx, access, id, ""); err != nil {
		return ReleaseDeploy{}, err
	}
	associated, err := releaseAssociatedProjects(ctx, tx, id)
	if err != nil {
		return ReleaseDeploy{}, err
	}
	projects := associated
	if len(input.Projects) == 0 {
		if err := releaseCoverage(access, associated); err != nil {
			return ReleaseDeploy{}, err
		}
	} else {
		projects, err = releaseProjectReferences(ctx, tx, access, input.Projects)
		if err != nil {
			return ReleaseDeploy{}, err
		}
		allowed := map[int64]bool{}
		for _, project := range associated {
			allowed[project] = true
		}
		for _, project := range projects {
			if !allowed[project] {
				return ReleaseDeploy{}, ErrNotFound
			}
		}
	}
	if err := releaseLockProjects(ctx, tx, access.OrganizationID, projects); err != nil {
		return ReleaseDeploy{}, err
	}
	var receipt time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&receipt); err != nil {
		return ReleaseDeploy{}, err
	}
	finished := receipt
	if input.DateFinished != nil {
		finished = *input.DateFinished
	}
	if input.DateStarted != nil && finished.Before(*input.DateStarted) {
		return ReleaseDeploy{}, ErrInvalidReleaseInput
	}
	var d ReleaseDeploy
	err = tx.QueryRow(ctx, `INSERT INTO release_deploys(release_id,environment,name,url,created_at,date_started,date_finished)
		VALUES($1::bigint,$2,$3,$4,$5,$6,$7) RETURNING id,environment,name,url,created_at,date_started,date_finished`, id, input.Environment, input.Name, input.URL, receipt, input.DateStarted, finished).
		Scan(&d.ID, &d.Environment, &d.Name, &d.URL, &d.DateCreated, &d.DateStarted, &d.DateFinished)
	if err != nil {
		return ReleaseDeploy{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO release_deploy_projects(deploy_id,release_id,project_id) SELECT $1,$2::bigint,unnest($3::bigint[])`, d.ID, id, projects); err != nil {
		return ReleaseDeploy{}, err
	}
	d.DateCreated = d.DateCreated.UTC()
	d.DateStarted = releaseTimeUTC(d.DateStarted)
	d.DateFinished = d.DateFinished.UTC()
	d.Projects, err = releaseDeployProjects(ctx, tx, access, d.ID)
	if err != nil {
		return ReleaseDeploy{}, err
	}
	return d, tx.Commit(ctx)
}

func (s *Store) ReleaseDeploys(ctx context.Context, access ManagementAccess, version ReleaseVersion, offset, limit int) (ReleaseDeploysPage, error) {
	result := ReleaseDeploysPage{Data: []ReleaseDeploy{}}
	tx, _, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM releases WHERE organization_id=$1::bigint AND version=$2`, access.OrganizationID, string(version)).Scan(&id); err != nil {
		return result, dbError(err)
	}
	if err := releaseVisible(ctx, tx, access, id, ""); err != nil {
		return result, err
	}
	result.Data, err = releaseDeployRows(ctx, tx, access, id, offset, limit)
	if err != nil {
		return result, err
	}
	if len(result.Data) > limit {
		result.HasMore = true
		result.Data = result.Data[:limit]
	}
	if offset > 0 {
		before, err := releaseDeployRows(ctx, tx, access, id, max(0, offset-limit), 1)
		if err != nil {
			return result, err
		}
		result.HasPrevious = len(before) > 0
	}
	return result, tx.Commit(ctx)
}
