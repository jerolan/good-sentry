//go:build integration

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

func TestCompatibleSocialOAuth2AppsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	base := "/api/0/organizations/" + f.org.Slug + "/social-apps/"
	var github, gitlab compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, socialAppBody("GitHub", "gh-client", "gh-secret", "github", ""), csrf, 201), &github); err != nil {
		t.Fatal(err)
	}
	if github.Provider != "github" || github.Brand != "github" || github.ServerURL == nil || *github.ServerURL != "https://github.com" {
		t.Fatalf("GitHub app=%+v", github)
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, socialAppBody("GitLab", "gl-client", "gl-secret", "gitlab", "https://gitlab.example.test/"), csrf, 201), &gitlab); err != nil {
		t.Fatal(err)
	}
	if gitlab.Provider != "gitlab" || gitlab.Brand != "gitlab" || gitlab.ServerURL == nil || *gitlab.ServerURL != "https://gitlab.example.test" {
		t.Fatalf("GitLab app=%+v", gitlab)
	}
	var gitea compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, socialAppBody("Gitea", "gitea-client", "gitea-secret", "gitea", "https://gitea.example.test/"), csrf, 201), &gitea); err != nil {
		t.Fatal(err)
	}
	if gitea.Provider != "gitea" || gitea.Brand != "gitea" || gitea.ServerURL == nil || *gitea.ServerURL != "https://gitea.example.test" {
		t.Fatalf("Gitea app=%+v", gitea)
	}
	var microsoft compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, socialAppBody("Microsoft", "ms-client", "ms-secret", "microsoft", ""), csrf, 201), &microsoft); err != nil {
		t.Fatal(err)
	}
	if microsoft.Provider != "openid_connect" || microsoft.Brand != "microsoft" || microsoft.ServerURL == nil || *microsoft.ServerURL != "https://login.microsoftonline.com/common/v2.0" {
		t.Fatalf("Microsoft app=%+v", microsoft)
	}
	f.request(t, f.client, "POST", base, socialAppBody("Bad", "client", "secret", "github", "ftp://github.example.test"), csrf, 400)
	f.request(t, f.client, "POST", base, socialAppBody("Gitea", "client", "secret", "gitea", ""), csrf, 400)
	var updated compatibleSocialView
	if err := json.Unmarshal(f.request(t, f.client, "PUT", base+itoaSocial(github.ID)+"/", socialAppBody("GitHub Enterprise", "gh-client", "", "", "https://github.example.test"), csrf, 200), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Provider != "github" || updated.ServerURL == nil || *updated.ServerURL != "https://github.example.test" {
		t.Fatalf("updated GitHub app=%+v", updated)
	}
}

type socialOAuth2TestProvider struct {
	server     *httptest.Server
	kind       string
	verified   bool
	confirmed  bool
	active     bool
	mu         sync.Mutex
	challenges map[string]string
}

func newSocialOAuth2TestProvider(t *testing.T, kind string, verified, confirmed bool) *socialOAuth2TestProvider {
	t.Helper()
	provider := &socialOAuth2TestProvider{kind: kind, verified: verified, confirmed: confirmed, active: true, challenges: map[string]string{}}
	mux := http.NewServeMux()
	authPath, tokenPath := "/oauth/authorize", "/oauth/token"
	if kind == "github" || kind == "gitea" {
		authPath, tokenPath = "/login/oauth/authorize", "/login/oauth/access_token"
	}
	mux.HandleFunc(authPath, provider.authorize)
	mux.HandleFunc(tokenPath, provider.token)
	if kind == "github" {
		mux.HandleFunc("/api/v3/user", provider.githubUser)
		mux.HandleFunc("/api/v3/user/emails", provider.githubEmails)
	} else if kind == "gitea" {
		mux.HandleFunc("/api/v1/user", provider.giteaUser)
	} else {
		mux.HandleFunc("/api/v4/user", provider.gitlabUser)
	}
	provider.server = httptest.NewTLSServer(mux)
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *socialOAuth2TestProvider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("client_id") != "oauth-client" || query.Get("response_type") != "code" || query.Get("state") == "" || query.Get("code_challenge") == "" || query.Get("redirect_uri") == "" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := "code-" + p.kind
	p.mu.Lock()
	p.challenges[code] = query.Get("code_challenge")
	p.mu.Unlock()
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect", http.StatusBadRequest)
		return
	}
	redirectQuery := redirect.Query()
	redirectQuery.Set("code", code)
	redirectQuery.Set("state", query.Get("state"))
	redirect.RawQuery = redirectQuery.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *socialOAuth2TestProvider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.FormValue("grant_type") != "authorization_code" || r.FormValue("client_id") != "oauth-client" || r.FormValue("client_secret") != "oauth-secret" || r.FormValue("code") != "code-"+p.kind {
		http.Error(w, "bad token request", http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256([]byte(r.FormValue("code_verifier")))
	p.mu.Lock()
	challenge := p.challenges[r.FormValue("code")]
	p.mu.Unlock()
	if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
		http.Error(w, "bad PKCE verifier", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-" + p.kind, "token_type": "Bearer", "expires_in": 3600})
}

func (p *socialOAuth2TestProvider) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer token-"+p.kind {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (p *socialOAuth2TestProvider) githubUser(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(w, r) {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": 4242, "login": "octocat", "name": "Octo User"})
}

func (p *socialOAuth2TestProvider) githubEmails(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(w, r) {
		return
	}
	_ = json.NewEncoder(w).Encode([]map[string]any{{"email": "oauth-member@resolution.test", "primary": true, "verified": p.verified}})
}

func (p *socialOAuth2TestProvider) gitlabUser(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(w, r) {
		return
	}
	user := map[string]any{"id": 4242, "username": "octocat", "name": "Octo User", "email": "oauth-member@resolution.test", "state": "active"}
	if p.confirmed {
		user["confirmed_at"] = "2026-01-01T00:00:00Z"
	}
	_ = json.NewEncoder(w).Encode(user)
}

func (p *socialOAuth2TestProvider) giteaUser(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(w, r) {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": 4242, "login": "octocat", "full_name": "Octo User", "email": "oauth-member@resolution.test", "active": p.active,
	})
}

func newSocialOAuth2TestServer(t *testing.T, f *resolutionHTTPFixture, provider *socialOAuth2TestProvider) (*httptest.Server, store.SocialApp) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Domain: "http://" + listener.Addr().String() + "/good", Prefix: "/good", Secret: strings.Repeat("s", 64), MaxBodyBytes: 1 << 20}
	sealer, err := newSecretSealer(cfg.Secret, "GoodSentry social apps v1", 191)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.seal([]byte("oauth-secret"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.db.CreateSocialApp(f.ctx, f.org.ID, store.SocialAppInput{
		ProviderID: "oauth2-" + provider.kind, Provider: provider.kind, Brand: provider.kind, Name: provider.kind,
		ClientID: "oauth-client", ClientSecret: sealed, ServerURL: provider.server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = provider.server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	srv := httptest.NewUnstartedServer(New(cfg, f.db))
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, app
}

func TestSocialOAuth2SignInCreatesOrganizationMember(t *testing.T) {
	for _, test := range []struct {
		kind, provider, email string
		verified, confirmed   bool
	}{
		{kind: "GitHub", provider: "github", email: "oauth-member@resolution.test", verified: true},
		{kind: "GitLab", provider: "gitlab", email: "oauth-member@resolution.test", confirmed: true},
		{kind: "Gitea", provider: "gitea", email: "oauth-member@resolution.test", verified: true},
	} {
		t.Run(test.kind, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			provider := newSocialOAuth2TestProvider(t, test.provider, test.verified, test.confirmed)
			srv, app := newSocialOAuth2TestServer(t, f, provider)
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Jar: jar, Transport: provider.server.Client().Transport}
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
			}
			if session.StatusCode != 200 || json.NewDecoder(session.Body).Decode(&me) != nil || me.User.Email != test.email {
				t.Fatalf("social session status=%d %+v", session.StatusCode, me)
			}
			var identities int
			if err = f.sql.QueryRow(f.ctx, "SELECT count(*) FROM user_identities WHERE provider=$1 AND subject='4242'", "social:"+app.ProviderID).Scan(&identities); err != nil || identities != 1 {
				t.Fatalf("identities=%d err=%v", identities, err)
			}
		})
	}
}

func TestSocialOAuth2RejectsUnverifiedProfiles(t *testing.T) {
	for _, test := range []struct {
		kind, provider      string
		verified, confirmed bool
		inactive            bool
	}{
		{kind: "GitHub", provider: "github"},
		{kind: "GitLab", provider: "gitlab", verified: true},
		{kind: "Gitea", provider: "gitea", verified: true, inactive: true},
	} {
		t.Run(test.kind, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			provider := newSocialOAuth2TestProvider(t, test.provider, test.verified, test.confirmed)
			provider.active = !test.inactive
			srv, app := newSocialOAuth2TestServer(t, f, provider)
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Jar: jar, Transport: provider.server.Client().Transport}
			response, err := client.Get(srv.URL + "/good/auth/social/" + app.ProviderID + "/start")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.Request.URL.Query().Get("authError") != "failed" {
				t.Fatalf("unverified flow ended at %s", response.Request.URL)
			}
			session, err := client.Get(srv.URL + "/good/api/v1/me")
			if err != nil {
				t.Fatal(err)
			}
			session.Body.Close()
			if session.StatusCode != http.StatusUnauthorized {
				t.Fatalf("unverified profile created a session: %d", session.StatusCode)
			}
			var identities int
			if err = f.sql.QueryRow(f.ctx, "SELECT count(*) FROM user_identities WHERE provider=$1", "social:"+app.ProviderID).Scan(&identities); err != nil || identities != 0 {
				t.Fatalf("identities=%d err=%v", identities, err)
			}
		})
	}
}

func TestMicrosoftSocialOIDCSignInUsesTenantIdentity(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	provider, idp := newOIDCTestProviderFull(t, "", "Microsoft Member")
	provider.emailVerified = false
	provider.preferredEmail = "microsoft-member@resolution.test"
	provider.tenant = "tenant-123"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Domain: "http://" + listener.Addr().String() + "/good", Prefix: "/good", Secret: strings.Repeat("m", 64), MaxBodyBytes: 1 << 20}
	sealer, err := newSecretSealer(cfg.Secret, "GoodSentry social apps v1", 191)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.seal([]byte(provider.clientSecret))
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.db.CreateSocialApp(f.ctx, f.org.ID, store.SocialAppInput{
		ProviderID: "microsoft-social", Provider: "openid_connect", Brand: "microsoft", Name: "Microsoft",
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
		t.Fatalf("Microsoft social flow ended at %s status=%d", response.Request.URL, response.StatusCode)
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
	}
	if session.StatusCode != 200 || json.NewDecoder(session.Body).Decode(&me) != nil || me.User.Email != provider.preferredEmail {
		t.Fatalf("Microsoft session status=%d %+v", session.StatusCode, me)
	}
	var identities int
	if err = f.sql.QueryRow(f.ctx, "SELECT count(*) FROM user_identities WHERE provider=$1 AND subject=$2", "social:"+app.ProviderID, "subject-"+provider.preferredEmail).Scan(&identities); err != nil || identities != 1 {
		t.Fatalf("Microsoft identities=%d err=%v", identities, err)
	}

	generic, err := f.db.CreateSocialApp(f.ctx, f.org.ID, store.SocialAppInput{
		ProviderID: "microsoft-generic", Provider: "openid_connect", Brand: "openid_connect", Name: "Generic",
		ClientID: provider.clientID, ClientSecret: sealed, ServerURL: idp.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.callbackSuffix = "/auth/social/" + generic.ProviderID + "/callback"
	genericClient := &http.Client{Jar: mustSocialJar(t)}
	response, err = genericClient.Get(srv.URL + "/good/auth/social/" + generic.ProviderID + "/start")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Request.URL.Query().Get("authError") != "failed" {
		t.Fatalf("generic OIDC accepted Microsoft unverified claims: %s", response.Request.URL)
	}
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
