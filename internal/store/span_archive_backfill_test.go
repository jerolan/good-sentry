//go:build integration

package store

import (
	"strconv"
	"testing"
	"time"

	"goodsentry/internal/spanarchive"
)

func TestBackfillSpanArchiveIndexesExistingObjectsBeforeCompletion(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	projectID, err := strconv.ParseInt(project.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	span := spanarchive.Span{
		ID: 900001, ProjectID: projectID, TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SpanID: "0000000000000001",
		ParentSpanID: "", Name: "backfill span", Status: "ok", IsSegment: true,
		StartTimestamp: created.UnixMicro(), EndTimestamp: created.Add(time.Second).UnixMicro(), Duration: 1000,
		Op: "http.server", Environment: "production", Release: "backfill@1",
		Attributes: []byte(`{"sentry":{"segment":{"name":"backfill span"}}}`), Data: []byte(`{"unknown":"preserved"}`),
		CreatedAt: created.UnixMicro(),
	}
	body, err := spanarchive.Encode([]spanarchive.Span{span})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects}
	key := "projects/" + project.ID + "/spans/backfill.parquet"
	if err := objects.Put(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	var objectID int64
	if err := db.pool.QueryRow(ctx, `INSERT INTO span_archive_objects(project_id,backend,object_key,row_count,object_size_bytes,
	 min_created_at,max_created_at,min_start_timestamp,max_start_timestamp,min_end_timestamp,max_end_timestamp)
	 VALUES($1,'local',$2,1,$3,$4,$4,$5,$5,$6,$6) RETURNING id`, projectID, key, len(body), created,
		created, created.Add(time.Second)).Scan(&objectID); err != nil {
		t.Fatal(err)
	}
	if err := db.BackfillSpanArchive(ctx, options); err != nil {
		t.Fatal(err)
	}
	var complete bool
	if err := db.pool.QueryRow(ctx, `SELECT index_complete FROM span_archive_objects WHERE id=$1`, objectID).Scan(&complete); err != nil || !complete {
		t.Fatalf("backfilled manifest complete=%v err=%v", complete, err)
	}
	var indexed int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM span_archive_rows
	 WHERE archive_object_id=$1 AND span_row_id=$2 AND project_id=$3 AND trace_id=$4 AND span_id=$5
	 AND start_timestamp=$6 AND end_timestamp=$7 AND created_at=$8 AND is_segment
	 AND environment='production' AND release='backfill@1'`, objectID, span.ID, projectID, span.TraceID, span.SpanID,
		time.UnixMicro(span.StartTimestamp), time.UnixMicro(span.EndTimestamp), created).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("indexed cold row count=%d err=%v", indexed, err)
	}
	if err := db.BackfillSpanArchive(ctx, options); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM span_archive_rows WHERE archive_object_id=$1", objectID).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("repeat backfill changed row count=%d err=%v", indexed, err)
	}

	missingKey := "projects/" + project.ID + "/spans/missing.parquet"
	var missingID int64
	if err := db.pool.QueryRow(ctx, `INSERT INTO span_archive_objects(project_id,backend,object_key,row_count,object_size_bytes,
	 min_created_at,max_created_at,min_start_timestamp,max_start_timestamp,min_end_timestamp,max_end_timestamp)
	 VALUES($1,'local',$2,1,$3,$4,$4,$5,$5,$6,$6) RETURNING id`, projectID, missingKey, len(body), created,
		created, created.Add(time.Second)).Scan(&missingID); err != nil {
		t.Fatal(err)
	}
	if err := db.BackfillSpanArchive(ctx, options); err == nil {
		t.Fatal("backfill accepted a manifest whose object is missing")
	}
	if err := db.pool.QueryRow(ctx, "SELECT index_complete FROM span_archive_objects WHERE id=$1", missingID).Scan(&complete); err != nil || complete {
		t.Fatalf("missing object was marked complete=%v err=%v", complete, err)
	}
}
