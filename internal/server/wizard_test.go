//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

func newWizardHTTPFixture(t *testing.T) *resolutionHTTPFixture {
	t.Helper()
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	f.server = httptest.NewServer(NewWithAccountMail(cfg, f.db, nil, box))
	t.Cleanup(f.server.Close)
	return f
}

func TestSentryWizardHTTPContract(t *testing.T) {
	f := newWizardHTTPFixture(t)
	var created struct{ Hash string }
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/wizard/", "", nil, 200), &created); err != nil || len(created.Hash) != 64 {
		t.Fatalf("wizard hash=%+v err=%v", created, err)
	}
	var rows int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM wizard_transfers`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("initial hash wrote rows=%d err=%v", rows, err)
	}
	body := resolutionJSON(t, map[string]string{"hash": created.Hash, "organization": f.org.Slug})
	f.request(t, f.client, "POST", "/api/0/wizard-set-token/", body, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
	var payload struct {
		Projects []struct {
			ID           string
			Slug         string
			Organization struct{ Name, Slug string }
			Keys         []struct {
				DSN struct{ Public string }
			}
		}
		APIKeys struct{ Token string } `json:"apiKeys"`
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/wizard/"+created.Hash+"/", "", nil, 200), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Projects) != 1 || payload.Projects[0].ID != f.project.ID || payload.Projects[0].Slug != f.project.Slug || payload.Projects[0].Organization.Slug != f.org.Slug || len(payload.Projects[0].Keys) != 1 || payload.APIKeys.Token == "" {
		t.Fatalf("wizard payload=%+v", payload)
	}
	dsn, err := url.Parse(payload.Projects[0].Keys[0].DSN.Public)
	if err != nil || dsn.User == nil || dsn.User.Username() == "" || dsn.Path != "/good/"+f.project.ID {
		t.Fatalf("wizard DSN=%q err=%v", payload.Projects[0].Keys[0].DSN.Public, err)
	}
	f.request(t, http.DefaultClient, "GET", "/api/0/organizations/"+f.org.Slug+"/releases/", "", http.Header{"Authorization": {"Bearer " + payload.APIKeys.Token}}, 200)
	f.request(t, http.DefaultClient, "DELETE", "/api/0/wizard/"+created.Hash+"/", "", nil, 200)
	f.request(t, http.DefaultClient, "GET", "/api/0/wizard/"+created.Hash+"/", "", nil, 404)

	bearer := "gs_" + strings.Repeat("a", 64)
	hasher := &Server{cfg: config.Config{Secret: strings.Repeat("r", 64)}}
	if _, err := f.db.CreateAPIToken(f.ctx, f.owner.ID, f.owner.AuthGeneration, f.org.Slug, store.APITokenInput{
		Name: "Wizard caller", Scopes: []string{"org:write"}, ExpiresInDays: 1,
	}, hasher.hash(bearer), bearer[:11]); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/wizard/", "", nil, 200), &created); err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "POST", "/api/0/wizard-set-token/", resolutionJSON(t, map[string]string{"hash": created.Hash, "organization": f.org.ID}), http.Header{"Authorization": {"Bearer " + bearer}}, 200)
}

func TestSentryWizardAdmissionExpiryAndBrowserRedirect(t *testing.T) {
	f := newWizardHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	var memberID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('wizard-member@example.test',$1,'Wizard member') RETURNING id::text`, f.hash).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, f.org.ID, memberID); err != nil {
		t.Fatal(err)
	}
	var created struct{ Hash string }
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/wizard/", "", nil, 200), &created); err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "POST", "/api/0/wizard-set-token/", `{"hash":"bad"}`, csrf, 400)
	f.request(t, f.client, "POST", "/api/0/wizard-set-token/", `{"hash":"`+created.Hash+`","unknown":true}`, csrf, 400)
	f.request(t, f.client, "POST", "/api/0/wizard-set-token/", resolutionJSON(t, map[string]string{"hash": created.Hash, "organization": f.org.Slug}), csrf, 200)
	if _, err := f.sql.Exec(f.ctx, `UPDATE wizard_transfers SET created_at=now()-interval '2 seconds',expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "GET", "/api/0/wizard/"+created.Hash+"/", "", nil, 404)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good/account/settings/wizard/"+created.Hash+"/?org_slug="+f.org.Slug, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "view=wizard") || !strings.Contains(res.Header.Get("Location"), "wizard="+created.Hash) {
		t.Fatalf("wizard redirect=%d %q", res.StatusCode, res.Header.Get("Location"))
	}
}
