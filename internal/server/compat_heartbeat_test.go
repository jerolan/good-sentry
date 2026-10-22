//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/uptime"
)

func TestCompatibleHeartbeatCheckHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := uptime.Defaults()
	cfg.Type, cfg.Name, cfg.IntervalSeconds = uptime.Heartbeat, "HTTP endpoint heartbeat", 60
	monitor, err := f.db.CreateMonitor(f.ctx, f.owner.ID, f.org.Slug, cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(config.Config{
		Domain: "http://example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64),
		MaxBodyBytes: 1 << 20, Uptime: true,
	}, f.db))
	defer srv.Close()
	token := issueManagementToken(t, f, f.org.Slug, "Heartbeat reader", []string{"org:read"}, []string{f.project.ID})
	base := srv.URL + "/good/api/0/organizations/" + f.org.Slug + "/monitors/"
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("monitor list=%d body=%s", res.StatusCode, raw)
	}
	var list []map[string]any
	if err = json.Unmarshal(raw, &list); err != nil || len(list) != 1 {
		t.Fatalf("monitor list=%+v err=%v", list, err)
	}
	wantEndpoint := "http://example.test/good/api/0/organizations/" + f.org.Slug + "/heartbeat_check/" + monitor.EndpointID + "/"
	if list[0]["endpointId"] != monitor.EndpointID || list[0]["heartbeatEndpoint"] != wantEndpoint {
		t.Fatalf("monitor endpoint shape=%+v", list[0])
	}
	heartbeatPath := srv.URL + "/good/api/0/organizations/" + f.org.Slug + "/heartbeat_check/" + monitor.EndpointID + "/"
	post := func(path, body string, want int) map[string]any {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("heartbeat %s=%d want=%d body=%s", path, res.StatusCode, want, raw)
		}
		if want == 200 {
			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			return result
		}
		return nil
	}
	result := post(heartbeatPath, "", 200)
	if result["isUp"] != true || result["reason"] != nil || result["startCheck"] == "" {
		t.Fatalf("heartbeat result=%+v", result)
	}
	post(heartbeatPath+"?unknown=1", "", 400)
	post(heartbeatPath, strings.Repeat("x", 1025), 413)
	post(srv.URL+"/good/api/0/organizations/foreign/heartbeat_check/"+monitor.EndpointID+"/", "", 404)
	post(srv.URL+"/good/api/0/organizations/"+f.org.Slug+"/heartbeat_check/not-a-uuid/", "", 404)
	if _, err := f.sql.Exec(f.ctx, `UPDATE monitors SET enabled=false WHERE id::text=$1`, monitor.ID); err != nil {
		t.Fatal(err)
	}
	post(heartbeatPath, "", 404)
}
