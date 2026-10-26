package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidProjectKey = errors.New("invalid project key")
var ErrInvalidProject = errors.New("invalid project")

type CompatibleKeyRateLimit struct {
	Window int `json:"window"`
	Count  int `json:"count"`
}

type CompatibleProjectKey struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Label       string                  `json:"label"`
	Public      string                  `json:"public"`
	ProjectID   int64                   `json:"projectID"`
	DateCreated time.Time               `json:"dateCreated"`
	RateLimit   *CompatibleKeyRateLimit `json:"rateLimit"`
	Enabled     bool                    `json:"isActive"`
}

type CompatibleOrganizationProject struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Slug              string               `json:"slug"`
	Platform          string               `json:"platform"`
	DateCreated       time.Time            `json:"dateCreated"`
	Avatar            CompatibleAvatar     `json:"avatar"`
	Color             string               `json:"color"`
	Features          []string             `json:"features"`
	HasAccess         bool                 `json:"hasAccess"`
	IsBookmarked      bool                 `json:"isBookmarked"`
	IsInternal        bool                 `json:"isInternal"`
	IsMember          bool                 `json:"isMember"`
	IsPublic          bool                 `json:"isPublic"`
	ScrubIPAddresses  bool                 `json:"scrubIPAddresses"`
	FirstEvent        *time.Time           `json:"firstEvent"`
	EventThrottleRate int                  `json:"eventThrottleRate"`
	Teams             []CompatibleTeamSlug `json:"teams"`
}

type CompatibleTeamSlug struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type CompatibleTeam struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	DateCreated time.Time `json:"dateCreated"`
	IsMember    bool      `json:"isMember"`
	MemberCount int       `json:"memberCount"`
}

func validCompatibleProjectName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 64 && !strings.ContainsFunc(name, unicode.IsControl)
}

func validCompatibleProjectSlug(slug string) bool {
	return slug != "" && len(slug) <= 64 && Slug(slug) == slug
}

type CompatibleOrganizationDetail struct {
	CompatibleOrganization
	Projects       []CompatibleOrganizationProject `json:"projects"`
	Teams          []CompatibleTeam                `json:"teams"`
	Access         []string                        `json:"access"`
	OpenMembership bool                            `json:"openMembership"`
}

func compatibleOrganizationAccess(role string) []string {
	access := []string{"org:read", "member:read", "team:read", "project:read", "project:releases", "event:read", "event:write"}
	if role == "owner" || role == "manager" || role == "admin" {
		access = append(access, "org:write", "member:write", "team:write", "project:write", "event:admin")
	}
	return access
}

func (s *Store) CompatibleOrganizationDetail(ctx context.Context, userID, reference string) (CompatibleOrganizationDetail, error) {
	var detail CompatibleOrganizationDetail
	var id, slug, name, role string
	var created time.Time
	var require2FA bool
	err := s.pool.QueryRow(ctx, `SELECT o.id::text,o.slug,o.name,o.created_at,o.require_2fa,m.role FROM organizations o
		JOIN memberships m ON m.organization_id=o.id AND m.user_id=$1::bigint
		WHERE o.slug=$2 OR o.id=$3 ORDER BY (o.slug=$2) DESC LIMIT 1`, userID, reference, referenceID(reference)).
		Scan(&id, &slug, &name, &created, &require2FA, &role)
	if err != nil {
		return detail, dbError(err)
	}
	detail.CompatibleOrganization = discoveryOrganization(id, slug, name, created)
	detail.CompatibleOrganization.Require2FA = require2FA
	detail.CompatibleOrganization.Role = role
	detail.Access = compatibleOrganizationAccess(role)
	detail.OpenMembership = false
	detail.Projects = []CompatibleOrganizationProject{}
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at,p.event_throttle_rate,
		COALESCE(array_agg(t.id::text ORDER BY lower(t.slug),t.id) FILTER (WHERE t.id IS NOT NULL),ARRAY[]::text[]),
		COALESCE(array_agg(t.slug ORDER BY lower(t.slug),t.id) FILTER (WHERE t.id IS NOT NULL),ARRAY[]::text[])
		FROM projects p
		LEFT JOIN project_teams pt ON pt.project_id=p.id
		LEFT JOIN teams t ON t.id=pt.team_id AND t.organization_id=p.organization_id
		WHERE p.organization_id=$1::bigint GROUP BY p.id ORDER BY p.id`, id)
	if err != nil {
		return detail, err
	}
	for rows.Next() {
		var project CompatibleOrganizationProject
		var teamIDs, teamSlugs []string
		if err = rows.Scan(&project.ID, &project.Name, &project.Slug, &project.Platform, &project.DateCreated, &project.EventThrottleRate, &teamIDs, &teamSlugs); err != nil {
			rows.Close()
			return detail, err
		}
		project.DateCreated = project.DateCreated.UTC()
		project.Features = []string{}
		project.HasAccess = true
		project.IsMember = true
		project.Teams = []CompatibleTeamSlug{}
		for index := range teamIDs {
			project.Teams = append(project.Teams, CompatibleTeamSlug{ID: teamIDs[index], Slug: teamSlugs[index]})
		}
		detail.Projects = append(detail.Projects, project)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return detail, err
	}
	rows.Close()

	detail.Teams = []CompatibleTeam{}
	rows, err = s.pool.Query(ctx, `SELECT t.id::text,t.slug,t.created_at,
		EXISTS(SELECT 1 FROM team_memberships tm WHERE tm.team_id=t.id AND tm.user_id=$2::bigint),
		(SELECT count(*) FROM team_memberships tm WHERE tm.team_id=t.id)
		FROM teams t WHERE t.organization_id=$1::bigint ORDER BY lower(t.slug),t.id`, id, userID)
	if err != nil {
		return detail, err
	}
	defer rows.Close()
	for rows.Next() {
		var team CompatibleTeam
		if err = rows.Scan(&team.ID, &team.Slug, &team.DateCreated, &team.IsMember, &team.MemberCount); err != nil {
			return detail, err
		}
		team.DateCreated = team.DateCreated.UTC()
		detail.Teams = append(detail.Teams, team)
	}
	return detail, rows.Err()
}

func (s *Store) CreateCompatibleProject(ctx context.Context, actorID, orgRef, teamSlug, name, projectSlug, platform string, eventThrottleRate int) (CompatibleOrganizationProject, error) {
	var result CompatibleOrganizationProject
	name = strings.TrimSpace(name)
	platform = strings.TrimSpace(platform)
	if projectSlug == "" {
		projectSlug = Slug(name)
	}
	if !validCompatibleProjectName(name) || !validCompatibleProjectSlug(projectSlug) || len(platform) > 80 || !utf8.ValidString(platform) || strings.ContainsFunc(platform, unicode.IsControl) || eventThrottleRate < 0 || eventThrottleRate > 100 {
		return result, ErrInvalidProject
	}
	if platform == "" {
		platform = "other"
	}
	key, err := randomKey()
	if err != nil {
		return result, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgRef)
	if err != nil {
		return result, err
	}
	var teamID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND slug=$2 FOR SHARE`, orgID, teamSlug).Scan(&teamID); err != nil {
		return result, dbError(err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO projects(organization_id,team_id,name,slug,platform,event_throttle_rate) VALUES($1::bigint,$2::bigint,$3,$4,$5,$6)
		RETURNING id::text,name,slug,platform,created_at,event_throttle_rate`, orgID, teamID, name, projectSlug, platform, eventThrottleRate).
		Scan(&result.ID, &result.Name, &result.Slug, &result.Platform, &result.DateCreated, &result.EventThrottleRate)
	if err != nil {
		return result, dbError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_keys(public_key,project_id) VALUES($1,$2::bigint)`, key, result.ID); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_teams(organization_id,project_id,team_id,created_at)
		SELECT organization_id,id,$2::bigint,created_at FROM projects WHERE id=$1::bigint`, result.ID, teamID); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO team_memberships(team_id,user_id) VALUES($1::bigint,$2::bigint) ON CONFLICT DO NOTHING`, teamID, actorID); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.DateCreated = result.DateCreated.UTC()
	result.Features = []string{}
	result.HasAccess = true
	result.IsMember = true
	result.Teams = []CompatibleTeamSlug{{ID: teamID, Slug: teamSlug}}
	return result, nil
}

func (s *Store) CompatibleSetProjectTeam(ctx context.Context, access ManagementAccess, projectRef, teamSlug string, attach bool) (CompatibleOrganizationProject, error) {
	var result CompatibleOrganizationProject
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var organizationID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM organizations WHERE id=$1::bigint FOR SHARE`, access.OrganizationID).Scan(&organizationID); err != nil {
		return result, dbError(err)
	}
	var primaryTeam string
	err = tx.QueryRow(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at,p.event_throttle_rate,COALESCE(p.team_id::text,'')
		FROM projects p JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
		WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[])) AND (p.slug=$5 OR p.id=$6)
		ORDER BY (p.slug=$5) DESC LIMIT 1 FOR UPDATE OF p`,
		access.UserID, organizationID, access.AllProjects, access.ProjectIDs, projectRef, referenceID(projectRef)).
		Scan(&result.ID, &result.Name, &result.Slug, &result.Platform, &result.DateCreated, &result.EventThrottleRate, &primaryTeam)
	if err != nil {
		return result, dbError(err)
	}
	var teamID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND slug=$2 FOR SHARE`, organizationID, teamSlug).Scan(&teamID); err != nil {
		return result, dbError(err)
	}
	if attach {
		if _, err = tx.Exec(ctx, `INSERT INTO project_teams(organization_id,project_id,team_id,created_at)
			SELECT organization_id,id,$2::bigint,created_at FROM projects WHERE id=$1::bigint
			ON CONFLICT(project_id,team_id) DO NOTHING`, result.ID, teamID); err != nil {
			return result, err
		}
		if primaryTeam == "" {
			if _, err = tx.Exec(ctx, `UPDATE projects SET team_id=$2::bigint WHERE id=$1::bigint`, result.ID, teamID); err != nil {
				return result, err
			}
		}
	} else {
		if _, err = tx.Exec(ctx, `DELETE FROM project_teams WHERE project_id=$1::bigint AND team_id=$2::bigint`, result.ID, teamID); err != nil {
			return result, err
		}
		if primaryTeam == teamID {
			if _, err = tx.Exec(ctx, `UPDATE projects SET team_id=(SELECT team_id FROM project_teams WHERE project_id=$1::bigint ORDER BY team_id LIMIT 1)
				WHERE id=$1::bigint`, result.ID); err != nil {
				return result, err
			}
		}
	}
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.slug FROM project_teams pt
		JOIN teams t ON t.id=pt.team_id AND t.organization_id=pt.organization_id
		WHERE pt.project_id=$1::bigint ORDER BY lower(t.slug),t.id`, result.ID)
	if err != nil {
		return result, err
	}
	result.Teams = []CompatibleTeamSlug{}
	for rows.Next() {
		var team CompatibleTeamSlug
		if err = rows.Scan(&team.ID, &team.Slug); err != nil {
			rows.Close()
			return result, err
		}
		result.Teams = append(result.Teams, team)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.DateCreated = result.DateCreated.UTC()
	result.Features = []string{}
	result.HasAccess = true
	result.IsMember = true
	return result, nil
}

func validKeyUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if character < '0' || character > '9' && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

const compatibleProjectKeyColumns = `api_id::text,public_key,label,rate_limit_window,rate_limit_count,created_at,enabled,project_id`

func scanCompatibleProjectKey(row interface{ Scan(...any) error }) (CompatibleProjectKey, error) {
	var key CompatibleProjectKey
	var window, count *int
	err := row.Scan(&key.ID, &key.Public, &key.Label, &window, &count, &key.DateCreated, &key.Enabled, &key.ProjectID)
	if err != nil {
		return key, dbError(err)
	}
	key.Name = key.Label
	key.DateCreated = key.DateCreated.UTC()
	if window != nil && count != nil {
		key.RateLimit = &CompatibleKeyRateLimit{Window: *window, Count: *count}
	}
	return key, nil
}

func (s *Store) CompatibleProjectKeys(ctx context.Context, access ManagementAccess, projectRef string, enabled *bool, offset, limit int) ([]CompatibleProjectKey, error) {
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return nil, err
	}
	projectID := referenceID(project.ID)
	rows, err := s.pool.Query(ctx, `SELECT `+compatibleProjectKeyColumns+` FROM project_keys WHERE project_id=$1::bigint
		AND ($2::boolean IS NULL OR enabled=$2) ORDER BY created_at,api_id LIMIT $3 OFFSET $4`, projectID, enabled, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CompatibleProjectKey{}
	for rows.Next() {
		key, err := scanCompatibleProjectKey(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	return result, rows.Err()
}

func (s *Store) CompatibleProjectKey(ctx context.Context, access ManagementAccess, projectRef, keyID string) (CompatibleProjectKey, error) {
	if !validKeyUUID(keyID) {
		return CompatibleProjectKey{}, ErrNotFound
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return CompatibleProjectKey{}, err
	}
	return scanCompatibleProjectKey(s.pool.QueryRow(ctx, `SELECT `+compatibleProjectKeyColumns+` FROM project_keys
		WHERE project_id=$1::bigint AND api_id::text=$2`, referenceID(project.ID), keyID))
}

func validKeyInput(label string, rate *CompatibleKeyRateLimit) bool {
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > 200 || strings.ContainsRune(label, 0) {
		return false
	}
	return rate == nil || rate.Window > 0 && rate.Window <= 604800 && rate.Count > 0 && rate.Count <= 1000000000
}

func (s *Store) CreateCompatibleProjectKey(ctx context.Context, access ManagementAccess, projectRef, label string, rate *CompatibleKeyRateLimit) (CompatibleProjectKey, error) {
	if !validKeyInput(label, rate) {
		return CompatibleProjectKey{}, ErrInvalidProjectKey
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return CompatibleProjectKey{}, err
	}
	public, err := randomKey()
	if err != nil {
		return CompatibleProjectKey{}, err
	}
	var window, count *int
	if rate != nil {
		window, count = &rate.Window, &rate.Count
	}
	return scanCompatibleProjectKey(s.pool.QueryRow(ctx, `INSERT INTO project_keys(public_key,project_id,label,rate_limit_window,rate_limit_count)
		VALUES($1,$2::bigint,$3,$4,$5) RETURNING `+compatibleProjectKeyColumns,
		public, referenceID(project.ID), label, window, count))
}

func (s *Store) UpdateCompatibleProjectKey(ctx context.Context, access ManagementAccess, projectRef, keyID, label string, rate *CompatibleKeyRateLimit, enabled *bool) (CompatibleProjectKey, error) {
	if !validKeyUUID(keyID) {
		return CompatibleProjectKey{}, ErrNotFound
	}
	if !validKeyInput(label, rate) {
		return CompatibleProjectKey{}, ErrInvalidProjectKey
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return CompatibleProjectKey{}, err
	}
	var window, count *int
	if rate != nil {
		window, count = &rate.Window, &rate.Count
	}
	return scanCompatibleProjectKey(s.pool.QueryRow(ctx, `UPDATE project_keys SET label=$3,rate_limit_window=$4,rate_limit_count=$5,
		enabled=COALESCE($6,enabled),updated_at=clock_timestamp() WHERE project_id=$1::bigint AND api_id::text=$2 RETURNING `+compatibleProjectKeyColumns,
		referenceID(project.ID), keyID, label, window, count, enabled))
}

func (s *Store) DeleteCompatibleProjectKey(ctx context.Context, access ManagementAccess, projectRef, keyID string) error {
	if !validKeyUUID(keyID) {
		return ErrNotFound
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return err
	}
	command, err := s.pool.Exec(ctx, `DELETE FROM project_keys WHERE project_id=$1::bigint AND api_id::text=$2`, referenceID(project.ID), keyID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}
