package store

import (
	"context"
	"time"
)

func (s *Store) ArchiveTransactions(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 250 {
		limit = 250
	}
	tag, err := s.pool.Exec(ctx, `WITH batch AS (
 SELECT id,project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at
 FROM events WHERE kind='transaction' AND created_at < $1 ORDER BY created_at,id LIMIT $2
)
INSERT INTO transaction_archive (id,project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at)
SELECT id,project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at
FROM batch ON CONFLICT DO NOTHING`, before, limit)
	if err != nil {
		return 0, dbError(err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) CleanupTransactionArchive(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `WITH batch AS (
 SELECT id FROM transaction_archive WHERE created_at < $1 ORDER BY created_at,id LIMIT 250
)
DELETE FROM transaction_archive WHERE id IN (SELECT id FROM batch)`, before)
	if err != nil {
		return 0, dbError(err)
	}
	return tag.RowsAffected(), nil
}
