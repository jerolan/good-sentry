package server

import "net/http"

func (s *Server) compatibleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !singleCurrentUser(w, r) {
		return
	}
	if err := s.db.DeleteCompatibleUser(r.Context(), current(r).User.ID); err != nil {
		report(w, err)
		return
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusNoContent, nil)
}
