package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// DeleteCompatibleUser removes an account and its credentials after locking
// every organization membership. Organization row locks serialize concurrent
// owner deletions so the final-owner check cannot race two successful claims.
func (s *Store) DeleteCompatibleUser(ctx context.Context, userID string) error {
	if referenceID(userID) == 0 {
		return ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73042191)`); err != nil {
		return err
	}

	var lockedUser string
	var activeAdmin bool
	if err = tx.QueryRow(ctx, `SELECT id::text,is_active AND is_instance_admin FROM users WHERE id=$1::bigint FOR UPDATE`, userID).Scan(&lockedUser, &activeAdmin); err != nil {
		return dbError(err)
	}
	rows, err := tx.Query(ctx, `SELECT organization_id::text FROM memberships WHERE user_id=$1::bigint ORDER BY organization_id`, userID)
	if err != nil {
		return err
	}
	var organizationIDs []string
	for rows.Next() {
		var organizationID string
		if err = rows.Scan(&organizationID); err != nil {
			rows.Close()
			return err
		}
		organizationIDs = append(organizationIDs, organizationID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, organizationID := range organizationIDs {
		var lockedOrganization string
		if err = tx.QueryRow(ctx, `SELECT id::text FROM organizations WHERE id=$1::bigint FOR UPDATE`, organizationID).Scan(&lockedOrganization); err != nil {
			return dbError(err)
		}
	}

	var lastOwner bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM memberships m
		WHERE m.user_id=$1::bigint AND m.role='owner'
		AND NOT EXISTS(
			SELECT 1 FROM memberships other
			WHERE other.organization_id=m.organization_id
			AND other.user_id<>$1::bigint AND other.role='owner'
		)
	)`, userID).Scan(&lastOwner); err != nil {
		return err
	}
	if lastOwner {
		return ErrLastOwner
	}
	if activeAdmin {
		var activeAdmins int64
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_active AND is_instance_admin`).Scan(&activeAdmins); err != nil {
			return err
		}
		if activeAdmins <= 1 {
			return ErrLastInstanceAdmin
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM users WHERE id=$1::bigint`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
