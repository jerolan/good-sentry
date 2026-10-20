package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidTeam = errors.New("invalid team")

type CompatibleOrganizationUser struct {
	ID          string          `json:"id"`
	Role        string          `json:"role"`
	RoleName    string          `json:"roleName"`
	DateCreated time.Time       `json:"dateCreated"`
	Email       string          `json:"email"`
	User        *CompatibleUser `json:"user"`
	Pending     bool            `json:"pending"`
	IsOwner     bool            `json:"isOwner"`
}

type CompatibleTeamProject struct {
	CompatibleTeam
	Projects []CompatibleOrganizationProject `json:"projects"`
}

func roleDisplay(role string) string {
	switch role {
	case "owner":
		return "Owner"
	case "admin":
		return "Admin"
	default:
		return "Member"
	}
}

func compatibleUser(id, email, name string, created time.Time) CompatibleUser {
	return CompatibleUser{
		ID: id, Email: email, Username: email, Name: name, DateJoined: created.UTC(),
		HasPasswordAuth: true, Identities: []any{}, IsActive: true,
	}
}

func (s *Store) CompatibleOrganizationMembers(ctx context.Context, userID, orgRef string) ([]CompatibleOrganizationUser, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	orgID, role, err := organizationMember(ctx, tx, userID, orgRef, false, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT u.id::text,u.email,u.name,u.created_at,m.role FROM memberships m
		JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1::bigint ORDER BY lower(u.name),u.id`, orgID)
	if err != nil {
		return nil, err
	}
	result := []CompatibleOrganizationUser{}
	for rows.Next() {
		var id, email, name, memberRole string
		var created time.Time
		if err = rows.Scan(&id, &email, &name, &created, &memberRole); err != nil {
			rows.Close()
			return nil, err
		}
		user := compatibleUser(id, email, name, created)
		result = append(result, CompatibleOrganizationUser{
			ID: id, Role: memberRole, RoleName: roleDisplay(memberRole), DateCreated: created.UTC(),
			Email: email, User: &user, IsOwner: memberRole == "owner",
		})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if role == "owner" || role == "admin" {
		rows, err = tx.Query(ctx, `SELECT i.id,i.email,i.role,i.created_at FROM organization_invitations i
			WHERE i.organization_id=$1::bigint AND i.status IN ('pending','processing','sent','failed')
			ORDER BY i.created_at DESC,i.id DESC`, orgID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var item CompatibleOrganizationUser
			if err = rows.Scan(&item.ID, &item.Email, &item.Role, &item.DateCreated); err != nil {
				return nil, err
			}
			item.DateCreated = item.DateCreated.UTC()
			item.RoleName = roleDisplay(item.Role)
			item.Pending = true
			result = append(result, item)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	return result, tx.Commit(ctx)
}

func compatibleTeamProjects(detail CompatibleOrganizationDetail) []CompatibleTeamProject {
	result := []CompatibleTeamProject{}
	for _, team := range detail.Teams {
		projects := []CompatibleOrganizationProject{}
		for _, project := range detail.Projects {
			for _, relation := range project.Teams {
				if relation.ID == team.ID {
					projects = append(projects, project)
					break
				}
			}
		}
		result = append(result, CompatibleTeamProject{CompatibleTeam: team, Projects: projects})
	}
	return result
}

func pageSlice[T any](values []T, offset, limit int) []T {
	if offset >= len(values) {
		return []T{}
	}
	end := offset + limit + 1
	if end > len(values) {
		end = len(values)
	}
	return values[offset:end]
}

func (s *Store) CompatibleOrganizationTeams(ctx context.Context, userID, orgRef string, offset, limit int) ([]CompatibleTeamProject, error) {
	detail, err := s.CompatibleOrganizationDetail(ctx, userID, orgRef)
	if err != nil {
		return nil, err
	}
	return pageSlice(compatibleTeamProjects(detail), offset, limit), nil
}

func (s *Store) CompatibleTeam(ctx context.Context, userID, orgRef, teamSlug string) (CompatibleTeamProject, error) {
	detail, err := s.CompatibleOrganizationDetail(ctx, userID, orgRef)
	if err != nil {
		return CompatibleTeamProject{}, err
	}
	for _, team := range compatibleTeamProjects(detail) {
		if team.Slug == teamSlug {
			return team, nil
		}
	}
	return CompatibleTeamProject{}, ErrNotFound
}

func (s *Store) CompatibleTeamMembers(ctx context.Context, userID, orgRef, teamSlug string) ([]CompatibleOrganizationUser, error) {
	team, err := s.CompatibleTeam(ctx, userID, orgRef, teamSlug)
	if err != nil {
		return nil, err
	}
	detail, err := s.CompatibleOrganizationDetail(ctx, userID, orgRef)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id::text,u.email,u.name,u.created_at,m.role FROM team_memberships tm
		JOIN teams t ON t.id=tm.team_id JOIN memberships m ON m.organization_id=t.organization_id AND m.user_id=tm.user_id
		JOIN users u ON u.id=tm.user_id WHERE t.organization_id=$1::bigint AND t.id=$2::bigint
		ORDER BY lower(u.name),u.id`, detail.ID, team.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CompatibleOrganizationUser{}
	for rows.Next() {
		var id, email, name, role string
		var created time.Time
		if err = rows.Scan(&id, &email, &name, &created, &role); err != nil {
			return nil, err
		}
		user := compatibleUser(id, email, name, created)
		result = append(result, CompatibleOrganizationUser{
			ID: id, Role: role, RoleName: roleDisplay(role), DateCreated: created.UTC(),
			Email: email, User: &user, IsOwner: role == "owner",
		})
	}
	return result, rows.Err()
}

func (s *Store) CompatibleTeamProjects(ctx context.Context, userID, orgRef, teamSlug string, offset, limit int) ([]CompatibleOrganizationProject, error) {
	team, err := s.CompatibleTeam(ctx, userID, orgRef, teamSlug)
	if err != nil {
		return nil, err
	}
	return pageSlice(team.Projects, offset, limit), nil
}

func (s *Store) CompatibleProjectTeams(ctx context.Context, access ManagementAccess, projectRef string) ([]CompatibleTeam, error) {
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return nil, err
	}
	detail, err := s.CompatibleOrganizationDetail(ctx, access.UserID, access.OrganizationID)
	if err != nil {
		return nil, err
	}
	result := []CompatibleTeam{}
	for _, candidate := range detail.Projects {
		if candidate.ID != project.ID {
			continue
		}
		for _, relation := range candidate.Teams {
			for _, team := range detail.Teams {
				if team.ID == relation.ID {
					result = append(result, team)
				}
			}
		}
	}
	return result, nil
}

func validCompatibleTeamSlug(value string) bool {
	return len(value) > 0 && len(value) <= 50 && Slug(value) == value
}

func (s *Store) UpdateCompatibleTeam(ctx context.Context, actorID, orgRef, currentSlug, newSlug string) (CompatibleTeamProject, error) {
	if !validCompatibleTeamSlug(newSlug) {
		return CompatibleTeamProject{}, ErrInvalidTeam
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompatibleTeamProject{}, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgRef)
	if err != nil {
		return CompatibleTeamProject{}, err
	}
	var teamID, oldSlug string
	if err = tx.QueryRow(ctx, `SELECT id::text,slug FROM teams WHERE organization_id=$1::bigint AND slug=$2 FOR UPDATE`, orgID, currentSlug).Scan(&teamID, &oldSlug); err != nil {
		return CompatibleTeamProject{}, dbError(err)
	}
	if oldSlug == "default" && newSlug != "default" {
		return CompatibleTeamProject{}, ErrForbidden
	}
	if _, err = tx.Exec(ctx, `UPDATE teams SET slug=$3,name=$3 WHERE organization_id=$1::bigint AND id=$2::bigint`, orgID, teamID, newSlug); err != nil {
		return CompatibleTeamProject{}, dbError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CompatibleTeamProject{}, err
	}
	return s.CompatibleTeam(ctx, actorID, orgRef, newSlug)
}

func (s *Store) DeleteCompatibleTeam(ctx context.Context, actorID, orgRef, slug string) error {
	var teamID string
	if err := s.pool.QueryRow(ctx, `SELECT t.id::text FROM teams t JOIN memberships m ON m.organization_id=t.organization_id
		WHERE m.user_id=$1::bigint AND t.organization_id=m.organization_id AND t.slug=$2 AND m.organization_id=(SELECT id FROM organizations WHERE slug=$3)`, actorID, slug, orgRef).Scan(&teamID); err != nil {
		return dbError(err)
	}
	return s.DeleteTeam(ctx, actorID, orgRef, teamID)
}
