package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/store"
)

func accepted(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct{ Email string }
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		accepted(w)
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		accepted(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		accepted(w)
		return
	}
	if s.mail == nil || s.account == nil {
		failure(w, 503, "Password recovery email is unavailable")
		return
	}
	token, err := token()
	if err != nil {
		failure(w, 503, "Password recovery is temporarily unavailable")
		return
	}
	u, err := s.db.UserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		accepted(w)
		return
	}
	if err != nil {
		failure(w, 503, "Password recovery is temporarily unavailable")
		return
	}
	hash := s.hash(token)
	ciphertext, err := s.account.Seal(hash, token)
	if err != nil {
		failure(w, 503, "Password recovery is temporarily unavailable")
		return
	}
	if _, err = s.db.EnqueuePasswordReset(r.Context(), u.ID, u.AuthGeneration, u.Email, hash, ciphertext, 30*time.Minute); errors.Is(err, store.ErrCredentialsChanged) {
		accepted(w)
		return
	} else if err != nil {
		failure(w, 503, "Password recovery is temporarily unavailable")
		return
	}
	accepted(w)
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if len(input.Token) != 64 || input.Token != strings.ToLower(input.Token) || len(input.NewPassword) < 12 || len(input.NewPassword) > 72 || !utf8.ValidString(input.NewPassword) {
		failure(w, 400, "Invalid password reset request")
		return
	}
	if _, err := hex.DecodeString(input.Token); err != nil {
		failure(w, 400, "Invalid password reset request")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		failure(w, 500, "Could not secure account")
		return
	}
	if err = s.db.ResetPassword(r.Context(), s.hash(input.Token), hash); err != nil {
		if errors.Is(err, store.ErrInvalidResetToken) {
			failure(w, 400, "Invalid or expired password reset link")
			return
		}
		report(w, err)
		return
	}
	s.clearSessionCookie(w)
	writeJSON(w, 204, nil)
}
