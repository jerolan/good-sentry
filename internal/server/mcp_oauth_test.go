//go:build integration

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goodsentry/internal/config"
)

func oauthForm(t *testing.T, client *http.Client, target string, values url.Values) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

func oauthJSON(t *testing.T, client *http.Client, target, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, data
}

func TestMCPOAuthAuthorizationCodeAndRefresh(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{MCP: true, Logs: true, Uptime: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, f.db))
	t.Cleanup(srv.Close)
	f.server = srv
	base := srv.URL + "/good"
	publicBase := strings.TrimRight(cfg.Domain, "/")
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		want := publicBase + "/oauth/authorize"
		if strings.Contains(path, "protected-resource") {
			want = publicBase + "/mcp"
		}
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Fatalf("metadata %s: %d %s", path, res.StatusCode, body)
		}
	}
	redirect := base + "/oauth/callback"
	res, body := oauthJSON(t, f.client, base+"/oauth/register", `{"client_name":"MCP acceptance","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d %s", res.StatusCode, body)
	}
	var client struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(body, &client); err != nil || client.ClientID == "" {
		t.Fatalf("client: %s %v", body, err)
	}
	for _, bad := range []string{
		`{"client_name":"bad","redirect_uris":["http://attacker.example/callback"],"token_endpoint_auth_method":"none"}`,
		`{"client_name":"bad","redirect_uris":["` + redirect + `"],"token_endpoint_auth_method":"client_secret_basic"}`,
		`{"client_name":"bad","redirect_uris":["` + redirect + `"],"token_endpoint_auth_method":"none","unknown":true}`,
	} {
		res, _ = oauthJSON(t, f.client, base+"/oauth/register", bad)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("unsafe registration accepted: %d", res.StatusCode)
		}
	}
	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	state := "state-acceptance"
	scope := "org:read project:read event:read log:read"
	authorize := url.Values{"client_id": {client.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {state}, "resource": {publicBase + "/mcp"}, "scope": {scope}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	noRedirect := &http.Client{Jar: f.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	loginGet, err := fresh.Get(base + "/oauth/authorize?" + authorize.Encode())
	if err != nil {
		t.Fatal(err)
	}
	loginBody, _ := io.ReadAll(loginGet.Body)
	loginGet.Body.Close()
	if loginGet.StatusCode != http.StatusOK || !bytes.Contains(loginBody, []byte("type=\"password\"")) {
		t.Fatalf("unauthenticated authorize did not render login: %d %s", loginGet.StatusCode, loginBody)
	}
	loginForm := url.Values{"next": {"/oauth/authorize?" + authorize.Encode()}, "email": {"owner@resolution.test"}, "password": {"resolution-http-password"}}
	loginPost, loginResponse := oauthForm(t, fresh, base+"/oauth/login", loginForm)
	if loginPost.StatusCode != http.StatusSeeOther {
		t.Fatalf("OAuth login: %d %s", loginPost.StatusCode, loginResponse)
	}
	loginConsent, err := fresh.Get(srv.URL + loginPost.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	loginConsent.Body.Close()
	if loginConsent.StatusCode != http.StatusOK {
		t.Fatalf("authorize after login=%d", loginConsent.StatusCode)
	}
	res, body = oauthJSON(t, f.client, base+"/oauth/authorize?"+authorize.Encode(), `{}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorize must reject JSON POST before form admission: %d %s", res.StatusCode, body)
	}
	get, err := f.client.Get(base + "/oauth/authorize?" + authorize.Encode())
	if err != nil {
		t.Fatal(err)
	}
	consent, _ := io.ReadAll(get.Body)
	get.Body.Close()
	if get.StatusCode != http.StatusOK || !bytes.Contains(consent, []byte("MCP acceptance")) || !bytes.Contains(consent, []byte("log:read")) {
		t.Fatalf("consent page: %d %s", get.StatusCode, consent)
	}
	form := url.Values{"client_id": {client.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {state}, "resource": {publicBase + "/mcp"}, "scope": {scope}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "csrf": {f.csrf}, "organization": {f.org.Slug}, "project": {f.project.ID}, "decision": {"approve"}}
	res, body = oauthForm(t, noRedirect, base+"/oauth/authorize", form)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("consent: %d %s", res.StatusCode, body)
	}
	callback, err := url.Parse(res.Header.Get("Location"))
	if err != nil || callback.Query().Get("state") != state || callback.Query().Get("code") == "" || callback.Query().Get("iss") != publicBase {
		t.Fatalf("callback: %q %v", res.Header.Get("Location"), err)
	}
	code := callback.Query().Get("code")
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
	res, body = oauthForm(t, noRedirect, base+"/oauth/token", tokenForm)
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("code exchange: %d %v %s", res.StatusCode, res.Header, body)
	}
	var issued struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &issued); err != nil || issued.AccessToken == "" || issued.RefreshToken == "" || issued.TokenType != "Bearer" || issued.ExpiresIn != 3600 {
		t.Fatalf("token response: %s %v", body, err)
	}
	clientSession := mcpClient(t, f, srv, issued.AccessToken, "2026-07-28")
	_ = mcpWorkspaceCall(t, f, clientSession, "list_organizations", map[string]any{})
	_ = mcpWorkspaceCall(t, f, clientSession, "list_projects", map[string]any{"organization": f.org.ID})
	_ = mcpWorkspaceCall(t, f, clientSession, "list_logs", map[string]any{"organization": f.org.ID})
	_ = mcpWorkspaceCall(t, f, clientSession, "list_transaction_groups", map[string]any{"organization": f.org.ID})
	_ = mcpWorkspaceCall(t, f, clientSession, "list_alerts", map[string]any{"organization": f.org.ID})
	_ = mcpWorkspaceCall(t, f, clientSession, "list_monitors", map[string]any{"organization": f.org.ID})
	// Code replay fails without issuing a second grant.
	res, _ = oauthForm(t, noRedirect, base+"/oauth/token", tokenForm)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorization code replay accepted: %d", res.StatusCode)
	}
	refreshForm := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ClientID}, "refresh_token": {issued.RefreshToken}}
	res, body = oauthForm(t, noRedirect, base+"/oauth/token", refreshForm)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("refresh: %d %s", res.StatusCode, body)
	}
	var rotated struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &rotated); err != nil || rotated.AccessToken == "" || rotated.RefreshToken == "" || rotated.AccessToken == issued.AccessToken || rotated.RefreshToken == issued.RefreshToken {
		t.Fatalf("rotated token: %s %v", body, err)
	}
	if _, err := clientSession.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("old access token survived rotation: %v", err)
	}
	res, _ = oauthForm(t, noRedirect, base+"/oauth/token", refreshForm)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh replay accepted: %d", res.StatusCode)
	}
	rotatedSession := mcpClient(t, f, srv, rotated.AccessToken, "2026-07-28")
	_ = mcpWorkspaceCall(t, f, rotatedSession, "list_organizations", map[string]any{})
	if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rotatedSession.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("OAuth access survived membership removal: %v", err)
	}
	removedRefresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ClientID}, "refresh_token": {rotated.RefreshToken}}
	res, _ = oauthForm(t, noRedirect, base+"/oauth/token", removedRefresh)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh survived membership removal: %d", res.StatusCode)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	// Membership removal cascaded the native access/refresh rows. A fresh consent
	// grant proves revocation independently once membership is restored.
	res, body = oauthForm(t, noRedirect, base+"/oauth/authorize", form)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("second consent: %d %s", res.StatusCode, body)
	}
	secondCallback, _ := url.Parse(res.Header.Get("Location"))
	secondForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {redirect}, "code": {secondCallback.Query().Get("code")}, "code_verifier": {verifier}}
	res, body = oauthForm(t, noRedirect, base+"/oauth/token", secondForm)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second code exchange: %d %s", res.StatusCode, body)
	}
	var secondIssued struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &secondIssued); err != nil || secondIssued.AccessToken == "" {
		t.Fatal(err)
	}
	secondSession := mcpClient(t, f, srv, secondIssued.AccessToken, "2026-07-28")
	_ = mcpWorkspaceCall(t, f, secondSession, "list_organizations", map[string]any{})
	tokens, err := f.db.APITokens(f.ctx, f.owner.ID, f.org.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	var accessID string
	for _, token := range tokens.Data {
		if token.Name == "OAuth MCP acceptance" && token.RevokedAt == nil {
			accessID = token.ID
		}
	}
	if accessID == "" {
		t.Fatal("OAuth native token row missing")
	}
	f.request(t, f.client, http.MethodDelete, "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+accessID, "", http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusNoContent)
	res, _ = oauthForm(t, noRedirect, base+"/oauth/revoke", url.Values{"client_id": {client.ClientID}, "token": {secondIssued.RefreshToken}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("OAuth refresh revoke: %d", res.StatusCode)
	}
	if _, err := secondSession.ListTools(f.ctx, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("revoked access token survived: %v", err)
	}
	badPKCE := url.Values{"client_id": {client.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "resource": {publicBase + "/mcp"}, "scope": {"org:read"}, "code_challenge": {"short"}, "code_challenge_method": {"S256"}}
	get, _ = noRedirect.Get(base + "/oauth/authorize?" + badPKCE.Encode())
	get.Body.Close()
	if get.StatusCode != http.StatusSeeOther || !strings.Contains(get.Header.Get("Location"), "error=invalid_request") {
		t.Fatalf("invalid PKCE: %d %q", get.StatusCode, get.Header.Get("Location"))
	}
	noLogs := cfg
	noLogs.Logs = false
	feature := httptest.NewServer(New(noLogs, f.db))
	defer feature.Close()
	res, err = http.Get(feature.URL + "/good/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if bytes.Contains(metadata, []byte("log:read")) {
		t.Fatalf("logs-disabled metadata advertised log:read: %s", metadata)
	}
	disabled := config.Config{Domain: cfg.Domain, Prefix: "/good", Secret: cfg.Secret, MaxBodyBytes: cfg.MaxBodyBytes}
	off := httptest.NewServer(New(disabled, f.db))
	defer off.Close()
	res, err = http.Get(off.URL + "/good/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled OAuth metadata=%d", res.StatusCode)
	}
}
