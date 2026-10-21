//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"goodsentry/internal/config"
)

func TestCompatibleMonitorContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	srv := httptest.NewServer(New(config.Config{
		Domain: "http://example.test/good", Prefix: "/good", Secret: "monitor-compatibility-secret-0123456789",
		MaxBodyBytes: 1 << 20, Uptime: true,
	}, f.db))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	call := func(method, path, body string, csrf string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+"/good"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s=%d want=%d %s", method, path, res.StatusCode, want, raw)
		}
		return raw
	}
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(call("POST", "/api/v1/login", `{"email":"owner@resolution.test","password":"resolution-http-password"}`, "", 200), &session); err != nil || session.CSRF == "" {
		t.Fatalf("login: %v %+v", err, session)
	}
	base := "/api/0/organizations/" + f.org.Slug + "/monitors/"
	create := map[string]any{
		"name": "Compatible API", "monitorType": "Ping", "url": "https://example.test/health",
		"project": f.project.Slug, "expectedStatus": 200, "expectedBody": "",
		"interval": 60, "timeout": 10, "confirmationThreshold": 1,
	}
	body, _ := json.Marshal(create)
	var monitor struct {
		ID             int64  `json:"id"`
		Name           string `json:"name"`
		MonitorType    string `json:"monitorType"`
		OrganizationID int64  `json:"organizationID"`
	}
	if err := json.Unmarshal(call("POST", base, string(body), session.CSRF, 201), &monitor); err != nil || monitor.ID == 0 || monitor.Name != "Compatible API" || monitor.MonitorType != "Ping" {
		t.Fatalf("created monitor=%+v err=%v", monitor, err)
	}
	var list []map[string]any
	if err := json.Unmarshal(call("GET", base+"?limit=1", "", "", 200), &list); err != nil || len(list) != 1 {
		t.Fatalf("monitor list=%+v err=%v", list, err)
	}
	detailPath := base + strconv.FormatInt(monitor.ID, 10) + "/"
	call("GET", detailPath, "", "", 200)
	update := map[string]any{"name": "Compatible API renamed", "monitorType": "Ping", "interval": 120}
	body, _ = json.Marshal(update)
	if err := json.Unmarshal(call("PUT", detailPath, string(body), session.CSRF, 200), &monitor); err != nil || monitor.Name != "Compatible API renamed" {
		t.Fatalf("updated monitor=%+v err=%v", monitor, err)
	}
	var checks []any
	if err := json.Unmarshal(call("GET", detailPath+"checks/", "", "", 200), &checks); err != nil || len(checks) != 0 {
		t.Fatalf("monitor checks=%+v err=%v", checks, err)
	}
	call("POST", base, `{"name":"Bad environment","monitorType":"Ping","environmentID":1}`, session.CSRF, 400)
	call("DELETE", detailPath, "", session.CSRF, 204)
	call("GET", detailPath, "", "", 404)
}
