package store

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) releaseTransaction(ctx context.Context, access ManagementAccess, write bool) (pgx.Tx, string, error) {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		options = pgx.TxOptions{}
	}
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, "", err
	}
	query := `SELECT role FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`
	if write {
		query += " FOR SHARE"
	}
	var role string
	if err = tx.QueryRow(ctx, query, access.OrganizationID, access.UserID).Scan(&role); err != nil {
		_ = tx.Rollback(ctx)
		return nil, "", dbError(err)
	}
	if write && (access.ReadOnly || role != "owner" && role != "admin") {
		_ = tx.Rollback(ctx)
		return nil, "", ErrForbidden
	}
	return tx, role, nil
}

func releaseTimeUTC(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func releaseProjectAllowed(access ManagementAccess, id int64) bool {
	if access.AllProjects {
		return true
	}
	for _, selected := range access.ProjectIDs {
		if selected == id {
			return true
		}
	}
	return false
}

func releaseProjectReferences(ctx context.Context, tx pgx.Tx, access ManagementAccess, references []string) ([]int64, error) {
	selected := map[int64]bool{}
	for _, reference := range references {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE organization_id=$1::bigint
			AND (slug=$2 OR id=$3) AND ($4 OR id=ANY($5::bigint[]))
			ORDER BY (slug=$2) DESC LIMIT 1`, access.OrganizationID, reference, referenceID(reference), access.AllProjects, access.ProjectIDs).Scan(&id)
		if err != nil {
			return nil, dbError(err)
		}
		if !releaseProjectAllowed(access, id) {
			return nil, ErrNotFound
		}
		selected[id] = true
	}
	ids := make([]int64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func releaseLockProjects(ctx context.Context, tx pgx.Tx, orgID string, ids []int64) error {
	rows, err := tx.Query(ctx, `SELECT id FROM projects WHERE organization_id=$1::bigint AND id=ANY($2::bigint[]) ORDER BY id FOR SHARE`, orgID, ids)
	if err != nil {
		return err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	if len(found) != len(ids) {
		return ErrNotFound
	}
	return nil
}

func releaseIdentity(ctx context.Context, tx pgx.Tx, orgID string, version ReleaseVersion, create bool) (string, bool, error) {
	for {
		var id string
		if create {
			err := tx.QueryRow(ctx, `INSERT INTO releases(organization_id,version) VALUES($1::bigint,$2)
				ON CONFLICT(organization_id,version) DO NOTHING RETURNING id::text`, orgID, string(version)).Scan(&id)
			if err == nil {
				return id, true, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return "", false, dbError(err)
			}
		}
		err := tx.QueryRow(ctx, `SELECT id::text FROM releases WHERE organization_id=$1::bigint AND version=$2 FOR UPDATE`, orgID, string(version)).Scan(&id)
		if create && errors.Is(err, pgx.ErrNoRows) {
			// A concurrent deletion can remove the conflicting identity before this
			// statement gets its row lock. Retry against a fresh statement snapshot.
			continue
		}
		return id, false, dbError(err)
	}
}

func releaseAssociatedProjects(ctx context.Context, tx pgx.Tx, id string) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT project_id FROM release_projects WHERE release_id=$1::bigint ORDER BY project_id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func releaseCoverage(access ManagementAccess, ids []int64) error {
	visible := 0
	for _, id := range ids {
		if releaseProjectAllowed(access, id) {
			visible++
		}
	}
	if visible == 0 {
		return ErrNotFound
	}
	if visible != len(ids) {
		return ErrForbidden
	}
	return nil
}

func releaseVisible(ctx context.Context, tx pgx.Tx, access ManagementAccess, id, projectRef string) error {
	ids := []int64{}
	var err error
	if projectRef != "" {
		ids, err = releaseProjectReferences(ctx, tx, access, []string{projectRef})
		if err != nil {
			return err
		}
	}
	var visible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM release_projects rp WHERE rp.release_id=$1::bigint
		AND ($2 OR rp.project_id=ANY($3::bigint[])) AND (cardinality($4::bigint[])=0 OR rp.project_id=ANY($4::bigint[])))`, id, access.AllProjects, access.ProjectIDs, ids).Scan(&visible)
	if err != nil {
		return err
	}
	if !visible {
		return ErrNotFound
	}
	return nil
}

func releaseProjects(ctx context.Context, tx pgx.Tx, access ManagementAccess, id string) ([]ManagementProject, error) {
	rows, err := tx.Query(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at FROM projects p
		JOIN release_projects rp ON rp.project_id=p.id WHERE rp.release_id=$1::bigint
		AND p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[])) ORDER BY p.id`, id, access.OrganizationID, access.AllProjects, access.ProjectIDs)
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

func loadRelease(ctx context.Context, tx pgx.Tx, access ManagementAccess, role, id string) (Release, error) {
	var r Release
	err := tx.QueryRow(ctx, `SELECT id::text,version,ref,url,status,created_at,date_started,date_released,
		(SELECT count(*) FROM release_commits WHERE release_id=releases.id)
		FROM releases WHERE id=$1::bigint AND organization_id=$2::bigint`, id, access.OrganizationID).
		Scan(&r.ID, &r.Version, &r.Ref, &r.URL, &r.Status, &r.DateCreated, &r.DateStarted, &r.DateReleased, &r.CommitCount)
	if err != nil {
		return Release{}, dbError(err)
	}
	r.DateCreated = r.DateCreated.UTC()
	r.DateStarted = releaseTimeUTC(r.DateStarted)
	r.DateReleased = releaseTimeUTC(r.DateReleased)
	r.ShortVersion = r.Version
	r.Projects, err = releaseProjects(ctx, tx, access, id)
	if err != nil {
		return Release{}, err
	}
	if len(r.Projects) == 0 {
		return Release{}, ErrNotFound
	}
	ids, err := releaseAssociatedProjects(ctx, tx, id)
	if err != nil {
		return Release{}, err
	}
	r.CanWrite = !access.ReadOnly && (role == "owner" || role == "admin") && releaseCoverage(access, ids) == nil
	commits, err := releaseCommitRows(ctx, tx, id, 0, 1)
	if err != nil {
		return Release{}, err
	}
	if len(commits) > 0 {
		r.LastCommit = &commits[0]
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM release_deploys d WHERE d.release_id=$1::bigint
		AND EXISTS(SELECT 1 FROM release_deploy_projects dp WHERE dp.deploy_id=d.id AND ($2 OR dp.project_id=ANY($3::bigint[])))`, id, access.AllProjects, access.ProjectIDs).Scan(&r.DeployCount)
	if err != nil {
		return Release{}, err
	}
	deploys, err := releaseDeployRows(ctx, tx, access, id, 0, 1)
	if err != nil {
		return Release{}, err
	}
	if len(deploys) > 0 {
		r.LastDeploy = &deploys[0]
	}
	return r, nil
}

func applyReleaseUpdate(ctx context.Context, tx pgx.Tx, orgID, id string, update ReleaseUpdate) error {
	_, err := tx.Exec(ctx, `UPDATE releases SET ref=CASE WHEN $2 THEN $3 ELSE ref END,url=CASE WHEN $4 THEN $5 ELSE url END,
		date_started=CASE WHEN $6 THEN $7::timestamptz ELSE date_started END,
		date_released=CASE WHEN $8 THEN $9::timestamptz ELSE date_released END,
		status=COALESCE($10::text,status) WHERE id=$1::bigint`, id, update.Ref.Set, update.Ref.Value, update.URL.Set, update.URL.Value,
		update.DateStarted.Set, update.DateStarted.Value, update.DateReleased.Set, update.DateReleased.Value, update.Status)
	if err != nil {
		return dbError(err)
	}
	if update.Commits != nil {
		return replaceReleaseCommits(ctx, tx, orgID, id, *update.Commits)
	}
	return nil
}

func (s *Store) CreateRelease(ctx context.Context, access ManagementAccess, input ReleaseCreate) (Release, bool, error) {
	tx, role, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return Release{}, false, err
	}
	defer tx.Rollback(ctx)
	projects, err := releaseProjectReferences(ctx, tx, access, input.Projects)
	if err != nil {
		return Release{}, false, err
	}
	if len(projects) == 0 {
		return Release{}, false, ErrInvalidReleaseInput
	}
	id, created, err := releaseIdentity(ctx, tx, access.OrganizationID, input.Version, true)
	if err != nil {
		return Release{}, false, err
	}
	associated, err := releaseAssociatedProjects(ctx, tx, id)
	if err != nil {
		return Release{}, false, err
	}
	if !created {
		if err := releaseCoverage(access, associated); err != nil {
			return Release{}, false, err
		}
	}
	union := map[int64]bool{}
	for _, p := range append(projects, associated...) {
		union[p] = true
	}
	all := make([]int64, 0, len(union))
	for p := range union {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	if err := releaseLockProjects(ctx, tx, access.OrganizationID, all); err != nil {
		return Release{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO release_projects(release_id,project_id) SELECT $1::bigint,unnest($2::bigint[]) ON CONFLICT DO NOTHING`, id, projects); err != nil {
		return Release{}, false, err
	}
	if err := applyReleaseUpdate(ctx, tx, access.OrganizationID, id, input.ReleaseUpdate); err != nil {
		return Release{}, false, err
	}
	result, err := loadRelease(ctx, tx, access, role, id)
	if err != nil {
		return Release{}, false, err
	}
	return result, created, tx.Commit(ctx)
}

func (s *Store) Release(ctx context.Context, access ManagementAccess, version ReleaseVersion, projectRef string) (Release, error) {
	tx, role, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return Release{}, err
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM releases WHERE organization_id=$1::bigint AND version=$2`, access.OrganizationID, string(version)).Scan(&id); err != nil {
		return Release{}, dbError(err)
	}
	if err := releaseVisible(ctx, tx, access, id, projectRef); err != nil {
		return Release{}, err
	}
	result, err := loadRelease(ctx, tx, access, role, id)
	if err != nil {
		return Release{}, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) UpdateRelease(ctx context.Context, access ManagementAccess, version ReleaseVersion, projectRef string, update ReleaseUpdate) (Release, error) {
	tx, role, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return Release{}, err
	}
	defer tx.Rollback(ctx)
	id, _, err := releaseIdentity(ctx, tx, access.OrganizationID, version, false)
	if err != nil {
		return Release{}, err
	}
	if err := releaseVisible(ctx, tx, access, id, projectRef); err != nil {
		return Release{}, err
	}
	ids, err := releaseAssociatedProjects(ctx, tx, id)
	if err != nil {
		return Release{}, err
	}
	if err := releaseCoverage(access, ids); err != nil {
		return Release{}, err
	}
	if update.ProjectAssertions != nil {
		assertions, err := releaseProjectReferences(ctx, tx, access, update.ProjectAssertions)
		if err != nil {
			return Release{}, err
		}
		associated := make(map[int64]bool, len(ids))
		for _, project := range ids {
			associated[project] = true
		}
		for _, project := range assertions {
			if !associated[project] {
				return Release{}, ErrNotFound
			}
		}
	}
	if err := releaseLockProjects(ctx, tx, access.OrganizationID, ids); err != nil {
		return Release{}, err
	}
	if err := applyReleaseUpdate(ctx, tx, access.OrganizationID, id, update); err != nil {
		return Release{}, err
	}
	result, err := loadRelease(ctx, tx, access, role, id)
	if err != nil {
		return Release{}, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) DeleteRelease(ctx context.Context, access ManagementAccess, version ReleaseVersion, projectRef string) error {
	tx, _, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	id, _, err := releaseIdentity(ctx, tx, access.OrganizationID, version, false)
	if err != nil {
		return err
	}
	if err := releaseVisible(ctx, tx, access, id, projectRef); err != nil {
		return err
	}
	ids, err := releaseAssociatedProjects(ctx, tx, id)
	if err != nil {
		return err
	}
	if projectRef == "" {
		if err := releaseCoverage(access, ids); err != nil {
			return err
		}
	} else {
		ids, err = releaseProjectReferences(ctx, tx, access, []string{projectRef})
		if err != nil {
			return err
		}
	}
	if err := releaseLockProjects(ctx, tx, access.OrganizationID, ids); err != nil {
		return err
	}
	for _, project := range ids {
		if err := lockReleaseArtifacts(ctx, tx, strconv.FormatInt(project, 10), string(version)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM source_maps WHERE project_id=ANY($1::bigint[]) AND release=$2`, ids, string(version)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM release_projects WHERE release_id=$1::bigint AND project_id=ANY($2::bigint[])`, id, ids); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM releases WHERE id=$1::bigint AND NOT EXISTS(SELECT 1 FROM release_projects WHERE release_id=$1::bigint)`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Releases(ctx context.Context, access ManagementAccess, filter ReleaseFilter) (ReleasePage, error) {
	result := ReleasePage{Data: []Release{}}
	tx, role, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	projects := []int64{}
	if len(filter.ProjectRefs) > 0 && !(len(filter.ProjectRefs) == 1 && filter.ProjectRefs[0] == "-1") {
		projects, err = releaseProjectReferences(ctx, tx, access, filter.ProjectRefs)
		if err != nil {
			return result, err
		}
	}
	where := ` FROM releases r WHERE r.organization_id=$1::bigint AND ($5='' OR strpos(lower(r.version),lower($5))>0)
		AND EXISTS(SELECT 1 FROM release_projects rp WHERE rp.release_id=r.id AND ($2 OR rp.project_id=ANY($3::bigint[]))
		AND (cardinality($4::bigint[])=0 OR rp.project_id=ANY($4::bigint[]))
		AND (cardinality($6::text[])=0
		OR EXISTS(SELECT 1 FROM events e WHERE e.project_id=rp.project_id AND e.release=r.version AND e.environment=ANY($6::text[]))
		OR EXISTS(SELECT 1 FROM spans s WHERE s.project_id=rp.project_id AND s.release=r.version AND s.environment=ANY($6::text[]))
		OR EXISTS(SELECT 1 FROM release_deploys d JOIN release_deploy_projects dp ON dp.deploy_id=d.id WHERE d.release_id=r.id AND dp.project_id=rp.project_id AND d.environment=ANY($6::text[]))))`
	environments := filter.Environments
	if environments == nil {
		environments = []string{}
	}
	args := []any{access.OrganizationID, access.AllProjects, access.ProjectIDs, projects, filter.Query, environments}
	rows, err := tx.Query(ctx, `SELECT r.id::text`+where+` ORDER BY r.created_at DESC,r.id DESC LIMIT $7 OFFSET $8`, append(args, filter.Limit+1, filter.Offset)...)
	if err != nil {
		return result, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return result, err
	}
	if len(ids) > filter.Limit {
		result.HasMore = true
		ids = ids[:filter.Limit]
	}
	if filter.Offset > 0 {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1`+where+` ORDER BY r.created_at DESC,r.id DESC LIMIT 1 OFFSET $7)`, append(args, max(0, filter.Offset-filter.Limit))...).Scan(&result.HasPrevious)
		if err != nil {
			return result, err
		}
	}
	for _, id := range ids {
		r, err := loadRelease(ctx, tx, access, role, id)
		if err != nil {
			return result, err
		}
		result.Data = append(result.Data, r)
	}
	return result, tx.Commit(ctx)
}

// observeRelease never changes metadata on an existing observed release.
func observeRelease(ctx context.Context, tx pgx.Tx, projectID, version string) error {
	parsed, err := ParseReleaseVersion(version)
	if err != nil {
		return nil
	}
	var orgID string
	if err := tx.QueryRow(ctx, `SELECT organization_id::text FROM projects WHERE id=$1::bigint`, projectID).Scan(&orgID); err != nil {
		return dbError(err)
	}
	id, _, err := releaseIdentity(ctx, tx, orgID, parsed, true)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO release_projects(release_id,project_id) VALUES($1::bigint,$2::bigint) ON CONFLICT DO NOTHING`, id, projectID)
	return err
}

func lockReleaseArtifacts(ctx context.Context, tx pgx.Tx, projectID, version string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "release-artifacts:"+projectID+":"+version)
	return err
}
