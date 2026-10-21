//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/otlp"
)

func TestMCPLogInvestigation(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{MCP: true, Logs: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20, OTLPMaxRecords: 100, OTLPMaxNodes: 10000}
	srv := httptest.NewServer(New(cfg, f.db))
	t.Cleanup(srv.Close)
	defaultServer := f.server
	f.server = srv
	credential := issueManagementToken(t, f, f.org.Slug, "MCP logs", []string{"log:read"}, []string{f.project.ID})
	client := mcpClient(t, f, srv, credential, "2026-07-28")
	stamp := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	trace := strings.Repeat("ab", 16)
	for i := range 3 {
		payload := fmt.Sprintf(`{"items":[{"timestamp":%d,"level":"info","body":"native-%d","trace_id":%q,"attributes":{"service.name":{"type":"string","value":"mcp-sentry"},"environment":{"type":"string","value":"staging"},"ready":{"type":"boolean","value":true}}}]}`, stamp.Add(time.Duration(i)*time.Second).Unix(), i, strings.ToUpper(trace))
		if n, err := f.db.IngestLogs(f.ctx, f.project.ID, json.RawMessage(payload)); err != nil || n != 1 {
			t.Fatalf("native logs setup: %d %v", n, err)
		}
	}
	payload := fmt.Sprintf(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"mcp-otlp"}},{"key":"deployment.environment.name","value":{"stringValue":"production"}}]},"scopeLogs":[{"scope":{"name":"mcp-log-fixture"},"logRecords":[{"timeUnixNano":"%d","severityNumber":9,"traceId":%q,"body":{"stringValue":"OTLP selected"},"attributes":[{"key":"exact","value":{"intValue":"9223372036854775807"}}]},{"timeUnixNano":"%d","severityNumber":9,"traceId":%q,"body":{"stringValue":%q}}]}]}]}`, stamp.Add(3*time.Second).UnixNano(), trace, stamp.Add(4*time.Second).UnixNano(), trace, "MCP sibling sentinel"+strings.Repeat("x", 4096))
	batch, err := otlp.Prepare(strings.NewReader(payload), otlp.JSON, "", otlp.Limits{MaxBytes: 1 << 20, MaxNodes: 10000, MaxRecords: 100}, stamp.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := f.db.IngestOTLP(f.ctx, f.project.ID, batch); err != nil || n != 2 {
		t.Fatalf("OTLP setup: %d %v", n, err)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Hidden log project", "go")
	if err != nil {
		t.Fatal(err)
	}
	hiddenPayload := fmt.Sprintf(`{"items":[{"timestamp":%d,"trace_id":%q,"level":"error","body":"excluded log"}]}`, stamp.Add(5*time.Second).Unix(), trace)
	if _, err := f.db.IngestLogs(f.ctx, other.ID, json.RawMessage(hiddenPayload)); err != nil {
		t.Fatal(err)
	}
	var hiddenID string
	if err := f.sql.QueryRow(f.ctx, `SELECT id::text FROM logs WHERE project_id=$1::bigint`, other.ID).Scan(&hiddenID); err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(name,slug) VALUES('Foreign log workspace','foreign-log-workspace') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, foreignOrg); err != nil {
		t.Fatal(err)
	}
	foreign, err := f.db.CreateProject(f.ctx, f.owner.ID, "foreign-log-workspace", "Foreign logs", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.IngestLogs(f.ctx, foreign.ID, json.RawMessage(`{"items":[{"timestamp":1791981000,"level":"error","body":"foreign log"}]}`)); err != nil {
		t.Fatal(err)
	}
	var foreignID string
	if err := f.sql.QueryRow(f.ctx, `SELECT id::text FROM logs WHERE project_id=$1::bigint`, foreign.ID).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	var selectedID, nativeID string
	for page := 1; page <= 3; page++ {
		result := mcpWorkspaceCall(t, f, client, "list_logs", map[string]any{"organization": f.org.ID, "page": page, "limit": 2, "trace_id": strings.ToUpper(trace)})
		if result.HasMore != (page < 3) || len(result.Data) != map[int]int{1: 2, 2: 2, 3: 1}[page] {
			t.Fatalf("incorrect log page: %+v", result)
		}
		for _, row := range result.Data {
			id := row["id"].(string)
			if ids[id] || row["projectId"] != f.project.ID {
				t.Fatalf("log page escaped or duplicated: %+v", row)
			}
			ids[id] = true
			if row["body"] == "OTLP selected" {
				selectedID = id
			}
			if row["body"] == "native-0" {
				nativeID = id
			}
		}
	}
	if selectedID == "" || nativeID == "" {
		t.Fatal("missing actual log occurrences")
	}
	detail := mcpObject(t, f, client, "get_log", map[string]any{"log_id": selectedID})
	data, _ := json.Marshal(detail["data"])
	if detail["source"] != "otlp" || detail["body"] != "OTLP selected" || !strings.Contains(string(data), `"9223372036854775807"`) || strings.Contains(string(data), "MCP sibling sentinel") {
		t.Fatalf("selected canonical OTLP data differs: %+v", detail)
	}
	native := mcpObject(t, f, client, "get_log", map[string]any{"log_id": nativeID})
	if native["source"] != "sentry" || native["data"].(map[string]any)["body"] != "native-0" {
		t.Fatalf("native retained JSON changed: %+v", native)
	}
	filtered := mcpWorkspaceCall(t, f, client, "list_logs", map[string]any{"organization": f.org.Slug, "project": f.project.Slug, "query": "native-%", "service": "mcp-sentry", "environment": "staging", "level": "info", "start": stamp.Format(time.RFC3339Nano), "end": stamp.Add(2 * time.Second).Format(time.RFC3339Nano)})
	if len(filtered.Data) != 2 || filtered.Data[0]["body"] != "native-1" || filtered.Data[1]["body"] != "native-0" {
		t.Fatalf("native filters or half-open range changed: %+v", filtered)
	}
	mcpExpectIssueError(t, f, client, "get_log", map[string]any{"log_id": hiddenID})
	mcpExpectIssueError(t, f, client, "get_log", map[string]any{"log_id": foreignID})
	mcpExpectIssueError(t, f, client, "list_logs", map[string]any{"organization": f.org.ID, "project": other.ID})
	mcpExpectIssueError(t, f, client, "get_issue", map[string]any{"issue_id": f.issueID})
	for _, args := range []map[string]any{{"organization": "foreign"}, {"organization": f.org.ID, "trace_id": "not-a-trace"}, {"organization": f.org.ID, "start": stamp.Format(time.RFC3339)}, {"organization": f.org.ID, "query": nil}, {"organization": f.org.ID, "service": "bad\x00service"}, {"organization": f.org.ID, "unknown": true}} {
		mcpExpectIssueError(t, f, client, "list_logs", args)
	}
	mcpExpectIssueError(t, f, client, "get_log", map[string]any{"log_id": "01"})
	prior := issueManagementToken(t, f, f.org.Slug, "MCP prior scopes", []string{"org:read", "project:read", "event:read", "event:write"}, []string{f.project.ID})
	priorClient := mcpClient(t, f, srv, prior, "2026-07-28")
	mcpExpectIssueError(t, f, priorClient, "get_log", map[string]any{"log_id": nativeID})
	mcpExpectIssueError(t, f, priorClient, "list_logs", map[string]any{"organization": f.org.ID})
	allToken := issueManagementToken(t, f, f.org.Slug, "MCP all log projects", []string{"log:read"}, []string{})
	all := mcpClient(t, f, srv, allToken, "2026-07-28")
	allRows := mcpWorkspaceCall(t, f, all, "list_logs", map[string]any{"organization": f.org.Slug})
	if len(allRows.Data) != 6 {
		t.Fatalf("all-project grant escaped its organization: %+v", allRows)
	}
	mcpExpectIssueError(t, f, all, "get_log", map[string]any{"log_id": foreignID})
	// The same native token lifecycle must refuse unavailable log authority.
	logsOnlyConfig := cfg
	logsOnlyConfig.MCP = false
	logsOnly := httptest.NewServer(New(logsOnlyConfig, f.db))
	t.Cleanup(logsOnly.Close)
	for _, target := range []*httptest.Server{defaultServer, logsOnly, mcpHTTPServer(t, f, 1<<20)} {
		f.server = target
		f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", `{"name":"Unavailable log scope","scopes":["log:read"],"projectIds":[],"expiresInDays":1}`, http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	}
	disabled := mcpClient(t, f, f.server, credential, "2026-07-28")
	tools, err := disabled.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "list_logs" || tool.Name == "get_log" {
			t.Fatal("logs-disabled handler advertised a log tool")
		}
	}
	byteLimited, nodeLimited := cfg, cfg
	byteLimited.MaxBodyBytes = 1024
	nodeLimited.OTLPMaxNodes = 1
	for _, reduced := range []config.Config{byteLimited, nodeLimited} {
		limited := httptest.NewServer(New(reduced, f.db))
		limitedClient := mcpClient(t, f, limited, credential, "2026-07-28")
		mcpExpectIssueError(t, f, limitedClient, "get_log", map[string]any{"log_id": selectedID})
		limited.Close()
	}
}
