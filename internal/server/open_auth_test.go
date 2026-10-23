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

func TestOpenAuthenticationInvitationsStayOnImplicitOwner(t *testing.T) {
	f, srv, cfg := newOpenAuthenticationFixture(t)
	owner, err := f.db.UserByEmail(f.ctx, "implicit@localhost")
	if err != nil {
		t.Fatal(err)
	}
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	hasher := &Server{cfg: cfg}
	createInvitation := func(email, rawToken string) {
		t.Helper()
		tokenHash := hasher.hash(rawToken)
		ciphertext, err := box.Seal(tokenHash, rawToken)
		if err != nil {
			t.Fatal(err)
		}
		invitation, err := f.db.InviteMember(f.ctx, f.owner.ID, f.org.Slug, email, "member", tokenHash, ciphertext, invitationLifetime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.sql.Exec(f.ctx, `UPDATE organization_invitations SET status='sent',sent_at=clock_timestamp() WHERE id=$1`, invitation.ID); err != nil {
			t.Fatal(err)
		}
	}
	accept := func(rawToken string) (int, []byte) {
		t.Helper()
		body := `{"token":"` + rawToken + `","name":"Invited User","password":"resolution-http-password"}`
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, srv.URL+cfg.Prefix+"/api/v1/invitations/accept", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://public.example.test")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, raw
	}

	otherEmailToken := strings.Repeat("a", 64)
	createInvitation("new-user@localhost", otherEmailToken)
	if status, body := accept(otherEmailToken); status != http.StatusConflict {
		t.Fatalf("open mode accepted a different account: status=%d body=%s", status, body)
	}
	if _, err := f.db.UserByEmail(f.ctx, "new-user@localhost"); err != store.ErrNotFound {
		t.Fatalf("open mode created a user whose session it cannot use: err=%v", err)
	}

	ownerToken := strings.Repeat("b", 64)
	createInvitation(owner.Email, ownerToken)
	if status, body := accept(ownerToken); status != http.StatusOK {
		t.Fatalf("implicit owner could not accept its invitation: status=%d body=%s", status, body)
	}
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, srv.URL+cfg.Prefix+"/api/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var me struct {
		User          store.User
		Organizations []store.Organization
	}
	if err := json.NewDecoder(res.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || me.User.ID != owner.ID {
		t.Fatalf("cookie-free identity changed after accepting invitation: status=%d user=%+v", res.StatusCode, me.User)
	}
	for _, org := range me.Organizations {
		if org.Slug == f.org.Slug {
			return
		}
	}
	t.Fatalf("accepted organization was not added to implicit owner: %+v", me.Organizations)
}
