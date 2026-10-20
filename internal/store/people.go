package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Member struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type Team struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	MemberCount  int    `json:"memberCount"`
	ProjectCount int    `json:"projectCount"`
}

type TeamMember struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type TeamDetail struct {
	Team
	Members []TeamMember `json:"members"`
}

func organizationMember(ctx context.Context, tx pgx.Tx, userID, slug string, lockOrganization, lockMembership bool) (string, string, error) {
	var orgID string
	query := `SELECT id::text FROM organizations WHERE slug=$1`
	if lockOrganization {
		query += " FOR UPDATE"
	}
	if err := tx.QueryRow(ctx, query, slug).Scan(&orgID); err != nil {
		return "", "", dbError(err)
	}
	var role string
	membershipQuery := `SELECT role FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`
	if lockMembership {
		membershipQuery += " FOR SHARE"
	}
	if err := tx.QueryRow(ctx, membershipQuery, orgID, userID).Scan(&role); err != nil {
		return "", "", dbError(err)
	}
	return orgID, role, nil
}

func manageableOrganization(ctx context.Context, tx pgx.Tx, userID, slug string) (string, string, error) {
	orgID, role, err := organizationMember(ctx, tx, userID, slug, true, true)
	if err != nil {
		return "", "", err
	}
	if role != "owner" && role != "admin" {
		return "", "", ErrForbidden
	}
	return orgID, role, nil
}

func (s *Store) Members(ctx context.Context, userID, orgSlug string) ([]Member, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := organizationMember(ctx, tx, userID, orgSlug, false, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT u.id::text,u.email,u.name,m.role FROM memberships m JOIN users u ON u.id=m.user_id
		WHERE m.organization_id=$1::bigint ORDER BY lower(u.name),u.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Member{}
	for rows.Next() {
		var item Member
		if err = rows.Scan(&item.ID, &item.Email, &item.Name, &item.Role); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, tx.Commit(ctx)
}

func (s *Store) UpdateMemberRole(ctx context.Context, actorID, orgSlug, targetID, role string) (Member, error) {
	var result Member
	if role != "owner" && role != "admin" && role != "member" {
		return result, errors.New("invalid membership role")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, actorRole, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return result, err
	}
	var targetRole string
	if err = tx.QueryRow(ctx, `SELECT role FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint FOR UPDATE`, orgID, targetID).Scan(&targetRole); err != nil {
		return result, dbError(err)
	}
	if targetRole == "owner" && actorRole != "owner" || role == "owner" && actorRole != "owner" {
		return result, ErrForbidden
	}
	if targetRole == "owner" && role != "owner" {
		var owners int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE organization_id=$1::bigint AND role='owner'`, orgID).Scan(&owners); err != nil {
			return result, err
		}
		if owners <= 1 {
			return result, ErrLastOwner
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE memberships SET role=$3 WHERE organization_id=$1::bigint AND user_id=$2::bigint`, orgID, targetID, role); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,m.role FROM memberships m JOIN users u ON u.id=m.user_id
		WHERE m.organization_id=$1::bigint AND m.user_id=$2::bigint`, orgID, targetID).
		Scan(&result.ID, &result.Email, &result.Name, &result.Role); err != nil {
		return result, dbError(err)
	}
	return result, tx.Commit(ctx)
}

func (s *Store) RemoveMember(ctx context.Context, actorID, orgSlug, targetID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	orgID, actorRole, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return err
	}
	var targetRole string
	if err = tx.QueryRow(ctx, `SELECT role FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint FOR UPDATE`, orgID, targetID).Scan(&targetRole); err != nil {
		return dbError(err)
	}
	if targetRole == "owner" && actorRole != "owner" {
		return ErrForbidden
	}
	if targetRole == "owner" {
		var owners int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE organization_id=$1::bigint AND role='owner'`, orgID).Scan(&owners); err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM team_memberships tm USING teams t WHERE tm.team_id=t.id AND t.organization_id=$1::bigint AND tm.user_id=$2::bigint`, orgID, targetID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, orgID, targetID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const teamColumns = `t.id::text,t.slug,t.name,
 (SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.user_id=tm.user_id AND m.organization_id=t.organization_id WHERE tm.team_id=t.id),
 (SELECT count(*) FROM projects p WHERE p.team_id=t.id)`

func scanTeam(row interface{ Scan(...any) error }) (Team, error) {
	var team Team
	err := row.Scan(&team.ID, &team.Slug, &team.Name, &team.MemberCount, &team.ProjectCount)
	return team, dbError(err)
}

func (s *Store) Teams(ctx context.Context, userID, orgSlug string) ([]Team, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := organizationMember(ctx, tx, userID, orgSlug, false, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT `+teamColumns+` FROM teams t WHERE t.organization_id=$1::bigint ORDER BY lower(t.name),t.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Team{}
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, team)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, tx.Commit(ctx)
}

func (s *Store) Team(ctx context.Context, userID, orgSlug, teamID string) (TeamDetail, error) {
	var result TeamDetail
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := organizationMember(ctx, tx, userID, orgSlug, false, false)
	if err != nil {
		return result, err
	}
	result.Team, err = scanTeam(tx.QueryRow(ctx, `SELECT `+teamColumns+` FROM teams t WHERE t.organization_id=$1::bigint AND t.id=$2::bigint`, orgID, teamID))
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT u.id::text,u.email,u.name FROM team_memberships tm JOIN teams t ON t.id=tm.team_id
		JOIN memberships m ON m.organization_id=t.organization_id AND m.user_id=tm.user_id JOIN users u ON u.id=tm.user_id
		WHERE t.organization_id=$1::bigint AND t.id=$2::bigint ORDER BY lower(u.name),u.id`, orgID, teamID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Members = []TeamMember{}
	for rows.Next() {
		var member TeamMember
		if err = rows.Scan(&member.ID, &member.Email, &member.Name); err != nil {
			return result, err
		}
		result.Members = append(result.Members, member)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) CreateTeam(ctx context.Context, actorID, orgSlug, name string) (Team, error) {
	var result Team
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return result, err
	}
	result, err = scanTeam(tx.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO teams(organization_id,slug,name) VALUES($1,$2,$3) RETURNING id,slug,name
	) SELECT i.id::text,i.slug,i.name,0,0 FROM inserted i`, orgID, Slug(name), name))
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) UpdateTeam(ctx context.Context, actorID, orgSlug, teamID, name string) (Team, error) {
	var result Team
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE teams SET name=$3 WHERE organization_id=$1::bigint AND id=$2::bigint`, orgID, teamID, name); err != nil {
		return result, err
	}
	result, err = scanTeam(tx.QueryRow(ctx, `SELECT `+teamColumns+` FROM teams t WHERE t.organization_id=$1::bigint AND t.id=$2::bigint`, orgID, teamID))
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) DeleteTeam(ctx context.Context, actorID, orgSlug, teamID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return err
	}
	var slug string
	if err = tx.QueryRow(ctx, `SELECT slug FROM teams WHERE organization_id=$1::bigint AND id=$2::bigint FOR UPDATE`, orgID, teamID).Scan(&slug); err != nil {
		return dbError(err)
	}
	if slug == "default" {
		return ErrForbidden
	}
	if _, err = tx.Exec(ctx, `DELETE FROM teams WHERE organization_id=$1::bigint AND id=$2::bigint`, orgID, teamID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AddTeamMember(ctx context.Context, actorID, orgSlug, teamID, userID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT user_id::text FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint FOR SHARE`, orgID, userID).Scan(&userID); err != nil {
		return dbError(err)
	}
	var team string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND id=$2::bigint FOR SHARE`, orgID, teamID).Scan(&team); err != nil {
		return dbError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO team_memberships(team_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, team, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RemoveTeamMember(ctx context.Context, actorID, orgSlug, teamID, userID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return err
	}
	var team string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND id=$2::bigint FOR SHARE`, orgID, teamID).Scan(&team); err != nil {
		return dbError(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM team_memberships WHERE team_id=$1::bigint AND user_id=$2::bigint`, team, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
