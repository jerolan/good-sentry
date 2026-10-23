package server

import "net/http"

func (s *Server) compatibleIssueTags(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compatibleIssueVisible(w, r); !ok {
		return
	}
	query, ok := credentialQuery(w, r)
	if !ok {
		return
	}
	if !discoveryQueryKeys(w, query, "key") {
		return
	}
	tags, err := s.db.IssueTags(r.Context(), current(r).User.ID, r.PathValue("issue"))
	if err != nil {
		report(w, err)
		return
	}
	if query.Has("key") {
		key := query.Get("key")
		filtered := tags[:0]
		for _, tag := range tags {
			if tag.Key == key {
				filtered = append(filtered, tag)
			}
		}
		tags = filtered
	}
	writeJSON(w, 200, tags)
}
