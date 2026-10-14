//go:build integration

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func spanFixture(id string, segment bool, start, end float64) map[string]any {
	return map[string]any{
		"trace_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "span_id": id,
		"name": "GET /checkout", "status": "ok", "is_segment": segment,
		"start_timestamp": start, "end_timestamp": end,
		"attributes": map[string]any{
			"sentry.op":          map[string]any{"type": "string", "value": "http.server"},
			"sentry.environment": map[string]any{"type": "string", "value": "production"},
			"sentry.release":     map[string]any{"type": "string", "value": "checkout@2"},
			"latency":            map[string]any{"type": "double", "value": 1.25, "unit": "second"},
			"attempt":            map[string]any{"type": "integer", "value": 3},
			"retry":              map[string]any{"type": "boolean", "value": true},
			"headers":            map[string]any{"type": "array", "value": []any{"application/json", 3, true}},
		},
		"future": map[string]any{"kept": "original"},
	}
}

func spanBatch(t *testing.T, items ...map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSpansDurableRetryIdentityAndChildContext(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	root := spanFixture("0000000000000001", true, 1791981000, 1791981000.5)
	child := spanFixture("0000000000000002", false, 1791981000.125, 1791981000.25)
	child["parent_span_id"] = "0000000000000001"
	child["name"] = "SELECT cart"
	raw := spanBatch(t, root, child)
	reversed := spanBatch(t, child, root)
	var wg sync.WaitGroup
	counts, errs := make(chan int, 8), make(chan error, 8)
	for i := range 8 {
		batch := raw
		if i%2 != 0 {
			batch = reversed
		}
		wg.Go(func() {
			n, err := db.IngestSpans(ctx, project.ID, batch, 2)
			counts <- n
			errs <- err
		})
	}
	wg.Wait()
	close(counts)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for n := range counts {
		total += n
	}
	if total != 2 {
		t.Fatalf("concurrent retries wrote %d spans, want 2", total)
	}
	spans, err := db.Spans(ctx, user.ID, org.Slug, project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(spans) != 2 {
		t.Fatalf("stored spans: %+v, %v", spans, err)
	}
	if spans[0].SpanID != "0000000000000001" || !spans[0].IsSegment || spans[1].ParentSpanID != spans[0].SpanID || spans[1].IsSegment || spans[1].Duration != 125 || spans[1].Op != "http.server" || spans[1].Release != "checkout@2" || spans[1].Environment != "production" || !spans[1].StartTimestamp.Equal(time.Unix(1791981000, 125000000)) {
		t.Fatalf("span relation/metadata lost: %+v", spans)
	}
	var attrs map[string]struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
		Unit  string          `json:"unit"`
	}
	if err := json.Unmarshal(spans[1].Attributes, &attrs); err != nil {
		t.Fatal(err)
	}
	var array []any
	if json.Unmarshal(attrs["headers"].Value, &array) != nil || len(array) != 3 || array[0] != "application/json" || array[1] != float64(3) || array[2] != true || attrs["headers"].Type != "array" || attrs["latency"].Type != "double" || attrs["latency"].Unit != "second" {
		t.Fatalf("typed attributes lost: %s", spans[1].Attributes)
	}
	var stored struct{ Future map[string]string }
	if err := json.Unmarshal(spans[1].RawSpan, &stored); err != nil || stored.Future["kept"] != "original" {
		t.Fatalf("raw span unknown fields lost: %s, %v", spans[1].RawSpan, err)
	}
	if _, err := db.Spans(ctx, "999999999", org.Slug, project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized trace read: %v", err)
	}
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other spans", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.IngestSpans(ctx, other.ID, raw, 2); err != nil || n != 2 {
		t.Fatalf("retry identity leaked across projects: %d, %v", n, err)
	}
}

func TestSpansValidateWholeBatchBeforePersisting(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"trace uppercase", func(v map[string]any) { v["trace_id"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" }},
		{"span invalid", func(v map[string]any) { v["span_id"] = "xyz" }},
		{"parent invalid", func(v map[string]any) { v["parent_span_id"] = "xyz" }},
		{"different trace", func(v map[string]any) { v["trace_id"] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }},
		{"missing name", func(v map[string]any) { delete(v, "name") }},
		{"status invalid", func(v map[string]any) { v["status"] = "unknown" }},
		{"segment missing", func(v map[string]any) { delete(v, "is_segment") }},
		{"segment invalid", func(v map[string]any) { v["is_segment"] = "true" }},
		{"timestamp null", func(v map[string]any) { v["start_timestamp"] = nil }},
		{"timestamp string", func(v map[string]any) { v["start_timestamp"] = "2026-10-14T00:00:00Z" }},
		{"timestamp outside range", func(v map[string]any) { v["end_timestamp"] = 1e100 }},
		{"negative duration", func(v map[string]any) { v["end_timestamp"] = 1791980999 }},
		{"attribute scalar mismatch", func(v map[string]any) {
			v["attributes"] = map[string]any{"bad": map[string]any{"type": "integer", "value": "3"}}
		}},
		{"attribute array mismatch", func(v map[string]any) {
			v["attributes"] = map[string]any{"bad": map[string]any{"type": "array", "value": "no array"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := spanFixture("0000000000000001", true, 1791981000, 1791981001)
			bad := spanFixture("0000000000000002", false, 1791981000, 1791981001)
			tc.edit(bad)
			if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, first, bad), 2); n != 0 || !errors.Is(err, ErrInvalidSpan) {
				t.Fatalf("invalid batch accepted: n=%d, %v", n, err)
			}
		})
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"items":[]}`), json.RawMessage(`{"items":null}`), json.RawMessage(`null`), spanBatch(t, spanFixture("0000000000000001", true, 1791981000, 1791981001))} {
		if _, err := db.IngestSpans(ctx, project.ID, raw, 2); !errors.Is(err, ErrInvalidSpan) {
			t.Fatalf("invalid batch/count accepted: %s, %v", raw, err)
		}
	}
	oversized := make([]map[string]any, 1001)
	for i := range oversized {
		oversized[i] = spanFixture(fmt.Sprintf("%016x", i), false, 1791981000, 1791981001)
	}
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, oversized...), 1001); !errors.Is(err, ErrInvalidSpan) {
		t.Fatalf("accepted oversized span batch: %v", err)
	}
	spans, err := db.Spans(ctx, user.ID, org.Slug, project.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(spans) != 0 {
		t.Fatalf("invalid batch partly persisted: %+v, %v", spans, err)
	}
}

func TestTransactionsCombineSegmentsWithLegacyWithoutCountingChildren(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	base := time.Now().Add(-time.Minute).Truncate(time.Second)
	items := []map[string]any{}
	for i, seconds := range []float64{0.125, 0.25, 0.5} {
		span := spanFixture(fmt.Sprintf("%016x", i+1), true, float64(base.Unix())+float64(i), float64(base.Unix())+float64(i)+seconds)
		if i == 1 {
			span["status"] = "error"
		}
		items = append(items, span)
	}
	child := spanFixture("0000000000000004", false, float64(base.Unix()), float64(base.Unix())+1.25)
	child["parent_span_id"] = "0000000000000001"
	items = append(items, child)
	if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, items...), 4); err != nil || n != 4 {
		t.Fatalf("ingest segments: %d, %v", n, err)
	}
	legacy := event.Event{ID: "ffffffffffffffffffffffffffffffff", Type: "transaction", Transaction: "GET /checkout", Title: "GET /checkout", Level: "info", Timestamp: base.Add(4 * time.Second), Duration: 625, TraceID: "ffffffffffffffffffffffffffffffff", Data: json.RawMessage(`{"contexts":{"trace":{"op":"http.server","status":"ok"}}}`)}
	if _, _, err := db.Ingest(ctx, project.ID, legacy); err != nil {
		t.Fatal(err)
	}
	groups, err := db.Transactions(ctx, user.ID, org.Slug, project.ID)
	if err != nil || len(groups) != 1 {
		t.Fatalf("combined groups: %+v, %v", groups, err)
	}
	g := groups[0]
	if g.Count != 4 || g.ErrorCount != 1 || math.Abs(g.AvgDuration-375) > 0.001 || math.Abs(g.P50-375) > 0.001 || math.Abs(g.P95-606.25) > 0.001 || g.LatestTraceID != legacy.TraceID {
		t.Fatalf("child spans inflated aggregate or transport omitted: %+v", g)
	}
}
