package server

import (
	"errors"
	"net/http"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/store"
)

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if len(input.CurrentPassword) < 1 || len(input.CurrentPassword) > 72 || len(input.NewPassword) < 12 || len(input.NewPassword) > 72 || !utf8.ValidString(input.NewPassword) {
		failure(w, 400, "Provide your current password and a new password of 12 to 72 bytes")
		return
	}
	admitted := current(r)
	u, err := s.db.UserByID(r.Context(), admitted.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		failure(w, 401, "Session expired")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	if u.AuthGeneration != admitted.User.AuthGeneration {
		failure(w, 401, "Session expired")
		return
	}
	if bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(input.CurrentPassword)) != nil {
		failure(w, 400, "Current password is incorrect")
		return
	}
	replacement, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		failure(w, 500, "Could not secure account")
		return
	}
	if err = s.db.ChangePassword(r.Context(), admitted.User.ID, admitted.User.AuthGeneration, replacement); err != nil {
		if errors.Is(err, store.ErrCredentialsChanged) {
			failure(w, 401, "Session expired")
			return
		}
		report(w, err)
		return
	}
	s.clearSessionCookie(w)
	writeJSON(w, 204, nil)
}
