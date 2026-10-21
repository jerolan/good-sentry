package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/store"
)

const (
	// loginChallengeLifetime keeps a password-verified second-factor step short.
	loginChallengeLifetime = 5 * time.Minute
	// loginChallengeAttempts bounds guessing against one challenge.
	loginChallengeAttempts = 5
	recoveryCodeHashPrefix = "totp-recovery:"
)

func (s *Server) registerTwoFactorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/login/two-factor", s.loginTwoFactor)
	mux.Handle("GET /api/v1/me/two-factor", s.auth(http.HandlerFunc(s.twoFactorState)))
	mux.Handle("POST /api/v1/me/two-factor", s.auth(http.HandlerFunc(s.startTwoFactor)))
	mux.Handle("POST /api/v1/me/two-factor/confirm", s.auth(http.HandlerFunc(s.confirmTwoFactor)))
	mux.Handle("DELETE /api/v1/me/two-factor", s.auth(http.HandlerFunc(s.disableTwoFactor)))
	mux.Handle("POST /api/v1/me/two-factor/recovery-codes", s.auth(http.HandlerFunc(s.regenerateRecoveryCodes)))
}

// requireSecondFactor answers a password-verified login with a single-use
// challenge instead of a session when the account has a confirmed secret.
func (s *Server) requireSecondFactor(w http.ResponseWriter, r *http.Request, u store.User) bool {
	state, err := s.db.TwoFactorState(r.Context(), u.ID)
	if err != nil {
		report(w, err)
		return false
	}
	if !state.Confirmed {
		return true
	}
	challenge, err := token()
	if err != nil {
		failure(w, 503, "Cannot start the second-factor step; retry shortly")
		return false
	}
	if _, err = s.db.CreateLoginChallenge(r.Context(), u.ID, u.AuthGeneration, s.hash(challenge), loginChallengeLifetime); err != nil {
		report(w, err)
		return false
	}
	writeJSON(w, 200, map[string]any{"twoFactorRequired": true, "challengeToken": challenge, "methods": []string{"totp", "recovery"}})
	return false
}

func (s *Server) loginTwoFactor(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct{ ChallengeToken, Code string }
	if !bodyJSON(w, r, &input) {
		return
	}
	input.Code = strings.TrimSpace(strings.ToLower(input.Code))
	if !validChallengeToken(input.ChallengeToken) || len(input.Code) > 32 {
		failure(w, 401, "Invalid or expired two-factor challenge")
		return
	}
	rejected := func() { failure(w, 401, "Invalid or expired two-factor challenge") }
	challenge, err := s.db.LoginChallenge(r.Context(), s.hash(input.ChallengeToken))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			rejected()
			return
		}
		report(w, err)
		return
	}
	if challenge.Consumed || challenge.Attempts >= loginChallengeAttempts || !time.Now().Before(challenge.ExpiresAt) {
		rejected()
		return
	}
	state, err := s.db.TwoFactorState(r.Context(), challenge.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			rejected()
			return
		}
		report(w, err)
		return
	}
	secret, err := s.totp.open(state.Secret)
	if err != nil || !state.Confirmed {
		rejected()
		return
	}
	var user store.User
	step, matched := matchTOTPStep(secret, input.Code, time.Now())
	switch {
	case matched:
		user, err = s.db.CompleteTOTPChallenge(r.Context(), s.hash(input.ChallengeToken), challenge.UserID, challenge.AuthGeneration, step)
	case validRecoveryCode(input.Code):
		user, err = s.db.CompleteRecoveryChallenge(r.Context(), s.hash(input.ChallengeToken), challenge.UserID, challenge.AuthGeneration, s.hash(recoveryCodeHashPrefix+input.Code))
	default:
		err = store.ErrNotFound
	}
	if err != nil {
		if !errors.Is(err, store.ErrTwoFactorReplay) && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrCredentialsChanged) {
			report(w, err)
			return
		}
		if failErr := s.db.FailLoginChallenge(r.Context(), s.hash(input.ChallengeToken), loginChallengeAttempts); failErr != nil && !errors.Is(failErr, store.ErrNotFound) {
			report(w, failErr)
			return
		}
		rejected()
		return
	}
	s.startSession(w, r, user, 200)
}

func validChallengeToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if c := value[index]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) twoFactorState(w http.ResponseWriter, r *http.Request) {
	state, err := s.db.TwoFactorState(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"enabled":                state.Confirmed,
		"pending":                len(state.Secret) > 0 && !state.Confirmed,
		"recoveryCodesRemaining": state.RecoveryCodesRemaining,
	})
}

func (s *Server) startTwoFactor(w http.ResponseWriter, r *http.Request) {
	admitted := current(r)
	raw, secret, err := newTOTPSecret()
	if err != nil {
		report(w, err)
		return
	}
	sealed, err := s.totp.seal(raw)
	if err != nil {
		report(w, err)
		return
	}
	err = s.db.StartTwoFactor(r.Context(), admitted.User.ID, admitted.User.AuthGeneration, sealed)
	switch {
	case errors.Is(err, store.ErrCredentialsChanged):
		failure(w, 401, "Session expired")
		return
	case errors.Is(err, store.ErrConflict):
		failure(w, 409, "Disable two-factor authentication before enrolling again")
		return
	case err != nil:
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"secret": secret, "otpauthUrl": totpURL(admitted.User.Email, secret)})
}

func (s *Server) confirmTwoFactor(w http.ResponseWriter, r *http.Request) {
	var input struct{ Code string }
	if !bodyJSON(w, r, &input) {
		return
	}
	admitted := current(r)
	state, err := s.db.TwoFactorState(r.Context(), admitted.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	if state.Confirmed {
		failure(w, 409, "Two-factor authentication is already enabled")
		return
	}
	secret, err := s.totp.open(state.Secret)
	if err != nil {
		failure(w, 400, "Start two-factor enrollment before confirming a code")
		return
	}
	step, matched := matchTOTPStep(secret, strings.TrimSpace(input.Code), time.Now())
	if !matched {
		failure(w, 400, "That code is not valid; check the time on your device and try again")
		return
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		report(w, err)
		return
	}
	hashes := make([]string, 0, len(codes))
	for _, code := range codes {
		hashes = append(hashes, s.hash(recoveryCodeHashPrefix+code))
	}
	err = s.db.ConfirmTwoFactor(r.Context(), admitted.User.ID, admitted.User.AuthGeneration, step, hashes)
	switch {
	case errors.Is(err, store.ErrCredentialsChanged):
		failure(w, 401, "Session expired")
		return
	case errors.Is(err, store.ErrTwoFactorUnavailable):
		failure(w, 409, "Two-factor enrollment changed; start again")
		return
	case err != nil:
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": true, "recoveryCodes": codes})
}

func (s *Server) disableTwoFactor(w http.ResponseWriter, r *http.Request) {
	var input struct{ Password string }
	if !bodyJSON(w, r, &input) {
		return
	}
	admitted := current(r)
	u, err := s.db.UserByID(r.Context(), admitted.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	if u.AuthGeneration != admitted.User.AuthGeneration {
		failure(w, 401, "Session expired")
		return
	}
	if bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(input.Password)) != nil {
		failure(w, 400, "Current password is incorrect")
		return
	}
	err = s.db.DisableTwoFactor(r.Context(), admitted.User.ID, admitted.User.AuthGeneration)
	if errors.Is(err, store.ErrCredentialsChanged) {
		failure(w, 401, "Session expired")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var input struct{ Password string }
	if !bodyJSON(w, r, &input) {
		return
	}
	admitted := current(r)
	u, err := s.db.UserByID(r.Context(), admitted.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	if u.AuthGeneration != admitted.User.AuthGeneration {
		failure(w, 401, "Session expired")
		return
	}
	if bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(input.Password)) != nil {
		failure(w, 400, "Current password is incorrect")
		return
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		report(w, err)
		return
	}
	hashes := make([]string, 0, len(codes))
	for _, code := range codes {
		hashes = append(hashes, s.hash(recoveryCodeHashPrefix+code))
	}
	err = s.db.ReplaceRecoveryCodes(r.Context(), admitted.User.ID, admitted.User.AuthGeneration, hashes)
	switch {
	case errors.Is(err, store.ErrCredentialsChanged):
		failure(w, 401, "Session expired")
		return
	case errors.Is(err, store.ErrTwoFactorUnavailable):
		failure(w, 409, "Two-factor authentication is not enabled")
		return
	case err != nil:
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"recoveryCodes": codes})
}
