//go:build integration

package server

import (
	"crypto/rand"
	"crypto/rsa"
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
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"

	"goodsentry/internal/config"
)

type oidcTestProvider struct {
	core         *oidctest.Server
	key          *rsa.PrivateKey
	clientID     string
	clientSecret string
	email        string
	name         string
	issuer       string
	mu           sync.Mutex
	nonce        string
	challenge    string
}

func (p *oidcTestProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth":
		if r.URL.Query().Get("response_type") != "code" || r.URL.Query().Get("client_id") != p.clientID ||
			r.URL.Query().Get("code_challenge_method") != "S256" || r.URL.Query().Get("code_challenge") == "" {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		redirect := r.URL.Query().Get("redirect_uri")
		target, err := url.Parse(redirect)
		if err != nil || target.Scheme != "http" || !strings.HasSuffix(target.Path, "/auth/oidc/callback") {
			http.Error(w, "invalid redirect", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		p.nonce, p.challenge = r.URL.Query().Get("nonce"), r.URL.Query().Get("code_challenge")
		p.mu.Unlock()
		query := target.Query()
		query.Set("code", "test-code")
		query.Set("state", r.URL.Query().Get("state"))
		target.RawQuery = query.Encode()
		http.Redirect(w, r, target.String(), http.StatusFound)
	case "/token":
		clientID, clientSecret, basic := r.BasicAuth()
		if !basic || clientID != p.clientID || clientSecret != p.clientSecret {
			http.Error(w, "invalid client", http.StatusUnauthorized)
			return
		}
		if r.FormValue("code") != "test-code" || r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "invalid grant", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		nonce, challenge := p.nonce, p.challenge
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if challenge == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, "invalid PKCE verifier", http.StatusBadRequest)
			return
		}
		claims := map[string]any{
			"iss": p.issuer, "aud": p.clientID, "sub": "subject-" + p.email, "nonce": nonce,
			"email": p.email, "email_verified": true, "name": p.name,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}
		raw, _ := json.Marshal(claims)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600,
			"id_token": oidctest.SignIDToken(p.key, "test-key", oidc.RS256, string(raw)),
		})
	default:
		p.core.ServeHTTP(w, r)
	}
}

func newOIDCTestProvider(t *testing.T, email, name string) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	provider := &oidcTestProvider{
		core: &oidctest.Server{PublicKeys: []oidctest.PublicKey{{PublicKey: key.Public(), KeyID: "test-key", Algorithm: oidc.RS256}}},
		key:  key, clientID: "goodsentry-test", clientSecret: "oidc-client-secret",
		email: email, name: name,
	}
	server := httptest.NewServer(provider)
	provider.issuer = server.URL
	provider.core.SetIssuer(server.URL)
	t.Cleanup(server.Close)
	return server
}

func TestOIDCPKCEAccountLinkAndSession(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	provider := newOIDCTestProvider(t, f.owner.Email, "OIDC Owner")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Domain: "http://" + listener.Addr().String() + "/good", Prefix: "/good",
		Secret: strings.Repeat("o", 64), MaxBodyBytes: 1 << 20,
		OIDCIssuer: provider.URL, OIDCClientID: "goodsentry-test", OIDCClientSecret: "oidc-client-secret",
		OIDCName: "Test identity", OIDCScopes: []string{"openid", "profile", "email"}, OIDCAutoCreate: false,
	}
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
	response, err := client.Get(srv.URL + "/good/api/v1/auth/config")
	if err != nil {
		t.Fatal(err)
	}
	var public struct {
		OIDC struct {
			Enabled bool   `json:"enabled"`
			Name    string `json:"name"`
		} `json:"oidc"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&public) != nil || !public.OIDC.Enabled || public.OIDC.Name != "Test identity" {
		response.Body.Close()
		t.Fatalf("OIDC public config: status=%d %+v", response.StatusCode, public)
	}
	response.Body.Close()

	response, err = client.Get(srv.URL + "/good/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Request.URL.Path != "/good/" {
		t.Fatalf("OIDC browser flow ended at %s status=%d", response.Request.URL, response.StatusCode)
	}
	response, err = client.Get(srv.URL + "/good/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		CSRF    string          `json:"csrfToken"`
		Feature map[string]bool `json:"features"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&session) != nil || session.User.ID != f.owner.ID ||
		session.User.Email != f.owner.Email || session.CSRF == "" || !session.Feature["oidc"] {
		t.Fatalf("OIDC session: status=%d %+v", response.StatusCode, session)
	}
	var identities int
	if err := f.sql.QueryRow(f.ctx, "SELECT count(*) FROM user_identities WHERE user_id=$1::bigint AND provider=$2", f.owner.ID, provider.URL).Scan(&identities); err != nil || identities != 1 {
		t.Fatalf("OIDC identities=%d err=%v", identities, err)
	}

	unstarted := httptest.NewServer(New(cfg, f.db))
	defer unstarted.Close()
	bad, err := http.Get(unstarted.URL + "/good/auth/oidc/callback?code=test-code&state=tampered")
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.Request.URL.Query().Get("authError") != "failed" {
		t.Fatalf("tampered callback ended at %s", bad.Request.URL)
	}
}

func TestOIDCDisabledPublicConfiguration(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	handler := httptest.NewServer(New(config.Config{Domain: "http://example.test", Prefix: "", Secret: strings.Repeat("d", 64)}, f.db))
	defer handler.Close()
	response, err := http.Get(handler.URL + "/api/v1/auth/config")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value struct {
		OIDC struct {
			Enabled bool `json:"enabled"`
		} `json:"oidc"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&value) != nil || value.OIDC.Enabled {
		t.Fatalf("disabled OIDC config: %+v", value)
	}
}
