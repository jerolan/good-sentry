package store

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// CleanupFiles expires immutable child receipts without changing retained event
// or release history. Gates precede every row lock; counts describe commit only.
func (s *Store) CleanupFiles(ctx context.Context, days int) (int64, int64, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':file-retention',0))`).Scan(&acquired); err != nil {
		return 0, 0, 0, err
	}
	if !acquired {
		return 0, 0, 0, nil
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, days).Scan(&cutoff); err != nil {
		return 0, 0, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,project_id,release FROM source_maps
 WHERE created_at<$1 ORDER BY created_at,id LIMIT 250`, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	type mapCandidate struct {
		id, project int64
		release     string
	}
	maps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (mapCandidate, error) {
		var c mapCandidate
		err := row.Scan(&c.id, &c.project, &c.release)
		return c, err
	})
	if err != nil {
		return 0, 0, 0, err
	}
	rows, err = tx.Query(ctx, `(SELECT 'attachment',id,project_id FROM attachments
 WHERE created_at<$1 ORDER BY created_at,id LIMIT 250)
 UNION ALL (SELECT 'item',id,project_id FROM envelope_items
 WHERE created_at<$1 ORDER BY created_at,id LIMIT 250)`, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	type itemCandidate struct {
		kind, id string
		project  int64
	}
	children, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (itemCandidate, error) {
		var c itemCandidate
		err := row.Scan(&c.kind, &c.id, &c.project)
		return c, err
	})
	if err != nil {
		return 0, 0, 0, err
	}
	if len(maps) == 0 && len(children) == 0 {
		return 0, 0, 0, tx.Commit(ctx)
	}
	attachmentIDs, itemIDs := []string{}, []string{}
	projects := make([]int64, 0, len(children))
	for _, c := range children {
		projects = append(projects, c.project)
		if c.kind == "attachment" {
			attachmentIDs = append(attachmentIDs, c.id)
		} else {
			itemIDs = append(itemIDs, c.id)
		}
	}
	if len(projects) > 0 {
		if err := lockBulkIssueProjects(ctx, tx, projects); err != nil {
			return 0, 0, 0, err
		}
	}
	// Match bundle/deletion project order. Never acquire a release or parent
	// row after these artifact gates: file expiry does not modify either parent.
	sort.Slice(maps, func(i, j int) bool {
		return maps[i].project < maps[j].project || maps[i].project == maps[j].project && maps[i].release < maps[j].release
	})
	mapIDs := make([]int64, 0, len(maps))
	for i, c := range maps {
		mapIDs = append(mapIDs, c.id)
		if i == 0 || c.project != maps[i-1].project || c.release != maps[i-1].release {
			if err := lockReleaseArtifacts(ctx, tx, strconv.FormatInt(c.project, 10), c.release); err != nil {
				return 0, 0, 0, err
			}
		}
	}
	removedMaps, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM source_maps WHERE id=ANY($1::bigint[]) AND created_at<$2 ORDER BY created_at,id FOR UPDATE SKIP LOCKED
 ) DELETE FROM source_maps m USING expired e WHERE m.id=e.id`, mapIDs, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	removedAttachments, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM attachments WHERE id=ANY($1::text[]) AND created_at<$2 ORDER BY created_at,id FOR UPDATE SKIP LOCKED
 ) DELETE FROM attachments a USING expired e WHERE a.id=e.id`, attachmentIDs, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	removedItems, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM envelope_items WHERE id=ANY($1::text[]) AND created_at<$2 ORDER BY created_at,id FOR UPDATE SKIP LOCKED
 ) DELETE FROM envelope_items item USING expired e WHERE item.id=e.id`, itemIDs, cutoff)
	if err != nil {
		return 0, 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, err
	}
	return removedMaps.RowsAffected(), removedAttachments.RowsAffected(), removedItems.RowsAffected(), nil
}
