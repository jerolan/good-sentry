package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/spanarchive"
)

const spanArchiveObjectCleanupLimit = 50

type SpanArchiveOptions struct {
	Backend   string
	AfterDays int
	S3Prefix  string
	Objects   spanarchive.Objects
}

const spanArchiveInputBytes = `octet_length(trace_id)+octet_length(span_id)+octet_length(parent_span_id)+
 octet_length(name)+octet_length(status)+octet_length(op)+octet_length(environment)+octet_length(release)+
 octet_length(attributes::text)+octet_length(data::text)`

func archiveOneSpanCohort(ctx context.Context, tx pgx.Tx, retainFrom, archiveBefore time.Time, options SpanArchiveOptions) (int64, error) {
	var projectID int64
	err := tx.QueryRow(ctx, `SELECT project_id FROM spans
 WHERE created_at >= $1 AND created_at < $2 AND `+spanArchiveInputBytes+` <= $3
 ORDER BY created_at,id LIMIT 1`, retainFrom, archiveBefore, spanarchive.MaxInputBytes).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("select span archive project: %w", err)
	}
	candidates, err := tx.Query(ctx, `SELECT id,trace_id,span_id,`+spanArchiveInputBytes+` AS estimated_bytes FROM spans
 WHERE project_id=$1 AND created_at >= $2 AND created_at < $3 AND `+spanArchiveInputBytes+` <= $4
 ORDER BY created_at,id LIMIT $5`, projectID, retainFrom, archiveBefore, spanarchive.MaxInputBytes, spanarchive.MaxRows)
	if err != nil {
		return 0, fmt.Errorf("select span archive cohort: %w", err)
	}
	ids := make([]int64, 0, spanarchive.MaxRows)
	identities := make([]spanArchiveIdentity, 0, spanarchive.MaxRows)
	var inputBytes int64
	for candidates.Next() {
		var id int64
		var traceID, spanID string
		var estimatedBytes int64
		if err := candidates.Scan(&id, &traceID, &spanID, &estimatedBytes); err != nil {
			candidates.Close()
			return 0, fmt.Errorf("read span archive cohort: %w", err)
		}
		if inputBytes+estimatedBytes > spanarchive.MaxInputBytes {
			break
		}
		ids = append(ids, id)
		identities = append(identities, spanArchiveIdentity{projectID: projectID, traceID: traceID, spanID: spanID})
		inputBytes += estimatedBytes
	}
	if err := candidates.Err(); err != nil {
		candidates.Close()
		return 0, fmt.Errorf("read span archive cohort: %w", err)
	}
	candidates.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	if err := lockSpanArchiveIdentities(ctx, tx, identities); err != nil {
		return 0, fmt.Errorf("lock span archive cohort identities: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT id,project_id,trace_id,span_id,parent_span_id,name,status,is_segment,
 start_timestamp,end_timestamp,duration,op,environment,release,attributes,data,created_at
 FROM spans WHERE project_id=$1 AND id=ANY($2::bigint[]) AND created_at >= $3 AND created_at < $4
 AND `+spanArchiveInputBytes+` <= $5
 ORDER BY created_at,id FOR UPDATE SKIP LOCKED`, projectID, ids, retainFrom, archiveBefore, spanarchive.MaxInputBytes)
	if err != nil {
		return 0, fmt.Errorf("lock span archive cohort: %w", err)
	}
	spans, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (spanarchive.Span, error) {
		var span spanarchive.Span
		var start, end, created time.Time
		err := row.Scan(&span.ID, &span.ProjectID, &span.TraceID, &span.SpanID, &span.ParentSpanID, &span.Name,
			&span.Status, &span.IsSegment, &start, &end, &span.Duration, &span.Op, &span.Environment, &span.Release,
			&span.Attributes, &span.Data, &created)
		if err == nil {
			span.StartTimestamp, span.EndTimestamp, span.CreatedAt = start.UnixMicro(), end.UnixMicro(), created.UnixMicro()
		}
		return span, err
	})
	if err != nil {
		return 0, fmt.Errorf("read locked span archive cohort: %w", err)
	}
	if len(spans) == 0 {
		return 0, nil
	}
	if len(spans) > spanarchive.MaxRows {
		return 0, errors.New("span archive cohort exceeded its row limit")
	}
	var body []byte
	for len(spans) > 0 {
		body, err = spanarchive.Encode(spans)
		if err == nil {
			break
		}
		if !errors.Is(err, spanarchive.ErrObjectTooLarge) {
			return 0, fmt.Errorf("encode span archive cohort: %w", err)
		}
		if len(spans) == 1 {
			return 0, fmt.Errorf("span %d cannot fit in the archive object limit: %w", spans[0].ID, err)
		}
		spans = spans[:len(spans)/2]
	}
	if len(spans) == 0 {
		return 0, nil
	}
	ids = ids[:0]
	for _, span := range spans {
		ids = append(ids, span.ID)
	}
	key, err := spanarchive.CohortKey(projectID, ids)
	if err != nil {
		return 0, fmt.Errorf("make span archive cohort key: %w", err)
	}
	if options.Backend == "s3" {
		key, err = spanarchive.PrefixedKey(options.S3Prefix, key)
		if err != nil {
			return 0, fmt.Errorf("make S3 span archive key: %w", err)
		}
	}
	if err := options.Objects.Put(ctx, key, body); err != nil {
		return 0, fmt.Errorf("upload span archive cohort: %w", err)
	}
	minCreated, maxCreated := time.UnixMicro(spans[0].CreatedAt), time.UnixMicro(spans[0].CreatedAt)
	minStart, maxStart := time.UnixMicro(spans[0].StartTimestamp), time.UnixMicro(spans[0].StartTimestamp)
	minEnd, maxEnd := time.UnixMicro(spans[0].EndTimestamp), time.UnixMicro(spans[0].EndTimestamp)
	for _, span := range spans[1:] {
		created, start, end := time.UnixMicro(span.CreatedAt), time.UnixMicro(span.StartTimestamp), time.UnixMicro(span.EndTimestamp)
		if created.Before(minCreated) {
			minCreated = created
		}
		if created.After(maxCreated) {
			maxCreated = created
		}
		if start.Before(minStart) {
			minStart = start
		}
		if start.After(maxStart) {
			maxStart = start
		}
		if end.Before(minEnd) {
			minEnd = end
		}
		if end.After(maxEnd) {
			maxEnd = end
		}
	}
	var objectID int64
	if err := tx.QueryRow(ctx, `INSERT INTO span_archive_objects(project_id,backend,object_key,row_count,object_size_bytes,
 min_created_at,max_created_at,min_start_timestamp,max_start_timestamp,min_end_timestamp,max_end_timestamp,index_complete)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,true) RETURNING id`, projectID, options.Backend, key, len(spans), len(body),
		minCreated, maxCreated, minStart, maxStart, minEnd, maxEnd).Scan(&objectID); err != nil {
		return 0, fmt.Errorf("insert span archive manifest: %w", err)
	}
	for _, span := range spans {
		if _, err := tx.Exec(ctx, `INSERT INTO span_archive_rows(archive_object_id,span_row_id,project_id,trace_id,span_id,
		 start_timestamp,end_timestamp,created_at,is_segment,environment,release)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, objectID, span.ID, span.ProjectID, span.TraceID, span.SpanID,
			time.UnixMicro(span.StartTimestamp), time.UnixMicro(span.EndTimestamp), time.UnixMicro(span.CreatedAt),
			span.IsSegment, span.Environment, span.Release); err != nil {
			return 0, fmt.Errorf("index span %d in archive manifest: %w", span.ID, err)
		}
	}
	deleted, err := tx.Exec(ctx, `DELETE FROM spans WHERE project_id=$1 AND id=ANY($2::bigint[])`, projectID, ids)
	if err != nil {
		return 0, fmt.Errorf("remove archived hot spans: %w", err)
	}
	if deleted.RowsAffected() != int64(len(spans)) {
		return 0, fmt.Errorf("removed %d hot spans after archiving %d", deleted.RowsAffected(), len(spans))
	}
	return int64(len(spans)), nil
}

func markExpiredSpanArchiveRows(ctx context.Context, tx pgx.Tx, cutoff time.Time, options SpanArchiveOptions) error {
	if _, err := tx.Exec(ctx, `DELETE FROM span_archive_rows r USING span_archive_objects o
	 WHERE o.id=r.archive_object_id AND o.backend=$1 AND o.state='ready' AND r.created_at<$2`, options.Backend, cutoff); err != nil {
		return fmt.Errorf("expire span archive catalog rows: %w", err)
	}
	rows, err := tx.Query(ctx, `WITH expired AS (
	 SELECT id FROM span_archive_objects
	 WHERE backend=$1 AND state='ready' AND (project_id IS NULL OR max_created_at<$2)
	 ORDER BY max_created_at,id LIMIT $3 FOR UPDATE SKIP LOCKED
	)
	UPDATE span_archive_objects o SET state='deleting'
	FROM expired e WHERE o.id=e.id RETURNING o.id`, options.Backend, cutoff, spanArchiveObjectCleanupLimit)
	if err != nil {
		return fmt.Errorf("mark expired span archive objects: %w", err)
	}
	if _, err := pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
		return fmt.Errorf("read marked span archive objects: %w", err)
	}
	return nil
}

func (s *Store) cleanupDeletingSpanArchives(ctx context.Context, options SpanArchiveOptions) (int64, error) {
	var removed int64
	for {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return removed, fmt.Errorf("begin deleting span archive cleanup: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT id,object_key FROM span_archive_objects
		 WHERE backend=$1 AND state='deleting' ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`, options.Backend, spanArchiveObjectCleanupLimit)
		if err != nil {
			_ = tx.Rollback(ctx)
			return removed, fmt.Errorf("select deleting span archive objects: %w", err)
		}
		type object struct {
			id  int64
			key string
		}
		objects, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (object, error) {
			var item object
			err := row.Scan(&item.id, &item.key)
			return item, err
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return removed, fmt.Errorf("read deleting span archive objects: %w", err)
		}
		for _, item := range objects {
			if err := options.Objects.Delete(ctx, item.key); err != nil {
				_ = tx.Rollback(ctx)
				return removed, fmt.Errorf("delete span archive object %d: %w", item.id, err)
			}
			deleted, err := tx.Exec(ctx, "DELETE FROM span_archive_objects WHERE id=$1 AND state='deleting'", item.id)
			if err != nil {
				_ = tx.Rollback(ctx)
				return removed, fmt.Errorf("remove deleting span archive manifest %d: %w", item.id, err)
			}
			if deleted.RowsAffected() != 1 {
				_ = tx.Rollback(ctx)
				return removed, fmt.Errorf("deleting span archive manifest %d changed during cleanup", item.id)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return removed, fmt.Errorf("commit deleting span archive cleanup: %w", err)
		}
		removed += int64(len(objects))
		if len(objects) < spanArchiveObjectCleanupLimit {
			return removed, nil
		}
	}
}
