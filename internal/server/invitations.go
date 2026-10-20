package server

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/store"
)

const invitationLifetime = 7 * 24 * time.Hour

func validInvitationToken(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *Server) invitations(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Invitations(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (s *Server) createInvitation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		failure(w, 400, "Invite a valid email address")
		return
	}
	if input.Role != "owner" && input.Role != "admin" && input.Role != "member" {
		failure(w, 400, "Invitation role must be owner, admin or member")
		return
	}
	if s.mail == nil || s.account == nil {
		failure(w, 503, "Invitation email is unavailable")
		return
	}
	token, err := token()
	if err != nil {
		failure(w, 503, "Invitation is temporarily unavailable")
		return
	}
	hash := s.hash(token)
	ciphertext, err := s.account.Seal(hash, token)
	if err != nil {
		failure(w, 503, "Invitation is temporarily unavailable")
		return
	}
	item, err := s.db.InviteMember(r.Context(), current(r).User.ID, r.PathValue("org"), email, input.Role, hash, ciphertext, invitationLifetime)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) cancelInvitation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("invitation")
	if len(id) != 32 {
		failure(w, 400, "Invalid invitation ID")
		return
	}
	if _, err := hex.DecodeString(id); err != nil {
		failure(w, 400, "Invalid invitation ID")
		return
	}
	if err := s.db.CancelInvitation(r.Context(), current(r).User.ID, r.PathValue("org"), id); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) inspectInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if !validInvitationToken(input.Token) {
		failure(w, 400, "Invalid or expired invitation")
		return
	}
	item, err := s.db.InspectInvitation(r.Context(), s.hash(input.Token))
	if errors.Is(err, store.ErrInvalidInvitation) {
		failure(w, 400, "Invalid or expired invitation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if !validInvitationToken(input.Token) || len(input.Password) < 12 || len(input.Password) > 72 || !utf8.ValidString(input.Password) {
		failure(w, 400, "Invalid invitation acceptance")
		return
	}
	inspection, err := s.db.InspectInvitation(r.Context(), s.hash(input.Token))
	if errors.Is(err, store.ErrInvalidInvitation) {
		failure(w, 400, "Invalid or expired invitation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	var user store.User
	var org store.Organization
	if inspection.ExistingAccount {
		user, err = s.db.UserByEmail(r.Context(), inspection.Email)
		if errors.Is(err, store.ErrNotFound) || err == nil && bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(input.Password)) != nil {
			failure(w, 401, "Invalid invitation credentials")
			return
		}
		if err != nil {
			report(w, err)
			return
		}
		user, org, err = s.db.AcceptInvitation(r.Context(), s.hash(input.Token), user.ID, user.AuthGeneration, "", nil)
	} else {
		name := strings.TrimSpace(input.Name)
		if name == "" || !credentialText(name, 120) {
			failure(w, 400, "Your name must contain 1 to 120 characters without controls")
			return
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			failure(w, 500, "Could not secure account")
			return
		}
		user, org, err = s.db.AcceptInvitation(r.Context(), s.hash(input.Token), "", 0, name, hash)
	}
	if errors.Is(err, store.ErrInvalidInvitation) {
		failure(w, 400, "Invalid or expired invitation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	_ = org
	s.startSession(w, r, user, 200)
}
