package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidRepository = errors.New("invalid repository")

func releaseCommitRows(ctx context.Context, tx pgx.Tx, id string, offset, limit int) ([]ReleaseCommit, error) {
	rows, err := tx.Query(ctx, `SELECT commit_id,repository,message,author_name,author_email,created_at,patch_set,data
		FROM release_commits WHERE release_id=$1::bigint ORDER BY ordinal LIMIT $2 OFFSET $3`, id, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReleaseCommit{}
	for rows.Next() {
		var c ReleaseCommit
		var patch []byte
		if err := rows.Scan(&c.ID, &c.Repository, &c.Message, &c.AuthorName, &c.AuthorEmail, &c.DateCreated, &patch, &c.Data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(patch, &c.PatchSet); err != nil {
			return nil, err
		}
		c.DateCreated = c.DateCreated.UTC()
		result = append(result, c)
	}
	return result, rows.Err()
}

func replaceReleaseCommits(ctx context.Context, tx pgx.Tx, orgID, id string, input []ReleaseCommitInput) error {
	type identity struct{ repository, id string }
	old := map[identity]time.Time{}
	rows, err := tx.Query(ctx, `SELECT repository,commit_id,received_at FROM release_commits WHERE release_id=$1::bigint`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var key identity
		var at time.Time
		if err := rows.Scan(&key.repository, &key.id, &at); err != nil {
			rows.Close()
			return err
		}
		old[key] = at
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	names := map[string]bool{}
	seen := map[identity]bool{}
	for _, c := range input {
		key := identity{c.Repository, c.ID}
		if seen[key] || len(c.Data) == 0 {
			return ErrInvalidReleaseInput
		}
		seen[key] = true
		if c.Repository != "" {
			names[c.Repository] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	repositories := map[string]int64{}
	for _, name := range sorted {
		var repository int64
		err := tx.QueryRow(ctx, `INSERT INTO release_repositories(organization_id,name) VALUES($1::bigint,$2)
			ON CONFLICT(organization_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, orgID, name).Scan(&repository)
		if err != nil {
			return err
		}
		repositories[name] = repository
	}
	var receipt time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&receipt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM release_commits WHERE release_id=$1::bigint`, id); err != nil {
		return err
	}
	for i, c := range input {
		first, ok := old[identity{c.Repository, c.ID}]
		if !ok {
			first = receipt
		}
		created := first
		if c.Timestamp != nil {
			created = *c.Timestamp
		}
		patches := c.PatchSet
		if patches == nil {
			patches = []CommitPatch{}
		}
		patch, err := json.Marshal(patches)
		if err != nil {
			return err
		}
		var repository any
		if c.Repository != "" {
			repository = repositories[c.Repository]
		}
		_, err = tx.Exec(ctx, `INSERT INTO release_commits(release_id,ordinal,repository_id,repository,commit_id,message,author_name,author_email,created_at,received_at,patch_set,data)
			VALUES($1::bigint,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::json)`, id, i, repository, c.Repository, c.ID, c.Message, c.AuthorName, c.AuthorEmail, created, first, patch, c.Data)
		if err != nil {
			return dbError(err)
		}
	}
	return nil
}

func (s *Store) ReplaceReleaseCommits(ctx context.Context, access ManagementAccess, version ReleaseVersion, commits []ReleaseCommitInput) (Release, error) {
	return s.UpdateRelease(ctx, access, version, "", ReleaseUpdate{Commits: &commits})
}

func (s *Store) ReleaseCommits(ctx context.Context, access ManagementAccess, version ReleaseVersion, projectRef string, offset, limit int) (ReleaseCommitsPage, error) {
	result := ReleaseCommitsPage{Data: []ReleaseCommit{}}
	tx, _, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM releases WHERE organization_id=$1::bigint AND version=$2`, access.OrganizationID, string(version)).Scan(&id); err != nil {
		return result, dbError(err)
	}
	if err := releaseVisible(ctx, tx, access, id, projectRef); err != nil {
		return result, err
	}
	result.Data, err = releaseCommitRows(ctx, tx, id, offset, limit)
	if err != nil {
		return result, err
	}
	if len(result.Data) > limit {
		result.HasMore = true
		result.Data = result.Data[:limit]
	}
	if offset > 0 {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM release_commits WHERE release_id=$1::bigint ORDER BY ordinal LIMIT 1 OFFSET $2)`, id, max(0, offset-limit)).Scan(&result.HasPrevious)
		if err != nil {
			return result, err
		}
	}
	return result, tx.Commit(ctx)
}

func (s *Store) ReleaseRepositories(ctx context.Context, access ManagementAccess, query string, offset, limit int) (ReleaseRepositoriesPage, error) {
	result := ReleaseRepositoriesPage{Data: []ReleaseRepository{}}
	tx, _, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	where := ` FROM release_repositories repo WHERE repo.organization_id=$1::bigint AND ($4='' OR strpos(lower(repo.name),lower($4))>0)
		AND ($2 OR EXISTS(SELECT 1 FROM release_commits c JOIN release_projects rp ON rp.release_id=c.release_id
		WHERE c.repository_id=repo.id AND rp.project_id=ANY($3::bigint[])))`
	args := []any{access.OrganizationID, access.AllProjects, access.ProjectIDs, query}
	rows, err := tx.Query(ctx, `SELECT repo.id::text,repo.name,repo.created_at,repo.url`+where+` ORDER BY repo.id LIMIT $5 OFFSET $6`, append(args, limit+1, offset)...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var repository ReleaseRepository
		if err := rows.Scan(&repository.ID, &repository.Name, &repository.DateCreated, &repository.URL); err != nil {
			rows.Close()
			return result, err
		}
		repository.DateCreated = repository.DateCreated.UTC()
		repository.Status = "active"
		repository.Provider = ReleaseRepositoryProvider{ID: "manual", Name: "Manual commit metadata"}
		result.Data = append(result.Data, repository)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(result.Data) > limit {
		result.HasMore = true
		result.Data = result.Data[:limit]
	}
	if offset > 0 {
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1`+where+` ORDER BY repo.id LIMIT 1 OFFSET $5)`, append(args, max(0, offset-limit))...).Scan(&result.HasPrevious); err != nil {
			return result, err
		}
	}
	return result, tx.Commit(ctx)
}

func (s *Store) CreateReleaseRepository(ctx context.Context, access ManagementAccess, name, repositoryURL string) (ReleaseRepository, error) {
	var result ReleaseRepository
	if name == "" || utf8.RuneCountInString(name) > 200 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) || len(repositoryURL) > 2048 || !utf8.ValidString(repositoryURL) || strings.ContainsFunc(repositoryURL, unicode.IsControl) {
		return result, ErrInvalidRepository
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var orgID string
	if err = tx.QueryRow(ctx, `SELECT organization_id::text FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint FOR SHARE`, access.UserID, access.OrganizationID).Scan(&orgID); err != nil {
		return result, dbError(err)
	}
	var url *string
	if repositoryURL != "" {
		url = &repositoryURL
	}
	if err = tx.QueryRow(ctx, `INSERT INTO release_repositories(organization_id,name,url) VALUES($1::bigint,$2,$3)
		RETURNING id::text,name,created_at,url`, orgID, name, url).Scan(&result.ID, &result.Name, &result.DateCreated, &result.URL); err != nil {
		return result, dbError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.DateCreated = result.DateCreated.UTC()
	result.Status = "active"
	result.Provider = ReleaseRepositoryProvider{ID: "manual", Name: "Manual repository"}
	return result, nil
}

func (s *Store) PreviousReleaseWithCommits(ctx context.Context, access ManagementAccess, version ReleaseVersion) (Release, error) {
	tx, role, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return Release{}, err
	}
	defer tx.Rollback(ctx)
	var target string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM releases WHERE organization_id=$1::bigint AND version=$2`, access.OrganizationID, string(version)).Scan(&target); err != nil {
		return Release{}, dbError(err)
	}
	if err := releaseVisible(ctx, tx, access, target, ""); err != nil {
		return Release{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT r.id::text FROM releases r JOIN releases target ON target.id=$1::bigint
		WHERE r.organization_id=target.organization_id AND (r.created_at,r.id)<(target.created_at,target.id)
		AND EXISTS(SELECT 1 FROM release_commits c WHERE c.release_id=r.id)
		AND EXISTS(SELECT 1 FROM release_projects a JOIN release_projects b ON b.project_id=a.project_id
		WHERE a.release_id=r.id AND b.release_id=target.id AND ($2 OR a.project_id=ANY($3::bigint[])))
		ORDER BY r.created_at DESC,r.id DESC LIMIT 1`, target, access.AllProjects, access.ProjectIDs).Scan(&id)
	if err != nil {
		return Release{}, dbError(err)
	}
	result, err := loadRelease(ctx, tx, access, role, id)
	if err != nil {
		return Release{}, err
	}
	return result, tx.Commit(ctx)
}
