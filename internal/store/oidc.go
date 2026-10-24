package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type OIDCIdentity struct {
	Provider   string
	Subject    string
	Email      string
	Name       string
	Password   []byte
	AutoCreate bool
}

// LoginOIDC links a verified external subject to an existing email account or
// creates an OIDC-only account when explicitly enabled. The unusable password
// keeps local password login closed until the account owner performs recovery.
func (s *Store) LoginOIDC(ctx context.Context, input OIDCIdentity) (User, error) {
	var user User
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return user, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.password_hash,u.auth_generation,u.is_instance_admin
FROM user_identities i JOIN users u ON u.id=i.user_id
WHERE i.provider=$1 AND i.subject=$2 AND u.is_active FOR SHARE OF u`, input.Provider, input.Subject).
		Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration, &user.IsInstanceAdmin)
	if err == nil {
		if _, err = tx.Exec(ctx, `UPDATE user_identities SET last_login_at=now() WHERE provider=$1 AND subject=$2`, input.Provider, input.Subject); err != nil {
			return User{}, err
		}
		return user, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	if input.AutoCreate {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73042190)`); err != nil {
			return User{}, err
		}
	}
	err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.password_hash,u.auth_generation,u.is_instance_admin FROM users u
		WHERE (lower(u.email)=lower($1) OR EXISTS(SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower($1)))
		AND u.is_active ORDER BY (lower(u.email)=lower($1)) DESC LIMIT 1 FOR UPDATE OF u`, input.Email).
		Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration, &user.IsInstanceAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		if !input.AutoCreate {
			return User{}, ErrNotFound
		}
		var matchingAccount string
		err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u
			WHERE lower(u.email)=lower($1) OR EXISTS(SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower($1))
			ORDER BY (lower(u.email)=lower($1)) DESC LIMIT 1 FOR UPDATE OF u`, input.Email).Scan(&matchingAccount)
		if err == nil {
			// Do not let an inactive primary or verified secondary address
			// become a second account through OIDC auto-creation.
			return User{}, ErrNotFound
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return User{}, err
		}
		err = tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3)
ON CONFLICT(email) DO NOTHING
			RETURNING id::text,email,name,password_hash,auth_generation,is_instance_admin`, input.Email, input.Password, input.Name).
			Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration, &user.IsInstanceAdmin)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.password_hash,u.auth_generation,u.is_instance_admin FROM users u
				WHERE (lower(u.email)=lower($1) OR EXISTS(SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower($1)))
				AND u.is_active ORDER BY (lower(u.email)=lower($1)) DESC LIMIT 1 FOR UPDATE OF u`, input.Email).
				Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration, &user.IsInstanceAdmin)
		}
	}
	if err != nil {
		return User{}, dbError(err)
	}
	var linkedUser string
	err = tx.QueryRow(ctx, `INSERT INTO user_identities(user_id,provider,subject,email)
VALUES($1::bigint,$2,$3,$4)
ON CONFLICT DO NOTHING
RETURNING user_id::text`, user.ID, input.Provider, input.Subject, input.Email).Scan(&linkedUser)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.password_hash,u.auth_generation,u.is_instance_admin
FROM user_identities i JOIN users u ON u.id=i.user_id
WHERE i.provider=$1 AND i.subject=$2 AND u.is_active FOR SHARE OF u`, input.Provider, input.Subject).
			Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration, &user.IsInstanceAdmin)
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrConflict
		}
	}
	if err != nil {
		return User{}, dbError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE user_identities SET last_login_at=now() WHERE provider=$1 AND subject=$2`, input.Provider, input.Subject); err != nil {
		return User{}, err
	}
	return user, tx.Commit(ctx)
}

// LoginSocialApp links an organization application identity and ensures the
// signed-in account is an active member of the application's organization. The
// identity provider for the organization vouches for that membership.
func (s *Store) LoginSocialApp(ctx context.Context, organizationID string, input OIDCIdentity) (User, error) {
	user, err := s.LoginOIDC(ctx, input)
	if err != nil {
		return User{}, err
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role)
		SELECT $1::bigint,id,'member' FROM users WHERE id=$2::bigint AND is_active
		ON CONFLICT(organization_id,user_id) DO NOTHING`, organizationID, user.ID); err != nil {
		return User{}, err
	}
	return user, nil
}
