//go:build integration

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"goodsentry/internal/config"
)

// HTTP owns the wire contract: the challenge response shape, cookie-free
// second-factor admission, the attempt budget, CSRF/origin separation and the
// enrollment endpoints. Storage owns replay, recovery and generation semantics.
type twoFactorHTTPState struct {
	Enabled                bool `json:"enabled"`
	Pending                bool `json:"pending"`
	RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
}

type twoFactorHTTPChallenge struct {
	TwoFactorRequired bool     `json:"twoFactorRequired"`
	ChallengeToken    string   `json:"challengeToken"`
	Methods           []string `json:"methods"`
}

type twoFactorHTTPEnrollment struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
}

type twoFactorHTTPConfirmation struct {
	Enabled       bool     `json:"enabled"`
	RecoveryCodes []string `json:"recoveryCodes"`
}

type twoFactorHTTPCodes struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

const twoFactorLoginBody = `{"email":"owner@resolution.test","password":"resolution-http-password"}`

func decodeTwoFactor(t *testing.T, raw []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// twoFactorCode returns the code for an offset inside the accepted window, so a
// test can use a step the account has not consumed yet.
func twoFactorCode(t *testing.T, secret string, offset int64) string {
	t.Helper()
	raw, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(raw, totpStep(time.Now())+offset)
}

func twoFactorUnusedCode(t *testing.T, secret string) string {
	t.Helper()
	raw, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	step := totpStep(time.Now())
	used := map[string]bool{}
	for delta := int64(-totpWindow); delta <= totpWindow; delta++ {
		used[totpCode(raw, step+delta)] = true
	}
	for candidate := range 1_000_000 {
		code := fmt.Sprintf("%06d", candidate)
		if !used[code] {
			return code
		}
	}
	t.Fatal("no unused code is available")
	return ""
}

func twoFactorFreshClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// twoFactorChallenge signs in with a cookie-free client and returns the pending
// challenge, proving the password step alone grants no session.
func twoFactorChallenge(t *testing.T, f *resolutionHTTPFixture) (*http.Client, twoFactorHTTPChallenge) {
	t.Helper()
	client := twoFactorFreshClient(t)
	var challenge twoFactorHTTPChallenge
	decodeTwoFactor(t, f.request(t, client, "POST", "/api/v1/login", twoFactorLoginBody, nil, 200), &challenge)
	if !challenge.TwoFactorRequired || len(challenge.ChallengeToken) != 64 || len(challenge.Methods) != 2 {
		t.Fatalf("challenge=%+v", challenge)
	}
	f.request(t, client, "GET", "/api/v1/me", "", nil, 401)
	return client, challenge
}

func TestTwoFactorEnrollmentAndLoginHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	var state twoFactorHTTPState
	decodeTwoFactor(t, f.request(t, f.client, "GET", "/api/v1/me/two-factor", "", nil, 200), &state)
	if state.Enabled || state.Pending || state.RecoveryCodesRemaining != 0 {
		t.Fatalf("initial state=%+v", state)
	}
	f.request(t, http.DefaultClient, "GET", "/api/v1/me/two-factor", "", nil, 401)
	f.request(t, f.client, "POST", "/api/v1/me/two-factor", "", nil, 403)
	f.request(t, f.client, "POST", "/api/v1/me/two-factor", "", http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, 403)
	var enrollment twoFactorHTTPEnrollment
	decodeTwoFactor(t, f.request(t, f.client, "POST", "/api/v1/me/two-factor", "", csrf, 200), &enrollment)
	if len(enrollment.Secret) != 32 || !strings.Contains(enrollment.OtpauthURL, "secret="+enrollment.Secret) || !strings.Contains(enrollment.OtpauthURL, "otpauth://totp/") {
		t.Fatalf("enrollment=%+v", enrollment)
	}
	decodeTwoFactor(t, f.request(t, f.client, "GET", "/api/v1/me/two-factor", "", nil, 200), &state)
	if state.Enabled || !state.Pending {
		t.Fatalf("pending state=%+v", state)
	}
	f.request(t, f.client, "POST", "/api/v1/me/two-factor/confirm", resolutionJSON(t, map[string]string{"code": twoFactorUnusedCode(t, enrollment.Secret)}), csrf, 400)
	var confirmation twoFactorHTTPConfirmation
	decodeTwoFactor(t, f.request(t, f.client, "POST", "/api/v1/me/two-factor/confirm", resolutionJSON(t, map[string]string{"code": twoFactorCode(t, enrollment.Secret, 0)}), csrf, 200), &confirmation)
	if !confirmation.Enabled || len(confirmation.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("confirmation=%+v", confirmation)
	}
	decodeTwoFactor(t, f.request(t, f.client, "GET", "/api/v1/me/two-factor", "", nil, 200), &state)
	if !state.Enabled || state.Pending || state.RecoveryCodesRemaining != recoveryCodeCount {
		t.Fatalf("enabled state=%+v", state)
	}
	f.request(t, f.client, "POST", "/api/v1/me/two-factor", "", csrf, 409)

	// The enrollment code is spent, so a login must use a later accepted step.
	client, challenge := twoFactorChallenge(t, f)
	f.request(t, client, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": challenge.ChallengeToken, "code": twoFactorUnusedCode(t, enrollment.Secret)}), nil, 401)
	f.request(t, client, "GET", "/api/v1/me", "", nil, 401)
	f.request(t, client, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": challenge.ChallengeToken, "code": twoFactorCode(t, enrollment.Secret, 0)}), nil, 401)
	var me struct{ CSRFToken string }
	accepted := resolutionJSON(t, map[string]string{"challengeToken": challenge.ChallengeToken, "code": twoFactorCode(t, enrollment.Secret, 1)})
	decodeTwoFactor(t, f.request(t, client, "POST", "/api/v1/login/two-factor", accepted, nil, 200), &me)
	if me.CSRFToken == "" {
		t.Fatal("the second-factor login did not return a session")
	}
	f.request(t, client, "GET", "/api/v1/me", "", nil, 200)
	f.request(t, client, "POST", "/api/v1/login/two-factor", accepted, nil, 401)

	// Recovery codes complete one login each and then stop working.
	recoveryClient, recovery := twoFactorChallenge(t, f)
	f.request(t, recoveryClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": recovery.ChallengeToken, "code": strings.ToUpper(confirmation.RecoveryCodes[0])}), nil, 200)
	f.request(t, recoveryClient, "GET", "/api/v1/me", "", nil, 200)
	reuseClient, reuse := twoFactorChallenge(t, f)
	f.request(t, reuseClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": reuse.ChallengeToken, "code": confirmation.RecoveryCodes[0]}), nil, 401)
	f.request(t, reuseClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": reuse.ChallengeToken, "code": confirmation.RecoveryCodes[1]}), nil, 200)
	decodeTwoFactor(t, f.request(t, f.client, "GET", "/api/v1/me/two-factor", "", nil, 200), &state)
	if state.RecoveryCodesRemaining != recoveryCodeCount-2 {
		t.Fatalf("remaining after recovery logins=%+v", state)
	}

	// Five rejected codes destroy the challenge even when a later code is right.
	exhaustClient, exhausted := twoFactorChallenge(t, f)
	for range 5 {
		f.request(t, exhaustClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": exhausted.ChallengeToken, "code": twoFactorUnusedCode(t, enrollment.Secret)}), nil, 401)
	}
	f.request(t, exhaustClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": exhausted.ChallengeToken, "code": twoFactorCode(t, enrollment.Secret, 2)}), nil, 401)
	f.request(t, exhaustClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": strings.Repeat("0", 64), "code": "000000"}), nil, 401)
	f.request(t, exhaustClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": "short", "code": "000000"}), nil, 401)
	f.request(t, exhaustClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": strings.Repeat("0", 64), "code": "000000"}), http.Header{"Origin": {"https://foreign.example.test"}}, 403)

	// Regenerating codes requires the password and invalidates the old set.
	f.request(t, f.client, "POST", "/api/v1/me/two-factor/recovery-codes", resolutionJSON(t, map[string]string{"password": "wrong-password"}), csrf, 400)
	var regenerated twoFactorHTTPCodes
	decodeTwoFactor(t, f.request(t, f.client, "POST", "/api/v1/me/two-factor/recovery-codes", resolutionJSON(t, map[string]string{"password": "resolution-http-password"}), csrf, 200), &regenerated)
	if len(regenerated.RecoveryCodes) != recoveryCodeCount || regenerated.RecoveryCodes[0] == confirmation.RecoveryCodes[0] {
		t.Fatalf("regenerated codes=%+v", regenerated)
	}
	replacedClient, replaced := twoFactorChallenge(t, f)
	f.request(t, replacedClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": replaced.ChallengeToken, "code": confirmation.RecoveryCodes[2]}), nil, 401)
	f.request(t, replacedClient, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": replaced.ChallengeToken, "code": regenerated.RecoveryCodes[0]}), nil, 200)

	// Disabling requires the password and restores the single-step login.
	f.request(t, f.client, "DELETE", "/api/v1/me/two-factor", resolutionJSON(t, map[string]string{"password": "wrong-password"}), csrf, 400)
	f.request(t, f.client, "DELETE", "/api/v1/me/two-factor", resolutionJSON(t, map[string]string{"password": "resolution-http-password"}), csrf, 204)
	decodeTwoFactor(t, f.request(t, f.client, "GET", "/api/v1/me/two-factor", "", nil, 200), &state)
	if state.Enabled || state.Pending || state.RecoveryCodesRemaining != 0 {
		t.Fatalf("disabled state=%+v", state)
	}
	f.request(t, f.client, "POST", "/api/v1/me/two-factor/recovery-codes", resolutionJSON(t, map[string]string{"password": "resolution-http-password"}), csrf, 409)
	direct := twoFactorFreshClient(t)
	decodeTwoFactor(t, f.request(t, direct, "POST", "/api/v1/login", twoFactorLoginBody, nil, 200), &me)
	if me.CSRFToken == "" {
		t.Fatal("the password login did not return a session after disabling")
	}
	f.request(t, direct, "GET", "/api/v1/me", "", nil, 200)
}

func TestTwoFactorStateAvailableWithoutSessionInOpenMode(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	f.server.Close()
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20, AuthMode: "disabled"}
	owner := f.owner
	f.server = httptest.NewServer(NewWithAccountMailAndDefaultUser(cfg, f.db, nil, nil, &owner))
	t.Cleanup(f.server.Close)
	var state twoFactorHTTPState
	decodeTwoFactor(t, f.request(t, http.DefaultClient, "GET", "/api/v1/me/two-factor", "", nil, 200), &state)
	if state.Enabled || state.Pending {
		t.Fatalf("open mode reported an enabled second factor: %+v", state)
	}
}

func TestTwoFactorChallengeFailsAfterACredentialChange(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	var enrollment twoFactorHTTPEnrollment
	decodeTwoFactor(t, f.request(t, f.client, "POST", "/api/v1/me/two-factor", "", csrf, 200), &enrollment)
	f.request(t, f.client, "POST", "/api/v1/me/two-factor/confirm", resolutionJSON(t, map[string]string{"code": twoFactorCode(t, enrollment.Secret, 0)}), csrf, 200)
	client, challenge := twoFactorChallenge(t, f)
	if _, err := f.sql.Exec(f.ctx, "UPDATE users SET auth_generation=auth_generation+1 WHERE id=$1::bigint", f.owner.ID); err != nil {
		t.Fatal(err)
	}
	f.request(t, client, "POST", "/api/v1/login/two-factor", resolutionJSON(t, map[string]string{"challengeToken": challenge.ChallengeToken, "code": twoFactorCode(t, enrollment.Secret, 1)}), nil, 401)
	f.request(t, client, "GET", "/api/v1/me", "", nil, 401)
}

func TestOrganizationRequire2FAGatesSessionsAndBearerTokens(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	orgPath := "/api/0/organizations/" + f.org.Slug + "/"
	f.request(t, f.client, "PUT", orgPath, `{"require2FA":true}`, csrf, 409)

	var ownerEnrollment twoFactorHTTPEnrollment
	decodeTwoFactor(t, f.request(t, f.client, "POST", "/api/v1/me/two-factor", "", csrf, 200), &ownerEnrollment)
	decodeTwoFactor(t, f.request(t, f.client, "POST", "/api/v1/me/two-factor/confirm", resolutionJSON(t, map[string]string{"code": twoFactorCode(t, ownerEnrollment.Secret, 0)}), csrf, 200), &twoFactorHTTPConfirmation{})

	var memberID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name)
 SELECT 'required-member@resolution.test',password_hash,'Required member' FROM users WHERE id=$1::bigint RETURNING id::text`, f.owner.ID).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, f.org.ID, memberID); err != nil {
		t.Fatal(err)
	}
	member := twoFactorFreshClient(t)
	var memberMe struct{ CSRFToken string }
	decodeTwoFactor(t, f.request(t, member, "POST", "/api/v1/login", `{"email":"required-member@resolution.test","password":"resolution-http-password"}`, nil, 200), &memberMe)
	if memberMe.CSRFToken == "" {
		t.Fatal("member login returned no CSRF token")
	}
	var accountToken struct{ Token string }
	decodeTwoFactor(t, f.request(t, member, "POST", "/api/0/api-tokens/", `{"scopes":["org:read"],"label":"2FA gate"}`, http.Header{"X-CSRF-Token": {memberMe.CSRFToken}}, 201), &accountToken)
	if len(accountToken.Token) != 64 {
		t.Fatalf("account token=%+v", accountToken)
	}
	var mcpToken struct{ Token string }
	decodeTwoFactor(t, f.request(t, member, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", `{"name":"MCP 2FA gate","scopes":["org:read"],"projectIds":[]}`, http.Header{"X-CSRF-Token": {memberMe.CSRFToken}}, 201), &mcpToken)
	if mcpToken.Token == "" {
		t.Fatal("MCP token was not issued")
	}

	var secured struct {
		Require2FA bool `json:"require2FA"`
	}
	decodeTwoFactor(t, f.request(t, f.client, "PUT", orgPath, `{"require2FA":true}`, csrf, 200), &secured)
	if !secured.Require2FA {
		t.Fatal("organization did not report require2FA")
	}
	var memberSession struct {
		TwoFactorRequired bool `json:"twoFactorRequired"`
	}
	decodeTwoFactor(t, f.request(t, member, "GET", "/api/v1/me", "", nil, 200), &memberSession)
	if !memberSession.TwoFactorRequired {
		t.Fatalf("member /me did not require 2FA: %+v", memberSession)
	}
	f.request(t, member, "GET", "/api/v1/organizations/"+f.org.Slug+"/projects", "", nil, 403)
	f.request(t, http.DefaultClient, "GET", "/api/0/organizations/"+f.org.Slug+"/projects/", "", http.Header{"Authorization": {"Bearer " + accountToken.Token}}, 403)
	mcpServer := mcpHTTPServer(t, f, 1<<20)
	mcpConnection := mcpClient(t, f, mcpServer, mcpToken.Token, "2026-07-28")
	if _, err := mcpConnection.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_organizations", Arguments: map[string]any{}}); err == nil || !strings.Contains(err.Error(), "two-factor") {
		t.Fatalf("MCP accepted a 2FA-blocked credential: %v", err)
	}
	base := mcpServer.URL + "/good"
	redirect := base + "/oauth/callback"
	response, body := oauthJSON(t, member, base+"/oauth/register", `{"client_name":"2FA OAuth","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("OAuth registration status=%d", response.StatusCode)
	}
	var client struct {
		ID string `json:"client_id"`
	}
	if json.Unmarshal(body, &client) != nil || client.ID == "" {
		t.Fatal("OAuth client missing identity")
	}
	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	form := url.Values{"client_id": {client.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"2fa"}, "resource": {"https://public.example.test/good/mcp"}, "scope": {"org:read"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "csrf": {memberMe.CSRFToken}, "organization": {f.org.Slug}, "project": {f.project.ID}, "decision": {"approve"}}
	noRedirect := &http.Client{Jar: member.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get, err := noRedirect.Get(base + "/oauth/authorize?" + form.Encode())
	if err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if get.StatusCode != http.StatusSeeOther || get.Header.Get("Location") != "/good/" {
		t.Fatalf("OAuth consent page before enrollment: %d %q", get.StatusCode, get.Header.Get("Location"))
	}
	response, _ = oauthForm(t, noRedirect, base+"/oauth/authorize", form)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("OAuth consent before enrollment: %d", response.StatusCode)
	}
	var codes int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1`, client.ID).Scan(&codes); err != nil || codes != 0 {
		t.Fatalf("OAuth code issued before enrollment: %d %v", codes, err)
	}

	var memberEnrollment twoFactorHTTPEnrollment
	decodeTwoFactor(t, f.request(t, member, "POST", "/api/v1/me/two-factor", "", http.Header{"X-CSRF-Token": {memberMe.CSRFToken}}, 200), &memberEnrollment)
	decodeTwoFactor(t, f.request(t, member, "POST", "/api/v1/me/two-factor/confirm", resolutionJSON(t, map[string]string{"code": twoFactorCode(t, memberEnrollment.Secret, 0)}), http.Header{"X-CSRF-Token": {memberMe.CSRFToken}}, 200), &twoFactorHTTPConfirmation{})
	f.request(t, member, "GET", "/api/v1/organizations/"+f.org.Slug+"/projects", "", nil, 200)
	if _, err := mcpConnection.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_organizations", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("MCP remained blocked after enrollment: %v", err)
	}
	get, err = noRedirect.Get(base + "/oauth/authorize?" + form.Encode())
	if err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("OAuth consent page after enrollment: %d", get.StatusCode)
	}
	response, _ = oauthForm(t, noRedirect, base+"/oauth/authorize", form)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("OAuth consent after enrollment: %d", response.StatusCode)
	}
	callback, err := url.Parse(response.Header.Get("Location"))
	if err != nil || callback.Query().Get("code") == "" {
		t.Fatalf("OAuth callback missing code: %v", err)
	}
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "redirect_uri": {redirect}, "code": {callback.Query().Get("code")}, "code_verifier": {verifier}}
	response, body = oauthForm(t, noRedirect, base+"/oauth/token", tokenForm)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("OAuth exchange after enrollment: %d", response.StatusCode)
	}
	var oauthToken struct {
		Access string `json:"access_token"`
	}
	if json.Unmarshal(body, &oauthToken) != nil || oauthToken.Access == "" {
		t.Fatal("OAuth exchange missing access token")
	}
	oauthSession := mcpClient(t, f, mcpServer, oauthToken.Access, "2026-07-28")
	if _, err := oauthSession.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_organizations", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("OAuth tool after enrollment: %v", err)
	}
	f.request(t, member, "DELETE", "/api/v1/me/two-factor", resolutionJSON(t, map[string]string{"password": "resolution-http-password"}), http.Header{"X-CSRF-Token": {memberMe.CSRFToken}}, 409)
}
