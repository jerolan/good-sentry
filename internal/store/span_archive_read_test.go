//go:build integration

package store

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"goodsentry/internal/spanarchive"
)

func TestSpanArchiveReadCombinesHotAndColdRowsAndExpandsSeedTraces(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	projectID, err := strconv.ParseInt(project.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects}
	if err := db.EnableSpanArchive(ctx, options, 30); err != nil {
		t.Fatal(err)
	}
	root := spanFixture("0000000000000041", true, 1_700_000_000, 1_700_000_001)
	child := spanFixture("0000000000000042", false, 1_700_000_002, 1_700_000_003)
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, root, child), 2); err != nil || count != 2 {
		t.Fatalf("initial hot spans=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-interval '10 days' WHERE span_id=$1", "0000000000000041"); err != nil {
		t.Fatal(err)
	}
	_, moved, _, err := db.CleanupTransactionsWithArchive(ctx, 30, options)
	if err != nil || moved != 1 {
		t.Fatalf("archive root moved=%d err=%v", moved, err)
	}
	count, err := withSpanArchiveRead(ctx, db, func(read *spanArchiveRead) (int, error) {
		if err := read.Load(ctx, spanArchiveSelector{ProjectID: projectID, TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
			return 0, err
		}
		var total int
		if err := read.tx.QueryRow(ctx, `SELECT count(*) FROM spans WHERE project_id=$1 AND trace_id=$2`, projectID,
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&total); err != nil {
			return 0, err
		}
		var raw []byte
		if err := read.tx.QueryRow(ctx, "SELECT data FROM spans WHERE project_id=$1 AND span_id=$2", projectID, "0000000000000041").Scan(&raw); err != nil {
			return 0, err
		}
		var preserved map[string]any
		if err := json.Unmarshal(raw, &preserved); err != nil || preserved["future"] == nil {
			return 0, err
		}
		return total, nil
	})
	if err != nil || count != 2 {
		t.Fatalf("hot+cold trace rows=%d err=%v", count, err)
	}
	spans, err := db.Spans(ctx, user.ID, org.Slug, project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(spans) != 2 {
		t.Fatalf("public span reader hot+cold rows=%d err=%v", len(spans), err)
	}
	var publicRaw map[string]any
	if err := json.Unmarshal(spans[0].RawSpan, &publicRaw); err != nil || publicRaw["future"] == nil {
		t.Fatalf("public span reader lost original payload: %s, %v", spans[0].RawSpan, err)
	}

	count, err = withSpanArchiveRead(ctx, db, func(read *spanArchiveRead) (int, error) {
		windowStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		windowEnd := windowStart.Add(24 * time.Hour)
		if err := read.Load(ctx, spanArchiveSelector{
			ProjectID: projectID, TimeColumn: "start_timestamp", Start: &windowStart, End: &windowEnd,
		}); err != nil {
			return 0, err
		}
		if err := read.AddTraceSeeds(ctx, `SELECT project_id,trace_id FROM spans WHERE project_id=$1 AND span_id=$2`,
			projectID, "0000000000000042"); err != nil {
			return 0, err
		}
		if err := read.LoadSeededTraces(ctx); err != nil {
			return 0, err
		}
		var total int
		if err := read.tx.QueryRow(ctx, `SELECT count(*) FROM spans WHERE project_id=$1 AND trace_id=$2`, projectID,
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&total); err != nil {
			return 0, err
		}
		return total, nil
	})
	if err != nil || count != 2 {
		t.Fatalf("trace expansion rows=%d err=%v", count, err)
	}
}
