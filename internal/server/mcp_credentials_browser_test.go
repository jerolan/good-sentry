//go:build integration

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"goodsentry/internal/config"
)

func TestMCPCredentialInterfaceInBrowser(t *testing.T) {
	module, executable := os.Getenv("MCP_BROWSER_PLAYWRIGHT"), os.Getenv("MCP_BROWSER_CHROMIUM")
	if module == "" || executable == "" {
		t.Skip("dedicated browser verification requires MCP_BROWSER_PLAYWRIGHT and MCP_BROWSER_CHROMIUM")
	}
	f := newResolutionHTTPFixture(t)
	start := func(mcpEnabled, logsEnabled bool) (*httptest.Server, string) {
		t.Helper()
		srv := httptest.NewUnstartedServer(nil)
		base := "http://" + srv.Listener.Addr().String() + "/good"
		cfg := config.Config{MCP: mcpEnabled, Logs: logsEnabled, Domain: base, Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
		srv.Config.Handler = New(cfg, f.db)
		srv.Start()
		t.Cleanup(srv.Close)
		return srv, base
	}
	_, enabled := start(true, true)
	_, noLogs := start(true, false)
	_, disabled := start(false, true)
	redirect := enabled + "/oauth/callback"
	registration := fmt.Sprintf(`{"client_name":"Browser MCP client","redirect_uris":[%q],"token_endpoint_auth_method":"none"}`, redirect)
	response, data := oauthJSON(t, f.client, enabled+"/oauth/register", registration)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("client registration status=%d", response.StatusCode)
	}
	var client struct {
		ID string `json:"client_id"`
	}
	if json.Unmarshal(data, &client) != nil || client.ID == "" {
		t.Fatal("client registration missing identity")
	}
	verifier := strings.Repeat("b", 43)
	digest := sha256.Sum256([]byte(verifier))
	form := url.Values{"client_id": {client.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"browser"}, "resource": {enabled + "/mcp"}, "scope": {"org:read project:read log:read"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "csrf": {f.csrf}, "organization": {f.org.Slug}, "project": {f.project.ID}, "decision": {"approve"}}
	noRedirect := &http.Client{Jar: f.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, _ = oauthForm(t, noRedirect, enabled+"/oauth/authorize", form)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("OAuth consent status=%d", response.StatusCode)
	}
	callback, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "redirect_uri": {redirect}, "code": {callback.Query().Get("code")}, "code_verifier": {verifier}}
	response, data = oauthForm(t, noRedirect, enabled+"/oauth/token", tokenForm)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("OAuth token status=%d", response.StatusCode)
	}
	var issued struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if json.Unmarshal(data, &issued) != nil || issued.Access == "" || issued.Refresh == "" {
		t.Fatal("OAuth grant missing tokens")
	}
	command := exec.CommandContext(f.ctx, "node", "../../tests/compat/mcp_credentials_browser.mjs")
	command.Env = append(os.Environ(), "PLAYWRIGHT_MODULE="+module, "CHROMIUM_EXECUTABLE="+executable, "MCP_BROWSER_ENABLED="+enabled, "MCP_BROWSER_NO_LOGS="+noLogs, "MCP_BROWSER_DISABLED="+disabled)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("browser verification: %v\n%s", err, output)
	}
	request, err := http.NewRequest(http.MethodPost, enabled+"/mcp", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+issued.Access)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked access status=%d", response.StatusCode)
	}
	response, _ = oauthForm(t, noRedirect, enabled+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {issued.Refresh}})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("revoked refresh status=%d", response.StatusCode)
	}
}
