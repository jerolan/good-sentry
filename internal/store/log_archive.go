package store

import (
	"context"
	"time"
)

// ArchiveLogs copies a bounded cohort of retained logs into the local durable
// archive. The hot row remains authoritative until the normal log-retention
// cutoff, so existing ingestion and OTLP batch references are unchanged.
func (s *Store) ArchiveLogs(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 250 {
		limit = 250
	}
	tag, err := s.pool.Exec(ctx, `WITH batch AS (
 SELECT id,project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,created_at,otlp_batch_id,otlp_record_index,host
 FROM logs WHERE created_at < $1 ORDER BY created_at,id LIMIT $2
)
INSERT INTO log_archive (id,project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,created_at,otlp_batch_id,otlp_record_index,host)
SELECT id,project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,created_at,otlp_batch_id,otlp_record_index,host
FROM batch ON CONFLICT DO NOTHING`, before, limit)
	if err != nil {
		return 0, dbError(err)
	}
	return tag.RowsAffected(), nil
}

// CleanupLogArchive removes archived copies at the same receipt cutoff as hot
// log retention. It never touches hot logs or OTLP batches.
func (s *Store) CleanupLogArchive(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `WITH batch AS (
 SELECT id FROM log_archive WHERE created_at < $1 ORDER BY created_at,id LIMIT 250
)
DELETE FROM log_archive WHERE id IN (SELECT id FROM batch)`, before)
	if err != nil {
		return 0, dbError(err)
	}
	return tag.RowsAffected(), nil
}
