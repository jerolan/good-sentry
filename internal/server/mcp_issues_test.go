//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"goodsentry/internal/event"
	"goodsentry/internal/store"
)

func mcpObject(t *testing.T, f *resolutionHTTPFixture, client *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s failed: %+v %v", name, result, err)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		t.Fatalf("invalid tool object %s: %v", raw, err)
	}
	return object
}

func mcpExpectIssueError(t *testing.T, f *resolutionHTTPFixture, client *mcp.ClientSession, name string, args map[string]any) {
	t.Helper()
	result, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || !result.IsError || result.StructuredContent != nil {
		t.Fatalf("%s should return a tool error: %+v %v", name, result, err)
	}
}

func mcpCapture(t *testing.T, f *resolutionHTTPFixture, project, id, message string, fields map[string]any) string {
	t.Helper()
	payload := map[string]any{"event_id": id, "message": message, "release": "app@1.0"}
	for key, value := range fields {
		payload[key] = value
	}
	stamp := time.Now()
	value, err := event.Normalize(json.RawMessage(resolutionJSON(t, payload)), "", stamp)
	if err != nil {
		t.Fatal(err)
	}
	issue, fresh, err := f.db.Ingest(f.ctx, project, value)
	if err != nil || !fresh {
		t.Fatalf("capture issue=%s fresh=%v err=%v", issue, fresh, err)
	}
	return issue
}

func TestMCPIssueInvestigation(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	latestID := strings.Repeat("b", 32)
	if issue := mcpCapture(t, f, f.project.ID, latestID, "Resolution HTTP fixture", map[string]any{"timestamp": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)}); issue != f.issueID {
		t.Fatalf("fixture did not create a second occurrence: %s", issue)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Excluded MCP", "go")
	if err != nil {
		t.Fatal(err)
	}
	hidden := mcpCapture(t, f, other.ID, latestID, "Excluded MCP issue", nil)
	var foreignOrg string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(name,slug) VALUES('Foreign MCP','foreign-mcp') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, foreignOrg); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := f.db.CreateProject(f.ctx, f.owner.ID, "foreign-mcp", "Foreign project", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreignEvent := strings.Repeat("e", 32)
	foreign := mcpCapture(t, f, foreignProject.ID, foreignEvent, "Foreign MCP issue", nil)
	reader := issueManagementToken(t, f, f.org.Slug, "MCP issue reader", []string{"event:read"}, []string{f.project.ID})
	writer := issueManagementToken(t, f, f.org.Slug, "MCP issue writer", []string{"event:write"}, []string{f.project.ID})
	metadata := issueManagementToken(t, f, f.org.Slug, "MCP metadata only", []string{"project:read"}, []string{f.project.ID})
	srv := mcpHTTPServer(t, f, 1<<20)
	read := mcpClient(t, f, srv, reader, "2026-07-28")
	write := mcpClient(t, f, srv, writer, "2026-07-28")
	meta := mcpClient(t, f, srv, metadata, "2026-07-28")
	tools, err := read.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_issues", "get_issue", "get_latest_event", "get_event", "update_issue"} {
		var discovered *mcp.Tool
		for _, tool := range tools.Tools {
			if tool.Name == name {
				discovered = tool
			}
		}
		if discovered == nil || discovered.OutputSchema == nil || discovered.Annotations == nil || discovered.Annotations.ReadOnlyHint != (name != "update_issue") {
			t.Fatalf("missing schema/side-effect annotation: %s %+v", name, discovered)
		}
	}
	detail := mcpObject(t, f, read, "get_issue", map[string]any{"issue_id": f.issueID})
	if detail["id"] != f.issueID || detail["count"] != "2" || detail["status"] != "unresolved" {
		t.Fatalf("wrong retained issue: %+v", detail)
	}
	latest := mcpObject(t, f, read, "get_latest_event", map[string]any{"issue_id": f.issueID})
	if latest["eventID"] != latestID || latest["groupID"] != f.issueID || latest["message"] != "Resolution HTTP fixture" {
		t.Fatalf("latest occurrence: %+v", latest)
	}
	// A duplicate in an excluded project must neither leak nor make this lookup ambiguous.
	pair := mcpObject(t, f, read, "get_event", map[string]any{"event_id": "BBBBBBBB-BBBB-BBBB-BBBB-BBBBBBBBBBBB"})
	if pair["event"].(map[string]any)["groupID"] != f.issueID || pair["issue"].(map[string]any)["id"] != f.issueID {
		t.Fatalf("wrong event/parent association: %+v", pair)
	}
	page := mcpWorkspaceCall(t, f, read, "list_issues", map[string]any{"organization": f.org.ID, "query": "is:unresolved", "limit": 1})
	if len(page.Data) != 1 || page.Data[0]["id"] != f.issueID || page.HasMore {
		t.Fatalf("issue grant page: %+v", page)
	}
	var activityBefore int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM issue_activity WHERE issue_id=$1::bigint`, f.issueID).Scan(&activityBefore); err != nil {
		t.Fatal(err)
	}
	for _, client := range []*mcp.ClientSession{read, meta} {
		mcpExpectIssueError(t, f, client, "update_issue", map[string]any{"issue_id": f.issueID, "status": "resolved"})
	}
	for _, name := range []string{"get_issue", "get_latest_event"} {
		mcpExpectIssueError(t, f, read, name, map[string]any{"issue_id": hidden})
		mcpExpectIssueError(t, f, read, name, map[string]any{"issue_id": foreign})
		mcpExpectIssueError(t, f, meta, name, map[string]any{"issue_id": f.issueID})
	}
	for _, id := range []string{hidden, foreign} {
		mcpExpectIssueError(t, f, write, "update_issue", map[string]any{"issue_id": id, "status": "resolved"})
	}
	var activityAfter int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM issue_activity WHERE issue_id=$1::bigint`, f.issueID).Scan(&activityAfter); err != nil || activityAfter != activityBefore {
		t.Fatalf("denied write changed activity: %d -> %d: %v", activityBefore, activityAfter, err)
	}
	if state := mcpObject(t, f, write, "get_issue", map[string]any{"issue_id": f.issueID}); state["status"] != "unresolved" {
		t.Fatalf("denied write changed native state: %+v", state)
	}
	mcpExpectIssueError(t, f, read, "get_event", map[string]any{"event_id": foreignEvent})
	mcpExpectIssueError(t, f, meta, "get_event", map[string]any{"event_id": latestID})
	mcpExpectIssueError(t, f, meta, "list_issues", map[string]any{"organization": f.org.ID})
	mcpExpectIssueError(t, f, read, "list_issues", map[string]any{"organization": f.org.ID, "project": other.ID})
	mcpExpectIssueError(t, f, read, "get_event", map[string]any{"event_id": latestID, "project": other.ID})
	mcpExpectIssueError(t, f, read, "list_issues", map[string]any{"organization": "unknown"})
	mcpExpectIssueError(t, f, read, "list_issues", map[string]any{"organization": f.org.ID, "query": "is:all"})
	mcpExpectIssueError(t, f, read, "get_issue", map[string]any{"issue_id": "01"})
	mcpExpectIssueError(t, f, read, "get_event", map[string]any{"event_id": "latest"})
	mcpExpectIssueError(t, f, write, "update_issue", map[string]any{"issue_id": f.issueID, "status": "deleted"})
	mcpExpectIssueError(t, f, write, "update_issue", map[string]any{"issue_id": f.issueID, "status": "resolved", "assignedTo": f.owner.ID})
	f.request(t, f.client, "PUT", "/api/0/issues/"+f.issueID+"/", `{"status":"resolvedInNextRelease"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
	resolved := mcpObject(t, f, write, "update_issue", map[string]any{"issue_id": f.issueID, "status": "resolved"})
	if resolved["status"] != "resolved" || len(resolved["statusDetails"].(map[string]any)) != 0 {
		t.Fatalf("immediate resolve retained a conditional policy: %+v", resolved)
	}
	before, err := f.db.CompatibleIssue(f.ctx, store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}, f.issueID, store.CompatibleIssueDetailFilter{StatsPeriod: "24h"})
	if err != nil || before.Issue.Status != "resolved" {
		t.Fatalf("committed native state: %+v %v", before, err)
	}
	var count int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM issue_activity WHERE issue_id=$1::bigint`, f.issueID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	_ = mcpObject(t, f, write, "update_issue", map[string]any{"issue_id": f.issueID, "status": "resolved"})
	var after int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM issue_activity WHERE issue_id=$1::bigint`, f.issueID).Scan(&after); err != nil || after != count {
		t.Fatalf("repeat status duplicated activity: %d -> %d: %v", count, after, err)
	}
	active := mcpWorkspaceCall(t, f, read, "list_issues", map[string]any{"organization": f.org.Slug})
	if len(active.Data) != 0 {
		t.Fatalf("default unresolved filter retained resolved issue: %+v", active)
	}
	f.request(t, f.client, "PUT", "/api/0/issues/"+f.issueID+"/", `{"status":"ignored","ignoreDuration":60}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
	ignored := mcpObject(t, f, write, "update_issue", map[string]any{"issue_id": f.issueID, "status": "ignored"})
	if ignored["status"] != "ignored" || len(ignored["statusDetails"].(map[string]any)) != 0 {
		t.Fatalf("indefinite ignore retained a timer: %+v", ignored)
	}
	_ = mcpObject(t, f, write, "update_issue", map[string]any{"issue_id": f.issueID, "status": "unresolved"})
	allToken := issueManagementToken(t, f, f.org.Slug, "MCP all issue projects", []string{"event:read"}, []string{})
	all := mcpClient(t, f, srv, allToken, "2026-07-28")
	mcpExpectIssueError(t, f, all, "get_event", map[string]any{"event_id": latestID})
	_ = mcpObject(t, f, all, "get_event", map[string]any{"event_id": latestID, "project": f.project.Slug})
	transactionID := strings.Repeat("c", 32)
	mcpCapture(t, f, f.project.ID, transactionID, "MCP transaction", map[string]any{"type": "transaction", "transaction": "MCP transaction", "start_timestamp": float64(time.Now().Add(-time.Second).Unix()), "timestamp": float64(time.Now().Unix())})
	mcpExpectIssueError(t, f, read, "get_event", map[string]any{"event_id": transactionID})
	mcpExpectIssueError(t, f, write, "get_event", map[string]any{"event_id": transactionID})
	projectEvents := issueManagementToken(t, f, f.org.Slug, "MCP issue and project events", []string{"event:read", "project:read"}, []string{f.project.ID})
	projectRead := mcpClient(t, f, srv, projectEvents, "2026-07-28")
	transaction := mcpObject(t, f, projectRead, "get_event", map[string]any{"event_id": transactionID})
	if transaction["issue"] != nil || transaction["event"].(map[string]any)["type"] != "transaction" {
		t.Fatalf("transaction invented an issue: %+v", transaction)
	}
	secondID := strings.Repeat("d", 32)
	second := mcpCapture(t, f, f.project.ID, secondID, "MCP second investigation", map[string]any{"environment": "staging", "timestamp": time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano)})
	firstPage := mcpWorkspaceCall(t, f, read, "list_issues", map[string]any{"organization": f.org.Slug, "query": "", "limit": 1})
	secondPage := mcpWorkspaceCall(t, f, read, "list_issues", map[string]any{"organization": f.org.Slug, "query": "", "limit": 1, "page": 2})
	if len(firstPage.Data) != 1 || firstPage.Data[0]["id"] != second || !firstPage.HasMore || len(secondPage.Data) != 1 || secondPage.Data[0]["id"] != f.issueID || secondPage.HasMore {
		t.Fatalf("issue pages lost or duplicated roots: first=%+v second=%+v", firstPage, secondPage)
	}
	filtered := mcpWorkspaceCall(t, f, read, "list_issues", map[string]any{"organization": f.org.ID, "project": f.project.ID, "environment": "staging"})
	if len(filtered.Data) != 1 || filtered.Data[0]["id"] != second {
		t.Fatalf("environment/project filter failed: %+v", filtered)
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{f.issueID, second}); err != nil {
		t.Fatal(err)
	}
	alias := mcpObject(t, f, read, "get_issue", map[string]any{"issue_id": second})
	mergedLatest := mcpObject(t, f, read, "get_latest_event", map[string]any{"issue_id": second})
	mergedPair := mcpObject(t, f, read, "get_event", map[string]any{"event_id": secondID})
	if alias["id"] != f.issueID || alias["count"] != "3" || mergedLatest["eventID"] != secondID || mergedLatest["groupID"] != f.issueID || mergedPair["issue"].(map[string]any)["id"] != f.issueID {
		t.Fatalf("merged alias investigation: issue=%+v latest=%+v pair=%+v", alias, mergedLatest, mergedPair)
	}
}
