package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"goodsentry/internal/spanarchive"
)

type logArchiveObject struct {
	ID              int64           `json:"id"`
	ProjectID       int64           `json:"project_id"`
	Timestamp       time.Time       `json:"timestamp"`
	TraceID         string          `json:"trace_id"`
	SpanID          string          `json:"span_id"`
	Level           string          `json:"level"`
	Body            string          `json:"body"`
	Service         string          `json:"service"`
	Environment     string          `json:"environment"`
	Attributes      json.RawMessage `json:"attributes"`
	Data            json.RawMessage `json:"data"`
	ItemHash        string          `json:"item_hash"`
	CreatedAt       time.Time       `json:"created_at"`
	OTLPBatchID     *int64          `json:"otlp_batch_id"`
	OTLPRecordIndex *int32          `json:"otlp_record_index"`
	Host            string          `json:"host"`
}

func logObjectKey(prefix string, projectID, id int64) (string, error) {
	return spanarchive.PrefixedKey(prefix, fmt.Sprintf("projects/%d/logs/%d.json", projectID, id))
}

func (s *Store) ArchiveLogsS3(ctx context.Context, before time.Time, limit int, objects spanarchive.Objects, prefix string) (int64, error) {
	if objects == nil {
		return 0, fmt.Errorf("log archive object store is required")
	}
	if limit < 1 || limit > 250 {
		limit = 250
	}
	rows, err := s.pool.Query(ctx, `SELECT id,project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,created_at,otlp_batch_id,otlp_record_index,host
 FROM logs WHERE created_at < $1 ORDER BY created_at,id LIMIT $2`, before, limit)
	if err != nil {
		return 0, dbError(err)
	}
	var candidates []logArchiveObject
	for rows.Next() {
		var row logArchiveObject
		if err := rows.Scan(&row.ID, &row.ProjectID, &row.Timestamp, &row.TraceID, &row.SpanID, &row.Level, &row.Body, &row.Service, &row.Environment, &row.Attributes, &row.Data, &row.ItemHash, &row.CreatedAt, &row.OTLPBatchID, &row.OTLPRecordIndex, &row.Host); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(candidates) == 0 {
		return 0, nil
	}
	type uploaded struct {
		row logArchiveObject
		key string
	}
	data := make([]uploaded, 0, len(candidates))
	for _, row := range candidates {
		key, err := logObjectKey(prefix, row.ProjectID, row.ID)
		if err != nil {
			return 0, err
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return 0, fmt.Errorf("encode log archive object: %w", err)
		}
		if err := objects.Put(ctx, key, encoded); err != nil {
			return 0, fmt.Errorf("upload log archive object: %w", err)
		}
		data = append(data, uploaded{row: row, key: key})
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var inserted int64
	for _, item := range data {
		row := item.row
		tag, err := tx.Exec(ctx, `INSERT INTO log_archive (id,project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,created_at,otlp_batch_id,otlp_record_index,host)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING`,
			row.ID, row.ProjectID, row.Timestamp, row.TraceID, row.SpanID, row.Level, row.Body, row.Service, row.Environment, row.Attributes, row.Data, row.ItemHash, row.CreatedAt, row.OTLPBatchID, row.OTLPRecordIndex, row.Host)
		if err != nil {
			return 0, dbError(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO log_archive_objects (project_id,id,object_key,created_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, row.ProjectID, row.ID, item.key, row.CreatedAt); err != nil {
			return 0, dbError(err)
		}
		inserted += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (s *Store) RestoreLogArchiveS3(ctx context.Context, objects spanarchive.Objects, limit int) (int64, error) {
	if objects == nil {
		return 0, fmt.Errorf("log archive object store is required")
	}
	if limit < 1 || limit > 250 {
		limit = 250
	}
	rows, err := s.pool.Query(ctx, `SELECT o.project_id,o.id,o.object_key FROM log_archive_objects o
 WHERE NOT EXISTS (SELECT 1 FROM log_archive l WHERE l.project_id=o.project_id AND l.id=o.id)
 ORDER BY o.created_at,o.project_id,o.id LIMIT $1`, limit)
	if err != nil {
		return 0, dbError(err)
	}
	type item struct {
		project int64
		id      int64
		key     string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.project, &it.id, &it.key); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	var restored int64
	for _, it := range items {
		data, err := objects.Get(ctx, it.key)
		if err != nil {
			return restored, fmt.Errorf("download log archive object %s: %w", it.key, err)
		}
		var row logArchiveObject
		if err := json.Unmarshal(data, &row); err != nil {
			return restored, fmt.Errorf("decode log archive object %s: %w", it.key, err)
		}
		if row.ProjectID != it.project || row.ID != it.id {
			return restored, fmt.Errorf("log archive object identity mismatch for %s", it.key)
		}
		tag, err := s.pool.Exec(ctx, `INSERT INTO log_archive (id,project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,created_at,otlp_batch_id,otlp_record_index,host)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING`,
			row.ID, row.ProjectID, row.Timestamp, row.TraceID, row.SpanID, row.Level, row.Body, row.Service, row.Environment, row.Attributes, row.Data, row.ItemHash, row.CreatedAt, row.OTLPBatchID, row.OTLPRecordIndex, row.Host)
		if err != nil {
			return restored, dbError(err)
		}
		restored += tag.RowsAffected()
	}
	return restored, nil
}

func (s *Store) CleanupLogArchiveS3(ctx context.Context, before time.Time, objects spanarchive.Objects, limit int) (int64, error) {
	if objects == nil {
		return 0, fmt.Errorf("log archive object store is required")
	}
	if limit < 1 || limit > 250 {
		limit = 250
	}
	rows, err := s.pool.Query(ctx, `SELECT project_id,id,object_key FROM log_archive_objects WHERE created_at < $1 ORDER BY created_at,project_id,id LIMIT $2`, before, limit)
	if err != nil {
		return 0, dbError(err)
	}
	type item struct {
		project int64
		id      int64
		key     string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.project, &it.id, &it.key); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	var removed int64
	for _, it := range items {
		if err := objects.Delete(ctx, it.key); err != nil {
			return removed, fmt.Errorf("delete log archive object %s: %w", it.key, err)
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM log_archive_objects WHERE project_id=$1 AND id=$2`, it.project, it.id); err != nil {
			return removed, dbError(err)
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM log_archive WHERE project_id=$1 AND id=$2`, it.project, it.id); err != nil {
			return removed, dbError(err)
		}
		removed++
	}
	return removed, nil
}
