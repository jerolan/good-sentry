//go:build integration

package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"goodsentry/internal/config"
	"goodsentry/internal/notify"
	"goodsentry/internal/uptime"
)

func mcpAlertFixture(t *testing.T, f *resolutionHTTPFixture, project, name, organization string) notify.Rule {
	t.Helper()
	c := notify.Defaults()
	c.Name = name
	c.Recipients = []notify.Recipient{{Type: "webhook", Destination: "https://private.example.test/" + name + "?token=never-return-this"}}
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, organization, project, c)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func mcpMonitorFixture(t *testing.T, f *resolutionHTTPFixture, project, name, organization string, kind uptime.Kind) uptime.Monitor {
	t.Helper()
	c := uptime.Defaults()
	c.Name, c.Type, c.Project, c.Enabled = name, kind, project, false
	c.URL = "https://target.example.test/health?private=never-return-this"
	m, err := f.db.CreateMonitor(f.ctx, f.owner.ID, organization, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMCPAlertAndMonitorInspection(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{MCP: true, Logs: true, Uptime: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, f.db))
	t.Cleanup(srv.Close)
	f.server = srv
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Excluded alerts", "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(name,slug) VALUES('Foreign alerts','foreign-alerts') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, foreignOrg); err != nil {
		t.Fatal(err)
	}
	foreign, err := f.db.CreateProject(f.ctx, f.owner.ID, "foreign-alerts", "Foreign alert project", "go")
	if err != nil {
		t.Fatal(err)
	}
	selectedAlerts := []notify.Rule{
		mcpAlertFixture(t, f, f.project.ID, "Selected alert A", f.org.Slug),
		mcpAlertFixture(t, f, f.project.ID, "Selected alert B", f.org.Slug),
		mcpAlertFixture(t, f, f.project.ID, "Selected alert C", f.org.Slug),
	}
	mcpAlertFixture(t, f, other.ID, "Excluded alert", f.org.Slug)
	mcpAlertFixture(t, f, foreign.ID, "Foreign alert", "foreign-alerts")
	monitors := []uptime.Monitor{
		mcpMonitorFixture(t, f, f.project.Slug, "Selected monitor", f.org.Slug, uptime.HEAD),
		mcpMonitorFixture(t, f, "", "Organization heartbeat", f.org.Slug, uptime.Heartbeat),
		mcpMonitorFixture(t, f, other.Slug, "Excluded monitor", f.org.Slug, uptime.GET),
		mcpMonitorFixture(t, f, foreign.Slug, "Foreign monitor", "foreign-alerts", uptime.GET),
	}
	alertToken := issueManagementToken(t, f, f.org.Slug, "MCP alert reader", []string{"project:read"}, []string{f.project.ID})
	monitorToken := issueManagementToken(t, f, f.org.Slug, "MCP monitor reader", []string{"org:read"}, []string{f.project.ID})
	alertClient := mcpClient(t, f, srv, alertToken, "2026-07-28")
	monitorClient := mcpClient(t, f, srv, monitorToken, "2026-07-28")
	tools, err := alertClient.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		if tool.Name == "list_alerts" || tool.Name == "list_monitors" {
			found[tool.Name] = tool
		}
	}
	for _, name := range []string{"list_alerts", "list_monitors"} {
		if found[name] == nil || found[name].Annotations == nil || !found[name].Annotations.ReadOnlyHint || found[name].OutputSchema == nil {
			t.Fatalf("missing read-only schema for %s: %+v", name, found[name])
		}
	}
	first := mcpWorkspaceCall(t, f, alertClient, "list_alerts", map[string]any{"organization": f.org.ID, "limit": 2})
	second := mcpWorkspaceCall(t, f, alertClient, "list_alerts", map[string]any{"organization": f.org.Slug, "limit": 2, "page": 2})
	if len(first.Data) != 2 || !first.HasMore || first.PageSize != 2 || len(second.Data) != 1 || second.HasMore || second.Page != 2 {
		t.Fatalf("alert pagination: first=%+v second=%+v", first, second)
	}
	seenAlerts := map[string]bool{}
	for _, row := range append(first.Data, second.Data...) {
		id := row["id"].(string)
		seenAlerts[id] = true
		if row["projectId"] != f.project.ID || row["recipientTypes"].([]any)[0] != "webhook" {
			t.Fatalf("wrong sanitized alert: %+v", row)
		}
		for _, forbidden := range []string{"recipients", "destination", "payload", "attempts"} {
			if _, present := row[forbidden]; present {
				t.Fatalf("alert exposed %s", forbidden)
			}
		}
	}
	if len(seenAlerts) != 3 || !seenAlerts[selectedAlerts[0].ID] {
		t.Fatalf("selected alert grant/paging: %+v", seenAlerts)
	}
	projectAlerts := mcpWorkspaceCall(t, f, alertClient, "list_alerts", map[string]any{"organization": f.org.ID, "project": f.project.Slug})
	if len(projectAlerts.Data) != 3 {
		t.Fatalf("alert project filter: %+v", projectAlerts)
	}
	monitorPage := mcpWorkspaceCall(t, f, monitorClient, "list_monitors", map[string]any{"organization": f.org.ID})
	if len(monitorPage.Data) != 2 {
		t.Fatalf("monitor grant/org-level inclusion: %+v", monitorPage)
	}
	seenMonitors := map[string]bool{}
	for _, row := range monitorPage.Data {
		id := row["id"].(string)
		seenMonitors[id] = true
		for _, forbidden := range []string{"heartbeatURL", "heartbeatKey", "leaseToken", "leaseExpiresAt"} {
			if _, present := row[forbidden]; present {
				t.Fatalf("monitor exposed %s", forbidden)
			}
		}
	}
	if !seenMonitors[monitors[0].ID] || !seenMonitors[monitors[1].ID] || seenMonitors[monitors[2].ID] {
		t.Fatalf("monitor selected/org-level projection: %+v", seenMonitors)
	}
	allAlertToken := issueManagementToken(t, f, f.org.Slug, "MCP all alerts", []string{"project:read"}, []string{})
	allAlerts := mcpClient(t, f, srv, allAlertToken, "2026-07-28")
	allAlertPage := mcpWorkspaceCall(t, f, allAlerts, "list_alerts", map[string]any{"organization": f.org.ID, "limit": 100})
	if len(allAlertPage.Data) != 4 {
		t.Fatalf("all-project alert organization boundary: %+v", allAlertPage)
	}
	allMonitorToken := issueManagementToken(t, f, f.org.Slug, "MCP all monitors", []string{"org:read"}, []string{})
	allMonitors := mcpClient(t, f, srv, allMonitorToken, "2026-07-28")
	allMonitorPage := mcpWorkspaceCall(t, f, allMonitors, "list_monitors", map[string]any{"organization": f.org.ID, "limit": 100})
	if len(allMonitorPage.Data) != 3 {
		t.Fatalf("all-project monitor organization boundary: %+v", allMonitorPage)
	}
	for _, check := range []struct {
		name   string
		client *mcp.ClientSession
	}{
		{"list_alerts", alertClient},
		{"list_monitors", monitorClient},
	} {
		for _, bad := range []map[string]any{{"organization": "foreign-alerts"}, {"organization": f.org.ID, "unknown": true}, {"organization": f.org.ID, "page": 0}, {"organization": f.org.ID, "limit": 101}, {"organization": "bad\x00org"}} {
			mcpExpectIssueError(t, f, check.client, check.name, bad)
		}
	}
	mcpExpectIssueError(t, f, monitorClient, "list_monitors", map[string]any{"organization": f.org.ID, "project": f.project.ID})
	mcpExpectIssueError(t, f, alertClient, "list_alerts", map[string]any{"organization": f.org.ID, "project": other.ID})
	mcpExpectIssueError(t, f, monitorClient, "list_alerts", map[string]any{"organization": f.org.ID})
	mcpExpectIssueError(t, f, alertClient, "list_monitors", map[string]any{"organization": f.org.ID})
	// Native alert reads require owner/admin; a member keeps org-level monitor read.
	if _, err := f.sql.Exec(f.ctx, `UPDATE memberships SET role='member' WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	mcpExpectIssueError(t, f, alertClient, "list_alerts", map[string]any{"organization": f.org.ID})
	_ = mcpWorkspaceCall(t, f, monitorClient, "list_monitors", map[string]any{"organization": f.org.ID})
	if _, err := f.sql.Exec(f.ctx, `UPDATE memberships SET role='owner' WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	noUptime := cfg
	noUptime.Uptime = false
	feature := httptest.NewServer(New(noUptime, f.db))
	defer feature.Close()
	featureClient := mcpClient(t, f, feature, monitorToken, "2026-07-28")
	listed, err := featureClient.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "list_monitors" {
			t.Fatal("uptime-disabled handler advertised list_monitors")
		}
	}
	// Revocation happens on the next request of the already connected client.
	if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := monitorClient.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("connected monitor client retained removed membership: %v", err)
	}
	_ = fmt.Sprint(selectedAlerts[0].ID)
}
