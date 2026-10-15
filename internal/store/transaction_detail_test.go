//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func detailEventID(t *testing.T, ctx context.Context, db *Store, projectID string, ev event.Event) string {
	t.Helper()
	if _, _, err := db.Ingest(ctx, projectID, ev); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.pool.QueryRow(ctx, "SELECT id::text FROM events WHERE project_id::text=$1 AND event_id=$2", projectID, ev.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func detailSpanID(t *testing.T, ctx context.Context, db *Store, projectID, spanID string) string {
	t.Helper()
	var id string
	if err := db.pool.QueryRow(ctx, "SELECT id::text FROM spans WHERE project_id::text=$1 AND span_id=$2", projectID, spanID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTransactionDetailLegacyPreservesRawOrderTimingAndRestart(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	children := []json.RawMessage{
		json.RawMessage(`{"span_id":"1111111111111111","parent_span_id":"2222222222222222","description":"SELECT <cart>","name":"ignored fallback","op":"db","status":"ok","start_timestamp":10.25,"timestamp":"1970-01-01T02:00:10.750000999+02:00","future":{"exact":9223372036854775807}}`),
		json.RawMessage(`{"description":"","name":"do not replace empty description","start_timestamp":20,"timestamp":19}`),
		json.RawMessage(`{"description":42,"name":"fallback","timestamp":"2026-01-01T02:00:00+02:00"}`),
		json.RawMessage(`{"start_timestamp":"0000-01-01T00:00:00Z","timestamp":"9999-12-31T23:30:00-01:00"}`),
		json.RawMessage(`{"start_timestamp":1e100,"timestamp":null}`),
		json.RawMessage(`42`), json.RawMessage(`null`), json.RawMessage(`[1,"unknown"]`),
		json.RawMessage(`{"start_timestamp":"0001-01-01T00:00:00Z","timestamp":"9999-12-31T23:59:59Z"}`),
	}
	for len(children) < 53 {
		children = append(children, json.RawMessage(fmt.Sprintf(`{"description":"ordered %d","start_timestamp":0,"timestamp":0}`, len(children)+1)))
	}
	childJSON, err := json.Marshal(children)
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(fmt.Sprintf(`{"start_timestamp":"2026-10-14T11:00:00.123456999-06:00","contexts":{"trace":{"span_id":"aaaaaaaaaaaaaaaa","parent_span_id":"bbbbbbbbbbbbbbbb","op":"http.server","status":"internal_error"}},"spans":%s,"unknown":{"exact":9223372036854775807}}`, childJSON))
	end := time.Date(2026, 10, 14, 17, 0, 1, 0, time.UTC)
	ev := event.Event{ID: "dddddddddddddddddddddddddddddddd", Type: "transaction", Transaction: "GET /stored", Title: "GET /stored", TraceID: "cccccccccccccccccccccccccccccccc", Environment: "production", Release: "checkout@42", Level: "info", Timestamp: end, Duration: 777, Data: raw}
	id := detailEventID(t, ctx, db, project.ID, ev)
	detail, err := db.TransactionDetail(ctx, user.ID, org.Slug, "event", id, 1)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 14, 17, 0, 0, 123456000, time.UTC)
	if detail.Sample != (TransactionReference{Source: "event", ID: id, ProjectID: project.ID}) || detail.ProjectName != project.Name || detail.EventID != ev.ID || detail.TraceID != ev.TraceID || detail.SpanID != "aaaaaaaaaaaaaaaa" || detail.ParentSpanID != "bbbbbbbbbbbbbbbb" || detail.Name != ev.Transaction || detail.Op != "http.server" || detail.Status != "internal_error" || detail.Environment != ev.Environment || detail.Release != ev.Release || detail.StartTimestamp == nil || !detail.StartTimestamp.Equal(start) || !detail.EndTimestamp.Equal(end) || detail.Duration != 777 {
		t.Fatalf("legacy occurrence metadata: %+v", detail)
	}
	if detail.StartTimestamp.Location() != time.UTC || detail.EndTimestamp.Location() != time.UTC || detail.SpanScope != "transaction" || detail.SpanCount != 53 || detail.Page != 1 || detail.PageSize != 50 || !detail.HasMore || len(detail.Spans) != 50 {
		t.Fatalf("legacy timing/scope/page: %+v", detail)
	}
	first := detail.Spans[0]
	if first.ID != "1" || first.Name != "SELECT <cart>" || first.SpanID != "1111111111111111" || first.ParentSpanID != "2222222222222222" || first.Op != "db" || first.Status != "ok" || first.StartTimestamp == nil || !first.StartTimestamp.Equal(time.Unix(10, 250000000)) || first.EndTimestamp == nil || !first.EndTimestamp.Equal(time.Unix(10, 750000000)) || first.Duration == nil || *first.Duration != 500 {
		t.Fatalf("legacy first child normalization: %+v", first)
	}
	if !strings.Contains(string(first.Data), "9223372036854775807") || !strings.Contains(string(detail.Data), "9223372036854775807") || !strings.Contains(string(detail.Data), "unknown") {
		t.Fatalf("raw context or integer fidelity lost: child=%s root=%s", first.Data, detail.Data)
	}
	if child := detail.Spans[1]; child.Name != "" || child.StartTimestamp == nil || child.EndTimestamp == nil || child.Duration != nil {
		t.Fatalf("inverted timing should preserve instants but omit duration: %+v", child)
	}
	if child := detail.Spans[2]; child.Name != "fallback" || child.StartTimestamp != nil || child.EndTimestamp == nil || !child.EndTimestamp.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || child.Duration != nil {
		t.Fatalf("partial timing/fallback: %+v", child)
	}
	for _, i := range []int{3, 4, 5, 6, 7} {
		child := detail.Spans[i]
		if child.StartTimestamp != nil || child.EndTimestamp != nil || child.Duration != nil || child.ID != strconv.Itoa(i+1) {
			t.Fatalf("malformed child %d was fabricated or reordered: %+v", i+1, child)
		}
	}
	if string(detail.Spans[5].Data) != "42" || string(detail.Spans[6].Data) != "null" || !strings.Contains(string(detail.Spans[7].Data), "unknown") {
		t.Fatalf("malformed array elements disappeared: %+v", detail.Spans[5:8])
	}
	if child := detail.Spans[8]; child.Duration == nil || *child.Duration != 315537897599000 {
		t.Fatalf("supported multi-century duration overflowed: %+v", child)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	after, err := reopened.TransactionDetail(ctx, user.ID, org.Slug, "event", id, 1)
	if err != nil || !reflect.DeepEqual(after, detail) {
		t.Fatalf("occurrence changed after restart: %+v, %v", after, err)
	}
	last, err := reopened.TransactionDetail(ctx, user.ID, org.Slug, "event", id, 2)
	if err != nil || len(last.Spans) != 3 || last.Spans[0].ID != "51" || last.Spans[2].ID != "53" || last.SpanCount != 53 || last.HasMore {
		t.Fatalf("legacy ordinal pagination: %+v, %v", last, err)
	}
	for i, data := range []string{`{}`, `{"spans":null}`, `{"spans":{"bad":"array"}}`, `{"spans":"bad"}`} {
		ev.ID, ev.Data = fmt.Sprintf("%032x", i+1), json.RawMessage(data)
		emptyID := detailEventID(t, ctx, reopened, project.ID, ev)
		empty, err := reopened.TransactionDetail(ctx, user.ID, org.Slug, "event", emptyID, 1)
		if err != nil || empty.SpanCount != 0 || len(empty.Spans) != 0 || empty.HasMore || empty.StartTimestamp != nil {
			t.Fatalf("missing/nonarray spans broke legacy access: %+v, %v", empty, err)
		}
	}
}

func TestTransactionDetailStandaloneTracePagesAcrossEnvelopes(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	base := float64(time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC).Unix())
	for first := 0; first < 1003; first += 1000 {
		items := []map[string]any{}
		for i := first; i < min(first+1000, 1003); i++ {
			start := base + float64(i/2)/1000
			span := spanFixture(fmt.Sprintf("%016x", i+1), i < 2, start, start+0.25)
			if i%3 == 1 {
				span["parent_span_id"] = "0000000000000001"
			}
			span["attributes"].(map[string]any)["exact"] = map[string]any{"type": "integer", "value": int64(9223372036854775807)}
			items = append(items, span)
		}
		if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, items...), len(items)); err != nil || n != len(items) {
			t.Fatalf("span envelope %d: n=%d, %v", first, n, err)
		}
	}
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other trace project", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreign := spanFixture("ffffffffffffffff", true, base, base+1)
	if n, err := db.IngestSpans(ctx, other.ID, spanBatch(t, foreign), 1); err != nil || n != 1 {
		t.Fatalf("other project span: n=%d, %v", n, err)
	}
	foreign["trace_id"] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, foreign), 1); err != nil || n != 1 {
		t.Fatalf("other trace span: n=%d, %v", n, err)
	}
	id := detailSpanID(t, ctx, db, project.ID, "0000000000000001")
	for page := 1; page <= 22; page++ {
		detail, err := db.TransactionDetail(ctx, user.ID, org.Slug, "span", id, page)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Sample != (TransactionReference{Source: "span", ID: id, ProjectID: project.ID}) || detail.ProjectName != project.Name || detail.EventID != "" || detail.SpanID != "0000000000000001" || detail.ParentSpanID != "" || detail.TraceID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || detail.Name != "GET /checkout" || detail.Op != "http.server" || detail.Status != "ok" || detail.Environment != "production" || detail.Release != "checkout@2" || detail.StartTimestamp == nil || detail.Duration != 250 || detail.StartTimestamp.Location() != time.UTC || detail.EndTimestamp.Location() != time.UTC {
			t.Fatalf("standalone root metadata: %+v", detail)
		}
		want := 50
		if page == 21 {
			want = 3
		} else if page == 22 {
			want = 0
		}
		if detail.SpanScope != "projectTrace" || detail.SpanCount != 1003 || detail.Page != page || detail.PageSize != 50 || detail.HasMore != (page < 21) || len(detail.Spans) != want || detail.Spans == nil {
			t.Fatalf("trace page %d capped/leaked or count disagrees: count=%d len=%d hasMore=%v", page, detail.SpanCount, len(detail.Spans), detail.HasMore)
		}
		for j, span := range detail.Spans {
			i := (page-1)*50 + j
			parent := ""
			if i%3 == 1 {
				parent = "0000000000000001"
			}
			if span.SpanID != fmt.Sprintf("%016x", i+1) || span.ParentSpanID != parent || span.StartTimestamp == nil || span.EndTimestamp == nil || span.Duration == nil || *span.Duration != 250 || !strings.Contains(string(span.Data), "9223372036854775807") {
				t.Fatalf("trace order/explicit parents/raw changed at %d: %+v", i, span)
			}
		}
		if page == 1 {
			original := detail
			databaseURL := db.pool.Config().ConnString()
			db.Close()
			db, err = Open(ctx, databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(db.Close)
			after, err := db.TransactionDetail(ctx, user.ID, org.Slug, "span", id, 1)
			if err != nil || !reflect.DeepEqual(after, original) {
				t.Fatalf("standalone detail changed after restart: %+v, %v", after, err)
			}
		}
		if page == 21 {
			again, err := db.TransactionDetail(ctx, user.ID, org.Slug, "span", id, page)
			if err != nil || !reflect.DeepEqual(again, detail) {
				t.Fatalf("last page unstable: %+v, %v", again, err)
			}
		}
	}
}

func TestTransactionDetailMembershipKindSegmentAndTenantScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	var member, foreignOrg, foreignProject string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('detail-member@example.test',$1,'Member') RETURNING id::text`, []byte("already-hashed")).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-detail','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'foreign','Foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	ev := event.Event{ID: "11111111111111111111111111111111", Type: "transaction", Transaction: "selected", Title: "selected", Timestamp: time.Now(), Level: "info", Data: json.RawMessage(`{}`)}
	rootID := detailEventID(t, ctx, db, project.ID, ev)
	errorID := detailEventID(t, ctx, db, project.ID, event.Event{ID: "22222222222222222222222222222222", Type: "error", Fingerprint: "separate", Title: "error", Timestamp: ev.Timestamp, Level: "error", Data: json.RawMessage(`{}`)})
	foreignEventID := detailEventID(t, ctx, db, foreignProject, ev)
	root, child := spanFixture("1111111111111111", true, 10, 11), spanFixture("2222222222222222", false, 10, 11)
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, root, child), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestSpans(ctx, foreignProject, spanBatch(t, root), 1); err != nil {
		t.Fatal(err)
	}
	spanID := detailSpanID(t, ctx, db, project.ID, "1111111111111111")
	childID := detailSpanID(t, ctx, db, project.ID, "2222222222222222")
	foreignSpanID := detailSpanID(t, ctx, db, foreignProject, "1111111111111111")
	for _, ref := range []TransactionReference{{Source: "event", ID: rootID}, {Source: "span", ID: spanID}} {
		if _, err := db.TransactionDetail(ctx, member, org.Slug, ref.Source, ref.ID, 1); err != nil {
			t.Fatalf("actual member cannot read %s: %v", ref.Source, err)
		}
		if _, err := db.TransactionDetail(ctx, "999999999", org.Slug, ref.Source, ref.ID, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("nonmember read %s: %v", ref.Source, err)
		}
	}
	for _, ref := range []TransactionReference{{Source: "event", ID: errorID}, {Source: "span", ID: childID}, {Source: "event", ID: foreignEventID}, {Source: "span", ID: foreignSpanID}, {Source: "event", ID: "999999999"}, {Source: "span", ID: "999999999"}, {Source: "invalid", ID: rootID}, {Source: "event", ID: "broken"}, {Source: "span", ID: "0"}} {
		if _, err := db.TransactionDetail(ctx, user.ID, org.Slug, ref.Source, ref.ID, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid/nonoccurrence/foreign reference %+v: %v", ref, err)
		}
	}
}
