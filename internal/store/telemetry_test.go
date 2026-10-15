//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

func telemetryStore(t *testing.T) (context.Context, *Store, User, Organization, Project) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	name := fmt.Sprintf("goodsentry_telemetry_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE") })
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	user, org, err := db.Bootstrap(ctx, "telemetry@example.test", []byte("already-hashed"), "Telemetry", "Example")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, user.ID, org.Slug, "Backend", "go")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, db, user, org, project
}

func TestOfficialGoSDKLogTimestamp(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	raw := json.RawMessage(`{"items":[{"timestamp":"2026-10-14T15:24:13.018053607-06:00","trace_id":"9c8b50bf355c21a5330de2029b331013","level":"info","body":"SDK timestamp acceptance","attributes":{"compat.attempt":{"type":"integer","value":7}}}]}`)
	if _, err := db.IngestLogs(ctx, project.ID, raw); err != nil {
		t.Fatal(err)
	}
	logs, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{Query: "SDK timestamp acceptance"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs=%+v error=%v", logs, err)
	}
	// PostgreSQL timestamptz stores microseconds. The original SDK timestamp
	// remains in data JSON; the indexed timestamp follows database precision.
	if want := time.Date(2026, 10, 14, 21, 24, 13, 18053000, time.UTC); !logs[0].Timestamp.Equal(want) {
		t.Fatalf("timestamp=%v want=%v", logs[0].Timestamp, want)
	}
}

func TestTransactionsAggregateStoredDurationsAndScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	for i, duration := range []float64{100, 200, 300, 400, 1000} {
		status := "ok"
		if i == 4 {
			status = "internal_error"
		}
		raw := json.RawMessage(fmt.Sprintf(`{"type":"transaction","event_id":"%032x","transaction":"GET /checkout","start_timestamp":%d,"timestamp":%.3f,"contexts":{"trace":{"op":"http.server","status":%q,"trace_id":"%032x"}}}`, i+1, stamp.Unix()+int64(i), float64(stamp.Unix()+int64(i))+duration/1000, status, i+1))
		ev, err := event.Normalize(raw, "", stamp)
		if err != nil {
			t.Fatal(err)
		}
		// Durations are the persisted contract. Avoid floating Unix timestamp
		// precision becoming the oracle for the database percentile results.
		ev.Duration = duration
		if _, _, err := db.Ingest(ctx, project.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Another project", "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg, foreignProject string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'foreign','Foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		id, project, op, kind string
		at                    time.Time
	}{
		{"00000000000000000000000000000010", project.ID, "http.server", "transaction", stamp.Add(-25 * time.Hour)},
		{"00000000000000000000000000000011", other.ID, "http.server", "transaction", stamp},
		{"00000000000000000000000000000012", project.ID, "db", "transaction", stamp},
		{"00000000000000000000000000000013", project.ID, "http.server", "error", stamp},
		{"00000000000000000000000000000014", project.ID, "http.server", "transaction", stamp.Add(time.Hour)},
		{"00000000000000000000000000000015", other.ID, "http.server", "transaction", stamp.Add(-25 * time.Hour)},
		{"00000000000000000000000000000016", foreignProject, "http.server", "transaction", stamp.Add(-25 * time.Hour)},
	} {
		data := json.RawMessage(fmt.Sprintf(`{"contexts":{"trace":{"op":%q}}}`, fixture.op))
		ev := event.Event{ID: fixture.id, Type: fixture.kind, Transaction: "GET /checkout", Title: "GET /checkout", Level: "info", Timestamp: fixture.at, Duration: 9000, Fingerprint: "separate", Data: data}
		if _, _, err := db.Ingest(ctx, fixture.project, ev); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := db.Transactions(ctx, user.ID, org.Slug, project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("transaction operations mixed or events leaked: %+v", groups)
	}
	var main *TransactionGroup
	for i := range groups {
		if groups[i].Op == "http.server" {
			main = &groups[i]
		}
	}
	if main == nil || main.Transaction != "GET /checkout" || main.Count != 5 || main.ErrorCount != 1 || math.Abs(main.AvgDuration-400) > 0.001 || math.Abs(main.P50-300) > 0.001 || math.Abs(main.P95-880) > 0.001 || main.LatestTraceID != "00000000000000000000000000000005" {
		t.Fatalf("wrong persisted aggregate: %+v", main)
	}
	var latestID string
	if err := db.pool.QueryRow(ctx, `SELECT id::text FROM events WHERE project_id=$1 AND event_id='00000000000000000000000000000005'`, project.ID).Scan(&latestID); err != nil {
		t.Fatal(err)
	}
	if main.LatestSample != (TransactionReference{Source: "event", ID: latestID, ProjectID: project.ID}) {
		t.Fatalf("aggregate cannot open its latest stored transaction: %+v", main.LatestSample)
	}
	if _, err := db.Transactions(ctx, "999999999", org.Slug, project.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized transaction read: %v", err)
	}
	historical := &TimeWindow{Start: stamp.Add(-25 * time.Hour), End: stamp.Add(-25*time.Hour + time.Second)}
	groups, err = db.Transactions(ctx, user.ID, org.Slug, project.ID, historical)
	if err != nil || len(groups) != 1 || groups[0].Count != 1 || groups[0].AvgDuration != 9000 {
		t.Fatalf("explicit historical window retained the default 24-hour restriction: %+v, %v", groups, err)
	}
	groups, err = db.Transactions(ctx, user.ID, org.Slug, "", historical)
	if err != nil || len(groups) != 1 || groups[0].Count != 2 {
		t.Fatalf("organization-wide window leaked another tenant or omitted sibling project: %+v, %v", groups, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT id::text FROM events WHERE project_id=$1 AND event_id='00000000000000000000000000000015'`, other.ID).Scan(&latestID); err != nil {
		t.Fatal(err)
	}
	if groups[0].LatestSample != (TransactionReference{Source: "event", ID: latestID, ProjectID: other.ID}) {
		t.Fatalf("equal-time aggregate lost its selected sibling-project identity: %+v", groups[0].LatestSample)
	}
	if groups, err := db.Transactions(ctx, user.ID, org.Slug, foreignProject, historical); err != nil || len(groups) != 0 {
		t.Fatalf("foreign project escaped transaction scope: %+v, %v", groups, err)
	}
	if _, err := db.Transactions(ctx, user.ID, "foreign", foreignProject, historical); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign organization transaction read: %v", err)
	}
}

func TestLogsFilterHalfOpenTimeWindowExactServiceAndScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other project", "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg, foreignProject string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'foreign','Foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2025, 1, 2, 3, 4, 5, 123456000, time.UTC)
	window := &TimeWindow{Start: start, End: start.Add(time.Hour)}
	const trace = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, fixture := range []struct {
		body, project, service, environment, level, trace string
		at                                                time.Time
	}{
		{"checkout before", project.ID, "payments", "production", "error", trace, start.Add(-time.Microsecond)},
		{"checkout start", project.ID, "payments", "production", "error", trace, start},
		{"checkout inside", project.ID, "payments", "production", "error", trace, start.Add(time.Minute)},
		{"checkout end", project.ID, "payments", "production", "error", trace, window.End},
		{"checkout service", project.ID, "payments-api", "production", "error", trace, start.Add(time.Minute)},
		{"checkout environment", project.ID, "payments", "staging", "error", trace, start.Add(time.Minute)},
		{"checkout level", project.ID, "payments", "production", "info", trace, start.Add(time.Minute)},
		{"checkout trace", project.ID, "payments", "production", "error", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", start.Add(time.Minute)},
		{"unrelated body", project.ID, "payments", "production", "error", trace, start.Add(time.Minute)},
		{"checkout other project", other.ID, "payments", "production", "error", trace, start.Add(time.Minute)},
		{"checkout foreign tenant", foreignProject, "payments", "production", "error", trace, start.Add(time.Minute)},
	} {
		raw, err := json.Marshal(map[string]any{"items": []any{map[string]any{
			"timestamp": fixture.at.Format(time.RFC3339Nano), "level": fixture.level, "body": fixture.body, "trace_id": fixture.trace,
			"attributes": map[string]any{
				"service.name":       map[string]any{"type": "string", "value": fixture.service},
				"sentry.environment": map[string]any{"type": "string", "value": fixture.environment},
				"exact":              map[string]any{"type": "integer", "value": int64(9223372036854775807)},
			},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if n, err := db.IngestLogs(ctx, fixture.project, raw); err != nil || n != 1 {
			t.Fatalf("ingest %s: n=%d, %v", fixture.body, n, err)
		}
	}
	filter := LogFilter{Project: project.ID, Query: "checkout", Level: "error", Environment: "production", TraceID: trace, Service: "payments", Window: window}
	logs, err := db.Logs(ctx, user.ID, org.Slug, filter)
	if err != nil || len(logs) != 2 || logs[0].Body != "checkout inside" || logs[1].Body != "checkout start" || !logs[1].Timestamp.Equal(start) {
		t.Fatalf("combined filters or half-open boundaries: %+v, %v", logs, err)
	}
	if !strings.Contains(string(logs[0].Attributes), "9223372036854775807") {
		t.Fatalf("filtered log lost exact typed integer: %s", logs[0].Attributes)
	}
	filter.Window = nil
	logs, err = db.Logs(ctx, user.ID, org.Slug, filter)
	if err != nil || len(logs) != 4 || logs[0].Body != "checkout end" || logs[3].Body != "checkout before" {
		t.Fatalf("nil window changed all-time log default: %+v, %v", logs, err)
	}
	filter.Window, filter.Project = window, ""
	logs, err = db.Logs(ctx, user.ID, org.Slug, filter)
	if err != nil || len(logs) != 3 {
		t.Fatalf("organization-wide window leaked another tenant or omitted sibling project: %+v, %v", logs, err)
	}
	filter.Project = foreignProject
	if logs, err := db.Logs(ctx, user.ID, org.Slug, filter); err != nil || len(logs) != 0 {
		t.Fatalf("foreign project escaped organization scope: %+v, %v", logs, err)
	}
	if _, err := db.Logs(ctx, user.ID, "foreign", filter); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign organization read: %v", err)
	}
}

func TestLogsPreserveTypedContextDeduplicateAndValidateAtomically(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	const payload = `{"items":[{"timestamp":1791981000.25,"trace_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","span_id":"1111111111111111","level":"error","body":"checkout declined <script>","attributes":{"service.name":{"type":"string","value":"payments"},"sentry.environment":{"type":"string","value":"production"},"retry":{"type":"boolean","value":true},"attempt":{"type":"integer","value":3},"latency":{"type":"double","value":1.5}},"unknown":{"kept":[1,2]}}]}`
	if n, err := db.IngestLogs(ctx, project.ID, json.RawMessage(payload)); err != nil || n != 1 {
		t.Fatalf("ingest logs: n=%d err=%v", n, err)
	}
	if n, err := db.IngestLogs(ctx, project.ID, json.RawMessage(" \n"+payload)); err != nil || n != 0 {
		t.Fatalf("retry duplicated logs: n=%d err=%v", n, err)
	}
	var reordered any
	if err := json.Unmarshal([]byte(payload), &reordered); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.IngestLogs(ctx, project.ID, canonical); err != nil || n != 0 {
		t.Fatalf("object key reordering duplicated a log: n=%d err=%v", n, err)
	}
	if n, err := db.IngestLogs(ctx, project.ID, json.RawMessage(`{"items":[{"timestamp":1791981001,"level":"info","body":"healthy","attributes":{"environment":{"type":"string","value":"staging"}}}]}`)); err != nil || n != 1 {
		t.Fatalf("second log ingest: n=%d err=%v", n, err)
	}
	logs, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{Project: project.ID, Query: "declined", Level: "error", Environment: "production", TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("filtered logs: %+v, %v", logs, err)
	}
	got := logs[0]
	if got.ProjectID != project.ID || got.Body != "checkout declined <script>" || got.Service != "payments" || got.Environment != "production" || got.SpanID != "1111111111111111" || !got.Timestamp.Equal(time.Unix(1791981000, 250000000)) {
		t.Fatalf("log metadata lost: %+v", got)
	}
	var attrs map[string]struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(got.Attributes, &attrs); err != nil || attrs["retry"].Type != "boolean" || string(attrs["retry"].Value) != "true" || attrs["attempt"].Type != "integer" || string(attrs["attempt"].Value) != "3" {
		t.Fatalf("typed attributes lost: %s, %v", got.Attributes, err)
	}
	var raw json.RawMessage
	if err := db.pool.QueryRow(ctx, "SELECT data FROM logs WHERE id::text=$1", got.ID).Scan(&raw); err != nil || !strings.Contains(string(raw), `"unknown"`) {
		t.Fatalf("raw SDK context lost: %s, %v", raw, err)
	}
	for _, bad := range []string{
		`{"items":[{"timestamp":1791981002,"level":"info","body":"must rollback"},{"timestamp":"broken","level":"error","body":"bad"}]}`,
		`{"items":[{"timestamp":1791981002,"level":"info","body":"must rollback"},{"timestamp":1791981003,"level":"error","body":42}]}`,
		`{"items":[{"timestamp":null,"level":"info","body":"bad"}]}`,
		`{"items":[` + strings.TrimSuffix(strings.Repeat(`{"timestamp":1791981003,"level":"info","body":"oversized"},`, 101), ",") + `]}`,
	} {
		if _, err := db.IngestLogs(ctx, project.ID, json.RawMessage(bad)); err == nil {
			t.Fatalf("accepted malformed log batch: %s", bad)
		}
	}
	all, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("invalid batch partly persisted: %+v, %v", all, err)
	}
	pageItems := make([]map[string]any, 26)
	for i := range pageItems {
		pageItems[i] = map[string]any{"timestamp": 1791981100 + i, "level": "info", "body": fmt.Sprintf("pagination %02d", i)}
	}
	pagePayload, err := json.Marshal(map[string]any{"items": pageItems})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.IngestLogs(ctx, project.ID, pagePayload); err != nil || n != 26 {
		t.Fatalf("pagination fixtures: n=%d err=%v", n, err)
	}
	first, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{Query: "pagination", Page: 1})
	if err != nil || len(first) != 25 || first[0].Body != "pagination 25" {
		t.Fatalf("first log page: %+v, %v", first, err)
	}
	second, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{Query: "pagination", Page: 2})
	if err != nil || len(second) != 1 || second[0].Body != "pagination 00" {
		t.Fatalf("second log page: %+v, %v", second, err)
	}
	if _, err := db.Logs(ctx, "999999999", org.Slug, LogFilter{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized log read: %v", err)
	}
}
