//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/uptime"
)

func TestStatusPageHTTPAndPublicView(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	monitor, err := f.db.CreateMonitor(f.ctx, f.owner.ID, f.org.Slug, uptime.Config{
		Name: "Public API", Type: uptime.HEAD, URL: "https://api.example.test",
		IntervalSeconds: 60, TimeoutSeconds: 10, ExpectedStatus: 200,
		ConfirmationThreshold: 1, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(config.Config{
		Domain: "http://example.test/good", Prefix: "/good", Secret: strings.Repeat("s", 64), MaxBodyBytes: 1 << 20, Uptime: true,
	}, f.db))
	defer srv.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	call := func(method, path, body string, want int) []byte {
		t.Helper()
		request, err := http.NewRequest(method, srv.URL+"/good"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&raw); err != nil && response.StatusCode != http.StatusNoContent {
			t.Fatalf("%s %s body: %v", method, path, err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s=%d want=%d body=%s", method, path, response.StatusCode, want, raw)
		}
		return raw
	}
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(call("POST", "/api/v1/login", `{"email":"owner@resolution.test","password":"resolution-http-password"}`, 200), &session); err != nil || session.CSRF == "" {
		t.Fatalf("login: %v %+v", err, session)
	}
	request, _ := http.NewRequest("POST", srv.URL+"/good/api/v1/organizations/"+f.org.Slug+"/status-pages",
		strings.NewReader(`{"name":"Public status","isPublic":true,"monitorIds":["`+monitor.ID+`"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRF)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var page struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		Name     string `json:"name"`
		IsPublic bool   `json:"isPublic"`
		Monitors []struct {
			ID string `json:"id"`
		} `json:"monitors"`
	}
	if response.StatusCode != 201 || json.NewDecoder(response.Body).Decode(&page) != nil || page.Slug == "" || !page.IsPublic || len(page.Monitors) != 1 {
		t.Fatalf("create status page=%d %+v", response.StatusCode, page)
	}
	compatible := call("GET", "/api/0/organizations/"+f.org.Slug+"/status-pages/", "", 200)
	if !strings.Contains(string(compatible), `"Public status"`) {
		t.Fatalf("compatible status pages=%s", compatible)
	}
	public, err := http.Get(srv.URL + "/good/status/" + page.Slug)
	if err != nil {
		t.Fatal(err)
	}
	defer public.Body.Close()
	body, err := io.ReadAll(public.Body)
	if err != nil {
		t.Fatal(err)
	}
	if public.StatusCode != 200 || !strings.Contains(string(body), "Public status") || !strings.Contains(string(body), "Public API") ||
		strings.Contains(string(body), "api.example.test") {
		t.Fatalf("public status page=%d %s", public.StatusCode, body)
	}
	privateRequest, _ := http.NewRequest("POST", srv.URL+"/good/api/v1/organizations/"+f.org.Slug+"/status-pages",
		strings.NewReader(`{"name":"Private status","isPublic":false}`))
	privateRequest.Header.Set("Content-Type", "application/json")
	privateRequest.Header.Set("X-CSRF-Token", session.CSRF)
	privateResponse, err := client.Do(privateRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer privateResponse.Body.Close()
	var private json.RawMessage
	if privateResponse.StatusCode != 201 || json.NewDecoder(privateResponse.Body).Decode(&private) != nil {
		t.Fatalf("private status page=%d %s", privateResponse.StatusCode, private)
	}
	var privatePage struct {
		Slug string `json:"slug"`
	}
	_ = json.Unmarshal(private, &privatePage)
	request, _ = http.NewRequest("GET", srv.URL+"/good/status/"+privatePage.Slug, nil)
	hidden, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	hidden.Body.Close()
	if hidden.StatusCode != 404 {
		t.Fatalf("private status page=%d", hidden.StatusCode)
	}
}
