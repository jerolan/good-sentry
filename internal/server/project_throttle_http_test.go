//go:build integration

package server

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestProjectThrottleRejectsSentryAndOTLPRequests(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	if _, err := f.sql.Exec(f.ctx, `UPDATE project_keys SET rate_limit_window=60,rate_limit_count=1 WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	envelopePath := "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + url.QueryEscape(f.project.PublicKey)
	callSentry := func(path, contentType, body string) (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header.Clone(), raw
	}
	first := "{}\n{\"type\":\"event\"}\n{\"event_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"message\":\"before project throttle\"}"
	if status, _, body := callSentry(envelopePath, "", first); status != http.StatusOK {
		t.Fatalf("project with default rate rejected envelope: status=%d body=%s", status, body)
	}
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	projectPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/"
	f.request(t, f.client, http.MethodPut, projectPath, `{"name":"Resolution project","platform":"javascript","eventThrottleRate":100}`, csrf, http.StatusOK)

	assertProjectThrottle := func(label string, status int, headers http.Header, body []byte) {
		t.Helper()
		if status != http.StatusTooManyRequests || headers.Get("Retry-After") != "600" || headers.Get("X-Sentry-Rate-Limits") != "600:event:project:project_throttled" {
			t.Fatalf("%s throttle response: status=%d headers=%v body=%s", label, status, headers, body)
		}
	}
	status, headers, body := callSentry(envelopePath, "", first)
	assertProjectThrottle("envelope", status, headers, body)
	malformedEvent := "{}\n{\"type\":\"event\"}\nnot-json"
	status, headers, body = callSentry(envelopePath, "", malformedEvent)
	assertProjectThrottle("malformed event", status, headers, body)
	securityPath := "/good/api/" + f.project.ID + "/security/?sentry_key=" + url.QueryEscape(f.project.PublicKey)
	status, headers, body = callSentry(securityPath, "application/csp-report", `{"csp-report":{}}`)
	assertProjectThrottle("security report", status, headers, body)

	otlp := New(otlpHTTPConfig("/good"), f.db)
	response := otlpHTTPCall(t, otlp, http.MethodPost, "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, "application/x-protobuf"), bytes.NewReader(otlpHTTPBinary(t, "throttled")))
	otlpHTTPStatus(t, response, http.StatusTooManyRequests, 8, true)
	if response.Header().Get("Retry-After") != "600" || response.Header().Get("X-Sentry-Rate-Limits") != "600:event:project:project_throttled" {
		t.Fatalf("OTLP project throttle headers: %v", response.Header())
	}

	var charged, events int
	if err := f.sql.QueryRow(f.ctx, `SELECT COALESCE(sum(u.count),0)::int FROM project_key_rate_usage u JOIN project_keys k ON k.api_id=u.api_id WHERE k.public_key=$1`, f.project.PublicKey).Scan(&charged); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE project_id=$1::bigint`, f.project.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if charged != 1 || events != 1 {
		t.Fatalf("throttled requests changed independent per-key quota or event storage: charged=%d events=%d", charged, events)
	}
}
