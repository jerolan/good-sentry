package server

import "net/http"

func (s *Server) compatibleIssueTags(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compatibleIssueVisible(w, r); !ok {
		return
	}
	if _, ok := discoveryNoQuery(w, r); !ok {
		return
	}
	tags, err := s.db.IssueTags(r.Context(), current(r).User.ID, r.PathValue("issue"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, tags)
}
