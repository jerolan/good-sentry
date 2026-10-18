package store

import (
	"context"
	"time"
)

// CleanupMonitorChecks expires completed history without touching monitor state,
// leases or transition deliveries. The count acknowledges a committed pass only.
func (s *Store) CleanupMonitorChecks(ctx context.Context, days int) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':uptime-retention',0))`).Scan(&acquired); err != nil {
		return 0, err
	}
	if !acquired {
		return 0, nil
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, days).Scan(&cutoff); err != nil {
		return 0, err
	}
	removed, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM monitor_checks WHERE completed_at<$1 ORDER BY completed_at,id LIMIT 250 FOR UPDATE SKIP LOCKED
 ) DELETE FROM monitor_checks c USING expired e WHERE c.id=e.id`, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return removed.RowsAffected(), nil
}
