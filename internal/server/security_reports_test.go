//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"goodsentry/internal/eventview"
	"goodsentry/internal/store"
)

// The real HTTP+PostgreSQL boundary owns public query authentication, the
// prefixed browser endpoints, no writes for rejected batches and usable native
// and compatible reads. Pure event tests own individual field normalization.
func TestSecurityReportCaptureAuthAndReads(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	collection := "/api/" + f.project.ID + "/security/"
	query := "?sentry_key=" + f.project.PublicKey
	body := `{"csp-report":{"document-uri":"https://web.example/account","effective-directive":"script-src-elem","blocked-uri":"https://blocked.example/script.js?version=1","original-policy":"script-src 'self'","line-number":12,"status-code":200,"browser-extension":{"future":true}}}`
	headers := http.Header{"Content-Type": {"application/csp-report"}, "Origin": {"https://web.example"}, "User-Agent": {"CSP actual HTTP browser"}}
	if raw := f.request(t, http.DefaultClient, "POST", collection+query+"&sentry_release=web%401&sentry_environment=production", body, headers, 204); len(raw) != 0 {
		t.Fatal("204 returned a body", string(raw))
	}
	var id, issue, kind string
	var data json.RawMessage
	if err := f.sql.QueryRow(f.ctx, `SELECT event_id,issue_id::text,kind,data FROM events WHERE project_id=$1::bigint AND kind='csp' ORDER BY id DESC LIMIT 1`, f.project.ID).Scan(&id, &issue, &kind, &data); err != nil {
		t.Fatal(err)
	}
	if kind != "csp" || !strings.Contains(string(data), "CSP actual HTTP browser") || !strings.Contains(string(data), `"future": true`) || !strings.Contains(string(data), `"environment": "production"`) {
		t.Fatalf("browser report was not stored faithfully: %s %s", kind, data)
	}
	var detail store.IssueDetail
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/v1/issues/"+issue, "", nil, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Issue.Count != 1 || detail.Issue.Category != "error" || detail.Issue.Release != "web@1" || detail.Issue.Environment != "production" || !strings.Contains(detail.Issue.Title, "script-src-elem") || len(detail.Events) != 1 || detail.Events[0].ID != id {
		t.Fatalf("CSP report has no usable native issue: %+v", detail)
	}
	var compatible eventview.Event
	eventPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/events/" + id + "/"
	if err := json.Unmarshal(f.request(t, f.client, "GET", eventPath, "", nil, 200), &compatible); err != nil {
		t.Fatal(err)
	}
	if compatible.Type != "csp" || compatible.GroupID != issue || compatible.Entries == nil {
		t.Fatalf("compatible event lost CSP type or issue: %+v", compatible)
	}
	foundCSP := false
	for _, entry := range *compatible.Entries {
		if entry.Type == "csp" && strings.Contains(string(entry.Data), `"blocked_uri":"https://blocked.example/script.js?version=1"`) {
			foundCSP = true
		}
	}
	if !foundCSP {
		t.Fatalf("compatible event did not expose the CSP details: %+v", *compatible.Entries)
	}
	modern := `[{"type":"csp-violation","url":"https://web.example/other","age":500,"user_agent":"Reporting API browser","body":{"effectiveDirective":"script-src-elem","blockedURL":"https://blocked.example/script.js?version=2","statusCode":200}},{"type":"csp-violation","url":"https://web.example/","body":{"effectiveDirective":"img-src","blockedURL":"data"}}]`
	modernPath := "/api/" + f.project.ID + "/csp-report/" + query
	f.request(t, http.DefaultClient, "POST", modernPath, modern, http.Header{"Content-Type": {"application/reports+json"}}, 204)
	var count int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE kind='csp'`).Scan(&count); err != nil || count != 3 {
		t.Fatal("Reporting API batch did not create all three real occurrences", count, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/v1/issues/"+issue, "", nil, 200), &detail); err != nil || detail.Issue.Count != 2 {
		t.Fatal("equivalent legacy/modern violations did not group", detail.Issue, err)
	}
	before := count
	for _, malformed := range []string{
		`[{"type":"csp-violation","url":"https://web.example/","body":{"effectiveDirective":"connect-src"}},{"type":"csp-violation","body":null}]`,
		`[{"type":"deprecation","url":"https://web.example/","body":{}}]`,
		`{"csp-report":{"document-uri":"https://web.example/","effective-directive":null}}`,
	} {
		f.request(t, http.DefaultClient, "POST", modernPath, malformed, nil, 400)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other security project", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	for _, badPath := range []string{
		collection, collection + "?sentry_key=invalid", collection + query + "&sentry_version=6",
		"/api/" + other.ID + "/security/" + query,
		"/api/99999999/security/" + query,
		"/api/not-a-project/security/" + query,
	} {
		f.request(t, f.client, "POST", badPath, body, headers, 403)
	}
	for _, badPath := range []string{
		collection + query + "&sentry_key=" + f.project.PublicKey,
		collection + query + "&unimplemented=true",
		collection + query + "&sentry_environment=" + url.QueryEscape(strings.Repeat("x", 129)),
		collection + query + "&sentry_release=%00",
	} {
		f.request(t, http.DefaultClient, "POST", badPath, body, headers, 400)
	}
	for _, mixed := range []http.Header{
		{"Authorization": {"Bearer invalid"}},
		{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + f.project.PublicKey}},
	} {
		f.request(t, f.client, "POST", collection+query, body, mixed, 403)
	}
	for _, badMedia := range []http.Header{
		{"Content-Type": {"text/plain"}},
		{"Content-Type": {"application/json", "application/csp-report"}},
		{"Content-Type": {"application/csp-report; charset=iso-8859-1"}},
		{"Content-Encoding": {"gzip"}},
		{"Content-Encoding": {"identity", "identity"}},
	} {
		f.request(t, http.DefaultClient, "POST", collection+query, body, badMedia, 415)
	}
	f.request(t, http.DefaultClient, "POST", collection+query, strings.Repeat(" ", (1<<20)+1), nil, 413)
	if _, err := f.sql.Exec(f.ctx, `UPDATE project_keys SET enabled=false WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "POST", collection+query, body, headers, 403)
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE kind='csp'`).Scan(&count); err != nil || count != before {
		t.Fatal("rejected reports made partial writes or crossed project keys", count, err)
	}
	// Check actual cross-origin response headers through the configured prefix.
	request, err := http.NewRequestWithContext(f.ctx, "OPTIONS", f.server.URL+"/good"+collection, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://web.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 204 || len(raw) != 0 || response.Header.Get("Access-Control-Allow-Origin") != "*" || response.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("real browser security preflight failed", response.StatusCode, response.Header, string(raw), err)
	}
}
