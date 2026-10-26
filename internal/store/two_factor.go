package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrTwoFactorReplay reports a TOTP step that was already accepted or is older
	// than the recorded step, so an observed code cannot be replayed.
	ErrTwoFactorReplay = errors.New("two-factor step already used")
	// ErrTwoFactorUnavailable reports a missing, unconfirmed or changed two-factor
	// enrollment for the expected account generation.
	ErrTwoFactorUnavailable = errors.New("two-factor authentication unavailable")
)

// TwoFactorState is the persisted two-factor enrollment for one account.
// Secret holds the sealed TOTP secret and is only interpreted by the server.
type TwoFactorState struct {
	Secret                 []byte
	Confirmed              bool
	LastStep               *int64
	RecoveryCodesRemaining int
}

// TwoFactorRequirement reports whether any current organization requires a
// second factor and whether the account has completed TOTP enrollment.
type TwoFactorRequirement struct {
	Organization bool
	Confirmed    bool
}

func (s *Store) TwoFactorRequirement(ctx context.Context, userID string) (TwoFactorRequirement, error) {
	var requirement TwoFactorRequirement
	err := s.pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM memberships m JOIN organizations o ON o.id=m.organization_id WHERE m.user_id=$1::bigint AND o.require_2fa),
 EXISTS(SELECT 1 FROM users WHERE id=$1::bigint AND totp_confirmed_at IS NOT NULL)`, userID).
		Scan(&requirement.Organization, &requirement.Confirmed)
	return requirement, err
}

// LoginChallenge is a pending second-factor login that must be completed or
// invalidated exactly once.
type LoginChallenge struct {
	UserID         string
	AuthGeneration int64
	Attempts       int
	ExpiresAt      time.Time
	Consumed       bool
}

func (s *Store) TwoFactorState(ctx context.Context, userID string) (TwoFactorState, error) {
	var state TwoFactorState
	var confirmed *time.Time
	err := s.pool.QueryRow(ctx, `SELECT totp_secret,totp_confirmed_at,totp_last_step,
		(SELECT count(*) FROM user_recovery_codes c WHERE c.user_id=u.id AND c.used_at IS NULL)
		FROM users u WHERE id=$1::bigint`, userID).
		Scan(&state.Secret, &confirmed, &state.LastStep, &state.RecoveryCodesRemaining)
	if err != nil {
		return TwoFactorState{}, dbError(err)
	}
	state.Confirmed = confirmed != nil
	return state, nil
}

// StartTwoFactor stores a sealed secret for an account that is not yet confirmed
// and drops any outstanding enrollment artifacts.
func (s *Store) StartTwoFactor(ctx context.Context, userID string, authGeneration int64, secret []byte) error {
	if len(secret) == 0 {
		return errors.New("sealed two-factor secret is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	var confirmed *time.Time
	if err = tx.QueryRow(ctx, `SELECT totp_confirmed_at FROM users WHERE id=$1::bigint`, userID).Scan(&confirmed); err != nil {
		return dbError(err)
	}
	if confirmed != nil {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET totp_secret=$2,totp_last_step=NULL WHERE id=$1::bigint`, userID, secret); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1::bigint`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConfirmTwoFactor confirms a pending secret and replaces every recovery code in
// one transaction. The caller hashes the codes outside the write transaction.
func (s *Store) ConfirmTwoFactor(ctx context.Context, userID string, authGeneration int64, step int64, hashes []string) error {
	if len(hashes) == 0 || len(hashes) > 20 {
		return errors.New("recovery codes are required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	var sealed []byte
	var confirmed *time.Time
	if err = tx.QueryRow(ctx, `SELECT totp_secret,totp_confirmed_at FROM users WHERE id=$1::bigint`, userID).Scan(&sealed, &confirmed); err != nil {
		return dbError(err)
	}
	if len(sealed) == 0 || confirmed != nil {
		return ErrTwoFactorUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET totp_confirmed_at=clock_timestamp(),totp_last_step=$2 WHERE id=$1::bigint`, userID, step); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1::bigint`, userID); err != nil {
		return err
	}
	for _, hash := range hashes {
		if _, err = tx.Exec(ctx, `INSERT INTO user_recovery_codes(user_id,code_hash) VALUES($1::bigint,$2)`, userID, hash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DisableTwoFactor clears the enrollment and every recovery code for an account.
func (s *Store) DisableTwoFactor(ctx context.Context, userID string, authGeneration int64) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET totp_secret=NULL,totp_confirmed_at=NULL,totp_last_step=NULL WHERE id=$1::bigint`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1::bigint`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM login_challenges WHERE user_id=$1::bigint`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReplaceRecoveryCodes swaps every outstanding recovery code for a new set.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID string, authGeneration int64, hashes []string) error {
	if len(hashes) == 0 || len(hashes) > 20 {
		return errors.New("recovery codes are required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	var confirmed *time.Time
	if err = tx.QueryRow(ctx, `SELECT totp_confirmed_at FROM users WHERE id=$1::bigint`, userID).Scan(&confirmed); err != nil {
		return dbError(err)
	}
	if confirmed == nil {
		return ErrTwoFactorUnavailable
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1::bigint`, userID); err != nil {
		return err
	}
	for _, hash := range hashes {
		if _, err = tx.Exec(ctx, `INSERT INTO user_recovery_codes(user_id,code_hash) VALUES($1::bigint,$2)`, userID, hash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CreateLoginChallenge stores a single-use second-factor challenge and drops
// this account's expired or consumed challenges.
func (s *Store) CreateLoginChallenge(ctx context.Context, userID string, authGeneration int64, tokenHash string, lifetime time.Duration) (time.Time, error) {
	if lifetime <= 0 || lifetime > 15*time.Minute {
		return time.Time{}, errors.New("invalid challenge lifetime")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if err = pinUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return time.Time{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM login_challenges WHERE user_id=$1::bigint AND (expires_at<=$2 OR consumed_at IS NOT NULL)`, userID, now); err != nil {
		return time.Time{}, err
	}
	expires := now.Add(lifetime)
	if _, err = tx.Exec(ctx, `INSERT INTO login_challenges(token_hash,user_id,auth_generation,expires_at) VALUES($1,$2::bigint,$3,$4)`,
		tokenHash, userID, authGeneration, expires); err != nil {
		return time.Time{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return expires, nil
}

func (s *Store) LoginChallenge(ctx context.Context, tokenHash string) (LoginChallenge, error) {
	var challenge LoginChallenge
	var consumed *time.Time
	err := s.pool.QueryRow(ctx, `SELECT user_id::text,auth_generation,attempts,expires_at,consumed_at FROM login_challenges WHERE token_hash=$1`, tokenHash).
		Scan(&challenge.UserID, &challenge.AuthGeneration, &challenge.Attempts, &challenge.ExpiresAt, &consumed)
	if err != nil {
		return LoginChallenge{}, dbError(err)
	}
	challenge.Consumed = consumed != nil
	return challenge, nil
}

// FailLoginChallenge records a rejected code. The challenge is destroyed once
// the attempt budget is spent so a challenge cannot be brute forced.
func (s *Store) FailLoginChallenge(ctx context.Context, tokenHash string, maxAttempts int) error {
	if maxAttempts < 1 || maxAttempts > 20 {
		return errors.New("invalid attempt budget")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int
	err = tx.QueryRow(ctx, `UPDATE login_challenges SET attempts=attempts+1 WHERE token_hash=$1 RETURNING attempts`, tokenHash).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if attempts >= maxAttempts {
		if _, err = tx.Exec(ctx, `DELETE FROM login_challenges WHERE token_hash=$1`, tokenHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CompleteTOTPChallenge consumes a challenge and advances the account's accepted
// step in one transaction, returning the authenticated account.
func (s *Store) CompleteTOTPChallenge(ctx context.Context, tokenHash, userID string, authGeneration, step int64) (User, error) {
	return s.completeChallenge(ctx, tokenHash, userID, authGeneration, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET totp_last_step=$2 WHERE id=$1::bigint AND totp_confirmed_at IS NOT NULL AND (totp_last_step IS NULL OR totp_last_step<$2)`, userID, step)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrTwoFactorReplay
		}
		return nil
	})
}

// CompleteRecoveryChallenge consumes a challenge and one unused recovery code in
// one transaction, returning the authenticated account.
func (s *Store) CompleteRecoveryChallenge(ctx context.Context, tokenHash, userID string, authGeneration int64, codeHash string) (User, error) {
	return s.completeChallenge(ctx, tokenHash, userID, authGeneration, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE user_recovery_codes SET used_at=clock_timestamp() WHERE user_id=$1::bigint AND code_hash=$2 AND used_at IS NULL`, userID, codeHash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) completeChallenge(ctx context.Context, tokenHash, userID string, authGeneration int64, apply func(context.Context, pgx.Tx) error) (User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	var storedUser, storedGeneration int64
	var expires time.Time
	var consumed *time.Time
	err = tx.QueryRow(ctx, `SELECT user_id,auth_generation,expires_at,consumed_at FROM login_challenges WHERE token_hash=$1 FOR UPDATE`, tokenHash).
		Scan(&storedUser, &storedGeneration, &expires, &consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return User{}, err
	}
	if consumed != nil || !now.Before(expires) || strconv.FormatInt(storedUser, 10) != userID || storedGeneration != authGeneration {
		return User{}, ErrNotFound
	}
	if err = apply(ctx, tx); err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE login_challenges SET consumed_at=$2 WHERE token_hash=$1`, tokenHash, now); err != nil {
		return User{}, err
	}
	var user User
	if err = tx.QueryRow(ctx, `SELECT id::text,email,name,password_hash,auth_generation,is_instance_admin FROM users WHERE id=$1::bigint AND is_active`, userID).
		Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.AuthGeneration, &user.IsInstanceAdmin); err != nil {
		return User{}, dbError(err)
	}
	// A credential change between the challenge and its completion invalidates
	// the second step even though the challenge row still carries the old value.
	if user.AuthGeneration != storedGeneration {
		return User{}, ErrNotFound
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return user, nil
}
