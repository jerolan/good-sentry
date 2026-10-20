package store

import (
	"context"
	"errors"
	"time"

	"goodsentry/internal/notify"

	"github.com/jackc/pgx/v5"
)

// Claims and completion lock delivery rows only. Rule replacement takes rule
// then delivery locks, so workers must never acquire the reverse dependency.
func (s *Store) ClaimAlertDelivery(ctx context.Context, maxAttempts int, timeout time.Duration) (notify.Lease, error) {
	if maxAttempts < 1 || maxAttempts > 20 || timeout <= 0 {
		return notify.Lease{}, errors.New("invalid alert delivery attempt/timeout configuration")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return notify.Lease{}, err
	}
	defer tx.Rollback(ctx)
	// Bound locks and transaction lifetime while draining an expired restart backlog.
	for range 128 {
		d, err := scanDelivery(tx.QueryRow(ctx, "SELECT "+deliveryColumns+` FROM alert_deliveries
			WHERE (status='pending' AND next_attempt<=clock_timestamp()) OR (status='processing' AND lease_expires_at<=clock_timestamp())
			ORDER BY next_attempt,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`))
		if errors.Is(err, ErrNotFound) {
			if err := tx.Commit(ctx); err != nil {
				return notify.Lease{}, err
			}
			return notify.Lease{}, ErrNotFound
		}
		if err != nil {
			return notify.Lease{}, err
		}
		now, err := alertNow(ctx, tx)
		if err != nil {
			return notify.Lease{}, err
		}
		if d.Status == notify.Processing {
			if _, err := tx.Exec(ctx, `UPDATE alert_attempts SET status='lease_expired',completed_at=$3,reason='lease_expired' WHERE delivery_id=$1 AND number=$2 AND status='processing'`, d.ID, d.Attempts, now); err != nil {
				return notify.Lease{}, err
			}
			d.LastReason, d.LastStatusCode = "lease_expired", nil
		}
		if d.Attempts >= maxAttempts {
			reason := d.LastReason
			if reason == "" {
				reason = "attempts_exhausted"
			}
			if _, err := tx.Exec(ctx, `UPDATE alert_deliveries SET status='failed',lease_token='',lease_expires_at=NULL,updated_at=$2,last_reason=$3,last_status_code=$4 WHERE id=$1`, d.ID, now, reason, d.LastStatusCode); err != nil {
				return notify.Lease{}, err
			}
			continue
		}
		token, err := randomKey()
		if err != nil {
			return notify.Lease{}, err
		}
		d.Attempts++
		expires := now.Add(timeout + 30*time.Second)
		if _, err := tx.Exec(ctx, `UPDATE alert_deliveries SET status='processing',attempts=$2,updated_at=$3,lease_token=$4,lease_expires_at=$5,last_reason=$6,last_status_code=$7 WHERE id=$1`, d.ID, d.Attempts, now, token, expires, d.LastReason, d.LastStatusCode); err != nil {
			return notify.Lease{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO alert_attempts(delivery_id,number,started_at,status) VALUES($1,$2,$3,'processing')`, d.ID, d.Attempts, now); err != nil {
			return notify.Lease{}, err
		}
		d.Status, d.UpdatedAt = notify.Processing, now
		if err := tx.Commit(ctx); err != nil {
			return notify.Lease{}, err
		}
		return notify.Lease{Delivery: d, Token: token, ExpiresAt: expires}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return notify.Lease{}, err
	}
	return notify.Lease{}, ErrNotFound
}

func (s *Store) CompleteAlertDelivery(ctx context.Context, lease notify.Lease, outcome notify.Outcome, maxAttempts int) error {
	if maxAttempts < 1 || maxAttempts > 20 || outcome.ElapsedMS < 0 || len(outcome.Reason) > 64 || outcome.RetryAfter < 0 || outcome.RetryAfter > 0 && outcome.Disposition != notify.Retry {
		return errors.New("invalid alert delivery outcome")
	}
	if outcome.Disposition != notify.Delivered && outcome.Disposition != notify.Retry && outcome.Disposition != notify.Permanent {
		return errors.New("invalid alert delivery disposition")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var d notify.Delivery
	var token string
	var expires *time.Time
	fields := append(deliveryFields(&d), &token, &expires)
	err = tx.QueryRow(ctx, "SELECT "+deliveryColumns+",lease_token,lease_expires_at FROM alert_deliveries WHERE id=$1 FOR UPDATE", lease.ID).Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	now, err := alertNow(ctx, tx)
	if err != nil {
		return err
	}
	if d.Status != notify.Processing || token == "" || token != lease.Token || expires == nil || !now.Before(*expires) {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE alert_attempts SET completed_at=$3,status=$4,reason=$5,status_code=$6,elapsed_ms=$7 WHERE delivery_id=$1 AND number=$2 AND status='processing'`,
		d.ID, d.Attempts, now, string(outcome.Disposition), outcome.Reason, outcome.StatusCode, outcome.ElapsedMS); err != nil {
		return err
	}
	status := notify.Failed
	next := d.NextAttempt
	var sent *time.Time
	if outcome.Disposition == notify.Delivered {
		status = notify.Sent
		sent = &now
	} else if outcome.Disposition == notify.Retry && d.Attempts < maxAttempts {
		status = notify.Pending
		next = now.Add(notify.RetryDelay(d.Attempts, outcome.RetryAfter))
	}
	if _, err := tx.Exec(ctx, `UPDATE alert_deliveries SET status=$2,next_attempt=$3,updated_at=$4,sent_at=$5,last_reason=$6,last_status_code=$7,lease_token='',lease_expires_at=NULL WHERE id=$1`,
		d.ID, status, next, now, sent, outcome.Reason, outcome.StatusCode); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
