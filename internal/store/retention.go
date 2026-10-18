package store

import (
	"context"
	"time"
)

// CleanupLogs removes one bounded cohort by receipt age. The caller supplies
// validated deployment days. Counts describe a committed transaction only.
// The existing batch/ordinal index supports the orphan-reference lookup.
func (s *Store) CleanupLogs(ctx context.Context, days int) (int64, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':log-retention',0))`).Scan(&acquired); err != nil {
		return 0, 0, err
	}
	if !acquired {
		return 0, 0, nil
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, days).Scan(&cutoff); err != nil {
		return 0, 0, err
	}
	removed, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM logs WHERE created_at<$1 ORDER BY created_at,id LIMIT 250 FOR UPDATE SKIP LOCKED
 ) DELETE FROM logs l USING expired e WHERE l.id=e.id`, cutoff)
	if err != nil {
		return 0, 0, err
	}
	logs := removed.RowsAffected()
	removed, err = tx.Exec(ctx, `WITH orphaned AS (
 SELECT b.id FROM otlp_log_batches b
 WHERE NOT EXISTS(SELECT 1 FROM logs l WHERE l.otlp_batch_id=b.id)
 ORDER BY b.id LIMIT 250 FOR UPDATE OF b SKIP LOCKED
 ) DELETE FROM otlp_log_batches b USING orphaned o WHERE b.id=o.id`)
	if err != nil {
		return 0, 0, err
	}
	batches := removed.RowsAffected()
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return logs, batches, nil
}
