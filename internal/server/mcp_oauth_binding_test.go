//go:build integration

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goodsentry/internal/config"
)

func TestOAuthCodeExchangeBindingAndVerifierAlphabet(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{MCP: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, f.db))
	defer srv.Close()
	base, redirect := srv.URL+"/good", srv.URL+"/good/oauth/callback"
	client := &http.Client{Jar: f.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	register := func(name string) string {
		t.Helper()
		response, body := oauthJSON(t, client, base+"/oauth/register", `{"client_name":"`+name+`","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("registration status=%d", response.StatusCode)
		}
		var value struct {
			ID string `json:"client_id"`
		}
		if json.Unmarshal(body, &value) != nil || value.ID == "" {
			t.Fatal("registration missing client ID")
		}
		return value.ID
	}
	id, otherID := register("binding client"), register("other binding client")
	authorize := func(verifier string) string {
		t.Helper()
		digest := sha256.Sum256([]byte(verifier))
		form := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"binding"}, "scope": {"org:read"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "csrf": {f.csrf}, "organization": {f.org.Slug}, "project": {f.project.ID}, "decision": {"approve"}}
		response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("authorization status=%d", response.StatusCode)
		}
		callback, err := url.Parse(response.Header.Get("Location"))
		if err != nil || callback.Query().Get("code") == "" {
			t.Fatalf("authorization missing code: %v", err)
		}
		return callback.Query().Get("code")
	}
	verifier := strings.Repeat("a", 43)
	code := authorize(verifier)
	for _, tc := range []struct{ name, client, redirect, verifier string }{
		{"wrong client", otherID, redirect, verifier},
		{"wrong redirect", id, redirect + "-other", verifier},
		{"wrong verifier", id, redirect, strings.Repeat("b", 43)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {tc.client}, "redirect_uri": {tc.redirect}, "code": {code}, "code_verifier": {tc.verifier}}
			response, _ := oauthForm(t, client, base+"/oauth/token", form)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("mismatched grant status=%d", response.StatusCode)
			}
		})
	}
	valid := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
	response, _ := oauthForm(t, client, base+"/oauth/token", valid)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("legitimate code consumed after mismatches: %d", response.StatusCode)
	}
	for _, tc := range []struct{ name, verifier string }{
		{"punctuation", strings.Repeat("a", 42) + "!"},
		{"padding", strings.Repeat("a", 42) + "="},
		{"short", strings.Repeat("a", 42)},
		{"long", strings.Repeat("a", 129)},
		{"non-ASCII", strings.Repeat("a", 41) + "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1 AND consumed_at IS NULL`, id).Scan(&before); err != nil {
				t.Fatal(err)
			}
			invalidCode := authorize(tc.verifier)
			invalidForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {invalidCode}, "code_verifier": {tc.verifier}}
			response, body := oauthForm(t, client, base+"/oauth/token", invalidForm)
			var problem struct {
				Error string `json:"error"`
			}
			if response.StatusCode != http.StatusBadRequest || response.Header.Get("Cache-Control") != "no-store" || json.Unmarshal(body, &problem) != nil || problem.Error != "invalid_grant" {
				t.Fatalf("invalid verifier status=%d error=%q", response.StatusCode, problem.Error)
			}
			var after int
			if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1 AND consumed_at IS NULL`, id).Scan(&after); err != nil || after != before+1 {
				t.Fatalf("invalid verifier consumed a code: %d -> %d: %v", before, after, err)
			}
		})
	}
	maximum := strings.Repeat("-_", 64)
	maxForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {authorize(maximum)}, "code_verifier": {maximum}}
	response, _ = oauthForm(t, client, base+"/oauth/token", maxForm)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid maximum verifier rejected: %d", response.StatusCode)
	}
}
