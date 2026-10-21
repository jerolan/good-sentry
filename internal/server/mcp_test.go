//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"goodsentry/internal/config"
)

type mcpBearerTransport struct{ token string }

func (a mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func mcpHTTPServer(t *testing.T, f *resolutionHTTPFixture, maximum int64) *httptest.Server {
	t.Helper()
	cfg := config.Config{MCP: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: maximum}
	srv := httptest.NewServer(New(cfg, f.db))
	t.Cleanup(srv.Close)
	return srv
}

func mcpClient(t *testing.T, f *resolutionHTTPFixture, srv *httptest.Server, token, protocol string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "GoodSentry acceptance", Version: "1"}, nil)
	session, err := client.Connect(f.ctx, &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/good/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{token}}, MaxRetries: -1, DisableStandaloneSSE: true,
	}, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if negotiated := session.InitializeResult().ProtocolVersion; negotiated != protocol {
		t.Fatalf("protocol silently changed: requested=%s negotiated=%s", protocol, negotiated)
	}
	return session
}

type mcpObservedWorkspacePage struct {
	Data     []map[string]any `json:"data"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
	HasMore  bool             `json:"hasMore"`
}

func mcpWorkspaceCall(t *testing.T, f *resolutionHTTPFixture, session *mcp.ClientSession, name string, args map[string]any) mcpObservedWorkspacePage {
	t.Helper()
	result, err := session.CallTool(f.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s: result=%+v error=%v", name, result, err)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var page mcpObservedWorkspacePage
	if err := json.Unmarshal(raw, &page); err != nil || page.Data == nil {
		t.Fatalf("invalid page %s: %v", raw, err)
	}
	// Check the actual projection, independently of the production DTO's tags.
	for _, row := range page.Data {
		for _, secret := range []string{"dsn", "publicKey", "role", "password", "email", "secret_hash"} {
			if _, exists := row[secret]; exists {
				t.Fatalf("%s exposed %s", name, secret)
			}
		}
	}
	return page
}

func TestMCPWorkspaceClient(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	second, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Second permitted", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Excluded project", "python"); err != nil {
		t.Fatal(err)
	}
	// The same user belongs to a different organization whose numeric slug
	// shadows the authorized organization's ID. Resolve within the grant first.
	var foreignID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(name,slug) VALUES('Foreign workspace',$1) RETURNING id::text`, f.org.ID).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, foreignID); err != nil {
		t.Fatal(err)
	}
	token := issueManagementToken(t, f, f.org.Slug, "MCP selected projects", []string{"org:read", "project:read"}, []string{f.project.ID, second.ID})
	srv := mcpHTTPServer(t, f, 1<<20)
	for _, protocol := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(protocol, func(t *testing.T) {
			client := mcpClient(t, f, srv, token, protocol)
			tools, err := client.ListTools(f.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"list_organizations", "list_projects"} {
				var discovered *mcp.Tool
				for _, tool := range tools.Tools {
					if tool.Name == name {
						discovered = tool
					}
				}
				if discovered == nil || discovered.InputSchema == nil || discovered.OutputSchema == nil || discovered.Annotations == nil || !discovered.Annotations.ReadOnlyHint || discovered.Annotations.OpenWorldHint == nil || *discovered.Annotations.OpenWorldHint {
					t.Fatalf("missing usable read-only schema for %s: %+v", name, discovered)
				}
			}
			orgs := mcpWorkspaceCall(t, f, client, "list_organizations", map[string]any{})
			if len(orgs.Data) != 1 || orgs.Data[0]["id"] != f.org.ID || orgs.Page != 1 || orgs.PageSize != 25 || orgs.HasMore {
				t.Fatalf("organization grant/defaults: %+v", orgs)
			}
			first := mcpWorkspaceCall(t, f, client, "list_projects", map[string]any{"organization": f.org.ID, "limit": 1})
			secondPage := mcpWorkspaceCall(t, f, client, "list_projects", map[string]any{"organization": f.org.Slug, "limit": 1, "page": 2})
			empty := mcpWorkspaceCall(t, f, client, "list_projects", map[string]any{"organization": f.org.ID, "limit": 1, "page": 3})
			if len(first.Data) != 1 || first.Data[0]["id"] != f.project.ID || !first.HasMore || first.PageSize != 1 || len(secondPage.Data) != 1 || secondPage.Data[0]["id"] != second.ID || secondPage.HasMore || secondPage.Page != 2 || len(empty.Data) != 0 || empty.HasMore {
				t.Fatalf("project pagination/grants: first=%+v second=%+v empty=%+v", first, secondPage, empty)
			}
			excluded := mcpWorkspaceCall(t, f, client, "list_projects", map[string]any{"organization": f.org.ID, "query": "Excluded"})
			if len(excluded.Data) != 0 {
				t.Fatalf("selected-project grant escaped: %+v", excluded)
			}
			for _, reference := range []string{foreignID, "missing-workspace"} {
				result, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{"organization": reference}})
				if err != nil || !result.IsError || result.StructuredContent != nil {
					t.Fatalf("foreign/missing organization %q: %+v %v", reference, result, err)
				}
			}
		})
	}
	client := mcpClient(t, f, srv, token, "2026-07-28")
	for _, args := range []map[string]any{
		{"page": 0}, {"page": 1000001}, {"page": 1.5}, {"page": "1"}, {"page": nil}, {"limit": 101}, {"unknown": true}, {"query": nil}, {"query": "bad\nquery"}, {"query": strings.Repeat("é", 201)},
	} {
		result, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_organizations", Arguments: args})
		if err != nil || !result.IsError {
			t.Fatalf("invalid arguments %v: %+v %v", args, result, err)
		}
	}
	result, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil || !result.IsError {
		t.Fatalf("missing organization: %+v %v", result, err)
	}
	if _, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: "unknown_tool", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unknown tool did not return a protocol error")
	}
	// Revoke through the real native lifecycle, then reuse the connected client.
	tokens, err := f.db.APITokens(f.ctx, f.owner.ID, f.org.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range tokens.Data {
		if credential.Name == "MCP selected projects" {
			f.request(t, f.client, "DELETE", fmt.Sprintf("/api/v1/organizations/%s/api-tokens/%s", f.org.Slug, credential.ID), "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
		}
	}
	if _, err := client.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), http.StatusText(http.StatusUnauthorized)) {
		t.Fatalf("connected client retained a revoked credential: %v", err)
	}
	allToken := issueManagementToken(t, f, f.org.Slug, "MCP all projects", []string{"project:read"}, []string{})
	allClient := mcpClient(t, f, srv, allToken, "2026-07-28")
	all := mcpWorkspaceCall(t, f, allClient, "list_projects", map[string]any{"organization": f.org.Slug})
	if len(all.Data) != 3 {
		t.Fatalf("all-project grant omitted real projects: %+v", all)
	}
	if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := allClient.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), http.StatusText(http.StatusUnauthorized)) {
		t.Fatalf("connected client retained removed membership: %v", err)
	}
}
