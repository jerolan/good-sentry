//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

// HTTP owns credential precedence, strict transport parsing and dispatch to
// the real durable DTO. Store tests cannot detect missing routes or bad wire
// admission; this test deliberately leaves receipt and lock behavior to them.
func TestUserReportSubmitHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/user-feedback/"
	input := store.UserReportInput{EventID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Reporter", Email: "reporter@example.test", Comments: "A useful report"}
	body := resolutionJSON(t, input)
	post := func(client *http.Client, p, b string, h http.Header, want int) []byte {
		t.Helper()
		return f.request(t, client, "POST", p, b, h, want)
	}
	decode := func(raw []byte) store.UserReport {
		t.Helper()
		var report store.UserReport
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatalf("report DTO: %v body=%s", err, raw)
		}
		return report
	}
	csrf := http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}
	got := decode(post(f.client, path, body, csrf, 200))
	if got.EventID != input.EventID || got.Name != input.Name || got.Email != input.Email || got.Comments != input.Comments || got.Event.EventID != input.EventID || got.Event.ID != nil || got.User != nil || got.Issue != nil || got.ID == "" || got.DateCreated.IsZero() {
		t.Fatalf("submission did not return durable report DTO: %+v", got)
	}
	if got := f.request(t, http.DefaultClient, "POST", path, body, nil, 401); len(got) == 0 {
		t.Fatal("missing-auth response body")
	}
	for _, headers := range []http.Header{{"Origin": {"https://public.example.test"}}, {"X-CSRF-Token": {f.csrf}, "Origin": {"https://evil.example"}}} {
		post(f.client, path, body, headers, 403)
	}
	issueToken := func(scope string) string {
		t.Helper()
		var credential struct{ Token string }
		raw := f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", resolutionJSON(t, map[string]any{"name": "Report " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}}), http.Header{"X-CSRF-Token": {f.csrf}}, 201)
		if err := json.Unmarshal(raw, &credential); err != nil || credential.Token == "" {
			t.Fatalf("token response: %v %s", err, raw)
		}
		return credential.Token
	}
	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	write := issueToken("project:write")
	post(http.DefaultClient, path, body, bearer(write), 200)
	f.request(t, http.DefaultClient, "GET", path, "", bearer(write), 403)
	for _, scope := range []string{"project:read", "event:read"} {
		post(http.DefaultClient, path, body, bearer(issueToken(scope)), 403)
	}
	post(f.client, path, body, http.Header{"Authorization": {"Bearer invalid"}, "X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 401)
	post(f.client, path, body, http.Header{"Authorization": {"Bearer " + write}, "X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + f.project.PublicKey}}, 403)
	post(f.client, "/api/0/projects/unknown/unknown/user-feedback/", body, csrf, 404)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other feedback project", "go")
	if err != nil {
		t.Fatal(err)
	}
	otherPath := "/api/0/projects/" + f.org.Slug + "/" + other.Slug + "/user-feedback/"
	post(http.DefaultClient, otherPath, body, bearer(write), 404)

	sdk := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + f.project.PublicKey}}
	post(f.client, path, body, sdk, 200)
	post(http.DefaultClient, otherPath, body, sdk, 403)
	post(http.DefaultClient, "/api/0/projects/unknown/"+f.project.Slug+"/user-feedback/", body, sdk, 403)
	post(f.client, path, body, http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=invalid"}, "X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 403)
	post(http.DefaultClient, path, body, http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=foreign"}}, 403)
	post(http.DefaultClient, path+"?sentry_key="+f.project.PublicKey+"&sentry_version=7", body, nil, 200)
	post(http.DefaultClient, path+"?sentry_key="+f.project.PublicKey+"&sentry_version=7&unsupported=1", body, nil, 400)
	post(f.client, path+"?unsupported=1", body, csrf, 400)
	f.request(t, http.DefaultClient, "GET", path, "", sdk, 401)

	for _, bad := range []string{`{"event_id":null,"name":"","email":"","comments":"valid"}`, `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","email":"","comments":"valid"}`, `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":2,"email":"","comments":"valid"}`, strings.TrimSuffix(body, "}") + `,"extra":1}`, body + ` {}`} {
		post(f.client, path, bad, csrf, 400)
	}
	post(f.client, path, `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"","email":"","comments":"bad`+string([]byte{0xff})+`"}`, csrf, 400)
	post(f.client, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}, "Content-Type": {"text/plain"}}, 415)
	post(f.client, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}, "Content-Type": {"application/json; charset=utf-8"}}, 200)
	post(f.client, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}, "Content-Encoding": {"gzip"}}, 415)
	post(f.client, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Content-Type": {"application/json", "text/plain"}}, 415)
	post(f.client, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Content-Encoding": {"identity", "gzip"}}, 415)
	post(f.client, path, strings.Repeat(" ", (1<<20)+1), csrf, 413)
	for _, method := range []string{"OPTIONS", "POST"} {
		req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://sdk.example.test")
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Sentry-Auth", sdk.Get("X-Sentry-Auth"))
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := 204
		if method == "POST" {
			want = 200
		}
		if res.StatusCode != want || res.Header.Get("Access-Control-Allow-Origin") != "*" || res.Header.Get("Access-Control-Allow-Credentials") != "" || method == "POST" && res.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal("SDK feedback CORS/cache policy", res.StatusCode, res.Header)
		}
	}
	if _, err := f.sql.Exec(f.ctx, `UPDATE user_reports SET created_at=clock_timestamp()-interval '6 minutes' WHERE id::text=$1`, got.ID); err != nil {
		t.Fatal(err)
	}
	post(f.client, path, body, csrf, 409)
}
