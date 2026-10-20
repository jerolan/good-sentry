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

	"goodsentry/internal/store"
)

func TestProjectKeyRateAPIAndIngest429(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	keysPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/keys/"
	created := f.request(t, f.client, "POST", keysPath, `{"name":"Rate limited","rateLimit":{"window":60,"count":1}}`, http.Header{"X-CSRF-Token": {f.csrf}}, 201)
	var key struct {
		RateLimit *store.CompatibleKeyRateLimit `json:"rateLimit"`
	}
	if err := json.Unmarshal(created, &key); err != nil || key.RateLimit == nil || key.RateLimit.Window != 60 || key.RateLimit.Count != 1 {
		t.Fatalf("key rate response: %+v %v", key, err)
	}
	f.request(t, f.client, "POST", keysPath, `{"name":"bad","rateLimit":{"window":0,"count":1}}`, http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	if _, err := f.sql.Exec(f.ctx, `UPDATE project_keys SET rate_limit_window=60,rate_limit_count=1 WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	endpoint := f.server.URL + "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + url.QueryEscape(f.project.PublicKey)
	call := func(seed int) (int, http.Header, []byte) {
		t.Helper()
		eventID := fmt.Sprintf("%032x", seed)
		body := fmt.Sprintf("{}\n{\"type\":\"event\"}\n{\"event_id\":%q,\"message\":\"rate limit event\",\"timestamp\":\"2026-01-01T00:00:00Z\"}", eventID)
		req, err := http.NewRequestWithContext(f.ctx, "POST", endpoint, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Clone(), raw
	}
	if code, _, raw := call(1); code != 200 {
		t.Fatalf("first rate-limited envelope=%d %s", code, raw)
	}
	code, headers, raw := call(2)
	if code != http.StatusTooManyRequests || headers.Get("Retry-After") == "" || !strings.Contains(headers.Get("X-Sentry-Rate-Limits"), ":event:key:rate_limited") {
		t.Fatalf("second rate-limited envelope=%d headers=%v body=%s", code, headers, raw)
	}
	// An invalid span count must not wrap the quota charge and let its parent
	// event bypass an already exhausted key before span validation runs.
	oversizedCount := "{}\n{\"type\":\"event\"}\n{\"event_id\":\"ffffffffffffffffffffffffffffffff\",\"message\":\"quota bypass\"}\n{\"type\":\"span\",\"item_count\":9223372036854775807}\n{}"
	req, err := http.NewRequestWithContext(f.ctx, "POST", endpoint, strings.NewReader(oversizedCount))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("oversized span count bypassed exhausted quota: status=%d", res.StatusCode)
	}
}
