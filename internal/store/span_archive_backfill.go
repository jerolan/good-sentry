package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"goodsentry/internal/spanarchive"
)

// EnableSpanArchive finishes any required index backfill before publishing the
// immutable read/ingest configuration to the rest of the process.
func (s *Store) EnableSpanArchive(ctx context.Context, options SpanArchiveOptions, retentionDays int) error {
	if retentionDays < 1 || options.AfterDays < 1 || options.AfterDays > retentionDays {
		return errors.New("span archive thresholds must be within transaction retention")
	}
	if err := s.BackfillSpanArchive(ctx, options); err != nil {
		return err
	}
	configured := options
	s.spanArchive = &configured
	s.spanArchiveRetentionDays = retentionDays
	return nil
}

// RequireSpanArchiveDisabled prevents startup from hiding objects left by an
// earlier archive-enabled deployment.
func (s *Store) RequireSpanArchiveDisabled(ctx context.Context) error {
	var objects int64
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM span_archive_objects").Scan(&objects); err != nil {
		return fmt.Errorf("check disabled archive state: %w", err)
	}
	if objects != 0 {
		return fmt.Errorf("cannot disable span archival while %d archive objects remain", objects)
	}
	return nil
}

// BackfillSpanArchive makes every ready manifest searchable before archive
// reads or identity checks are enabled. Each object is indexed in its own
// transaction so startup recovery remains bounded and retryable.
func (s *Store) BackfillSpanArchive(ctx context.Context, options SpanArchiveOptions) error {
	if options.Objects == nil {
		return errors.New("span archive object storage is required")
	}
	if options.Backend != "local" && options.Backend != "s3" {
		return errors.New("span archive backend must be local or s3")
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire archive backfill connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended(
	 current_database()||':'||current_schema()||':span-archive-catalog-backfill',0))`); err != nil {
		return fmt.Errorf("lock archive catalog backfill: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended(
		 current_database()||':'||current_schema()||':span-archive-catalog-backfill',0))`)
	}()

	var mismatched int64
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM span_archive_objects WHERE backend<>$1", options.Backend).Scan(&mismatched); err != nil {
		return fmt.Errorf("check archive object backends: %w", err)
	}
	if mismatched != 0 {
		return fmt.Errorf("archive backend %q cannot read %d existing objects", options.Backend, mismatched)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := s.backfillNextSpanArchive(ctx, conn.Conn(), options)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// backfillNextSpanArchive returns done when there are no unindexed ready
// objects left on the serialized startup path.
func (s *Store) backfillNextSpanArchive(ctx context.Context, conn *pgx.Conn, options SpanArchiveOptions) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin archive catalog backfill: %w", err)
	}
	defer tx.Rollback(ctx)
	var id int64
	var projectID pgtype.Int8
	var key string
	var rowCount int32
	var objectSize int64
	err = tx.QueryRow(ctx, `SELECT id,project_id,object_key,row_count,object_size_bytes
	 FROM span_archive_objects WHERE backend=$1 AND state='ready' AND NOT index_complete
	 ORDER BY id LIMIT 1 FOR UPDATE`, options.Backend).Scan(&id, &projectID, &key, &rowCount, &objectSize)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("finish empty archive catalog backfill batch: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("select incomplete archive manifest: %w", err)
	}
	if !projectID.Valid {
		if _, err := tx.Exec(ctx, "UPDATE span_archive_objects SET index_complete=true WHERE id=$1 AND state='ready'", id); err != nil {
			return false, fmt.Errorf("complete detached archive manifest: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit detached archive manifest: %w", err)
		}
		return false, nil
	}
	body, err := options.Objects.Get(ctx, key)
	if err != nil {
		return false, fmt.Errorf("read archive object %d during startup backfill: %w", id, err)
	}
	if int64(len(body)) != objectSize {
		return false, fmt.Errorf("archive object %d size does not match its manifest", id)
	}
	spans, err := spanarchive.Decode(body)
	if err != nil {
		return false, fmt.Errorf("decode archive object %d during startup backfill: %w", id, err)
	}
	if len(spans) != int(rowCount) {
		return false, fmt.Errorf("archive object %d row count does not match its manifest", id)
	}
	for _, span := range spans {
		if span.ProjectID != projectID.Int64 {
			return false, fmt.Errorf("archive object %d contains a span from another project", id)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO span_archive_rows(archive_object_id,span_row_id,project_id,trace_id,span_id,
		 start_timestamp,end_timestamp,created_at,is_segment,environment,release)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, span.ID, span.ProjectID, span.TraceID, span.SpanID,
			time.UnixMicro(span.StartTimestamp), time.UnixMicro(span.EndTimestamp), time.UnixMicro(span.CreatedAt),
			span.IsSegment, span.Environment, span.Release); err != nil {
			return false, fmt.Errorf("index span %d from archive object %d: %w", span.ID, id, err)
		}
	}
	updated, err := tx.Exec(ctx, "UPDATE span_archive_objects SET index_complete=true WHERE id=$1 AND state='ready' AND NOT index_complete", id)
	if err != nil {
		return false, fmt.Errorf("mark archive object %d indexed: %w", id, err)
	}
	if updated.RowsAffected() != 1 {
		return false, fmt.Errorf("archive object %d changed while its catalog was backfilled", id)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit archive catalog object %d: %w", id, err)
	}
	return false, nil
}
