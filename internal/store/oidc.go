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
	err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.password_hash,u.auth_generation
FROM user_identities i JOIN users u ON u.id=i.user_id
WHERE i.provider=$1 AND i.subject=$2`, input.Provider, input.Subject).
		Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration)
	if err == nil {
		if _, err = tx.Exec(ctx, `UPDATE user_identities SET last_login_at=now() WHERE provider=$1 AND subject=$2`, input.Provider, input.Subject); err != nil {
			return User{}, err
		}
		return user, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	err = tx.QueryRow(ctx, `SELECT id::text,email,name,password_hash,auth_generation FROM users WHERE lower(email)=lower($1) FOR UPDATE`, input.Email).
		Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		if !input.AutoCreate {
			return User{}, ErrNotFound
		}
		err = tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3)
ON CONFLICT(email) DO NOTHING
RETURNING id::text,email,name,password_hash,auth_generation`, input.Email, input.Password, input.Name).
			Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id::text,email,name,password_hash,auth_generation FROM users WHERE lower(email)=lower($1) FOR UPDATE`, input.Email).
				Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration)
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
		err = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.password_hash,u.auth_generation
FROM user_identities i JOIN users u ON u.id=i.user_id
WHERE i.provider=$1 AND i.subject=$2`, input.Provider, input.Subject).
			Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration)
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
