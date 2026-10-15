//go:build integration

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func operationEvent(id int, at time.Time, spans string) event.Event {
	return event.Event{
		ID: fmt.Sprintf("%032x", id), Type: "transaction", Level: "info", Title: "GET /checkout",
		Transaction: "GET /checkout", Timestamp: at, Duration: 500, TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Data: json.RawMessage(`{"contexts":{"trace":{"op":"http.server"}},"spans":` + spans + `}`),
	}
}

func TestTransactionSpanGroupsCombineProjectedLegacyAndTypedStandalone(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	ev := operationEvent(1, base, `[
	 {"op":"db","description":"SELECT cart","start_timestamp":0,"timestamp":0.1},
	 {"op":"db","description":"SELECT cart","start_timestamp":0,"timestamp":0.3},
	 {"op":"db","description":"SELECT cart"},
	 {"op":"cache","description":"load","start_timestamp":2,"timestamp":1},42]`)
	id := detailEventID(t, ctx, db, project.ID, ev)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other operations", "go")
	if err != nil {
		t.Fatal(err)
	}
	detailEventID(t, ctx, db, other.ID, operationEvent(2, base.Add(time.Second), `[{"op":"db","description":"SELECT cart","start_timestamp":0,"timestamp":0.2}]`))
	root := spanFixture("1234567890123456", true, float64(base.Unix())-1, float64(base.Unix()))
	root["attributes"].(map[string]any)["sentry.segment.id"] = map[string]any{"type": "string", "value": "1234567890123456"}
	child := spanFixture("2222222222222222", false, 10, 10.4)
	child["name"] = "SELECT cart"
	child["attributes"].(map[string]any)["sentry.op"] = map[string]any{"type": "string", "value": "db"}
	child["attributes"].(map[string]any)["sentry.segment.id"] = map[string]any{"type": "string", "value": "1234567890123456"}
	if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, root, child), 2); err != nil || n != 2 {
		t.Fatalf("standalone fixture: %d, %v", n, err)
	}
	for i, fixture := range []struct {
		project, trace string
		attribute      any
		segment        bool
	}{
		{project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"type": "integer", "value": int64(1234567890123456)}, false},
		{project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"type": "string", "value": "ffffffffffffffff"}, false},
		{project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, false},
		{other.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"type": "string", "value": "1234567890123456"}, false},
		{project.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", map[string]any{"type": "string", "value": "1234567890123456"}, false},
		{project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"type": "string", "value": "1234567890123456"}, true},
	} {
		span := spanFixture(fmt.Sprintf("%016x", i+100), fixture.segment, 10, 11)
		span["trace_id"], span["name"] = fixture.trace, "must not attribute"
		if fixture.attribute != nil {
			span["attributes"].(map[string]any)["sentry.segment.id"] = fixture.attribute
		}
		if n, err := db.IngestSpans(ctx, fixture.project, spanBatch(t, span), 1); err != nil || n != 1 {
			t.Fatalf("attribution control %d: %d, %v", i, n, err)
		}
	}
	// The relational schema permits historical attributes whose declared string
	// type disagrees with their JSON value; neither alone establishes membership.
	badValue := spanFixture("fffffffffffffffe", false, 10, 11)
	badValue["attributes"].(map[string]any)["sentry.segment.id"] = map[string]any{"type": "integer", "value": int64(1234567890123456)}
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, badValue), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE spans SET attributes=jsonb_set(attributes,'{sentry.segment.id,type}','"string"') WHERE project_id::text=$1 AND span_id='fffffffffffffffe'`, project.ID); err != nil {
		t.Fatal(err)
	}
	groups, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, "", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if groups.Sample != (TransactionReference{Source: "event", ID: id, ProjectID: project.ID}) || groups.Transaction != "GET /checkout" || groups.Op != "http.server" || groups.TransactionCount != 3 || groups.SpanCount != 7 || groups.TimedSpanCount != 4 || groups.GroupCount != 3 || groups.Page != 1 || groups.PageSize != 50 || groups.HasMore || len(groups.Data) != 3 {
		t.Fatalf("mixed totals: %+v", groups)
	}
	dbGroup := groups.Data[0]
	if dbGroup.Op != "db" || dbGroup.Description != "SELECT cart" || dbGroup.Count != 5 || dbGroup.TimedCount != 4 || dbGroup.AvgDuration == nil || math.Abs(*dbGroup.AvgDuration-250) > .001 || dbGroup.P95Duration == nil || math.Abs(*dbGroup.P95Duration-385) > .001 || dbGroup.TotalTime == nil || math.Abs(*dbGroup.TotalTime-1000) > .001 {
		t.Fatalf("continuous p95/mixed valid durations: %+v", dbGroup)
	}
	for _, g := range groups.Data[1:] {
		if g.Count != 1 || g.TimedCount != 0 || g.AvgDuration != nil || g.P95Duration != nil || g.TotalTime != nil {
			t.Fatalf("missing/inverted timing acquired zero metrics: %+v", g)
		}
	}
	if groups.Data[1].Op != "" || groups.Data[1].Description != "" || groups.Data[2].Op != "cache" {
		t.Fatalf("null-time groups lost stable C ordering: %+v", groups.Data)
	}
	if _, created, err := db.Ingest(ctx, project.ID, ev); err != nil || created {
		t.Fatalf("duplicate ingest: %v, %v", created, err)
	}
	after, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, project.ID, nil, 1)
	if err != nil || after.TransactionCount != 2 || after.SpanCount != 6 || after.TimedSpanCount != 3 || after.Data[0].Count != 4 {
		t.Fatalf("duplicate or sibling project leaked into scoped group: %+v, %v", after, err)
	}
	spanID := detailSpanID(t, ctx, db, project.ID, "1234567890123456")
	if got, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "span", spanID, "", nil, 1); err != nil || got.TransactionCount != 3 || got.SpanCount != 7 {
		t.Fatalf("standalone reference resolved a different transaction group: %+v, %v", got, err)
	}
	if _, err := db.TransactionSpanGroups(ctx, "999999999", org.Slug, "event", id, "", nil, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonmember read: %v", err)
	}
	childID := detailSpanID(t, ctx, db, project.ID, "2222222222222222")
	if _, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "span", childID, "", nil, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonsegment reference acquired a group: %v", err)
	}
}

func TestTransactionSpanGroupsPageAllEnvelopesWithStableOrderingAfterRestart(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	names := []string{"A", "Z", "a"}
	for i := range 49 {
		names = append(names, fmt.Sprintf("g%02d", i))
	}
	names = append(names, "ä")
	base := float64(time.Now().Add(-time.Minute).Unix())
	items := []map[string]any{spanFixture("0000000000000001", true, base, base+1)}
	for i := range 1060 {
		child := spanFixture(fmt.Sprintf("%016x", i+2), false, 10, 11)
		child["name"] = names[i%len(names)]
		attrs := child["attributes"].(map[string]any)
		attrs["sentry.op"] = map[string]any{"type": "string", "value": "db"}
		attrs["sentry.segment.id"] = map[string]any{"type": "string", "value": "0000000000000001"}
		items = append(items, child)
	}
	for first := 0; first < len(items); first += 1000 {
		batch := items[first:min(first+1000, len(items))]
		if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, batch...), len(batch)); err != nil || n != len(batch) {
			t.Fatalf("envelope %d: %d, %v", first, n, err)
		}
	}
	id := detailSpanID(t, ctx, db, project.ID, "0000000000000001")
	for page := 1; page <= 3; page++ {
		groups, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "span", id, "", nil, page)
		want := max(0, min(50, len(names)-(page-1)*50))
		if err != nil || groups.TransactionCount != 1 || groups.SpanCount != 1060 || groups.TimedSpanCount != 1060 || groups.GroupCount != 53 || groups.Page != page || groups.PageSize != 50 || groups.HasMore != (page == 1) || len(groups.Data) != want || groups.Data == nil {
			t.Fatalf("page %d capped, lost totals or reordered: %+v, %v", page, groups, err)
		}
		for i, row := range groups.Data {
			if row.Op != "db" || row.Description != names[(page-1)*50+i] || row.Count != 20 || row.TimedCount != 20 || row.AvgDuration == nil || *row.AvgDuration != 1000 || row.P95Duration == nil || *row.P95Duration != 1000 || row.TotalTime == nil || *row.TotalTime != 20000 {
				t.Fatalf("C-order or complete envelope aggregation at page %d row %d: %+v", page, i, row)
			}
		}
		if page == 1 {
			databaseURL := db.pool.Config().ConnString()
			db.Close()
			db, err = Open(ctx, databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(db.Close)
			after, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "span", id, "", nil, page)
			if err != nil || !reflect.DeepEqual(after, groups) {
				t.Fatalf("first group page changed after restart: %+v, %v", after, err)
			}
		} else if page == 2 {
			again, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "span", id, "", nil, page)
			if err != nil || !reflect.DeepEqual(again, groups) {
				t.Fatalf("last group page unstable: %+v, %v", again, err)
			}
		}
	}
}

func historicalOperationEvent(t *testing.T, ctx context.Context, db *Store, projectID string, ev event.Event) string {
	t.Helper()
	var id string
	if err := db.pool.QueryRow(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,transaction_name,trace_id,duration,data)
		VALUES($1,$2,'transaction',$3,'info',$4,$5,$6,$7,$8) RETURNING id::text`, projectID, ev.ID, ev.Title, ev.Timestamp, ev.Transaction, ev.TraceID, ev.Duration, ev.Data).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProjectTransactionSpansHistoricalPendingAndExactNormalization(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	ev := operationEvent(1, time.Now().UTC().Add(-time.Minute), `[
	 {"op":"db","description":"fraction","start_timestamp":1791997200.004,"timestamp":"2026-10-14T17:00:00.005Z"},
	 {"description":false,"name":"fallback"},
	 {"op":"case","description":"","name":"ignored","start_timestamp":0,"timestamp":0},
	 42,null,[],{"op":"reverse","description":"reverse","start_timestamp":2,"timestamp":1},
	 {"op":"bad","description":"year","start_timestamp":"0000-01-01T00:00:00Z","timestamp":1},
	 {"op":"long","description":"centuries","start_timestamp":"0001-01-01T00:00:00Z","timestamp":"9999-12-31T23:59:59Z"},
	 {"op":"comma","description":"offset","start_timestamp":"1970-01-01T02:00:00,001234999+02:00","timestamp":"1970-01-01T00:00:00.002234Z"}]`)
	ev.Data = bytes.Replace(ev.Data, []byte(`"op":"http.server"`), []byte(`"op":42`), 1)
	id := historicalOperationEvent(t, ctx, db, project.ID, ev)
	var original json.RawMessage
	if err := db.pool.QueryRow(ctx, "SELECT data FROM events WHERE id::text=$1", id).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, "", nil, 1); !errors.Is(err, ErrSpanProjectionPending) {
		t.Fatalf("historical transaction returned partial metrics: %v", err)
	}
	for i, payload := range []string{`{}`, `{"spans":null}`, `{"spans":{}}`, `{"spans":"malformed"}`} {
		other := operationEvent(i+2, ev.Timestamp, `[]`)
		other.Data = json.RawMessage(payload)
		historicalOperationEvent(t, ctx, db, project.ID, other)
	}
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || !processed {
		t.Fatalf("first historical event: processed=%v %v", processed, err)
	}
	groups, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, "", nil, 1)
	if err != nil || groups.Op != "42" || groups.TransactionCount != 1 || groups.SpanCount != 10 || groups.TimedSpanCount != 4 || groups.GroupCount != 8 {
		t.Fatalf("projection normalization or unrelated pending rows: %+v, %v", groups, err)
	}
	for _, row := range groups.Data {
		switch row.Description {
		case "fraction":
			if row.TotalTime == nil || math.Abs(*row.TotalTime-1.001) > 1e-9 {
				t.Fatalf("projection changed existing Go float/truncation semantics: %+v", row)
			}
		case "centuries":
			if row.TotalTime == nil || *row.TotalTime != 315537897599000 {
				t.Fatalf("long interval overflowed: %+v", row)
			}
		case "offset":
			if row.TotalTime == nil || *row.TotalTime != 1 {
				t.Fatalf("valid comma/offset timestamp was discarded: %+v", row)
			}
		default:
			if row.Op == "case" {
				if row.Description != "" || row.TimedCount != 1 || row.TotalTime == nil || *row.TotalTime != 0 {
					t.Fatalf("zero duration or empty description changed: %+v", row)
				}
			} else if row.TimedCount != 0 || row.AvgDuration != nil || row.P95Duration != nil || row.TotalTime != nil {
				t.Fatalf("unavailable timing became valid: %+v", row)
			}
		}
	}
	var after json.RawMessage
	var remaining int
	if err := db.pool.QueryRow(ctx, "SELECT data FROM events WHERE id::text=$1", id).Scan(&after); err != nil || !bytes.Equal(after, original) {
		t.Fatalf("projection rewrote historical JSON: %s, %v", after, err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE kind='transaction' AND span_projection_version=0").Scan(&remaining); err != nil || remaining != 4 {
		t.Fatalf("one call projected more than one historical row: pending=%d, %v", remaining, err)
	}
	for range 4 {
		if processed, err := db.ProjectTransactionSpans(ctx); err != nil || !processed {
			t.Fatalf("missing/nonarray historical spans: processed=%v %v", processed, err)
		}
	}
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || processed {
		t.Fatalf("empty historical queue: processed=%v %v", processed, err)
	}
}

func TestTransactionSpanGroupsRootWindowsReferenceAndOrganizationScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	start := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
	window := &TimeWindow{Start: start, End: start.Add(time.Hour)}
	refID := historicalOperationEvent(t, ctx, db, project.ID, operationEvent(1, start.Add(-24*time.Hour), `[]`))
	for i, ev := range []event.Event{
		operationEvent(2, window.End, `[]`),
		operationEvent(3, start, `[]`),
		operationEvent(4, start, `[]`),
	} {
		if i == 1 {
			ev.Transaction = "GET /unrelated"
		} else if i == 2 {
			ev.Data = bytes.Replace(ev.Data, []byte(`"http.server"`), []byte(`"different"`), 1)
		}
		historicalOperationEvent(t, ctx, db, project.ID, ev)
	}
	const child = `[{"op":"db","description":"window","start_timestamp":0,"timestamp":0.1}]`
	detailEventID(t, ctx, db, project.ID, operationEvent(5, start, child))
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Window sibling", "go")
	if err != nil {
		t.Fatal(err)
	}
	detailEventID(t, ctx, db, other.ID, operationEvent(6, start.Add(time.Minute), child))
	for i, end := range []time.Time{start, window.End} {
		spanID := fmt.Sprintf("%016x", i+1)
		root := spanFixture(spanID, true, float64(end.Unix())-1, float64(end.Unix()))
		span := spanFixture(fmt.Sprintf("%016x", i+10), false, 10, 10.2)
		span["name"] = "window"
		span["attributes"].(map[string]any)["sentry.op"] = map[string]any{"type": "string", "value": "db"}
		span["attributes"].(map[string]any)["sentry.segment.id"] = map[string]any{"type": "string", "value": spanID}
		if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, root, span), 2); err != nil {
			t.Fatal(err)
		}
	}
	var foreignOrg, foreignProject, member string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-operations','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'foreign','Foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	foreignID := detailEventID(t, ctx, db, foreignProject, operationEvent(7, start, child))
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('operation-member@example.test',$1,'Member') RETURNING id::text`, []byte("already-hashed")).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		user, project string
		transactions  int64
		spans         int64
	}{{user.ID, "", 3, 3}, {member, "", 3, 3}, {user.ID, project.ID, 2, 2}, {user.ID, other.ID, 1, 1}, {user.ID, foreignProject, 0, 0}} {
		got, err := db.TransactionSpanGroups(ctx, fixture.user, org.Slug, "event", refID, fixture.project, window, 1)
		if err != nil || got.TransactionCount != fixture.transactions || got.SpanCount != fixture.spans {
			t.Fatalf("window/project/member %+v: %+v, %v", fixture, got, err)
		}
	}
	empty, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", refID, "", nil, 1)
	if err != nil || empty.TransactionCount != 0 || empty.SpanCount != 0 || empty.TimedSpanCount != 0 || empty.GroupCount != 0 || empty.HasMore || len(empty.Data) != 0 || empty.Data == nil {
		t.Fatalf("default window or empty summary inherited reference time: %+v, %v", empty, err)
	}
	if _, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", foreignID, "", window, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign occurrence read: %v", err)
	}
	if _, err := db.TransactionSpanGroups(ctx, user.ID, "foreign-operations", "event", foreignID, "", window, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign organization read: %v", err)
	}
}

func TestProjectTransactionSpansConcurrentClaimsCancellationAndRestart(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	base := time.Now().UTC().Add(-time.Minute)
	ids := []string{}
	for i := range 12 {
		ids = append(ids, historicalOperationEvent(t, ctx, db, project.ID, operationEvent(i+1, base, `[{"op":"db","description":"worker","start_timestamp":0,"timestamp":0.01}]`)))
	}
	locked, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(ctx)
	if _, err := locked.Exec(ctx, "SELECT id FROM events WHERE id::text=$1 FOR UPDATE", ids[0]); err != nil {
		t.Fatal(err)
	}
	bounded, stop := context.WithTimeout(ctx, 2*time.Second)
	processed, err := db.ProjectTransactionSpans(bounded)
	stop()
	if err != nil || !processed {
		t.Fatalf("locked oldest event blocked another available event: %v, %v", processed, err)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			count := 0
			for {
				processed, err := db.ProjectTransactionSpans(ctx)
				if err != nil {
					errs <- err
					return
				}
				if !processed {
					counts <- count
					return
				}
				count++
			}
		})
	}
	wg.Wait()
	close(counts)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	completed := 1
	for count := range counts {
		completed += count
	}
	if completed != 11 {
		t.Fatalf("concurrent claims completed %d events, want 11 unlocked events", completed)
	}
	if err := locked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	blocked, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Rollback(ctx)
	if _, err := blocked.Exec(ctx, "LOCK TABLE transaction_span_operations IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	type completion struct {
		processed bool
		err       error
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan completion, 1)
	go func() {
		processed, err := db.ProjectTransactionSpans(canceled)
		done <- completion{processed, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='transaction_span_operations'::regclass AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("historical projection never reached the held table lock")
		}
		select {
		case result := <-done:
			t.Fatalf("projection completed before lock cancellation: %+v", result)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case result := <-done:
		if result.processed || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled work reported completion: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("projection ignored cancellation while waiting for PostgreSQL")
	}
	if err := blocked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var version, partial int
	if err := db.pool.QueryRow(ctx, `SELECT span_projection_version,(SELECT count(*) FROM transaction_span_operations WHERE event_id=events.id)
		FROM events WHERE id::text=$1`, ids[0]).Scan(&version, &partial); err != nil || version != 0 || partial != 0 {
		t.Fatalf("cancellation committed a marker/partial projection: version=%d rows=%d, %v", version, partial, err)
	}
	if _, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", ids[0], "", nil, 1); !errors.Is(err, ErrSpanProjectionPending) {
		t.Fatalf("partly indexed group exposed partial counts: %v", err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	db, err = Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || !processed {
		t.Fatalf("restart did not resume canceled event: %v, %v", processed, err)
	}
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || processed {
		t.Fatalf("restart reprocessed completed history: %v, %v", processed, err)
	}
	groups, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", ids[0], "", nil, 1)
	if err != nil || groups.TransactionCount != 12 || groups.SpanCount != 12 || groups.TimedSpanCount != 12 || len(groups.Data) != 1 || groups.Data[0].TotalTime == nil || *groups.Data[0].TotalTime != 120 {
		t.Fatalf("recovered concurrent projection duplicated/lost events: %+v, %v", groups, err)
	}
}

func TestTransactionSpanProjectionCommitFailureIsAtomicForIngestAndHistory(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	ev := operationEvent(1, time.Now().UTC().Add(-time.Minute), `[{"op":"db","description":"atomic","start_timestamp":0,"timestamp":0.01}]`)
	historical := ev
	historical.ID = fmt.Sprintf("%032x", 2)
	historicalID := historicalOperationEvent(t, ctx, db, project.ID, historical)
	if _, err := db.pool.Exec(ctx, `CREATE TABLE projection_allowed_ops(op text PRIMARY KEY);
		ALTER TABLE transaction_span_operations ADD CONSTRAINT projection_guard FOREIGN KEY(op)
		REFERENCES projection_allowed_ops DEFERRABLE INITIALLY DEFERRED`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := db.Ingest(ctx, project.ID, ev); err == nil || created {
		t.Fatalf("failed transaction accepted an event without its projection: created=%v, %v", created, err)
	}
	var fresh, partial, version int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE event_id=$1", ev.ID).Scan(&fresh); err != nil || fresh != 0 {
		t.Fatalf("failed ingest left an event: %d, %v", fresh, err)
	}
	if processed, err := db.ProjectTransactionSpans(ctx); err == nil || processed {
		t.Fatalf("failed historical commit reported completion: processed=%v, %v", processed, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT span_projection_version,(SELECT count(*) FROM transaction_span_operations WHERE event_id=events.id)
		FROM events WHERE id::text=$1`, historicalID).Scan(&version, &partial); err != nil || version != 0 || partial != 0 {
		t.Fatalf("failed commit retained projection state: version=%d rows=%d, %v", version, partial, err)
	}
	if _, err := db.pool.Exec(ctx, "ALTER TABLE transaction_span_operations DROP CONSTRAINT projection_guard"); err != nil {
		t.Fatal(err)
	}
	if _, created, err := db.Ingest(ctx, project.ID, ev); err != nil || !created {
		t.Fatalf("retry after rollback: created=%v, %v", created, err)
	}
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || !processed {
		t.Fatalf("historical retry after rollback: processed=%v, %v", processed, err)
	}
	groups, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", historicalID, "", nil, 1)
	if err != nil || groups.TransactionCount != 2 || groups.SpanCount != 2 || groups.TimedSpanCount != 2 {
		t.Fatalf("atomic retries did not converge: %+v, %v", groups, err)
	}
}
