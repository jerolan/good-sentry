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
)

func TestInstanceUserAdministrationHTTPAuthorizationAndUpdates(t *testing.T) {
	f := newResolutionHTTPFixtureWithConfig(t, func(c *config.Config) { c.Admin = true })
	users := f.request(t, f.client, "GET", "/api/v1/admin/users", "", nil, 200)
	var page struct {
		Data    []map[string]any `json:"data"`
		Total   int              `json:"total"`
		Page    int              `json:"page"`
		PerPage int              `json:"perPage"`
	}
	if err := json.Unmarshal(users, &page); err != nil || page.Total != 1 || page.Page != 1 || page.PerPage != 25 || len(page.Data) != 1 {
		t.Fatalf("unexpected admin user page: %+v err=%v", page, err)
	}
	for _, field := range []string{"id", "email", "name", "isActive", "isInstanceAdmin", "organizationCount"} {
		if _, exists := page.Data[0][field]; !exists {
			t.Fatalf("admin user projection lacks %q: %s", field, users)
		}
	}
	for _, forbidden := range []string{"password", "hash", "secret", "authGeneration", "csrf", "oauth"} {
		if strings.Contains(strings.ToLower(string(users)), forbidden) {
			t.Fatalf("admin user response contains private field %q: %s", forbidden, users)
		}
	}
	f.request(t, f.client, "GET", "/api/v1/admin/users?page=1&page=2", "", nil, 400)
	f.request(t, f.client, "GET", "/api/v1/admin/users?perPage=101", "", nil, 400)
	f.request(t, f.client, "GET", "/api/v1/admin/users?unknown=true", "", nil, 400)
	f.request(t, f.client, "PATCH", "/api/v1/admin/users/01", `{"isActive":false}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 400)
	ownerPath := "/api/v1/admin/users/" + f.owner.ID
	f.request(t, f.client, "PATCH", ownerPath, `{}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 400)
	f.request(t, f.client, "PATCH", ownerPath, `{"isActive":true,"unexpected":false}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 400)
	f.request(t, f.client, "PATCH", ownerPath, `{"isActive":null,"isInstanceAdmin":true}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 400)
	f.request(t, f.client, "PATCH", ownerPath, `{"isInstanceAdmin":null}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}, 400)
	f.request(t, f.client, "PATCH", ownerPath, `{"isActive":false}`, nil, 403)

	var memberID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('org-admin@example.test',$1,'Organization admin') RETURNING id::text`, f.hash).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, f.org.ID, memberID); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	memberClient := &http.Client{Jar: jar}
	f.request(t, memberClient, "POST", "/api/v1/login", `{"email":"org-admin@example.test","password":"resolution-http-password"}`, nil, 200)
	f.request(t, memberClient, "GET", "/api/v1/admin/overview", "", nil, 200)
	f.request(t, memberClient, "GET", "/api/v1/admin/users", "", nil, 403)

	csrf := http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://public.example.test"}}
	updated := f.request(t, f.client, "PATCH", "/api/v1/admin/users/"+memberID, `{"isInstanceAdmin":true}`, csrf, 200)
	var projection map[string]any
	if err := json.Unmarshal(updated, &projection); err != nil || projection["isInstanceAdmin"] != true {
		t.Fatalf("global administrator promotion failed: %s err=%v", updated, err)
	}
	f.request(t, f.client, "PATCH", "/api/v1/admin/users/"+memberID, `{"isActive":false}`, csrf, 200)
	f.request(t, memberClient, "GET", "/api/v1/me", "", nil, 401)
	updated = f.request(t, f.client, "PATCH", "/api/v1/admin/users/"+memberID, `{"isActive":true}`, csrf, 200)
	if err := json.Unmarshal(updated, &projection); err != nil || projection["isInstanceAdmin"] != false {
		t.Fatalf("reactivation restored instance-admin authority: %s err=%v", updated, err)
	}
	f.request(t, f.client, "PATCH", "/api/v1/admin/users/"+memberID, `{"isInstanceAdmin":true}`, csrf, 200)
	f.request(t, f.client, "PATCH", ownerPath, `{"isInstanceAdmin":false}`, csrf, 200)
	f.request(t, f.client, "GET", "/api/v1/admin/users", "", nil, 403)
	f.request(t, f.client, "GET", "/api/v1/admin/overview", "", nil, 200)
}

func TestInstanceAdministrationRequiresFeatureAndProtectsOpenOwner(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	f.request(t, f.client, "GET", "/api/v1/admin/users", "", nil, 404)
	cfg := config.Config{AuthMode: "disabled", Admin: true, DefaultEmail: f.owner.Email, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64)}
	srv := httptest.NewServer(NewWithAccountMailAndDefaultUser(cfg, f.db, nil, nil, &f.owner))
	t.Cleanup(srv.Close)
	client := &http.Client{}
	request := func(method, path, body string, headers http.Header, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for key, values := range headers {
			req.Header[key] = values
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
		}
		return raw
	}
	var me struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(request("GET", "/api/v1/me", "", nil, 200), &me); err != nil || me.CSRFToken == "" {
		t.Fatalf("open owner csrf missing: %v", err)
	}
	headers := http.Header{"X-CSRF-Token": {me.CSRFToken}, "Origin": {"https://public.example.test"}}
	request("GET", "/api/v1/admin/users", "", nil, 200)
	request("GET", "/api/v1/admin/overview", "", nil, 200)
	request("PATCH", "/api/v1/admin/users/"+f.owner.ID, `{"isActive":false}`, headers, 403)
	request("PATCH", "/api/v1/admin/users/"+f.owner.ID, `{"isInstanceAdmin":false}`, headers, 403)
	if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/v1/admin/users", "", nil, 200)
	request("GET", "/api/v1/admin/overview", "", nil, 403)
}
