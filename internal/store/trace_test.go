//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"goodsentry/internal/event"
)

func traceLegacy(t *testing.T, ctx context.Context, db *Store, projectID, traceID, spanID, parentID string, sequence int, start any, end time.Time, children []any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"contexts": map[string]any{"trace": map[string]any{"span_id": spanID, "parent_span_id": parentID, "op": "trace.test"}}, "start_timestamp": start, "spans": children, "exact": int64(9223372036854775807)})
	if err != nil {
		t.Fatal(err)
	}
	ev := event.Event{ID: fmt.Sprintf("%032x", sequence), Type: "transaction", Level: "info", Title: "Trace", Transaction: "Trace", TraceID: traceID, Timestamp: end, Duration: 777, Data: raw}
	return detailEventID(t, ctx, db, projectID, ev)
}

func traceChild(span, parent string) map[string]any {
	return map[string]any{"span_id": span, "parent_span_id": parent, "description": "child", "start_timestamp": 1, "timestamp": 2}
}

func TestTraceResolvesMixedSourcesWithoutGuessingContainerParents(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	const trace = "aabbccddaabbccddaabbccddaabbccdd"
	ev := operationEvent(1, time.Unix(12, 0), `[{"span_id":"2222222222222222","parent_span_id":"1111111111111111","description":"child","start_timestamp":10,"timestamp":11},{"description":"no identifiers"}]`)
	ev.TraceID = trace
	ev.Data = json.RawMessage(`{"contexts":{"trace":{"span_id":"1111111111111111","op":"http.server"}},"start_timestamp":9,"spans":[{"span_id":"2222222222222222","parent_span_id":"1111111111111111","description":"child","start_timestamp":10,"timestamp":11},{"description":"no identifiers"}],"exact":9223372036854775807}`)
	id := detailEventID(t, ctx, db, project.ID, ev)
	root := spanFixture("3333333333333333", true, 11, 13)
	root["trace_id"], root["parent_span_id"] = trace, "2222222222222222"
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, root), 1); err != nil {
		t.Fatal(err)
	}
	got, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeCount != 4 || got.ProjectCount != 1 || got.Relationships.Resolved != 2 || got.Relationships.None != 2 || got.UntimedCount != 1 || got.InvalidSpanIDCount != 0 || len(got.Data) != 4 {
		t.Fatalf("mixed trace summary: %+v", got)
	}
	if got.StartTimestamp == nil || !got.StartTimestamp.Equal(time.Unix(9, 0)) || got.EndTimestamp == nil || !got.EndTimestamp.Equal(time.Unix(13, 0)) || got.Duration == nil || *got.Duration != 4000 {
		t.Fatalf("global timing: %+v", got)
	}
	rootID := "event-" + id
	if got.Data[0].ID != rootID || got.Data[1].ID != rootID+"-1" || got.Data[1].ParentID != rootID || got.Data[1].Depth != 1 || got.Data[2].Source != "span" || got.Data[2].ParentID != rootID+"-1" || got.Data[2].Depth != 2 || got.Data[3].ID != rootID+"-2" || got.Data[3].ParentStatus != "none" || got.Data[3].Depth != 0 {
		t.Fatalf("forest inferred or dropped hierarchy: %+v", got.Data)
	}
	for _, node := range got.Data {
		raw, err := db.TraceNodeData(ctx, user.ID, org.Slug, trace, node.ID, "")
		if err != nil || raw.ID != node.ID || raw.Source != node.Source || raw.ProjectID != project.ID || !json.Valid(raw.Data) {
			t.Fatalf("raw %s: %+v, %v", node.ID, raw, err)
		}
	}
	if got.Data[0].Duration == nil || *got.Data[0].Duration != 3000 {
		t.Fatalf("timeline reused persisted event duration instead of endpoints: %+v", got.Data[0])
	}
}

func TestTraceExplicitParentPreferenceAmbiguityAndAllCycleEdges(t *testing.T) {
	ctx, db, user, org, a := telemetryStore(t)
	b, err := db.CreateProject(ctx, user.ID, org.Slug, "B", "go")
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateProject(ctx, user.ID, org.Slug, "C", "go")
	if err != nil {
		t.Fatal(err)
	}
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	children := []any{traceChild("0101010101010101", "aaaaaaaaaaaaaaaa"), traceChild("0202020202020202", "bbbbbbbbbbbbbbbb"), traceChild("0303030303030303", "CCCCCCCCCCCCCCCC"), traceChild("0404040404040404", "dddddddddddddddd"), traceChild("0505050505050505", "ffffffffffffffff"), traceChild("0606060606060606", "not-hex"), traceChild("", ""), traceChild("bad-span", ""), traceChild("1111111111111111", "2222222222222222"), traceChild("2222222222222222", "1111111111111111"), traceChild("3333333333333333", "1111111111111111"), traceChild("4444444444444444", "4444444444444444")}
	id := traceLegacy(t, ctx, db, a.ID, strings.ToUpper(trace), "AAAAAAAAAAAAAAAA", "", 1, 0, time.Unix(3, 0), children)
	traceLegacy(t, ctx, db, b.ID, trace, "aaaaaaaaaaaaaaaa", "", 2, 0, time.Unix(3, 0), nil)
	traceLegacy(t, ctx, db, a.ID, trace, "bbbbbbbbbbbbbbbb", "", 3, 0, time.Unix(3, 0), nil)
	cross := traceLegacy(t, ctx, db, b.ID, trace, "cccccccccccccccc", "", 4, 0, time.Unix(3, 0), nil)
	traceLegacy(t, ctx, db, b.ID, trace, "dddddddddddddddd", "", 5, 0, time.Unix(3, 0), nil)
	traceLegacy(t, ctx, db, c.ID, trace, "dddddddddddddddd", "", 6, 0, time.Unix(3, 0), nil)
	duplicate := spanFixture("bbbbbbbbbbbbbbbb", true, 0, 3)
	if _, err := db.IngestSpans(ctx, a.ID, spanBatch(t, duplicate), 1); err != nil {
		t.Fatal(err)
	}
	got, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]TraceNode{}
	for _, n := range got.Data {
		nodes[n.ID] = n
	}
	prefix := "event-" + id
	for ordinal, status := range map[int]string{1: "resolved", 2: "ambiguous", 3: "resolved", 4: "ambiguous", 5: "missing", 6: "invalid", 7: "none", 8: "none", 9: "cycle", 10: "cycle", 11: "resolved", 12: "cycle"} {
		n := nodes[fmt.Sprintf("%s-%d", prefix, ordinal)]
		if n.ParentStatus != status {
			t.Fatalf("ordinal%d parent: %+v", ordinal, n)
		}
	}
	if nodes[prefix+"-1"].ParentID != prefix || nodes[prefix+"-3"].ParentID != "event-"+cross || nodes[prefix+"-11"].ParentID != prefix+"-9" || nodes[prefix+"-11"].Depth != 1 || nodes[prefix+"-9"].ChildCount != 1 {
		t.Fatalf("local/cross/descendant resolution: %+v", nodes)
	}
	if got.Relationships.Cycle != 3 || got.InvalidSpanIDCount != 1 || nodes[prefix+"-7"].SpanIDStatus != "missing" || nodes[prefix+"-8"].SpanIDStatus != "invalid" || nodes[prefix].SpanID != "AAAAAAAAAAAAAAAA" {
		t.Fatalf("identity/cycle counts: %+v", got)
	}
	counts := got.Relationships
	if counts.None+counts.Resolved+counts.Missing+counts.Ambiguous+counts.Invalid+counts.Cycle != got.NodeCount {
		t.Fatalf("relationship counts do not cover every stored node: %+v", got)
	}
	for _, ordinal := range []int{9, 10, 12} {
		n := nodes[fmt.Sprintf("%s-%d", prefix, ordinal)]
		if n.ParentID != "" || n.ParentPage != 0 || n.Depth != 0 {
			t.Fatalf("cycle edge retained: %+v", n)
		}
	}
	scoped, err := db.Trace(ctx, user.ID, org.Slug, trace, a.ID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range scoped.Data {
		if n.ID == prefix+"-3" && n.ParentStatus != "missing" {
			t.Fatalf("unselected project supplied an ancestor: %+v", n)
		}
	}
}

func TestTracePagesDeepChainsAcrossEnvelopesAndRestart(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	const total = 2001
	for first := 0; first < total; first += 1000 {
		items := []map[string]any{}
		for i := first; i < min(first+1000, total); i++ {
			n := spanFixture(fmt.Sprintf("%016x", i+1), i == 0, 0, 1)
			if i > 0 {
				n["parent_span_id"] = fmt.Sprintf("%016x", i)
			}
			items = append(items, n)
		}
		if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, items...), len(items)); err != nil {
			t.Fatal(err)
		}
	}
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var last TraceDetail
	for _, page := range []int{1, 2, 41, 42} {
		got, err := db.Trace(ctx, user.ID, org.Slug, trace, "", page, true)
		if err != nil {
			t.Fatal(err)
		}
		want := min(50, max(0, total-(page-1)*50))
		if got.NodeCount != total || got.Relationships.Resolved != total-1 || len(got.Data) != want || got.HasMore != (page < 41) || got.Page != page || got.PageSize != 50 || got.Data == nil {
			t.Fatalf("deep forest page%d: %+v", page, got)
		}
		for j, n := range got.Data {
			position := (page-1)*50 + j
			if n.Depth != position || n.SpanID != fmt.Sprintf("%016x", position+1) || n.Ordinal != 0 || n.Source != "span" || n.Duration == nil || *n.Duration != 1000 || n.Offset == nil || *n.Offset != 0 {
				t.Fatalf("deep span%d: %+v", position, n)
			}
			if position > 0 && n.ParentPage != (position-1)/50+1 {
				t.Fatalf("parent page%d: %+v", position, n)
			}
		}
		if page == 41 {
			last = got
		}
	}
	url := db.pool.Config().ConnString()
	db.Close()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	after, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 41, true)
	if err != nil || !reflect.DeepEqual(after, last) {
		t.Fatalf("deep graph changed after reopen: %+v, %v", after, err)
	}
}

func TestTraceTimingBoundsPreservePartialReversedAndZeroIntervals(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	id := traceLegacy(t, ctx, db, project.ID, trace, "1111111111111111", "", 1, 10, time.Unix(12, 0), []any{
		map[string]any{"start_timestamp": 20, "timestamp": 15}, map[string]any{"timestamp": 0}, map[string]any{"start_timestamp": 5, "timestamp": 5}, map[string]any{"start_timestamp": "bad", "timestamp": nil},
	})
	got, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartTimestamp == nil || !got.StartTimestamp.Equal(time.Unix(0, 0)) || got.EndTimestamp == nil || !got.EndTimestamp.Equal(time.Unix(20, 0)) || got.Duration == nil || *got.Duration != 20000 || got.UntimedCount != 3 {
		t.Fatalf("partial/reversed endpoints lost: %+v", got)
	}
	wantIDs := []string{"event-" + id + "-2", "event-" + id + "-3", "event-" + id, "event-" + id + "-1", "event-" + id + "-4"}
	for i, n := range got.Data {
		if n.ID != wantIDs[i] {
			t.Fatalf("fallback/null timing sort: %+v", got.Data)
		}
	}
	if got.Data[0].Duration != nil || got.Data[0].Offset == nil || *got.Data[0].Offset != 0 || got.Data[1].Duration == nil || *got.Data[1].Duration != 0 || got.Data[3].Duration != nil || got.Data[3].StartTimestamp == nil || got.Data[3].EndTimestamp == nil || got.Data[3].Offset == nil || *got.Data[3].Offset != 20000 || got.Data[4].Offset != nil {
		t.Fatalf("drawable/point/offset semantics: %+v", got.Data)
	}
}

func TestTracePreviewsRawScopeRelatedPagesAndLogFlags(t *testing.T) {
	ctx, db, user, org, a := telemetryStore(t)
	b, err := db.CreateProject(ctx, user.ID, org.Slug, "Errors", "go")
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateProject(ctx, user.ID, org.Slug, "Logs", "go")
	if err != nil {
		t.Fatal(err)
	}
	const trace = "abcdefabcdefabcdefabcdefabcdefab"
	long := strings.Repeat("界", 2001)
	id := traceLegacy(t, ctx, db, a.ID, strings.ToUpper(trace), long, "", 1, 0, time.Unix(1, 0), []any{nil, 42})
	if _, err := db.pool.Exec(ctx, "UPDATE events SET transaction_name=$1,environment=$1,release=$1 WHERE id::text=$2", long, id); err != nil {
		t.Fatal(err)
	}
	for i := range 27 {
		ev := event.Event{ID: fmt.Sprintf("%032x", i+100), Type: "message", Title: long, Level: "info", Fingerprint: "related", TraceID: strings.ToUpper(trace), Timestamp: time.Unix(int64(i/2), 0), Data: json.RawMessage(`{"contexts":{"trace":{"span_id":"root-original"}},"exact":9223372036854775807}`)}
		if _, _, err := db.Ingest(ctx, b.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.IngestLogs(ctx, c.ID, json.RawMessage(fmt.Sprintf(`{"items":[{"timestamp":0,"trace_id":%q,"level":"info","body":"related"}]}`, strings.ToUpper(trace)))); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		got, err := db.Trace(ctx, user.ID, org.Slug, strings.ToUpper(trace), "", 1, enabled)
		if err != nil {
			t.Fatal(err)
		}
		wantProjects, wantLogs := int64(2), int64(0)
		if enabled {
			wantProjects, wantLogs = 3, 1
		}
		if got.TraceID != trace || got.ProjectCount != wantProjects || got.ErrorCount != 27 || got.LogCount != wantLogs || got.LogsEnabled != enabled || got.InvalidSpanIDCount != 1 {
			t.Fatalf("related counts/flag: %+v", got)
		}
		for _, n := range got.Data {
			if utf8.RuneCountInString(n.Name) > 2000 || utf8.RuneCountInString(n.SpanID) > 2000 {
				t.Fatalf("unbounded preview: %+v", n)
			}
		}
		if !got.Data[0].SummaryTruncated || utf8.RuneCountInString(got.Data[0].SpanID) != 2000 {
			t.Fatalf("truncation flag/Unicode boundary: %+v", got.Data[0])
		}
	}
	for _, ordinal := range []int{0, 1, 2} {
		node := "event-" + id
		if ordinal > 0 {
			node += fmt.Sprintf("-%d", ordinal)
		}
		raw, err := db.TraceNodeData(ctx, user.ID, org.Slug, trace, node, "")
		if err != nil {
			t.Fatal(err)
		}
		if ordinal == 0 {
			if !strings.Contains(string(raw.Data), "9223372036854775807") || !strings.Contains(string(raw.Data), long) {
				t.Fatalf("raw original truncated/rounded: %s", raw.Data)
			}
		} else if string(raw.Data) != []string{"", "null", "42"}[ordinal] {
			t.Fatalf("scalar/null raw changed: %s", raw.Data)
		}
	}
	for page := 1; page <= 3; page++ {
		got, err := db.TraceErrors(ctx, user.ID, org.Slug, trace, "", page)
		want := min(25, max(0, 27-(page-1)*25))
		if err != nil || got.Total != 27 || len(got.Data) != want || got.HasMore != (page == 1) || got.PageSize != 25 || got.Data == nil {
			t.Fatalf("related errors page%d: %+v, %v", page, got, err)
		}
		for i, e := range got.Data {
			if e.EventID != fmt.Sprintf("%032x", 100+(page-1)*25+i) || !e.SummaryTruncated || utf8.RuneCountInString(e.Title) != 2000 || e.SpanID != "root-original" || e.IssueID == "" {
				t.Fatalf("error ordering/preview/correlation: %+v", e)
			}
		}
	}
	logOnly, err := db.Trace(ctx, user.ID, org.Slug, trace, c.ID, 1, true)
	if err != nil || logOnly.NodeCount != 0 || logOnly.LogCount != 1 || logOnly.ProjectCount != 1 || logOnly.StartTimestamp != nil || logOnly.Duration != nil {
		t.Fatalf("logs-only trace: %+v, %v", logOnly, err)
	}
	if _, err := db.Trace(ctx, user.ID, org.Slug, trace, c.ID, 1, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled logs exposed logs-only trace: %v", err)
	}
	errorOnly, err := db.Trace(ctx, user.ID, org.Slug, trace, b.ID, 1, false)
	if err != nil || errorOnly.NodeCount != 0 || errorOnly.ErrorCount != 27 || errorOnly.StartTimestamp != nil {
		t.Fatalf("errors-only trace: %+v, %v", errorOnly, err)
	}
	empty, err := db.TraceErrors(ctx, user.ID, org.Slug, strings.Repeat("0", 32), "", 1)
	if err != nil || empty.Total != 0 || empty.Data == nil || len(empty.Data) != 0 {
		t.Fatalf("empty authorized errors: %+v, %v", empty, err)
	}
	if _, err := db.TraceNodeData(ctx, user.ID, org.Slug, trace, "event-"+id, b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other project raw read: %v", err)
	}
	if _, err := db.TraceNodeData(ctx, user.ID, org.Slug, strings.Repeat("0", 32), "event-"+id, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other trace raw read: %v", err)
	}
	if _, err := db.Trace(ctx, "999999999", org.Slug, trace, "", 1, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonmember trace read: %v", err)
	}
}

func TestTracePendingVersionOneAndForeignScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	id := traceLegacy(t, ctx, db, project.ID, trace, "1111111111111111", "", 1, 0, time.Unix(1, 0), []any{traceChild("2222222222222222", "")})
	if _, err := db.pool.Exec(ctx, "UPDATE events SET span_projection_version=1 WHERE id::text=$1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true); !errors.Is(err, ErrSpanProjectionPending) {
		t.Fatalf("pending trace exposed partial graph: %v", err)
	}
	for _, node := range []string{"event-" + id, "event-" + id + "-1"} {
		if _, err := db.TraceNodeData(ctx, user.ID, org.Slug, trace, node, ""); !errors.Is(err, ErrSpanProjectionPending) {
			t.Fatalf("pending raw %s: %v", node, err)
		}
	}
	if _, err := db.TraceNodeData(ctx, user.ID, org.Slug, trace, "event-"+id+"-99", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing pending ordinal: %v", err)
	}
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || !processed {
		t.Fatalf("upgrade: %v %v", processed, err)
	}
	if _, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true); err != nil {
		t.Fatal(err)
	}
	var foreignOrg, foreignProject string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-trace','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'foreign','Foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	foreign := traceLegacy(t, ctx, db, foreignProject, trace, "3333333333333333", "", 2, 0, time.Unix(1, 0), nil)
	for _, call := range []func() error{
		func() error { _, err := db.Trace(ctx, user.ID, org.Slug, trace, foreignProject, 1, true); return err },
		func() error { _, err := db.TraceErrors(ctx, user.ID, org.Slug, trace, foreignProject, 1); return err },
		func() error {
			_, err := db.TraceNodeData(ctx, user.ID, org.Slug, trace, "event-"+foreign, "")
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign data authorized: %v", err)
		}
	}
	var member string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('trace-member@example.test',$1,'Member') RETURNING id::text`, []byte("hash")).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	if got, err := db.Trace(ctx, member, org.Slug, trace, "", 1, true); err != nil || got.NodeCount != 2 {
		t.Fatalf("member or foreign isolation: %+v, %v", got, err)
	}
}

func TestTraceEqualTimeOrderUsesNumericProjectSourceRowAndOrdinal(t *testing.T) {
	ctx, db, user, org, _ := telemetryStore(t)
	var a, b Project
	for i := 2; i <= 12; i++ {
		project, err := db.CreateProject(ctx, user.ID, org.Slug, fmt.Sprintf("Project %d", i), "go")
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			a = project
		}
		b = project
	}
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	children := []any{}
	for i := 1; i <= 12; i++ {
		child := traceChild(fmt.Sprintf("%016x", i), "")
		child["start_timestamp"], child["timestamp"] = 0, 1
		children = append(children, child)
	}
	want := []string{}
	for i := 1; i <= 12; i++ {
		var nested []any
		if i == 1 {
			nested = children
		}
		id := traceLegacy(t, ctx, db, a.ID, trace, fmt.Sprintf("%016x", 1000+i), "", i, 0, time.Unix(1, 0), nested)
		want = append(want, "event-"+id)
	}
	for i := 1; i <= 12; i++ {
		want = append(want, fmt.Sprintf("%s-%d", want[0], i))
	}
	for _, spanID := range []string{"eeeeeeeeeeeeeeee", "ffffffffffffffff"} {
		if _, err := db.IngestSpans(ctx, a.ID, spanBatch(t, spanFixture(spanID, false, 0, 1)), 1); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := db.pool.QueryRow(ctx, "SELECT id::text FROM spans WHERE project_id=$1 AND span_id=$2", a.ID, spanID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, "span-"+id)
	}
	last := traceLegacy(t, ctx, db, b.ID, trace, "abababababababab", "", 100, 0, time.Unix(1, 0), nil)
	want = append(want, "event-"+last)
	got, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, n := range got.Data {
		ids = append(ids, n.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("equal-time forest order: got %v, want %v", ids, want)
	}
}

func TestTraceSnapshotKeepsGraphMetadataRelatedCountsAndErrorPagesCoherent(t *testing.T) {
	ctx, db, user, org, a := telemetryStore(t)
	b, err := db.CreateProject(ctx, user.ID, org.Slug, "Concurrent", "go")
	if err != nil {
		t.Fatal(err)
	}
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := db.IngestSpans(ctx, a.ID, spanBatch(t, spanFixture("1111111111111111", true, 0, 1)), 1); err != nil {
		t.Fatal(err)
	}
	writerCtx, stop := context.WithCancel(ctx)
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		present := false
		for i := 0; ; i++ {
			tx, err := db.pool.Begin(writerCtx)
			if err == nil {
				if !present {
					_, err = tx.Exec(writerCtx, `INSERT INTO spans(project_id,trace_id,span_id,name,status,is_segment,start_timestamp,end_timestamp,duration,data)
					 VALUES($1,$2,'2222222222222222','Concurrent span','ok',false,to_timestamp(0),to_timestamp(1),1000,'{}')`, b.ID, trace)
					if err == nil {
						_, err = tx.Exec(writerCtx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,trace_id,data)
						 SELECT $1,lpad(n::text,32,'0'),'message','Concurrent error','error',to_timestamp(n),$2,'{}' FROM generate_series(1,27) n`, b.ID, trace)
					}
					if err == nil {
						_, err = tx.Exec(writerCtx, `INSERT INTO logs(project_id,timestamp,trace_id,level,body,data,item_hash)
						 VALUES($1,to_timestamp(0),$2,'info','Concurrent log','{}','concurrent')`, b.ID, trace)
					}
				} else {
					for _, table := range []string{"logs", "events", "spans"} {
						if _, err = tx.Exec(writerCtx, "DELETE FROM "+table+" WHERE project_id=$1 AND trace_id=$2", b.ID, trace); err != nil {
							break
						}
					}
				}
				if err == nil {
					err = tx.Commit(writerCtx)
				} else {
					_ = tx.Rollback(context.Background())
				}
			}
			if err != nil {
				if writerCtx.Err() != nil {
					err = nil
				}
				done <- err
				return
			}
			present = !present
			if i == 0 {
				close(started)
			}
		}
	}()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("concurrent writer: %v", err)
		}
	}()
	select {
	case <-started:
	case err := <-done:
		done <- err
		t.Fatalf("start concurrent writer: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	seen := [2]bool{}
	for i := 0; i < 100; i++ {
		got, err := db.Trace(ctx, user.ID, org.Slug, trace, "", 1, true)
		if err != nil {
			t.Fatal(err)
		}
		state := got.LogCount
		if state < 0 || state > 1 || got.NodeCount != 1+state || got.ProjectCount != 1+state || got.ErrorCount != 27*state || int64(len(got.Data)) != got.NodeCount {
			t.Fatalf("torn cross-source trace snapshot: %+v", got)
		}
		seen[state] = true
		for _, n := range got.Data {
			if n.Name == "" || n.ProjectName == "" {
				t.Fatalf("selected metadata disagrees with graph snapshot: %+v", n)
			}
		}
		page := i%3 + 1
		errors, err := db.TraceErrors(ctx, user.ID, org.Slug, trace, "", page)
		if err != nil {
			t.Fatal(err)
		}
		want := min(25, max(0, int(errors.Total)-(page-1)*25))
		if errors.Total != 0 && errors.Total != 27 || len(errors.Data) != want || errors.HasMore != (int64(page*25) < errors.Total) {
			t.Fatalf("torn related-error count/page snapshot: %+v", errors)
		}
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("writer did not expose both committed states: %v", seen)
	}
}
