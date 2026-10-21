package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/store"
)

func compatibleLogQueryFor(t *testing.T, raw string, repeatable ...string) (url.Values, int) {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/0/organizations/example/logs/?"+raw, nil)
	w := httptest.NewRecorder()
	query, valid := compatibleLogQuery(w, r, repeatable...)
	if !valid {
		return nil, w.Code
	}
	return query, 200
}

func TestCompatibleLogQueryAdmission(t *testing.T) {
	if _, status := compatibleLogQueryFor(t, "level=error&level=warning", "level"); status != 200 {
		t.Fatalf("repeated level status=%d", status)
	}
	if _, status := compatibleLogQueryFor(t, "service=api&service=web"); status != 400 {
		t.Fatalf("repeated service status=%d", status)
	}
	if _, status := compatibleLogQueryFor(t, "unknown=1"); status != 400 {
		t.Fatalf("unknown key status=%d", status)
	}
	if _, status := compatibleLogQueryFor(t, "project=1&project=2&level=error&host=web-1"); status != 200 {
		t.Fatalf("admitted query status=%d", status)
	}
}

func TestCompatibleLogLevels(t *testing.T) {
	levels, valid := compatibleLogLevels(url.Values{"level": {"warning", "warn", "error"}})
	if !valid || len(levels) != 2 || levels[0] != "warning" || levels[1] != "error" {
		t.Fatalf("levels=%v valid=%t", levels, valid)
	}
	if _, valid := compatibleLogLevels(url.Values{"level": {"critical"}}); valid {
		t.Fatal("an unpublished level was accepted")
	}
	if levels, valid := compatibleLogLevels(url.Values{}); !valid || len(levels) != 0 {
		t.Fatalf("empty levels=%v valid=%t", levels, valid)
	}

	if store.StoredCompatibleLevel("warning") != "warn" || store.PublishedCompatibleLevel("warn") != "warning" {
		t.Fatal("the published level spelling is not mapped")
	}
}

func TestCompatibleTraceID(t *testing.T) {
	want := strings.Repeat("ab", 16)
	for _, value := range []string{want, strings.ToUpper(want), "ABABABAB-ABAB-ABAB-ABAB-ABABABABABAB"} {
		got, valid := compatibleTraceID(value)
		if !valid || got != want {
			t.Fatalf("trace %q => %q valid=%t", value, got, valid)
		}
	}
	for _, value := range []string{"", "abc", strings.Repeat("z", 32), strings.Repeat("a", 31)} {
		if _, valid := compatibleTraceID(value); valid {
			t.Fatalf("trace %q was accepted", value)
		}
	}
}

func TestCompatibleLogCursor(t *testing.T) {
	query, status := compatibleLogQueryFor(t, "cursor=2026-10-21T12:00:00Z%7C42")
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	cursor, valid := compatibleLogCursor(query)
	if !valid || cursor.ID != 42 || !cursor.Timestamp.Equal(time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("cursor=%+v valid=%t", cursor, valid)
	}
	for _, value := range []string{"42", "2026-10-21T12:00:00Z", "2026-10-21T12:00:00Z|0", "2026-10-21T12:00:00Z|-1", "not-a-time|4"} {
		query, status := compatibleLogQueryFor(t, "cursor="+url.QueryEscape(value))
		if status != 200 {
			t.Fatalf("cursor %q admission status=%d", value, status)
		}
		if _, valid := compatibleLogCursor(query); valid {
			t.Fatalf("cursor %q was accepted", value)
		}
	}
	next := compatibleLogNextQuery(url.Values{"level": {"error"}, "cursor": {"old"}, "limit": {"2"}}, store.CompatibleLogCursor{Timestamp: time.Unix(100, 0).UTC(), ID: 7})
	if next.Get("cursor") != "1970-01-01T00:01:40Z|7" || next.Get("limit") != "2" || next.Get("level") != "error" || len(next["cursor"]) != 1 {
		t.Fatalf("next query=%v", next)
	}
}

func TestCompatibleLogDataUnwrapsTypedAttributes(t *testing.T) {
	data := compatibleLogData(json.RawMessage(`{"count":{"type":"integer","value":7},"service":{"type":"string","value":"api"},"plain":{"nested":1},"null":null}`))
	if data["count"] != float64(7) || data["service"] != "api" {
		t.Fatalf("data=%v", data)
	}
	if nested, ok := data["plain"].(map[string]any); !ok || nested["nested"] != float64(1) {
		t.Fatalf("plain=%v", data["plain"])
	}
	if value, exists := data["null"]; !exists || value != nil {
		t.Fatalf("null=%v", value)
	}
	if len(compatibleLogData(nil)) != 0 || len(compatibleLogData(json.RawMessage(`not json`))) != 0 {
		t.Fatal("empty attributes must produce an empty object")
	}
}

func TestCompatibleLogViewCarriesPublishedNames(t *testing.T) {
	severity := 13
	view := compatibleLogView(store.CompatibleLog{
		ID: "9", ProjectID: "3", Timestamp: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), Level: "warning",
		Body: "slow query", Service: "api", Environment: "production", Host: "web-1", TraceID: strings.Repeat("a", 32),
		SpanID: strings.Repeat("b", 16), Attributes: json.RawMessage(`{"duration":{"type":"integer","value":12}}`),
	})
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"traceID"`, `"spanID"`, `"severityNumber":13`, `"projectId":"3"`, `"host":"web-1"`, `"duration":12`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("response %s is missing %s", encoded, want)
		}
	}
	if view.SeverityNumber == nil || *view.SeverityNumber != severity {
		t.Fatalf("severity=%v", view.SeverityNumber)
	}
	unknown := compatibleLogView(store.CompatibleLog{Level: "unknown"})
	if unknown.SeverityNumber != nil {
		t.Fatalf("an unmapped level reported severity %v", *unknown.SeverityNumber)
	}
}

func TestCompatibleLogStatsResponseAlignsBuckets(t *testing.T) {
	window := &store.TimeWindow{
		Start: time.Date(2026, 10, 21, 10, 30, 0, 0, time.UTC),
		End:   time.Date(2026, 10, 21, 13, 15, 0, 0, time.UTC),
	}
	body := compatibleLogStatsResponse(window, []store.CompatibleLogStat{
		{Interval: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC), Level: "error", Count: 2},
		{Interval: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC), Level: "warning", Count: 1},
		{Interval: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), Level: "error", Count: 3},
		{Interval: time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC), Level: "error", Count: 9},
	})
	// The window covers four partially covered hours: 10:30 through 13:15.
	if len(body.Intervals) != 4 {
		t.Fatalf("intervals=%v", body.Intervals)
	}
	if !body.Intervals[0].Equal(time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)) || !body.Intervals[3].Equal(time.Date(2026, 10, 21, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("intervals=%v", body.Intervals)
	}
	if len(body.Series) != 2 || body.Series[0].Name != "error" || body.Series[1].Name != "warning" {
		t.Fatalf("series=%+v", body.Series)
	}
	if got := body.Series[0].Data; len(got) != 4 || got[0] != 2 || got[1] != 0 || got[2] != 3 || got[3] != 0 {
		t.Fatalf("error series=%v", got)
	}
	if got := body.Series[1].Data; len(got) != 4 || got[0] != 1 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("warning series=%v", got)
	}
}

func TestCompatibleLogFilterWindowRules(t *testing.T) {
	build := func(t *testing.T, raw string, stats bool) (store.CompatibleLogFilter, int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/0/organizations/example/logs/?"+raw, nil)
		w := httptest.NewRecorder()
		query, valid := compatibleLogQuery(w, r, "level", "service", "environment")
		if !valid {
			return store.CompatibleLogFilter{}, w.Code
		}
		f, valid := compatibleLogFilter(w, r, query, stats)
		if !valid {
			return store.CompatibleLogFilter{}, w.Code
		}
		return f, 200
	}
	f, status := build(t, "level=warning&limit=2&project=1&project=2", false)
	if status != 200 || f.Limit != 2 || len(f.Levels) != 1 || f.Levels[0] != "warning" || len(f.ProjectIDs) != 2 {
		t.Fatalf("filter=%+v status=%d", f, status)
	}
	if _, status := build(t, "limit=0", false); status != 400 {
		t.Fatalf("zero limit status=%d", status)
	}
	if _, status := build(t, "limit=201", false); status != 400 {
		t.Fatalf("oversized limit status=%d", status)
	}
	if _, status := build(t, "start=2026-10-21T12:00:00Z", false); status != 400 {
		t.Fatalf("unpaired window status=%d", status)
	}
	if _, status := build(t, "start=2026-01-01T00:00:00Z&end=2027-06-01T00:00:00Z", false); status != 400 {
		t.Fatalf("oversized window status=%d", status)
	}
	stats, status := build(t, "service=api&service=web&environment=production", true)
	if status != 200 || len(stats.Services) != 2 || len(stats.Environments) != 1 || stats.Window == nil {
		t.Fatalf("stats filter=%+v status=%d", stats, status)
	}
	if stats.Window.End.Sub(stats.Window.Start) != compatibleLogStatsDefaultH*time.Hour {
		t.Fatalf("default statistics window=%s", stats.Window.End.Sub(stats.Window.Start))
	}
	if _, status := build(t, "start=2026-01-01T00:00:00Z&end=2026-06-01T00:00:00Z", true); status != 400 {
		t.Fatalf("statistics window beyond ninety days status=%d", status)
	}
}
