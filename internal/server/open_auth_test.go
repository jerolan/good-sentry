//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

func newOpenAuthenticationFixture(t *testing.T) (*resolutionHTTPFixture, *httptest.Server, config.Config) {
	t.Helper()
	f := newResolutionHTTPFixture(t)
	owner, _, err := f.db.EnsureDefaultOwner(f.ctx, "implicit@localhost", f.hash, "Implicit Owner", "Implicit Organization")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		AuthMode: "disabled", DefaultEmail: owner.Email, DefaultName: owner.Name, DefaultOrganization: "Implicit Organization",
		Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20,
	}
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewWithAccountMailAndDefaultUser(cfg, f.db, nil, box, &owner))
	t.Cleanup(srv.Close)
	return f, srv, cfg
}

func TestOpenAuthenticationUsesImplicitOwnerAndKeepsCSRF(t *testing.T) {
	f, srv, cfg := newOpenAuthenticationFixture(t)
	client := &http.Client{}
	request := func(method, path, body string, headers http.Header, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
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
	var setup struct{ Required bool }
	if err := json.Unmarshal(request("GET", "/api/v1/setup", "", nil, 200), &setup); err != nil || setup.Required {
		t.Fatalf("setup=%+v err=%v", setup, err)
	}
	var me struct {
		User          store.User
		Organizations []store.Organization
		CSRFToken     string `json:"csrfToken"`
	}
	if err := json.Unmarshal(request("GET", "/api/v1/me", "", nil, 200), &me); err != nil || me.User.Email != "implicit@localhost" || len(me.Organizations) != 1 || me.Organizations[0].Slug != "implicit-organization" || me.CSRFToken == "" {
		t.Fatalf("open me=%+v err=%v", me, err)
	}
	path := "/api/v1/organizations/" + me.Organizations[0].Slug + "/projects"
	request("POST", path, `{"name":"No CSRF","platform":"go"}`, nil, 403)
	request("POST", path, `{"name":"Foreign","platform":"go"}`, http.Header{"X-CSRF-Token": {me.CSRFToken}, "Origin": {"https://foreign.example.test"}}, 403)
	request("POST", path, `{"name":"Open project","platform":"go"}`, http.Header{"X-CSRF-Token": {me.CSRFToken}}, 201)
	restarted := httptest.NewServer(NewWithAccountMailAndDefaultUser(cfg, f.db, nil, nil, &me.User))
	defer restarted.Close()
	res, err := http.Get(restarted.URL + cfg.Prefix + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("implicit owner after restart=%d", res.StatusCode)
	}
	required := httptest.NewServer(New(config.Config{AuthMode: "required", Domain: cfg.Domain, Prefix: cfg.Prefix, Secret: cfg.Secret}, f.db))
	defer required.Close()
	res, err = http.Get(required.URL + cfg.Prefix + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("required mode accepted open identity=%d", res.StatusCode)
	}
}
