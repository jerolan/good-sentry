package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminOrganization is the non-secret operational summary used by the
// opt-in administration surface. It is limited to organizations where the
// caller currently has owner or admin authority.
type AdminOrganization struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Role     string `json:"role"`
	Projects int64  `json:"projects"`
	Members  int64  `json:"members"`
	Issues   int64  `json:"issues"`
	Events   int64  `json:"events"`
	Logs     int64  `json:"logs"`
}

func (s *Store) AdminOrganizations(ctx context.Context, userID string) ([]AdminOrganization, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id::text,o.name,o.slug,m.role,
		(SELECT count(*) FROM projects p WHERE p.organization_id=o.id),
		(SELECT count(*) FROM memberships mm WHERE mm.organization_id=o.id),
		(SELECT count(*) FROM issues i JOIN projects p ON p.id=i.project_id WHERE p.organization_id=o.id),
		(SELECT count(*) FROM events e JOIN projects p ON p.id=e.project_id WHERE p.organization_id=o.id),
		(SELECT count(*) FROM logs l JOIN projects p ON p.id=l.project_id WHERE p.organization_id=o.id)
		FROM organizations o JOIN memberships m ON m.organization_id=o.id
		WHERE m.user_id=$1::bigint AND m.role IN ('owner','admin')
		ORDER BY lower(o.name),o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AdminOrganization{}
	for rows.Next() {
		var organization AdminOrganization
		if err := rows.Scan(&organization.ID, &organization.Name, &organization.Slug, &organization.Role, &organization.Projects, &organization.Members, &organization.Issues, &organization.Events, &organization.Logs); err != nil {
			return nil, err
		}
		result = append(result, organization)
	}
	return result, rows.Err()
}

// AdminUser is the deliberately small account projection used by global
// administration. Credential material and authentication state never leave
// the store boundary.
type AdminUser struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	IsActive          bool   `json:"isActive"`
	IsInstanceAdmin   bool   `json:"isInstanceAdmin"`
	OrganizationCount int64  `json:"organizationCount"`
}

type AdminUsersPage struct {
	Data    []AdminUser `json:"data"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"perPage"`
}

// AdminUsers locks the currently authorized actor for the duration of the
// read, so a concurrent suspension or demotion cannot race past the check.
func (s *Store) AdminUsers(ctx context.Context, actorID, query string, page, perPage int) (AdminUsersPage, error) {
	result := AdminUsersPage{Data: []AdminUser{}, Page: page, PerPage: perPage}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var active, admin bool
	if err = tx.QueryRow(ctx, `SELECT is_active,is_instance_admin FROM users WHERE id=$1::bigint FOR SHARE`, actorID).Scan(&active, &admin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrForbidden
		}
		return result, err
	}
	if !active || !admin {
		return result, ErrForbidden
	}
	pattern := query
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users u
		WHERE $1='' OR strpos(lower(u.email),lower($1))>0 OR strpos(lower(u.name),lower($1))>0`, pattern).Scan(&result.Total); err != nil {
		return result, err
	}
	offset := (page - 1) * perPage
	rows, err := tx.Query(ctx, `SELECT u.id::text,u.email,u.name,u.is_active,u.is_instance_admin,
		(SELECT count(*) FROM memberships m WHERE m.user_id=u.id)
		FROM users u WHERE $1='' OR strpos(lower(u.email),lower($1))>0 OR strpos(lower(u.name),lower($1))>0
		ORDER BY lower(u.email),u.id LIMIT $2 OFFSET $3`, pattern, perPage, offset)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item AdminUser
		if err = rows.Scan(&item.ID, &item.Email, &item.Name, &item.IsActive, &item.IsInstanceAdmin, &item.OrganizationCount); err != nil {
			rows.Close()
			return result, err
		}
		result.Data = append(result.Data, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

type AdminUserChange struct {
	IsActive        *bool
	IsInstanceAdmin *bool
}

func (c AdminUserChange) Empty() bool { return c.IsActive == nil && c.IsInstanceAdmin == nil }

// UpdateAdminUser serializes all instance-admin changes and rechecks the
// actor after waiting for the lock. Credential revocation uses fresh
// ReadCommitted statements after the target row UPDATE lock is acquired.
func (s *Store) UpdateAdminUser(ctx context.Context, actorID, targetID, protectedID string, change AdminUserChange) (AdminUser, error) {
	var result AdminUser
	if change.Empty() {
		return result, ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73042191)`); err != nil {
		return result, err
	}
	var actorActive, actorAdmin bool
	if err = tx.QueryRow(ctx, `SELECT is_active,is_instance_admin FROM users WHERE id=$1::bigint FOR SHARE`, actorID).Scan(&actorActive, &actorAdmin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrForbidden
		}
		return result, err
	}
	if !actorActive || !actorAdmin {
		return result, ErrForbidden
	}
	var before AdminUser
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.is_active,u.is_instance_admin,
		u.auth_generation,(SELECT count(*) FROM memberships m WHERE m.user_id=u.id)
		FROM users u WHERE u.id=$1::bigint FOR UPDATE OF u`, targetID).
		Scan(&before.ID, &before.Email, &before.Name, &before.IsActive, &before.IsInstanceAdmin, &generation, &before.OrganizationCount); err != nil {
		return result, dbError(err)
	}
	if protectedID != "" && targetID == protectedID &&
		((change.IsActive != nil && !*change.IsActive) || (change.IsInstanceAdmin != nil && !*change.IsInstanceAdmin)) {
		return result, ErrForbidden
	}
	active, admin := before.IsActive, before.IsInstanceAdmin
	if change.IsActive != nil {
		active = *change.IsActive
	}
	if !active {
		if change.IsInstanceAdmin != nil && *change.IsInstanceAdmin {
			return result, ErrConflict
		}
		admin = false
	} else if change.IsInstanceAdmin != nil {
		admin = *change.IsInstanceAdmin
	}
	if admin && !active {
		return result, ErrConflict
	}
	if active != before.IsActive || admin != before.IsInstanceAdmin {
		if _, err = tx.Exec(ctx, `UPDATE users SET is_active=$2,is_instance_admin=$3,
			auth_generation=CASE WHEN is_active AND NOT $2 THEN auth_generation+1 ELSE auth_generation END
			WHERE id=$1::bigint`, targetID, active, admin); err != nil {
			return result, err
		}
		if before.IsActive && !active {
			var now time.Time
			if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
				return result, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1::bigint`, targetID); err != nil {
				return result, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM login_challenges WHERE user_id=$1::bigint`, targetID); err != nil {
				return result, err
			}
			if _, err = tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,$2) WHERE user_id=$1::bigint`, targetID, now); err != nil {
				return result, err
			}
			if _, err = tx.Exec(ctx, `UPDATE oauth_refresh_tokens SET revoked_at=COALESCE(revoked_at,$2) WHERE user_id=$1::bigint`, targetID, now); err != nil {
				return result, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM user_api_tokens WHERE user_id=$1::bigint`, targetID); err != nil {
				return result, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM account_password_resets WHERE user_id=$1::bigint AND consumed_at IS NULL`, targetID); err != nil {
				return result, err
			}
		}
	}
	var activeAdmins int64
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_active AND is_instance_admin`).Scan(&activeAdmins); err != nil {
		return result, err
	}
	if activeAdmins == 0 {
		return result, ErrLastInstanceAdmin
	}
	result = AdminUser{ID: before.ID, Email: before.Email, Name: before.Name, IsActive: active, IsInstanceAdmin: admin, OrganizationCount: before.OrganizationCount}
	if err = tx.Commit(ctx); err != nil {
		return AdminUser{}, err
	}
	return result, nil
}

// GrantInstanceAdmin bootstraps a legacy instance without making account
// creation or account reactivation part of the operator command.
func (s *Store) GrantInstanceAdmin(ctx context.Context, email string) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73042191)`); err != nil {
		return false, err
	}
	var id string
	var active, admin bool
	err = tx.QueryRow(ctx, `SELECT id::text,is_active,is_instance_admin FROM users WHERE lower(email)=lower($1) FOR UPDATE`, email).
		Scan(&id, &active, &admin)
	if err != nil {
		return false, dbError(err)
	}
	if !active {
		return false, ErrForbidden
	}
	if admin {
		return false, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET is_instance_admin=true WHERE id=$1::bigint`, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
