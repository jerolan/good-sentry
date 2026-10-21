package store

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidEmail = errors.New("invalid email")

type CompatibleUserEmail struct {
	Email      string `json:"email"`
	IsPrimary  bool   `json:"isPrimary"`
	IsVerified bool   `json:"isVerified"`
}

func normalizeEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	return value, err == nil && address.Address == value && len(value) <= 254
}

func (s *Store) CompatibleUserEmails(ctx context.Context, userID string) ([]CompatibleUserEmail, error) {
	var primary string
	if err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1::bigint`, userID).Scan(&primary); err != nil {
		return nil, dbError(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT email,is_verified FROM user_emails WHERE user_id=$1::bigint ORDER BY is_verified DESC,lower(email),email`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CompatibleUserEmail{{Email: primary, IsPrimary: true, IsVerified: true}}
	for rows.Next() {
		var item CompatibleUserEmail
		if err = rows.Scan(&item.Email, &item.IsVerified); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateCompatibleUserEmail(ctx context.Context, userID, email, tokenHash string, ciphertext []byte, expires time.Time) (CompatibleUserEmail, error) {
	email, valid := normalizeEmail(email)
	if !valid || len(tokenHash) != 64 || len(ciphertext) != 92 || expires.IsZero() {
		return CompatibleUserEmail{}, ErrInvalidEmail
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompatibleUserEmail{}, err
	}
	defer tx.Rollback(ctx)
	var primary string
	if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1::bigint FOR UPDATE`, userID).Scan(&primary); err != nil {
		return CompatibleUserEmail{}, dbError(err)
	}
	if strings.EqualFold(primary, email) {
		return CompatibleUserEmail{}, ErrConflict
	}
	var existingUser string
	var verified bool
	err = tx.QueryRow(ctx, `SELECT user_id::text,is_verified FROM user_emails WHERE lower(email)=lower($1) FOR UPDATE`, email).Scan(&existingUser, &verified)
	if err == nil {
		if existingUser != userID || verified {
			return CompatibleUserEmail{}, ErrConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE user_emails SET verification_hash=$3,verification_ciphertext=$4,verification_expires_at=$5,updated_at=clock_timestamp() WHERE user_id=$1::bigint AND lower(email)=lower($2)`, userID, email, tokenHash, ciphertext, expires); err != nil {
			return CompatibleUserEmail{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return CompatibleUserEmail{}, err
		}
		return CompatibleUserEmail{Email: email}, nil
	}
	if !errors.Is(dbError(err), ErrNotFound) {
		return CompatibleUserEmail{}, err
	}
	var conflict bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, email).Scan(&conflict); err != nil {
		return CompatibleUserEmail{}, err
	}
	if conflict {
		return CompatibleUserEmail{}, ErrConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_emails(user_id,email,verification_hash,verification_ciphertext,verification_expires_at)
		VALUES($1::bigint,$2,$3,$4,$5)`, userID, email, tokenHash, ciphertext, expires); err != nil {
		return CompatibleUserEmail{}, dbError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CompatibleUserEmail{}, err
	}
	return CompatibleUserEmail{Email: email}, nil
}

func (s *Store) CompatibleUserEmailVerification(ctx context.Context, userID, email string) (string, []byte, time.Time, error) {
	email, valid := normalizeEmail(email)
	if !valid {
		return "", nil, time.Time{}, ErrInvalidEmail
	}
	var hash string
	var ciphertext []byte
	var expires time.Time
	err := s.pool.QueryRow(ctx, `SELECT verification_hash,verification_ciphertext,verification_expires_at FROM user_emails
		WHERE user_id=$1::bigint AND lower(email)=lower($2) AND NOT is_verified`, userID, email).Scan(&hash, &ciphertext, &expires)
	return hash, ciphertext, expires, dbError(err)
}

func (s *Store) ConfirmCompatibleUserEmail(ctx context.Context, tokenHash string) (string, string, error) {
	if len(tokenHash) != 64 {
		return "", "", ErrInvalidEmail
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var userID, email string
	err = tx.QueryRow(ctx, `SELECT user_id::text,email FROM user_emails WHERE verification_hash=$1
		AND NOT is_verified AND verification_expires_at>clock_timestamp() FOR UPDATE`, tokenHash).Scan(&userID, &email)
	if err != nil {
		return "", "", dbError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE user_emails SET is_verified=true,verification_hash=NULL,verification_ciphertext=NULL,verification_expires_at=NULL,updated_at=clock_timestamp() WHERE user_id=$1::bigint AND lower(email)=lower($2)`, userID, email); err != nil {
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return userID, email, nil
}

func (s *Store) SetCompatiblePrimaryEmail(ctx context.Context, userID, email string) (CompatibleUserEmail, error) {
	email, valid := normalizeEmail(email)
	if !valid {
		return CompatibleUserEmail{}, ErrInvalidEmail
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompatibleUserEmail{}, err
	}
	defer tx.Rollback(ctx)
	var primary string
	if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1::bigint FOR UPDATE`, userID).Scan(&primary); err != nil {
		return CompatibleUserEmail{}, dbError(err)
	}
	if strings.EqualFold(primary, email) {
		return CompatibleUserEmail{Email: primary, IsPrimary: true, IsVerified: true}, nil
	}
	var verified bool
	if err = tx.QueryRow(ctx, `SELECT is_verified FROM user_emails WHERE user_id=$1::bigint AND lower(email)=lower($2) FOR UPDATE`, userID, email).Scan(&verified); err != nil {
		return CompatibleUserEmail{}, dbError(err)
	}
	if !verified {
		return CompatibleUserEmail{}, ErrConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_emails(user_id,email,is_verified) VALUES($1::bigint,$2,true)`, userID, primary); err != nil {
		return CompatibleUserEmail{}, dbError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET email=$2 WHERE id=$1::bigint`, userID, email); err != nil {
		return CompatibleUserEmail{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_emails WHERE user_id=$1::bigint AND lower(email)=lower($2)`, userID, email); err != nil {
		return CompatibleUserEmail{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CompatibleUserEmail{}, err
	}
	return CompatibleUserEmail{Email: email, IsPrimary: true, IsVerified: true}, nil
}

func (s *Store) DeleteCompatibleUserEmail(ctx context.Context, userID, email string) error {
	email, valid := normalizeEmail(email)
	if !valid {
		return ErrInvalidEmail
	}
	var primary string
	if err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1::bigint`, userID).Scan(&primary); err != nil {
		return dbError(err)
	}
	if strings.EqualFold(primary, email) {
		return ErrForbidden
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM user_emails WHERE user_id=$1::bigint AND lower(email)=lower($2)`, userID, email)
	return err
}
