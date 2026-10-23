//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
