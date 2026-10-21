//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"goodsentry/internal/config"
	"goodsentry/internal/event"
)

func mcpPerformanceTransaction(t *testing.T, f *resolutionHTTPFixture, project string, eventNumber, traceNumber int, transaction, op string, at time.Time, duration float64, children int) string {
	t.Helper()
	spans := make([]map[string]any, 0, children)
	for i := range children {
		spans = append(spans, map[string]any{
			"span_id": fmt.Sprintf("%016x", i+1), "parent_span_id": strings.Repeat("a", 16),
			"op": "db.query", "description": "SELECT mcp_performance",
			"start_timestamp": float64(at.Unix()) + 0.01, "timestamp": float64(at.Unix()) + 0.02,
		})
	}
	payload := map[string]any{
		"type": "transaction", "event_id": fmt.Sprintf("%032x", eventNumber), "transaction": transaction,
		"start_timestamp": float64(at.Unix()), "timestamp": float64(at.Unix()) + duration/1000,
		"contexts": map[string]any{"trace": map[string]any{"trace_id": fmt.Sprintf("%032x", traceNumber), "span_id": strings.Repeat("a", 16), "op": op, "status": "ok"}},
		"spans":    spans,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", at)
	if err != nil {
		t.Fatal(err)
	}
	ev.Duration = duration
	_, _, err = f.db.Ingest(f.ctx, project, ev)
	if err != nil {
		t.Fatal(err)
	}
	return ev.ID
}

func TestMCPPerformanceInvestigation(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{MCP: true, Logs: true, Uptime: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, f.db))
	t.Cleanup(srv.Close)
	f.server = srv
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Excluded performance", "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(name,slug) VALUES('Foreign performance','foreign-performance') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, foreignOrg); err != nil {
		t.Fatal(err)
	}
	foreign, err := f.db.CreateProject(f.ctx, f.owner.ID, "foreign-performance", "Foreign performance", "go")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 5; i++ {
		at := base.Add(time.Duration(i/2)*24*time.Hour + time.Duration(i)*time.Minute)
		mcpPerformanceTransaction(t, f, f.project.ID, 100+i, 200+i, "GET /mcp/performance", "http.server", at, float64(100+i*100), 6)
	}
	mcpPerformanceTransaction(t, f, f.project.ID, 110, 210, "GET /mcp/performance", "background", base.Add(time.Hour), 700, 2)
	mcpPerformanceTransaction(t, f, other.ID, 120, 220, "GET /mcp/performance", "http.server", base.Add(time.Hour), 900, 7)
	mcpPerformanceTransaction(t, f, foreign.ID, 121, 221, "GET /mcp/performance", "http.server", base.Add(time.Hour), 950, 8)
	token := issueManagementToken(t, f, f.org.Slug, "MCP performance selected", []string{"event:read"}, []string{f.project.ID})
	client := mcpClient(t, f, srv, token, "2026-07-28")
	tools, err := client.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_transaction_groups", "get_transaction_group", "list_transaction_spans", "list_span_groups", "detect_n_plus_one", "get_transaction_trend"} {
		var found *mcp.Tool
		for _, tool := range tools.Tools {
			if tool.Name == name {
				found = tool
			}
		}
		if found == nil || found.OutputSchema == nil || found.Annotations == nil || !found.Annotations.ReadOnlyHint {
			t.Fatalf("missing read-only schema for %s: %+v", name, found)
		}
	}
	start, end := base.Add(-time.Hour).Format(time.RFC3339), base.Add(72*time.Hour).Format(time.RFC3339)
	groups := mcpWorkspaceCall(t, f, client, "list_transaction_groups", map[string]any{"organization": f.org.ID, "start": start, "end": end, "limit": 1})
	if len(groups.Data) != 1 || groups.Data[0]["transaction"] != "GET /mcp/performance" || groups.Data[0]["op"] != "http.server" || groups.Data[0]["count"].(float64) != 5 || !groups.HasMore {
		t.Fatalf("wrong transaction group page: %+v", groups)
	}
	group := mcpObject(t, f, client, "get_transaction_group", map[string]any{"organization": f.org.Slug, "transaction": "GET /mcp/performance", "op": "http.server", "start": start, "end": end})
	sample := group["latestSample"].(map[string]any)
	if group["count"].(float64) != 5 || sample["projectId"] != f.project.ID || sample["source"] == "" {
		t.Fatalf("wrong selected transaction group/sample: %+v", group)
	}
	mcpExpectIssueError(t, f, client, "get_transaction_group", map[string]any{"organization": f.org.ID, "transaction": "GET /mcp/performance", "start": start, "end": end})
	spans := mcpObject(t, f, client, "list_transaction_spans", map[string]any{"organization": f.org.ID, "transaction": "GET /mcp/performance", "op": "http.server", "start": start, "end": end, "page": 1})
	if spans["transactionCount"].(float64) != 5 || spans["spanCount"].(float64) != 30 || len(spans["data"].([]any)) == 0 {
		t.Fatalf("span breakdown: %+v", spans)
	}
	spanGroups := mcpWorkspaceCall(t, f, client, "list_span_groups", map[string]any{"organization": f.org.ID, "op": "db.query", "query": "mcp_performance", "start": start, "end": end})
	if len(spanGroups.Data) != 1 || spanGroups.Data[0]["count"].(float64) != 32 || spanGroups.Data[0]["errorCount"].(float64) != 0 {
		t.Fatalf("cross-transaction span groups: %+v", spanGroups)
	}
	nplus := mcpWorkspaceCall(t, f, client, "detect_n_plus_one", map[string]any{"organization": f.org.ID, "threshold": 5, "start": start, "end": end})
	if len(nplus.Data) != 5 || nplus.Data[0]["count"].(float64) != 6 || nplus.Data[0]["transaction"] != "GET /mcp/performance" {
		t.Fatalf("N+1 findings: %+v", nplus)
	}
	trend := mcpObject(t, f, client, "get_transaction_trend", map[string]any{"organization": f.org.ID, "transaction": "GET /mcp/performance", "op": "http.server", "start": start, "end": end})
	if len(trend["data"].([]any)) != 3 {
		t.Fatalf("daily trend buckets: %+v", trend)
	}
	allToken := issueManagementToken(t, f, f.org.Slug, "MCP performance all", []string{"event:write"}, []string{})
	all := mcpClient(t, f, srv, allToken, "2026-07-28")
	allGroups := mcpWorkspaceCall(t, f, all, "list_transaction_groups", map[string]any{"organization": f.org.ID, "start": start, "end": end})
	if len(allGroups.Data) != 2 {
		t.Fatalf("all-project transaction groups: %+v", allGroups)
	}
	mcpExpectIssueError(t, f, client, "list_transaction_groups", map[string]any{"organization": f.org.ID, "project": other.ID, "start": start, "end": end})
	mcpExpectIssueError(t, f, client, "list_transaction_groups", map[string]any{"organization": "foreign-performance"})
	mcpExpectIssueError(t, f, client, "list_transaction_groups", map[string]any{"organization": f.org.ID, "start": start})
	mcpExpectIssueError(t, f, client, "detect_n_plus_one", map[string]any{"organization": f.org.ID, "threshold": 1})
	mcpExpectIssueError(t, f, client, "get_transaction_trend", map[string]any{"organization": f.org.ID, "transaction": "bad\x00transaction"})
	mcpExpectIssueError(t, f, client, "list_span_groups", map[string]any{"organization": f.org.ID, "unknown": true})
	noEvent := issueManagementToken(t, f, f.org.Slug, "MCP performance no event", []string{"project:read"}, []string{f.project.ID})
	noEventClient := mcpClient(t, f, srv, noEvent, "2026-07-28")
	mcpExpectIssueError(t, f, noEventClient, "list_transaction_groups", map[string]any{"organization": f.org.ID})
	if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("removed membership remained connected: %v", err)
	}
}
