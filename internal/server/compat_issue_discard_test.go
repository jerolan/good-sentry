//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/config"
)

func TestCompatibleIssueDiscardRemovesErrorIssue(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/0/organizations/" + f.org.Slug + "/issues/" + f.issueID + "/"
	f.request(t, f.client, http.MethodPut, path, `{"discard":true}`, nil, http.StatusForbidden)
	f.request(t, f.client, http.MethodPut, path, `{"discard":true}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, http.StatusForbidden)
	body := f.request(t, f.client, http.MethodPut, path, `{"discard":true}`, http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusNoContent)
	if len(body) != 0 {
		t.Fatalf("discard response must be empty: %s", body)
	}
	f.request(t, f.client, http.MethodGet, "/api/v1/issues/"+f.issueID, "", nil, http.StatusNotFound)
}

func TestCompatibleIssueDiscardKeepsEventAdminOutOfTriage(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	management := "/api/v1/organizations/" + f.org.Slug + "/api-tokens"
	created := f.request(t, f.client, http.MethodPost, management, `{"name":"Discard only","scopes":["event:admin"],"projectIds":["`+f.project.ID+`"]}`, csrf, http.StatusCreated)
	var token struct{ Token string }
	if err := json.Unmarshal(created, &token); err != nil || token.Token == "" {
		t.Fatalf("event:admin token=%s err=%v", created, err)
	}
	bearer := http.Header{"Authorization": {"Bearer " + token.Token}}
	path := "/api/0/organizations/" + f.org.Slug + "/issues/" + f.issueID + "/"
	f.request(t, http.DefaultClient, http.MethodPut, path, `{"status":"resolved"}`, bearer, http.StatusForbidden)
	f.request(t, http.DefaultClient, http.MethodPut, path, `{"discard":true,"status":"resolved"}`, bearer, http.StatusBadRequest)
	f.request(t, f.client, http.MethodGet, "/api/v1/issues/"+f.issueID, "", nil, http.StatusOK)
	body := f.request(t, http.DefaultClient, http.MethodPut, path, `{"discard":true}`, bearer, http.StatusNoContent)
	if len(body) != 0 {
		t.Fatalf("discard response must be empty: %s", body)
	}
	f.request(t, f.client, http.MethodGet, "/api/v1/issues/"+f.issueID, "", nil, http.StatusNotFound)
}

func TestDiscardedMixedEnvelopeRetainsProjectThrottleAndSiblingQuota(t *testing.T) {
	f := newResolutionHTTPFixtureWithConfig(t, func(cfg *config.Config) { cfg.Logs = true })
	if _, err := f.sql.Exec(f.ctx, `UPDATE project_keys SET rate_limit_window=60,rate_limit_count=4 WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	endpoint := "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + url.QueryEscape(f.project.PublicKey)
	call := func(payload string) (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+endpoint, strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var raw []byte
		raw, err = io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header.Clone(), raw
	}
	const fingerprint = "discarded-mixed-envelope"
	firstID := "00000000000000000000000000000831"
	first := fmt.Sprintf("{}\n{\"type\":\"event\"}\n{\"event_id\":%q,\"message\":\"Discard mixed parent\",\"fingerprint\":[%q]}", firstID, fingerprint)
	if status, _, body := call(first); status != http.StatusOK {
		t.Fatalf("initial capture status=%d body=%s", status, body)
	}
	var listed struct{ Data []struct{ ID string } }
	if err := json.Unmarshal(f.request(t, f.client, http.MethodGet, "/api/v1/organizations/"+f.org.Slug+"/issues?project="+f.project.ID+"&query="+url.QueryEscape("Discard mixed parent"), "", nil, http.StatusOK), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 {
		t.Fatalf("initial issue lookup=%+v", listed)
	}
	issueID := listed.Data[0].ID
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	projectPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/"
	f.request(t, f.client, http.MethodPut, projectPath, `{"name":"Resolution project","platform":"javascript","eventThrottleRate":100}`, csrf, http.StatusOK)
	issuePath := "/api/0/organizations/" + f.org.Slug + "/issues/" + issueID + "/"
	f.request(t, f.client, http.MethodPut, issuePath, `{"discard":true}`, csrf, http.StatusNoContent)

	traceID := "11111111111111111111111111111131"
	stamp := time.Now().Add(-time.Minute).Unix()
	spanPayload := fmt.Sprintf(`{"version":2,"items":[{"trace_id":%q,"span_id":"2222222222222231","name":"discard sibling span","status":"ok","is_segment":true,"start_timestamp":%d,"end_timestamp":%d},{"trace_id":%q,"span_id":"3333333333333331","parent_span_id":"2222222222222231","name":"discard sibling child span","status":"ok","is_segment":false,"start_timestamp":%d,"end_timestamp":%d}]}`, traceID, stamp, stamp+1, traceID, stamp, stamp+1)
	logPayload := fmt.Sprintf(`{"items":[{"timestamp":%d,"trace_id":%q,"span_id":"2222222222222231","level":"info","body":"discard sibling log"}]}`, stamp, traceID)
	eventID := "00000000000000000000000000000832"
	parent := fmt.Sprintf(`{"event_id":%q,"message":"Discarded duplicate parent","fingerprint":[%q]}`, eventID, fingerprint)
	mixed := "{}\n{\"type\":\"event\"}\n" + parent +
		"\n{\"type\":\"log\",\"content_type\":\"application/vnd.sentry.items.log+json\",\"item_count\":1}\n" + logPayload +
		"\n{\"type\":\"span\",\"content_type\":\"application/vnd.sentry.items.span.v2+json\",\"item_count\":2}\n" + spanPayload
	if status, _, body := call(mixed); status != http.StatusTooManyRequests {
		t.Fatalf("project-throttled mixed discard status=%d body=%s", status, body)
	}
	var charges int
	if err := f.sql.QueryRow(f.ctx, `SELECT COALESCE(sum(u.count),0)::int FROM project_key_rate_usage u JOIN project_keys k ON k.api_id=u.api_id WHERE k.public_key=$1`, f.project.PublicKey).Scan(&charges); err != nil || charges != 1 {
		t.Fatalf("throttled envelope quota charged=%d want only initial event charge 1 err=%v", charges, err)
	}
	var eventCount, logCount, spanCount int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, eventID).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("discarded duplicate parent event count=%d err=%v", eventCount, err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM logs WHERE project_id=$1::bigint AND body='discard sibling log'`, f.project.ID).Scan(&logCount); err != nil || logCount != 0 {
		t.Fatalf("throttled independent logs count=%d want 0 err=%v", logCount, err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM spans WHERE project_id=$1::bigint AND trace_id=$2`, f.project.ID, traceID).Scan(&spanCount); err != nil || spanCount != 0 {
		t.Fatalf("throttled independent spans count=%d want 0 err=%v", spanCount, err)
	}
	f.request(t, f.client, http.MethodPut, projectPath, `{"name":"Resolution project","platform":"javascript","eventThrottleRate":0}`, csrf, http.StatusOK)
	if status, _, body := call(mixed); status != http.StatusOK {
		t.Fatalf("unthrottled discarded parent should admit independent telemetry: status=%d body=%s", status, body)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT COALESCE(sum(u.count),0)::int FROM project_key_rate_usage u JOIN project_keys k ON k.api_id=u.api_id WHERE k.public_key=$1`, f.project.PublicKey).Scan(&charges); err != nil || charges != 4 {
		t.Fatalf("quota charged=%d want initial event plus 1 log and 2 spans err=%v", charges, err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, eventID).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("discarded duplicate parent event count=%d err=%v", eventCount, err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM logs WHERE project_id=$1::bigint AND body='discard sibling log'`, f.project.ID).Scan(&logCount); err != nil || logCount != 1 {
		t.Fatalf("independent logs count=%d err=%v", logCount, err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM spans WHERE project_id=$1::bigint AND trace_id=$2`, f.project.ID, traceID).Scan(&spanCount); err != nil || spanCount != 2 {
		t.Fatalf("independent spans count=%d err=%v", spanCount, err)
	}
}
