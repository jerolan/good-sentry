package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// pinUserGeneration keeps the observed authentication generation locked until
// the credential commits. Fresh Read Committed statements are required after a
// concurrent wait so a committed generation change cannot be missed.
func pinUserGeneration(ctx context.Context, tx pgx.Tx, userID string, expected int64) error {
	if expected <= 0 {
		return ErrCredentialsChanged
	}
	var current int64
	err := tx.QueryRow(ctx, `SELECT auth_generation FROM users WHERE id=$1::bigint FOR SHARE`, userID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentialsChanged
	}
	if err != nil {
		return err
	}
	if current != expected {
		return ErrCredentialsChanged
	}
	return nil
}

func lockUserGeneration(ctx context.Context, tx pgx.Tx, userID string, expected int64) error {
	if expected <= 0 {
		return ErrCredentialsChanged
	}
	var current int64
	err := tx.QueryRow(ctx, `SELECT auth_generation FROM users WHERE id=$1::bigint FOR UPDATE`, userID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentialsChanged
	}
	if err != nil {
		return err
	}
	if current != expected {
		return ErrCredentialsChanged
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, authGeneration int64, tokenHash, csrf string, expires time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = pinUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,$4)", tokenHash, userID, csrf, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ChangePassword performs the complete credential revocation in one transaction.
// The caller must hash the replacement password outside the write transaction.
func (s *Store) ChangePassword(ctx context.Context, userID string, authGeneration int64, passwordHash []byte) error {
	if len(passwordHash) == 0 {
		return errors.New("password hash is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	var revokedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&revokedAt); err != nil {
		return err
	}
	if err = changePasswordLocked(ctx, tx, userID, passwordHash, revokedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func changePasswordLocked(ctx context.Context, tx pgx.Tx, userID string, passwordHash []byte, changedAt time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2,auth_generation=auth_generation+1 WHERE id=$1::bigint`, userID, passwordHash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1::bigint`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=$2 WHERE user_id=$1::bigint AND revoked_at IS NULL`, userID, changedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM account_password_resets WHERE user_id=$1::bigint AND consumed_at IS NULL`, userID); err != nil {
		return err
	}
	return nil
}
