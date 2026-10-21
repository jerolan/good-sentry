package server

import (
	"net/http"
	"strings"

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
	user := current(r).User
	writeJSON(w, 200, []map[string]any{{"email": user.Email, "isPrimary": true, "isVerified": true}})
}

func (s *Server) compatibleUserEmailUnsupported(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	failure(w, 400, "Additional email verification is not implemented")
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
