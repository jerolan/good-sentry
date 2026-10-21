//go:build integration

package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPAdmission(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	credential := issueManagementToken(t, f, f.org.Slug, "MCP admission", []string{"org:read"}, []string{})
	srv := mcpHTTPServer(t, f, 1024)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	for _, tc := range []struct {
		name, method, path, body string
		host                     string
		headers                  http.Header
		want                     int
		chunked                  bool
	}{
		{name: "legacy stateless", body: body, want: 200},
		{name: "no bearer with valid cookie", headers: http.Header{"Authorization": {}}, want: 401},
		{name: "SDK key", headers: http.Header{"Authorization": {"Bearer " + strings.Repeat("a", 32)}}, want: 401},
		{name: "wrong bearer despite cookie", headers: http.Header{"Authorization": {"Bearer gs_" + strings.Repeat("0", 64)}}, want: 401},
		{name: "duplicate bearer", headers: http.Header{"Authorization": {"Bearer " + credential, "Bearer " + credential}}, want: 401},
		{name: "foreign origin", headers: http.Header{"Origin": {"https://other.example.test"}}, want: 403},
		{name: "duplicate origin", headers: http.Header{"Origin": {"https://public.example.test", "https://other.example.test"}}, want: 403},
		{name: "empty origin", headers: http.Header{"Origin": {""}}, want: 403},
		{name: "null origin", headers: http.Header{"Origin": {"null"}}, want: 403},
		{name: "cross site", headers: http.Header{"Origin": {"https://public.example.test"}, "Sec-Fetch-Site": {"cross-site"}}, want: 403},
		{name: "accepted public origin", body: body, headers: http.Header{"Origin": {"https://public.example.test"}}, want: 200},
		{name: "localhost DNS rebinding", body: body, host: "attacker.example.test", want: 403},
		{name: "query credential", path: "/mcp?access_token=untrusted", want: 400},
		{name: "empty query marker", path: "/mcp?", want: 400},
		{name: "no subscription stream", method: "GET", want: 405},
		{name: "no persisted session", method: "DELETE", want: 405},
		{name: "no CORS preflight", method: "OPTIONS", want: 405},
		{name: "wrong method", method: "PUT", want: 405},
		{name: "wrong media", body: body, headers: http.Header{"Content-Type": {"text/plain"}}, want: 415},
		{name: "missing accept", body: body, headers: http.Header{"Accept": {}}, want: 400},
		{name: "malformed body", body: "{", want: 400},
		{name: "configured body limit", body: strings.Repeat(" ", 1025) + body, want: 413},
		{name: "chunked body limit", body: strings.Repeat(" ", 1025) + body, want: 413, chunked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method, path := tc.method, tc.path
			if method == "" {
				method = "POST"
			}
			if path == "" {
				path = "/mcp"
			}
			req, err := http.NewRequestWithContext(f.ctx, method, srv.URL+"/good"+path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.chunked {
				req.ContentLength = -1
			}
			if tc.host != "" {
				req.Host = tc.host
			}
			req.Header = http.Header{"Authorization": {"Bearer " + credential}, "Content-Type": {"application/json"}, "Accept": {"application/json, text/event-stream"}, "Mcp-Protocol-Version": {"2025-11-25"}}
			for key, values := range tc.headers {
				req.Header[key] = values
			}
			res, err := f.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, _ := io.ReadAll(res.Body)
			if res.StatusCode != tc.want || res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Mcp-Session-Id") != "" || res.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("status=%d want=%d headers=%v body=%s", res.StatusCode, tc.want, res.Header, raw)
			}
			if tc.want == 401 && !strings.HasPrefix(res.Header.Get("WWW-Authenticate"), "Bearer resource_metadata=") || tc.want == 405 && res.Header.Get("Allow") != "POST" {
				t.Fatalf("missing authorization/method challenge: %v", res.Header)
			}
		})
	}
	for _, method := range []string{"GET", "POST"} {
		f.request(t, f.client, method, "/mcp", body, nil, 404)
	}
	// A large native ingestion limit must not enlarge the MCP JSON budget.
	large := mcpHTTPServer(t, f, 32<<20)
	req, _ := http.NewRequestWithContext(f.ctx, "POST", large.URL+"/good/mcp", strings.NewReader(strings.Repeat(" ", 1<<20)+body))
	req.Header = http.Header{"Authorization": {"Bearer " + credential}, "Content-Type": {"application/json"}, "Accept": {"application/json, text/event-stream"}, "Mcp-Protocol-Version": {"2025-11-25"}}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 413 {
		t.Fatalf("native JSON budget bypassed: %d", res.StatusCode)
	}
	client := mcpClient(t, f, srv, credential, "2026-07-28")
	result, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{"organization": f.org.ID}})
	if err != nil || !result.IsError {
		t.Fatalf("missing project:read scope: %+v %v", result, err)
	}
	if _, err := f.sql.Exec(f.ctx, `UPDATE api_tokens SET expires_at=now()-interval '1 second' WHERE name='MCP admission'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), http.StatusText(http.StatusUnauthorized)) {
		t.Fatalf("connected client retained expired credential: %v", err)
	}
	f.db.Close()
	req, _ = http.NewRequestWithContext(f.ctx, "POST", srv.URL+"/good/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+credential)
	res, err = f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 503 || strings.Contains(string(raw), "pool") || strings.Contains(string(raw), "postgres") {
		t.Fatalf("storage outage response=%d %s", res.StatusCode, raw)
	}
}

func TestMCPToolDisconnectCancelsDatabase(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	credential := issueManagementToken(t, f, f.org.Slug, "MCP cancellation", []string{"org:read"}, []string{})
	srv := mcpHTTPServer(t, f, 1<<20)
	for _, protocol := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(protocol, func(t *testing.T) {
			client := mcpClient(t, f, srv, credential, protocol)
			tx, err := f.sql.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(f.ctx, `LOCK TABLE organizations IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "list_organizations", Arguments: map[string]any{}})
				done <- err
			}()
			awaitLock := func(want bool) {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					var waiting bool
					if err := tx.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='organizations'::regclass AND NOT granted)`).Scan(&waiting); err != nil {
						t.Fatal(err)
					}
					if waiting == want {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("database lock waiting never became %v", want)
			}
			awaitLock(true)
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled tool call succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("disconnected tool call did not return")
			}
			awaitLock(false)
		})
	}
}
