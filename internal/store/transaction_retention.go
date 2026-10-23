package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// CleanupTransactions removes bounded legacy receipts and standalone spans by
// receipt age. Dependent capture items leave atomically with their receipt;
// unrelated telemetry and release observations retain their own lifetimes.
func (s *Store) CleanupTransactions(ctx context.Context, days int) (int64, int64, error) {
	transactions, spans, _, err := s.cleanupTransactions(ctx, days, nil)
	return transactions, spans, err
}

func (s *Store) CleanupTransactionsWithArchive(ctx context.Context, days int, archive SpanArchiveOptions) (int64, int64, int64, error) {
	if archive.Objects == nil {
		return 0, 0, 0, errors.New("span archive object storage is required")
	}
	if archive.Backend != "local" && archive.Backend != "s3" {
		return 0, 0, 0, errors.New("span archive backend must be local or s3")
	}
	if archive.AfterDays < 1 || archive.AfterDays > days {
		return 0, 0, 0, errors.New("span archive threshold must be within transaction retention")
	}
	return s.cleanupTransactions(ctx, days, &archive)
}

func (s *Store) cleanupTransactions(ctx context.Context, days int, archive *SpanArchiveOptions) (int64, int64, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':transaction-retention',0))`).Scan(&acquired); err != nil {
		return 0, 0, 0, err
	}
	if !acquired {
		return 0, 0, 0, nil
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, days).Scan(&cutoff); err != nil {
		return 0, 0, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,project_id FROM events
 WHERE kind='transaction' AND created_at<$1 ORDER BY created_at,id LIMIT 250`, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	type candidate struct{ id, project int64 }
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var c candidate
		err := row.Scan(&c.id, &c.project)
		return c, err
	})
	if err != nil {
		return 0, 0, 0, err
	}
	var transactions int64
	if len(candidates) > 0 {
		ids, projects := make([]int64, 0, len(candidates)), make([]int64, 0, len(candidates))
		for _, c := range candidates {
			ids, projects = append(ids, c.id), append(projects, c.project)
		}
		// Capture takes its receipt gate before the shared project gate. Never
		// reverse those gates or lock an event before the exclusive project gate.
		if err := lockBulkIssueProjects(ctx, tx, projects); err != nil {
			return 0, 0, 0, err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM events
 WHERE id=ANY($1::bigint[]) AND kind='transaction' AND created_at<$2
 ORDER BY created_at,id FOR UPDATE SKIP LOCKED`, ids, cutoff)
		if err != nil {
			return 0, 0, 0, err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return 0, 0, 0, err
		}
		if len(ids) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM envelope_items item USING events e
 WHERE e.id=ANY($1::bigint[]) AND item.project_id=e.project_id AND item.event_id=e.event_id`, ids); err != nil {
				return 0, 0, 0, err
			}
			removed, err := tx.Exec(ctx, `DELETE FROM events WHERE id=ANY($1::bigint[])`, ids)
			if err != nil {
				return 0, 0, 0, err
			}
			transactions = removed.RowsAffected()
		}
	}
	var archivedSpans, expiredObjects int64
	if archive != nil {
		var archiveCutoff time.Time
		if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, archive.AfterDays).Scan(&archiveCutoff); err != nil {
			return 0, 0, 0, err
		}
		archivedSpans, err = archiveOneSpanCohort(ctx, tx, cutoff, archiveCutoff, *archive)
		if err != nil {
			return 0, 0, 0, err
		}
		expiredObjects, err = cleanupExpiredSpanArchives(ctx, tx, cutoff, *archive)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	removed, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM spans WHERE created_at<$1 ORDER BY created_at,id LIMIT 250 FOR UPDATE SKIP LOCKED
 ) DELETE FROM spans s USING expired e WHERE s.id=e.id`, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	spans := archivedSpans + removed.RowsAffected()
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, err
	}
	return transactions, spans, expiredObjects, nil
}
