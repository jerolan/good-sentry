package store

import (
	"context"
	"time"
)

// ArchiveEvents copies a bounded cohort of retained error/message receipts into
// the local durable archive. The hot rows remain authoritative until the normal
// event-retention cutoff, so child rows, counts and existing write paths are not
// disturbed. Idempotent retries conflict on the original receipt identity.
func (s *Store) ArchiveEvents(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 250 {
		limit = 250
	}
	tag, err := s.pool.Exec(ctx, `WITH batch AS (
 SELECT id,project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at
 FROM events WHERE kind IN ('error','message') AND created_at < $1
 ORDER BY created_at,id LIMIT $2
)
INSERT INTO event_archive (id,project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at)
SELECT id,project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at
FROM batch ON CONFLICT DO NOTHING`, before, limit)
	if err != nil {
		return 0, dbError(err)
	}
	return tag.RowsAffected(), nil
}

// CleanupEventArchive removes archived copies at the same receipt-age cutoff as
// hot event retention. It is bounded and never touches hot events or children.
func (s *Store) CleanupEventArchive(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `WITH batch AS (
 SELECT id FROM event_archive WHERE created_at < $1 ORDER BY created_at,id LIMIT 250
)
DELETE FROM event_archive WHERE id IN (SELECT id FROM batch)`, before)
	if err != nil {
		return 0, dbError(err)
	}
	return tag.RowsAffected(), nil
}
