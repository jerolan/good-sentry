//go:build integration

package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

type compatibleSocialView struct {
	ID          int64   `json:"id"`
	Provider    string  `json:"provider"`
	ProviderID  string  `json:"providerID"`
	Name        string  `json:"name"`
	ClientID    string  `json:"clientID"`
	ServerURL   *string `json:"serverUrl"`
	CallbackURL *string `json:"callbackUrl"`
	LoginURL    string  `json:"loginUrl"`
	Brand       string  `json:"brand"`
}

func socialAppBody(name, clientID, secret, provider, serverURL string) string {
	body := map[string]any{"name": name, "clientID": clientID}
	if secret != "" {
		body["clientSecret"] = secret
	}
	if provider != "" {
		body["provider"] = provider
	}
	if serverURL != "" {
		body["serverUrl"] = serverURL
	}
	encoded, _ := json.Marshal(body)
	return string(encoded)
}

func TestCompatibleSocialAppsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	base := "/api/0/organizations/" + f.org.Slug + "/social-apps/"
	var apps []compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "GET", base, "", nil, 200), &apps); err != nil || len(apps) != 0 {
		t.Fatalf("initial apps=%v err=%v", apps, err)
	}
	f.request(t, f.client, "POST", base, socialAppBody("Corp SSO", "client-1", "secret-1", "", "http://insecure.example.test"), csrf, 400)
	f.request(t, f.client, "POST", base, socialAppBody("Corp SSO", "client-1", "", "", "https://issuer.example.test"), csrf, 400)
	f.request(t, f.client, "POST", base, socialAppBody("Corp SSO", "client-1", "secret-1", "saml", "https://issuer.example.test"), csrf, 400)
	f.request(t, f.client, "POST", base, socialAppBody("Corp SSO", "client-1", "secret-1", "openid_connect", ""), csrf, 400)
	f.request(t, f.client, "POST", base, socialAppBody("Corp SSO", "client-1", "secret-1", "openid_connect", "https://issuer.example.test"), nil, 403)
	var created compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, socialAppBody("Corp SSO", "client-1", "secret-1", "openid_connect", "https://issuer.example.test/"), csrf, 201), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || created.Provider != "openid_connect" || created.Brand != "openid_connect" || created.Name != "Corp SSO" || created.ClientID != "client-1" {
		t.Fatalf("created=%+v", created)
	}
	if !strings.HasPrefix(created.ProviderID, f.org.Slug+"-") || len(created.ProviderID) != len(f.org.Slug)+7 {
		t.Fatalf("providerID=%q", created.ProviderID)
	}
	if created.ServerURL == nil || *created.ServerURL != "https://issuer.example.test" {
		t.Fatalf("serverUrl=%v", created.ServerURL)
	}
	wantCallback := "https://public.example.test/good/auth/social/" + created.ProviderID + "/callback"
	if created.CallbackURL == nil || *created.CallbackURL != wantCallback {
		t.Fatalf("callback=%v want=%s", created.CallbackURL, wantCallback)
	}
	if created.LoginURL != "https://public.example.test/good/login/"+f.org.Slug {
		t.Fatalf("loginUrl=%q", created.LoginURL)
	}

	if err := json.Unmarshal(f.request(t, f.client, "GET", base, "", nil, 200), &apps); err != nil || len(apps) != 1 || apps[0].ID != created.ID {
		t.Fatalf("apps=%v err=%v", apps, err)
	}
	item := base + itoaSocial(created.ID) + "/"
	var detail compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "GET", item, "", nil, 200), &detail); err != nil || detail.ProviderID != created.ProviderID {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	f.request(t, f.client, "GET", base+"0/", "", nil, 400)
	f.request(t, f.client, "GET", base+"999999/", "", nil, 404)

	var updated compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "PUT", item, socialAppBody("Corp SSO 2", "client-2", "secret-2", "", ""), csrf, 200), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Corp SSO 2" || updated.ClientID != "client-2" || updated.ServerURL == nil || *updated.ServerURL != "https://issuer.example.test" || updated.ProviderID != created.ProviderID {
		t.Fatalf("updated=%+v", updated)
	}
	f.request(t, f.client, "PUT", item, socialAppBody("Corp SSO 2", "client-2", "", "google", ""), csrf, 400)
	f.request(t, f.client, "PUT", item, socialAppBody("Corp SSO 2", "client-2", "", "", "ftp://issuer.example.test"), csrf, 400)

	reader := issueManagementToken(t, f, f.org.Slug, "SSO reader", []string{"org:read"}, []string{f.project.ID})
	bearer := http.Header{"Authorization": {"Bearer " + reader}}
	f.request(t, http.DefaultClient, "GET", base, "", bearer, 200)
	f.request(t, http.DefaultClient, "POST", base, socialAppBody("Denied", "client-3", "secret-3", "", "https://issuer.example.test"), bearer, 403)
	writer := issueManagementToken(t, f, f.org.Slug, "SSO writer", []string{"org:write"}, []string{f.project.ID})
	writeHeaders := http.Header{"Authorization": {"Bearer " + writer}}
	if code := socialStatus(t, f, "POST", base, socialAppBody("Second", "client-4", "secret-4", "google", ""), writeHeaders); code != 201 {
		t.Fatalf("bearer create=%d", code)
	}

	f.request(t, f.client, "DELETE", item, "", csrf, 204)
	f.request(t, f.client, "GET", item, "", nil, 404)
	f.request(t, f.client, "DELETE", item, "", csrf, 404)
}

func itoaSocial(id int64) string {
	if id <= 0 {
		return "0"
	}
	digits := ""
	for id > 0 {
		digits = string(rune('0'+id%10)) + digits
		id /= 10
	}
	return digits
}

func socialStatus(t *testing.T, f *resolutionHTTPFixture, method, path, body string, headers http.Header) int {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

func TestPublicSocialAppDiscovery(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	response, err := http.Get(f.server.URL + "/good/login/" + f.org.Slug)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Request.URL.Path != "/good/" || response.Request.URL.Query().Get("sso") != f.org.Slug {
		t.Fatalf("login page ended at %s status=%d", response.Request.URL, response.StatusCode)
	}
	var discovery struct {
		Organization struct{ Slug, Name string }              `json:"organization"`
		Apps         []struct{ Name, Brand, StartURL string } `json:"apps"`
	}
	response, err = http.Get(f.server.URL + "/good/api/v1/organizations/" + f.org.Slug + "/sso")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&discovery) != nil || discovery.Organization.Slug != f.org.Slug || len(discovery.Apps) != 0 {
		t.Fatalf("discovery=%+v status=%d", discovery, response.StatusCode)
	}
	if response, err = http.Get(f.server.URL + "/good/api/v1/organizations/absent-org/sso"); err != nil || response.StatusCode != 404 {
		t.Fatalf("absent organization status=%d err=%v", response.StatusCode, err)
	}
	response.Body.Close()
	if response, err = http.Get(f.server.URL + "/good/login/absent-org"); err != nil || response.StatusCode != 404 {
		t.Fatalf("absent login page status=%d err=%v", response.StatusCode, err)
	}
	response.Body.Close()
}

func TestSocialAppSignInCreatesOrganizationMember(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	provider, idp := newOIDCTestProviderFull(t, "sso-member@resolution.test", "SSO Member")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Domain: "http://" + listener.Addr().String() + "/good", Prefix: "/good", Secret: strings.Repeat("s", 64), MaxBodyBytes: 1 << 20}
	sealer, err := newSecretSealer(cfg.Secret, "GoodSentry social apps v1", 191)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.seal([]byte(provider.clientSecret))
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.db.CreateSocialApp(f.ctx, f.org.ID, store.SocialAppInput{
		ProviderID: "resolution-http-sso", Provider: "openid_connect", Brand: "openid_connect", Name: "Corp SSO",
		ClientID: provider.clientID, ClientSecret: sealed, ServerURL: idp.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.callbackSuffix = "/auth/social/" + app.ProviderID + "/callback"
	srv := httptest.NewUnstartedServer(New(cfg, f.db))
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.Get(srv.URL + "/good/auth/social/" + app.ProviderID + "/start")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Request.URL.Path != "/good/" {
		t.Fatalf("social flow ended at %s status=%d", response.Request.URL, response.StatusCode)
	}
	session, err := client.Get(srv.URL + "/good/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Body.Close()
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
		Orgs []struct {
			Slug string `json:"slug"`
		} `json:"organizations"`
	}
	if session.StatusCode != 200 || json.NewDecoder(session.Body).Decode(&me) != nil || me.User.Email != "sso-member@resolution.test" {
		t.Fatalf("social session status=%d %+v", session.StatusCode, me)
	}
	joined := false
	for _, org := range me.Orgs {
		if org.Slug == f.org.Slug {
			joined = true
		}
	}
	if !joined {
		t.Fatalf("social sign-in did not join the organization: %+v", me.Orgs)
	}
	var identities int
	if err = f.sql.QueryRow(f.ctx, "SELECT count(*) FROM user_identities WHERE provider=$1 AND subject=$2", "social:"+app.ProviderID, "subject-sso-member@resolution.test").Scan(&identities); err != nil || identities != 1 {
		t.Fatalf("identities=%d err=%v", identities, err)
	}
	var memberships int
	if err = f.sql.QueryRow(f.ctx, "SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1::bigint AND u.email='sso-member@resolution.test' AND m.role='member'", f.org.ID).Scan(&memberships); err != nil || memberships != 1 {
		t.Fatalf("memberships=%d err=%v", memberships, err)
	}

	// A flow started for one application cannot complete on another.
	mismatch := &http.Client{Jar: mustSocialJar(t)}
	if response, err = mismatch.Get(srv.URL + "/good/auth/social/" + app.ProviderID + "/start"); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	other, err := f.db.CreateSocialApp(f.ctx, f.org.ID, store.SocialAppInput{
		ProviderID: "resolution-http-other", Provider: "openid_connect", Brand: "google", Name: "Other",
		ClientID: provider.clientID, ClientSecret: sealed, ServerURL: idp.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err = mismatch.Get(srv.URL + "/good/auth/social/" + other.ProviderID + "/callback?code=test-code&state=state")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Request.URL.Query().Get("authError") != "failed" {
		t.Fatalf("mismatched callback ended at %s", response.Request.URL)
	}
}

func mustSocialJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}
