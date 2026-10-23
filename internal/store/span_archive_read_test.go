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
	var archivedRootID string
	if err := db.pool.QueryRow(ctx, `SELECT span_row_id::text FROM span_archive_rows
		WHERE project_id=$1 AND trace_id=$2 AND span_id=$3`, projectID,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "0000000000000041").Scan(&archivedRootID); err != nil {
		t.Fatal(err)
	}
	detail, err := db.TransactionDetail(ctx, user.ID, org.Slug, "span", archivedRootID, 1)
	if err != nil || detail.SpanCount != 2 || len(detail.Spans) != 2 || detail.Spans[0].SpanID != "0000000000000041" {
		t.Fatalf("transaction detail cold root and hot children: %+v err=%v", detail, err)
	}
	window := &TimeWindow{Start: time.Unix(1_700_000_000, 0).UTC(), End: time.Unix(1_700_000_004, 0).UTC()}
	groups, err := db.Transactions(ctx, user.ID, org.Slug, project.ID, window)
	if err != nil || len(groups) != 1 || groups[0].Count != 1 || groups[0].LatestSample.ID != archivedRootID || groups[0].LatestSample.Source != "span" {
		t.Fatalf("transaction groups include archived segment: %+v err=%v", groups, err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	stats, err := db.StatsV2(ctx, access, "transaction", []int64{projectID}, window.Start, window.End, "1d")
	if err != nil || len(stats.Groups) != 1 || stats.Groups[0].Totals.SumTimesSeen != 1 {
		t.Fatalf("transaction stats include archived segment: %+v err=%v", stats, err)
	}
	trace, err := db.Trace(ctx, user.ID, org.Slug, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", project.ID, 1, false)
	if err != nil || trace.NodeCount != 2 || len(trace.Data) != 2 {
		t.Fatalf("trace graph hot+cold nodes: %+v err=%v", trace, err)
	}
	var archivedNode string
	for _, node := range trace.Data {
		if node.Source == "span" && node.RecordID == archivedRootID {
			archivedNode = node.ID
		}
	}
	if archivedNode == "" {
		t.Fatalf("trace graph omitted archived span row %s: %+v", archivedRootID, trace.Data)
	}
	rawNode, err := db.TraceNodeData(ctx, user.ID, org.Slug, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", archivedNode, project.ID)
	if err != nil || !json.Valid(rawNode.Data) {
		t.Fatalf("archived trace raw data: %+v err=%v", rawNode, err)
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
