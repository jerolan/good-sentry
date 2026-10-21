//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/otlp"
)

// This real-database boundary owns the published log projection: filter
// containment, keyset paging, resource aggregation, hourly statistics and the
// host projection added by migration 054.
type compatibleLogFixture struct {
	ctx     context.Context
	db      *Store
	sql     *pgx.Conn
	owner   User
	org     Organization
	project Project
	foreign Project

	base time.Time
}

func newCompatibleLogFixture(t *testing.T) *compatibleLogFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	schema := fmt.Sprintf("goodsentry_compat_logs_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sql, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sql.Close(context.Background()) })
	owner, org, err := db.Bootstrap(ctx, "owner@compat-logs.test", []byte("hashed"), "Log owner", "Compat logs")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "Log project", "go")
	if err != nil {
		t.Fatal(err)
	}
	// Bootstrap owns the first-owner invariant, so the second organization is
	// created directly to keep the isolation fixture independent of it.
	var foreignOrgID string
	if err = sql.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-logs','Foreign logs') RETURNING id::text").Scan(&foreignOrgID); err != nil {
		t.Fatal(err)
	}
	var foreignProjectID string
	if err = sql.QueryRow(ctx, "INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Foreign project','foreign-project','go') RETURNING id::text", foreignOrgID).Scan(&foreignProjectID); err != nil {
		t.Fatal(err)
	}
	foreign := Project{ID: foreignProjectID, Name: "Foreign project", Slug: "foreign-project", Platform: "go"}
	return &compatibleLogFixture{ctx: ctx, db: db, sql: sql, owner: owner, org: org, project: project, foreign: foreign, base: time.Now().UTC().Truncate(time.Second)}
}

func (f *compatibleLogFixture) access() ManagementAccess {
	return ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true, ReadOnly: true}
}

// ingest writes one Sentry log batch whose attributes carry service,
// environment and host.
func (f *compatibleLogFixture) ingest(t *testing.T, items ...map[string]any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	count, err := f.db.IngestLogs(f.ctx, f.project.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(items) {
		t.Fatalf("ingested %d of %d logs", count, len(items))
	}
}

func (f *compatibleLogFixture) item(body, level, service, environment, host string, at time.Time) map[string]any {
	attributes := map[string]any{}
	for name, value := range map[string]string{"service.name": service, "environment": environment, "host.name": host} {
		if value != "" {
			attributes[name] = map[string]any{"type": "string", "value": value}
		}
	}
	return map[string]any{
		"timestamp":  at.Format(time.RFC3339Nano),
		"level":      level,
		"body":       body,
		"attributes": attributes,
	}
}

func TestCompatibleLogsFiltersPagingAndContainment(t *testing.T) {
	f := newCompatibleLogFixture(t)
	f.ingest(t,
		f.item("boot complete", "info", "api", "production", "web-1", f.base.Add(-4*time.Hour)),
		f.item("slow query", "warn", "api", "production", "web-1", f.base.Add(-3*time.Hour)),
		f.item("timeout", "error", "worker", "production", "worker-1", f.base.Add(-2*time.Hour)),
		f.item("retry", "warning", "worker", "staging", "worker-2", f.base.Add(-time.Hour)),
	)
	access := f.access()

	page, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 4 || page[0].Body != "retry" || page[3].Body != "boot complete" {
		t.Fatalf("page=%+v", page)
	}
	if page[0].Level != "warning" || page[1].Level != "error" {
		t.Fatalf("published levels=%q %q", page[0].Level, page[1].Level)
	}
	if page[0].Host != "worker-2" || page[0].Service != "worker" || page[0].Environment != "staging" || page[0].ProjectID != f.project.ID {
		t.Fatalf("projection=%+v", page[0])
	}

	// A published "warning" filter must match the stored OpenTelemetry spelling.
	warnings, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Levels: []string{"warning"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings=%d", len(warnings))
	}
	services, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Service: "api", Limit: 10})
	if err != nil || len(services) != 2 {
		t.Fatalf("service filter=%d err=%v", len(services), err)
	}
	hosts, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Host: "worker-1", Limit: 10})
	if err != nil || len(hosts) != 1 || hosts[0].Body != "timeout" {
		t.Fatalf("host filter=%+v err=%v", hosts, err)
	}
	searched, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Query: "quer", Limit: 10})
	if err != nil || len(searched) != 1 || searched[0].Body != "slow query" {
		t.Fatalf("search=%+v err=%v", searched, err)
	}
	// The window is half open: it keeps the two logs at -3h and -2h and excludes
	// the log exactly at its end boundary.
	window, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{
		Window: &TimeWindow{Start: f.base.Add(-3*time.Hour - time.Minute), End: f.base.Add(-time.Hour)}, Limit: 10})
	if err != nil || len(window) != 2 || window[0].Body != "timeout" || window[1].Body != "slow query" {
		t.Fatalf("window=%+v err=%v", window, err)
	}
	if _, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{ProjectIDs: []int64{0}, Limit: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid project=%v", err)
	}
	if _, err = f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Limit: 0}); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatalf("zero limit=%v", err)
	}
	foreignID, err := parsePositiveInt64(f.foreign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{ProjectIDs: []int64{foreignID}, Limit: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project=%v", err)
	}

	// Keyset paging walks the retained history once.
	seen := map[string]bool{}
	cursor := CompatibleLogCursor{}
	for page := 0; page < 4; page++ {
		items, err := f.db.CompatibleLogs(f.ctx, access, CompatibleLogFilter{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			break
		}
		if seen[items[0].ID] {
			t.Fatalf("log %s was returned twice", items[0].ID)
		}
		seen[items[0].ID] = true
		next, ok := CompatibleLogCursorFrom(items)
		if !ok {
			t.Fatal("cursor was not produced")
		}
		cursor = next
	}
	if len(seen) != 4 {
		t.Fatalf("paged %d distinct logs", len(seen))
	}

	// The same identity filter cannot reach another organization's row.
	foreignItem := f.item("foreign", "error", "api", "production", "web-1", f.base)
	payload, err := json.Marshal(map[string]any{"items": []map[string]any{foreignItem}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.IngestLogs(f.ctx, f.foreign.ID, payload); err != nil {
		t.Fatal(err)
	}
	var foreignLogID string
	if err = f.sql.QueryRow(f.ctx, "SELECT id::text FROM logs WHERE project_id=$1::bigint", f.foreign.ID).Scan(&foreignLogID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.CompatibleLog(f.ctx, access, foreignLogID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign log detail=%v", err)
	}
	if _, err = f.db.CompatibleLog(f.ctx, access, page[0].ID); err != nil {
		t.Fatalf("authorized detail=%v", err)
	}

	// A restricted token that lacks the project cannot read its rows.
	restricted := ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: false, ProjectIDs: []int64{foreignID}, ReadOnly: true}
	if _, err = f.db.CompatibleLogs(f.ctx, restricted, CompatibleLogFilter{ProjectIDs: []int64{foreignID}, Limit: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restricted grant=%v", err)
	}
}

func TestCompatibleLogResourcesAndStats(t *testing.T) {
	f := newCompatibleLogFixture(t)
	f.ingest(t,
		f.item("one", "error", "api", "production", "web-1", f.base.Add(-3*time.Hour)),
		f.item("two", "error", "api", "staging", "web-2", f.base.Add(-2*time.Hour)),
		f.item("three", "info", "worker", "production", "web-1", f.base.Add(-90*time.Minute)),
	)
	access := f.access()
	resources, err := f.db.CompatibleLogResources(f.ctx, access, "")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	newest := map[string]string{}
	for _, resource := range resources {
		counts[resource.Type]++
		newest[resource.Type+"/"+resource.Name] = resource.LastSeen.UTC().Format(time.RFC3339)
	}
	if counts["service"] != 2 || counts["environment"] != 2 || counts["host"] != 2 {
		t.Fatalf("resources=%+v counts=%v", resources, counts)
	}
	if resources[0].LastSeen.Before(resources[len(resources)-1].LastSeen) {
		t.Fatalf("resources are not ordered by last use: %+v", resources)
	}
	hosts, err := f.db.CompatibleLogResources(f.ctx, access, "host")
	if err != nil || len(hosts) != 2 {
		t.Fatalf("host resources=%+v err=%v", hosts, err)
	}
	for _, host := range hosts {
		if host.Type != "host" {
			t.Fatalf("filtered resource=%+v", host)
		}
	}
	if _, err = f.db.CompatibleLogResources(f.ctx, access, "session"); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatalf("unknown resource type=%v", err)
	}

	stats, err := f.db.CompatibleLogStats(f.ctx, access, CompatibleLogFilter{
		Window: &TimeWindow{Start: f.base.Add(-4 * time.Hour), End: f.base}, Levels: []string{"error"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("statistics=%+v", stats)
	}
	for _, bucket := range stats {
		if bucket.Level != "error" || bucket.Count != 1 || bucket.Interval.Minute() != 0 || bucket.Interval.Second() != 0 {
			t.Fatalf("bucket=%+v", bucket)
		}
	}
	if _, err = f.db.CompatibleLogStats(f.ctx, access, CompatibleLogFilter{Window: &TimeWindow{Start: f.base.Add(-91 * 24 * time.Hour), End: f.base}}); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatalf("oversized statistics window=%v", err)
	}
}

func TestCompatibleLogsProjectHostFromOTLPResources(t *testing.T) {
	f := newCompatibleLogFixture(t)
	payload, err := json.Marshal(map[string]any{
		"resourceLogs": []any{map[string]any{
			"resource": map[string]any{"attributes": []any{
				map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "otlp-api"}},
				map[string]any{"key": "host.name", "value": map[string]any{"stringValue": "otlp-host"}},
				map[string]any{"key": "deployment.environment", "value": map[string]any{"stringValue": "production"}},
			}},
			"scopeLogs": []any{map[string]any{"logRecords": []any{map[string]any{
				"timeUnixNano":   fmt.Sprintf("%d", f.base.UnixNano()),
				"severityNumber": 17,
				"body":           map[string]any{"stringValue": "otlp failure"},
			}}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := otlp.Prepare(strings.NewReader(string(payload)), otlp.JSON, "", otlp.Limits{MaxBytes: 1 << 20, MaxRecords: 100, MaxNodes: 10000}, f.base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.IngestOTLP(f.ctx, f.project.ID, batch); err != nil {
		t.Fatal(err)
	}
	page, err := f.db.CompatibleLogs(f.ctx, f.access(), CompatibleLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Host != "otlp-host" || page[0].Service != "otlp-api" || page[0].Level != "error" || page[0].Source != "otlp" {
		t.Fatalf("otlp projection=%+v", page)
	}
	resources, err := f.db.CompatibleLogResources(f.ctx, f.access(), "host")
	if err != nil || len(resources) != 1 || resources[0].Name != "otlp-host" {
		t.Fatalf("otlp host resources=%+v err=%v", resources, err)
	}
}
