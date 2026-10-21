package server

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

func singleCurrentUser(w http.ResponseWriter, r *http.Request) bool {
	reference := r.PathValue("user")
	if reference != "me" && reference != current(r).User.ID {
		failure(w, 404, "Not found")
		return false
	}
	return true
}

func (s *Server) compatibleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	var input struct {
		Name    *string                     `json:"name"`
		Options store.CompatibleUserOptions `json:"options"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	user, err := s.db.CompatibleUser(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	name := user.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	for _, value := range []*string{input.Options.Timezone, input.Options.Language, input.Options.PreferredTheme} {
		if value != nil && (len(*value) > 255 || strings.ContainsRune(*value, 0)) {
			failure(w, 400, "User option strings must contain at most 255 characters without NUL")
			return
		}
	}
	user, err = s.db.UpdateCompatibleUser(r.Context(), user.ID, name, input.Options)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, user)
}

func (s *Server) compatibleUserEmails(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	emails, err := s.db.CompatibleUserEmails(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, emails)
}

func compatibleEmailInput(w http.ResponseWriter, r *http.Request) (string, bool) {
	var input struct {
		Email string `json:"email"`
	}
	if !bodyJSON(w, r, &input) {
		return "", false
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		failure(w, 400, "Enter a valid email address")
		return "", false
	}
	return email, true
}

func (s *Server) sendEmailVerification(r *http.Request, user store.User, email, token, hash string) error {
	if s.mail == nil || s.account == nil {
		return errors.New("email verification is unavailable")
	}
	expires := time.Now().Add(24 * time.Hour)
	verifyURL := strings.TrimRight(s.cfg.Domain, "/") + "/account/emails/confirm?token=" + url.QueryEscape(token)
	outcome, err := s.mail.DeliverEmailVerificationMail(r.Context(), notify.EmailVerificationMail{
		ID: hash[:32], Name: user.Name, Recipient: email, VerifyURL: verifyURL, ExpiresAt: expires,
	})
	if err != nil || outcome.Disposition != notify.Delivered {
		if err != nil {
			return err
		}
		return errors.New("email verification delivery failed: " + outcome.Reason)
	}
	return nil
}

func (s *Server) compatibleCreateUserEmail(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	email, valid := compatibleEmailInput(w, r)
	if !valid {
		return
	}
	if s.mail == nil || s.account == nil {
		failure(w, 503, "Email verification is unavailable")
		return
	}
	token, err := token()
	if err != nil {
		failure(w, 503, "Email verification is unavailable")
		return
	}
	hash := s.hash(token)
	ciphertext, err := s.account.SealEmailVerification(hash, token)
	if err != nil {
		failure(w, 503, "Email verification is unavailable")
		return
	}
	created, err := s.db.CreateCompatibleUserEmail(r.Context(), current(r).User.ID, email, hash, ciphertext, time.Now().Add(24*time.Hour))
	if errors.Is(err, store.ErrInvalidEmail) {
		failure(w, 400, "Enter a valid email address")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		failure(w, 400, "Email is already in use")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	if err = s.sendEmailVerification(r, current(r).User, email, token, hash); err != nil {
		failure(w, 503, "Email verification delivery failed")
		return
	}
	writeJSON(w, 201, created)
}

func (s *Server) compatibleSetPrimaryUserEmail(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	email, valid := compatibleEmailInput(w, r)
	if !valid {
		return
	}
	updated, err := s.db.SetCompatiblePrimaryEmail(r.Context(), current(r).User.ID, email)
	if errors.Is(err, store.ErrInvalidEmail) {
		failure(w, 400, "Enter a valid email address")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		failure(w, 400, "Primary email must be an existing verified email")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, updated)
}

func (s *Server) compatibleDeleteUserEmail(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	email, valid := compatibleEmailInput(w, r)
	if !valid {
		return
	}
	err := s.db.DeleteCompatibleUserEmail(r.Context(), current(r).User.ID, email)
	if errors.Is(err, store.ErrForbidden) {
		failure(w, 400, "The primary email cannot be removed")
		return
	}
	if errors.Is(err, store.ErrInvalidEmail) {
		failure(w, 400, "Enter a valid email address")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleConfirmUserEmail(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	email, valid := compatibleEmailInput(w, r)
	if !valid {
		return
	}
	if s.mail == nil || s.account == nil {
		failure(w, 503, "Email verification is unavailable")
		return
	}
	hash, ciphertext, _, err := s.db.CompatibleUserEmailVerification(r.Context(), current(r).User.ID, email)
	if err != nil {
		report(w, err)
		return
	}
	token, err := s.account.OpenEmailVerification(hash, ciphertext)
	if err != nil {
		failure(w, 503, "Email verification is unavailable")
		return
	}
	if err = s.sendEmailVerification(r, current(r).User, email, token, hash); err != nil {
		failure(w, 503, "Email verification delivery failed")
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) confirmUserEmailLink(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if len(token) != 64 || token != strings.ToLower(token) {
		failure(w, 400, "Invalid or expired email verification link")
		return
	}
	if _, err := hex.DecodeString(token); err != nil {
		failure(w, 400, "Invalid or expired email verification link")
		return
	}
	if _, _, err := s.db.ConfirmCompatibleUserEmail(r.Context(), s.hash(token)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 400, "Invalid or expired email verification link")
		} else {
			report(w, err)
		}
		return
	}
	http.Redirect(w, r, s.cfg.Prefix+"/?emailVerified=1", http.StatusSeeOther)
}

func (s *Server) compatibleUserNotifications(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	user, err := s.db.CompatibleUser(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"subscribeByDefault": user.SubscribeByDefault})
}

func (s *Server) compatibleUpdateUserNotifications(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	var input struct {
		SubscribeByDefault *bool `json:"subscribeByDefault"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	user, err := s.db.CompatibleUser(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	value := user.SubscribeByDefault
	if input.SubscribeByDefault != nil {
		value = *input.SubscribeByDefault
	}
	value, err = s.db.UpdateUserNotifications(r.Context(), user.ID, value)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"subscribeByDefault": value})
}
