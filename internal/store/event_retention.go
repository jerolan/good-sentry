package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// CleanupEvents expires a bounded receipt cohort and recounts only its current
// owners. Fresh-capture progress and issue topology/history retain their lifetime.
func (s *Store) CleanupEvents(ctx context.Context, days int) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':event-retention',0))`).Scan(&acquired); err != nil {
		return 0, err
	}
	if !acquired {
		return 0, nil
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, days).Scan(&cutoff); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,project_id FROM events
 WHERE kind<>'transaction' AND created_at<$1 ORDER BY created_at,id LIMIT 250`, cutoff)
	if err != nil {
		return 0, err
	}
	type candidate struct{ id, project int64 }
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var c candidate
		err := row.Scan(&c.id, &c.project)
		return c, err
	})
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, tx.Commit(ctx)
	}
	ids, projects := make([]int64, 0, len(candidates)), make([]int64, 0, len(candidates))
	for _, c := range candidates {
		ids, projects = append(ids, c.id), append(projects, c.project)
	}
	// Capture owns receipt then shared project then issue/event rows. Cleanup
	// never takes a receipt gate and acquires every project gate before rows.
	if err := lockBulkIssueProjects(ctx, tx, projects); err != nil {
		return 0, err
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT issue_id FROM events
 WHERE id=ANY($1::bigint[]) AND kind<>'transaction' AND created_at<$2 AND issue_id IS NOT NULL`, ids, cutoff)
	if err != nil {
		return 0, err
	}
	roots, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	// Membership is stable behind the exclusive project gates. Only these
	// bounded owners change; merged descendants need no row lock or recount.
	if _, err := tx.Exec(ctx, `SELECT id FROM issues WHERE id=ANY($1::bigint[]) ORDER BY id DESC FOR UPDATE`, roots); err != nil {
		return 0, err
	}
	rows, err = tx.Query(ctx, `SELECT id,COALESCE(issue_id,0) FROM events
 WHERE id=ANY($1::bigint[]) AND kind<>'transaction' AND created_at<$2
 ORDER BY created_at,id FOR UPDATE SKIP LOCKED`, ids, cutoff)
	if err != nil {
		return 0, err
	}
	type receipt struct{ id, owner int64 }
	pinned, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (receipt, error) {
		var r receipt
		err := row.Scan(&r.id, &r.owner)
		return r, err
	})
	if err != nil {
		return 0, err
	}
	ids, roots = ids[:0], roots[:0]
	seenOwners := make(map[int64]bool, len(pinned))
	for _, r := range pinned {
		ids = append(ids, r.id)
		if r.owner != 0 && !seenOwners[r.owner] {
			roots = append(roots, r.owner)
			seenOwners[r.owner] = true
		}
	}
	if len(ids) == 0 {
		return 0, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM envelope_items item USING events e
 WHERE e.id=ANY($1::bigint[]) AND item.project_id=e.project_id AND item.event_id=e.event_id`, ids); err != nil {
		return 0, err
	}
	removed, err := tx.Exec(ctx, `DELETE FROM events WHERE id=ANY($1::bigint[])`, ids)
	if err != nil {
		return 0, err
	}
	if err := recountIssueGroups(ctx, tx, roots); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return removed.RowsAffected(), nil
}
