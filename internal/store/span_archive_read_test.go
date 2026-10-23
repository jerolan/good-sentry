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
	root["attributes"].(map[string]any)["sentry.environment"] = map[string]any{"type": "string", "value": "cold-production"}
	root["attributes"].(map[string]any)["sentry.release"] = map[string]any{"type": "string", "value": "cold-checkout@2"}
	child := spanFixture("0000000000000042", false, 1_700_000_002, 1_700_000_003)
	childTwo := spanFixture("0000000000000043", false, 1_700_000_002.5, 1_700_000_003.5)
	child["name"] = "SELECT cart"
	child["attributes"].(map[string]any)["sentry.op"] = map[string]any{"type": "string", "value": "db"}
	child["attributes"].(map[string]any)["sentry.segment.id"] = map[string]any{"type": "string", "value": "0000000000000041"}
	childTwo["name"] = "SELECT cart"
	childTwo["attributes"].(map[string]any)["sentry.op"] = map[string]any{"type": "string", "value": "db"}
	childTwo["attributes"].(map[string]any)["sentry.segment.id"] = map[string]any{"type": "string", "value": "0000000000000041"}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, root, child, childTwo), 3); err != nil || count != 3 {
		t.Fatalf("initial hot spans=%d err=%v", count, err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "cold-checkout@2", Projects: []string{project.ID}}); err != nil {
		t.Fatalf("create release for cold span: %v", err)
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
	if err != nil || count != 3 {
		t.Fatalf("hot+cold trace rows=%d err=%v", count, err)
	}
	spans, err := db.Spans(ctx, user.ID, org.Slug, project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(spans) != 3 {
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
	if err != nil || detail.SpanCount != 3 || len(detail.Spans) != 3 || detail.Spans[0].SpanID != "0000000000000041" {
		t.Fatalf("transaction detail cold root and hot children: %+v err=%v", detail, err)
	}
	window := &TimeWindow{Start: time.Unix(1_700_000_000, 0).UTC(), End: time.Unix(1_700_000_004, 0).UTC()}
	groups, err := db.Transactions(ctx, user.ID, org.Slug, project.ID, window)
	if err != nil || len(groups) != 1 || groups[0].Count != 1 || groups[0].LatestSample.ID != archivedRootID || groups[0].LatestSample.Source != "span" {
		t.Fatalf("transaction groups include archived segment: %+v err=%v", groups, err)
	}
	stats, err := db.StatsV2(ctx, access, "transaction", []int64{projectID}, window.Start, window.End, "1d")
	if err != nil || len(stats.Groups) != 1 || stats.Groups[0].Totals.SumTimesSeen != 1 {
		t.Fatalf("transaction stats include archived segment: %+v err=%v", stats, err)
	}
	trace, err := db.Trace(ctx, user.ID, org.Slug, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", project.ID, 1, false)
	if err != nil || trace.NodeCount != 3 || len(trace.Data) != 3 {
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
	projectEnvironments, err := db.CompatibleProjectEnvironments(ctx, access, project.ID, "visible", 0, 100)
	if err != nil || len(projectEnvironments) != 2 || projectEnvironments[0].Name != "cold-production" || projectEnvironments[1].Name != "production" {
		t.Fatalf("project environment discovery includes cold spans: %+v err=%v", projectEnvironments, err)
	}
	organizationEnvironments, err := db.CompatibleOrganizationEnvironments(ctx, access, "visible", 0, 100)
	if err != nil || len(organizationEnvironments) != 2 || organizationEnvironments[0].Name != "cold-production" || organizationEnvironments[1].Name != "production" {
		t.Fatalf("organization environment discovery includes cold spans: %+v err=%v", organizationEnvironments, err)
	}
	if _, err := db.SetCompatibleProjectEnvironmentVisibility(ctx, access, project.ID, "cold-production", true); err != nil {
		t.Fatalf("set visibility for cold environment: %v", err)
	}
	projectEnvironments, err = db.CompatibleProjectEnvironments(ctx, access, project.ID, "visible", 0, 100)
	if err != nil || len(projectEnvironments) != 1 || projectEnvironments[0].Name != "production" {
		t.Fatalf("hidden cold environment remains visible: %+v err=%v", projectEnvironments, err)
	}
	releases, err := db.Releases(ctx, access, ReleaseFilter{Environments: []string{"cold-production"}, Limit: 25})
	if err != nil || len(releases.Data) != 1 || releases.Data[0].Version != "cold-checkout@2" {
		t.Fatalf("release environment filter includes cold spans: %+v err=%v", releases, err)
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
	if err != nil || count != 3 {
		t.Fatalf("trace expansion rows=%d err=%v", count, err)
	}
	for _, spanID := range []string{"0000000000000042", "0000000000000043"} {
		if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-interval '10 days' WHERE span_id=$1", spanID); err != nil {
			t.Fatal(err)
		}
		_, moved, _, err = db.CleanupTransactionsWithArchive(ctx, 30, options)
		if err != nil || moved != 1 {
			t.Fatalf("archive trace child %s moved=%d err=%v", spanID, moved, err)
		}
	}
	spanGroups, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "span", archivedRootID, "", window, 1)
	if err != nil || spanGroups.TransactionCount != 1 || spanGroups.SpanCount != 2 || spanGroups.GroupCount != 1 || len(spanGroups.Data) != 1 ||
		spanGroups.Data[0].Op != "db" || spanGroups.Data[0].Description != "SELECT cart" || spanGroups.Data[0].Count != 2 {
		t.Fatalf("transaction span groups include archived root and child: %+v err=%v", spanGroups, err)
	}
	scopedTransactions, err := db.ScopedTransactions(ctx, access, TransactionFilter{Project: project.ID, Window: window, Limit: 10})
	if err != nil || len(scopedTransactions) != 1 || scopedTransactions[0].Count != 1 {
		t.Fatalf("scoped transaction groups include archived segment: %+v err=%v", scopedTransactions, err)
	}
	trend, err := db.ScopedTransactionTrend(ctx, access, TransactionFilter{Project: project.ID, Transaction: "GET /checkout", Op: "http.server", Window: window})
	if err != nil || len(trend) != 1 || trend[0].Count != 1 {
		t.Fatalf("scoped transaction trend includes archived segment: %+v err=%v", trend, err)
	}
	spanGroupRows, err := db.ScopedSpanGroups(ctx, access, SpanGroupFilter{Project: project.ID, ProjectIDs: []int64{projectID}, Window: window, Limit: 10})
	if err != nil || len(spanGroupRows) != 1 || spanGroupRows[0].Count != 2 || spanGroupRows[0].Description != "SELECT cart" {
		t.Fatalf("scoped span groups include archived children: %+v err=%v", spanGroupRows, err)
	}
	findings, err := db.DetectNPlusOne(ctx, access, NPlusOneFilter{Project: project.ID, Window: window, Threshold: 2, Limit: 10})
	if err != nil || len(findings) != 1 || findings[0].Count != 2 || findings[0].Transaction != "GET /checkout" {
		t.Fatalf("N+1 findings expand archived trace roots: %+v err=%v", findings, err)
	}
	patterns, err := db.ScopedNPlusOnePatterns(ctx, access, NPlusOneFilter{Project: project.ID, Window: window, Op: "db", Threshold: 2, Limit: 10})
	if err != nil || len(patterns) != 1 || patterns[0].TotalSpans != 2 || patterns[0].TransactionCount != 1 {
		t.Fatalf("N+1 patterns include archived children: %+v err=%v", patterns, err)
	}
	compatibleGroups, err := db.CompatibleTransactionGroups(ctx, access, CompatibleTransactionFilter{
		ProjectIDs: []int64{projectID}, Transaction: "GET /checkout", Op: "http.server", Window: window, Limit: 10,
	})
	if err != nil || len(compatibleGroups) != 1 || compatibleGroups[0].Count != 1 || compatibleGroups[0].ID <= 0 {
		t.Fatalf("compatible transaction groups include archived segment: %+v err=%v", compatibleGroups, err)
	}
	identity, err := db.CompatibleTransactionGroupIdentity(ctx, access, compatibleGroups[0].ID)
	if err != nil || identity.ProjectID != projectID || identity.Transaction != "GET /checkout" || identity.Op != "http.server" {
		t.Fatalf("compatible group identity after read retry boundary: %+v err=%v", identity, err)
	}
}
