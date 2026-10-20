package store

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"goodsentry/internal/notify"

	"github.com/jackc/pgx/v5"
)

type PasswordReset struct {
	ID              string
	UserID          string
	UserName        string
	Recipient       string
	TokenHash       string
	TokenCiphertext []byte
	AuthGeneration  int64
	ExpiresAt       time.Time
	ConsumedAt      *time.Time
	Status          string
	Attempts        int
	NextAttempt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SentAt          *time.Time
	LastReason      string
}

type PasswordResetLease struct {
	PasswordReset
	Token          string
	LeaseExpiresAt time.Time
}

const passwordResetColumns = `r.id::text,r.user_id::text,u.name,r.recipient,r.token_hash,r.token_ciphertext,
 r.auth_generation,r.expires_at,r.consumed_at,r.status,r.attempts,r.next_attempt,r.created_at,r.updated_at,r.sent_at,r.last_reason`

func scanPasswordReset(row interface{ Scan(...any) error }) (PasswordReset, error) {
	var reset PasswordReset
	err := row.Scan(&reset.ID, &reset.UserID, &reset.UserName, &reset.Recipient, &reset.TokenHash, &reset.TokenCiphertext,
		&reset.AuthGeneration, &reset.ExpiresAt, &reset.ConsumedAt, &reset.Status, &reset.Attempts, &reset.NextAttempt, &reset.CreatedAt, &reset.UpdatedAt, &reset.SentAt, &reset.LastReason)
	return reset, dbError(err)
}

func validResetInput(tokenHash string, ciphertext []byte) bool {
	if len(tokenHash) != 64 || len(ciphertext) != 92 {
		return false
	}
	_, err := hex.DecodeString(tokenHash)
	return err == nil
}

// EnqueuePasswordReset atomically replaces prior unconsumed recovery links and
// applies a per-user request floor before adding one encrypted outbox item.
func (s *Store) EnqueuePasswordReset(ctx context.Context, userID string, authGeneration int64, recipient, tokenHash string, ciphertext []byte, lifetime time.Duration) (bool, error) {
	if !validResetInput(tokenHash, ciphertext) || lifetime != 30*time.Minute {
		return false, errors.New("invalid password reset request")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = pinUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return false, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return false, err
	}
	var recent bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_password_resets WHERE user_id=$1::bigint AND created_at>$2::timestamptz-interval '60 seconds')`, userID, now).Scan(&recent); err != nil {
		return false, err
	}
	if recent {
		return false, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM account_password_resets WHERE user_id=$1::bigint AND consumed_at IS NULL`, userID); err != nil {
		return false, err
	}
	expiresAt := now.Add(lifetime)
	id, err := randomKey()
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO account_password_resets(id,user_id,recipient,token_hash,token_ciphertext,auth_generation,expires_at,status,attempts,next_attempt,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'pending',0,$8,$8,$8)`, id, userID, recipient, tokenHash, ciphertext, authGeneration, expiresAt, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) ClaimPasswordReset(ctx context.Context, maxAttempts int, timeout time.Duration) (PasswordResetLease, error) {
	if maxAttempts < 1 || maxAttempts > 20 || timeout <= 0 {
		return PasswordResetLease{}, errors.New("invalid password-reset delivery configuration")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return PasswordResetLease{}, err
	}
	defer tx.Rollback(ctx)
	for range 128 {
		reset, err := scanPasswordReset(tx.QueryRow(ctx, `SELECT `+passwordResetColumns+` FROM account_password_resets r JOIN users u ON u.id=r.user_id
			WHERE (r.status='pending' AND r.next_attempt<=clock_timestamp()) OR (r.status='processing' AND r.lease_expires_at<=clock_timestamp())
			ORDER BY r.next_attempt,r.created_at,r.id LIMIT 1 FOR UPDATE OF r SKIP LOCKED`))
		if errors.Is(err, ErrNotFound) {
			if err = tx.Commit(ctx); err != nil {
				return PasswordResetLease{}, err
			}
			return PasswordResetLease{}, ErrNotFound
		}
		if err != nil {
			return PasswordResetLease{}, err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return PasswordResetLease{}, err
		}
		if !now.Before(reset.ExpiresAt) {
			if _, err = tx.Exec(ctx, `UPDATE account_password_resets SET status='failed',last_reason='expired',updated_at=$2,lease_token='',lease_expires_at=NULL WHERE id=$1`, reset.ID, now); err != nil {
				return PasswordResetLease{}, err
			}
			continue
		}
		if reset.Attempts >= maxAttempts {
			if _, err = tx.Exec(ctx, `UPDATE account_password_resets SET status='failed',last_reason='attempts_exhausted',updated_at=$2,lease_token='',lease_expires_at=NULL WHERE id=$1`, reset.ID, now); err != nil {
				return PasswordResetLease{}, err
			}
			continue
		}
		token, err := randomKey()
		if err != nil {
			return PasswordResetLease{}, err
		}
		reset.Attempts++
		leaseExpires := now.Add(timeout + 30*time.Second)
		reason := reset.LastReason
		if reset.Status == "processing" {
			reason = "lease_expired"
		}
		if _, err = tx.Exec(ctx, `UPDATE account_password_resets SET status='processing',attempts=$2,updated_at=$3,last_reason=$4,lease_token=$5,lease_expires_at=$6 WHERE id=$1`, reset.ID, reset.Attempts, now, reason, token, leaseExpires); err != nil {
			return PasswordResetLease{}, err
		}
		reset.Status, reset.UpdatedAt, reset.LastReason = "processing", now, reason
		if err = tx.Commit(ctx); err != nil {
			return PasswordResetLease{}, err
		}
		return PasswordResetLease{PasswordReset: reset, Token: token, LeaseExpiresAt: leaseExpires}, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return PasswordResetLease{}, err
	}
	return PasswordResetLease{}, ErrNotFound
}

func (s *Store) CompletePasswordReset(ctx context.Context, lease PasswordResetLease, outcome notify.Outcome, maxAttempts int) error {
	if maxAttempts < 1 || maxAttempts > 20 || outcome.ElapsedMS < 0 || len(outcome.Reason) > 64 || outcome.RetryAfter != 0 {
		return errors.New("invalid password-reset delivery outcome")
	}
	if outcome.Disposition != notify.Delivered && outcome.Disposition != notify.Retry && outcome.Disposition != notify.Permanent {
		return errors.New("invalid password-reset delivery disposition")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	reset, token, leaseExpires, err := scanPasswordResetLease(tx.QueryRow(ctx, `SELECT `+passwordResetColumns+`,r.lease_token,r.lease_expires_at FROM account_password_resets r JOIN users u ON u.id=r.user_id WHERE r.id=$1 FOR UPDATE OF r`, lease.ID))
	if errors.Is(err, ErrNotFound) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if reset.Status != "processing" || token != lease.Token || leaseExpires == nil || !now.Before(*leaseExpires) {
		return ErrConflict
	}
	status, reason := "failed", outcome.Reason
	next, sentAt := reset.NextAttempt, (*time.Time)(nil)
	if !now.Before(reset.ExpiresAt) {
		status, reason = "failed", "expired"
	} else if outcome.Disposition == notify.Delivered {
		status, sentAt = "sent", &now
	} else if outcome.Disposition == notify.Retry && reset.Attempts < maxAttempts {
		status = "pending"
		next = now.Add(notify.RetryDelay(reset.Attempts, 0))
	}
	if _, err = tx.Exec(ctx, `UPDATE account_password_resets SET status=$2,next_attempt=$3,updated_at=$4,sent_at=$5,last_reason=$6,lease_token='',lease_expires_at=NULL WHERE id=$1`, reset.ID, status, next, now, sentAt, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanPasswordResetLease(row interface{ Scan(...any) error }) (PasswordReset, string, *time.Time, error) {
	var reset PasswordReset
	var token string
	var expires *time.Time
	fields := []any{&reset.ID, &reset.UserID, &reset.UserName, &reset.Recipient, &reset.TokenHash, &reset.TokenCiphertext,
		&reset.AuthGeneration, &reset.ExpiresAt, &reset.ConsumedAt, &reset.Status, &reset.Attempts, &reset.NextAttempt, &reset.CreatedAt, &reset.UpdatedAt, &reset.SentAt, &reset.LastReason, &token, &expires}
	err := row.Scan(fields...)
	return reset, token, expires, dbError(err)
}

func (s *Store) ResetPassword(ctx context.Context, tokenHash string, passwordHash []byte) error {
	if len(passwordHash) == 0 || len(tokenHash) != 64 {
		return ErrInvalidResetToken
	}
	if _, err := hex.DecodeString(tokenHash); err != nil {
		return ErrInvalidResetToken
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT user_id::text,auth_generation FROM account_password_resets WHERE token_hash=$1`, tokenHash).Scan(&userID, &generation); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	if err = lockUserGeneration(ctx, tx, userID, generation); err != nil {
		if errors.Is(err, ErrCredentialsChanged) {
			return ErrInvalidResetToken
		}
		return err
	}
	reset, err := scanPasswordReset(tx.QueryRow(ctx, `SELECT `+passwordResetColumns+` FROM account_password_resets r JOIN users u ON u.id=r.user_id WHERE r.token_hash=$1 FOR UPDATE OF r`, tokenHash))
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if reset.Status != "sent" || reset.ConsumedAt != nil || !now.Before(reset.ExpiresAt) || reset.AuthGeneration != generation {
		return ErrInvalidResetToken
	}
	if _, err = tx.Exec(ctx, `UPDATE account_password_resets SET status='consumed',consumed_at=$2,updated_at=$2 WHERE id=$1`, reset.ID, now); err != nil {
		return err
	}
	if err = changePasswordLocked(ctx, tx, userID, passwordHash, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
