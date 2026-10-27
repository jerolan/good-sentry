package store

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

type CompatibleEnvironment struct {
	ID   *int64 `json:"id"`
	Name string `json:"name"`
}

type CompatibleEnvironmentProject struct {
	ID       *int64 `json:"id"`
	Name     string `json:"name"`
	IsHidden bool   `json:"isHidden"`
}

func ValidEnvironmentVisibility(value string) bool {
	return value == "all" || value == "hidden" || value == "visible"
}

func ValidCompatibleEnvironmentName(value string) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 255 && !strings.ContainsRune(value, '/') && !strings.ContainsFunc(value, unicode.IsControl)
}

const projectEnvironmentNames = `SELECT environment AS name FROM events WHERE project_id=$1::bigint AND environment<>'' AND char_length(environment)<=255
 UNION SELECT environment FROM monitors WHERE project_id=$1::bigint AND environment<>'' AND char_length(environment)<=255
 UNION SELECT environment FROM logs_with_archive WHERE project_id=$1::bigint AND environment<>'' AND char_length(environment)<=255
 UNION SELECT environment FROM spans WHERE project_id=$1::bigint AND environment<>'' AND char_length(environment)<=255
 UNION SELECT name FROM project_environments WHERE project_id=$1::bigint`

func environmentVisibilityPredicate(value, visibility string) string {
	return `(` + visibility + `='all' OR ` + visibility + `='visible' AND NOT (` + value + `) OR ` + visibility + `='hidden' AND (` + value + `))`
}

func loadProjectEnvironmentArchive(ctx context.Context, read *spanArchiveRead, projectID int64) error {
	return read.Load(ctx, spanArchiveSelector{ProjectID: projectID, RequireEnvironment: true})
}

func (s *Store) CompatibleProjectEnvironments(ctx context.Context, access ManagementAccess, projectRef, visibility string, offset, limit int) ([]CompatibleEnvironmentProject, error) {
	if !ValidEnvironmentVisibility(visibility) || offset < 0 || limit < 1 {
		return nil, ErrNotFound
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) ([]CompatibleEnvironmentProject, error) {
		projectID, err := read.managementProjectID(ctx, access, projectRef)
		if err != nil {
			return nil, err
		}
		if err := loadProjectEnvironmentArchive(ctx, read, projectID); err != nil {
			return nil, err
		}
		rows, err := read.tx.Query(ctx, `WITH names AS (`+projectEnvironmentNames+`), states AS (
		SELECT n.name,COALESCE(pe.is_hidden,false) AS is_hidden FROM names n
		LEFT JOIN project_environments pe ON pe.project_id=$1::bigint AND pe.name=n.name)
		SELECT name,is_hidden FROM states WHERE `+environmentVisibilityPredicate("is_hidden", "$2")+`
			ORDER BY lower(name),name LIMIT $3 OFFSET $4`, projectID, visibility, limit+1, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []CompatibleEnvironmentProject{}
		for rows.Next() {
			var item CompatibleEnvironmentProject
			if err = rows.Scan(&item.Name, &item.IsHidden); err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, rows.Err()
	})
}

func (s *Store) CompatibleProjectEnvironment(ctx context.Context, access ManagementAccess, projectRef, name string) (CompatibleEnvironmentProject, error) {
	if !ValidCompatibleEnvironmentName(name) {
		return CompatibleEnvironmentProject{}, ErrNotFound
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) (CompatibleEnvironmentProject, error) {
		projectID, err := read.managementProjectID(ctx, access, projectRef)
		if err != nil {
			return CompatibleEnvironmentProject{}, err
		}
		if err := loadProjectEnvironmentArchive(ctx, read, projectID); err != nil {
			return CompatibleEnvironmentProject{}, err
		}
		var item CompatibleEnvironmentProject
		err = read.tx.QueryRow(ctx, `WITH names AS (`+projectEnvironmentNames+`)
		SELECT n.name,COALESCE(pe.is_hidden,false) FROM names n
		LEFT JOIN project_environments pe ON pe.project_id=$1::bigint AND pe.name=n.name
			WHERE n.name=$2 ORDER BY n.name LIMIT 1`, projectID, name).Scan(&item.Name, &item.IsHidden)
		return item, dbError(err)
	})
}

func (s *Store) SetCompatibleProjectEnvironmentVisibility(ctx context.Context, access ManagementAccess, projectRef, name string, hidden bool) (CompatibleEnvironmentProject, error) {
	if !ValidCompatibleEnvironmentName(name) {
		return CompatibleEnvironmentProject{}, ErrNotFound
	}
	type environmentMutationTarget struct {
		projectID int64
		exists    bool
	}
	target, err := withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) (environmentMutationTarget, error) {
		projectID, err := read.managementProjectID(ctx, access, projectRef)
		if err != nil {
			return environmentMutationTarget{}, err
		}
		if err := loadProjectEnvironmentArchive(ctx, read, projectID); err != nil {
			return environmentMutationTarget{}, err
		}
		target := environmentMutationTarget{projectID: projectID}
		if err := read.tx.QueryRow(ctx, `WITH names AS (`+projectEnvironmentNames+`) SELECT EXISTS(SELECT 1 FROM names WHERE name=$2)`, projectID, name).Scan(&target.exists); err != nil {
			return environmentMutationTarget{}, err
		}
		return target, nil
	})
	if err != nil {
		return CompatibleEnvironmentProject{}, err
	}
	if !target.exists {
		return CompatibleEnvironmentProject{}, ErrNotFound
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO project_environments(project_id,name,is_hidden)
		VALUES($1::bigint,$2,$3) ON CONFLICT(project_id,name) DO UPDATE SET is_hidden=EXCLUDED.is_hidden,updated_at=now()`,
		target.projectID, name, hidden); err != nil {
		return CompatibleEnvironmentProject{}, err
	}
	return CompatibleEnvironmentProject{Name: name, IsHidden: hidden}, nil
}

func (s *Store) CompatibleOrganizationEnvironments(ctx context.Context, access ManagementAccess, visibility string, offset, limit int) ([]CompatibleEnvironment, error) {
	if !ValidEnvironmentVisibility(visibility) || offset < 0 || limit < 1 {
		return nil, ErrNotFound
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) ([]CompatibleEnvironment, error) {
		if err := read.loadAccessSpans(ctx, access, spanArchiveSelector{RequireEnvironment: true}); err != nil {
			return nil, err
		}
		rows, err := read.tx.Query(ctx, `WITH env_projects AS (
		SELECT e.project_id,e.environment AS name FROM events e JOIN projects p ON p.id=e.project_id
		WHERE p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[])) AND e.environment<>'' AND char_length(e.environment)<=255
		UNION SELECT m.project_id,m.environment FROM monitors m JOIN projects p ON p.id=m.project_id
		WHERE p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[])) AND m.environment<>'' AND char_length(m.environment)<=255
		UNION SELECT l.project_id,l.environment FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
		WHERE p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[])) AND l.environment<>'' AND char_length(l.environment)<=255
		UNION SELECT s.project_id,s.environment FROM spans s JOIN projects p ON p.id=s.project_id
		WHERE p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[])) AND s.environment<>'' AND char_length(s.environment)<=255
		UNION SELECT pe.project_id,pe.name FROM project_environments pe JOIN projects p ON p.id=pe.project_id
		WHERE p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[]))
	), states AS (
		SELECT ep.name,bool_and(COALESCE(pe.is_hidden,false)) AS all_hidden
		FROM env_projects ep LEFT JOIN project_environments pe ON pe.project_id=ep.project_id AND pe.name=ep.name
		GROUP BY ep.name
	)
	SELECT name FROM states WHERE `+environmentVisibilityPredicate("all_hidden", "$4")+`
		ORDER BY lower(name),name LIMIT $5 OFFSET $6`, access.OrganizationID, access.AllProjects, access.ProjectIDs, visibility, limit+1, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []CompatibleEnvironment{}
		for rows.Next() {
			var item CompatibleEnvironment
			if err = rows.Scan(&item.Name); err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, rows.Err()
	})
}
