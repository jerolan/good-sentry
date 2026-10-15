package server

import (
	"errors"
	"net/http"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleIssueDeleteRoutes(mux *http.ServeMux) {
	for _, path := range []string{"/api/0/issues/{issue}/", "/api/0/organizations/{org}/issues/{issue}/"} {
		mux.Handle("DELETE "+path+"{$}", s.compatible("event:admin", s.compatibleIssueDelete))
	}
	for _, path := range []string{"/api/0/organizations/{org}/issues/", "/api/0/projects/{org}/{project}/issues/"} {
		mux.Handle("DELETE "+path+"{$}", s.compatible("event:admin", s.compatibleIssueBulkDelete))
	}
}

func reportCompatibleIssueDelete(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidCompatibleIssueFilter) {
		reportCompatibleIssue(w, err)
		return
	}
	reportCompatibleIssueChange(w, err)
}

func (s *Server) compatibleIssueDelete(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	// The event:admin middleware admitted this deletion and retained its grants.
	access.ReadOnly = false
	if !issueTargetID(r.PathValue("issue")) {
		failure(w, 400, "Issue IDs must be canonical positive decimal int64 strings")
		return
	}
	query, ok := credentialQuery(w, r)
	if !ok {
		return
	}
	if len(query) != 0 {
		failure(w, 400, "Individual issue deletion does not accept query parameters")
		return
	}
	if !compatibleIssueDeleteBody(w, r) {
		return
	}
	if err := s.db.DeleteCompatibleIssue(r.Context(), access, r.PathValue("issue")); err != nil {
		reportCompatibleIssueDelete(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compatibleIssueBulkDelete(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	access.ReadOnly = false
	filter, ok := compatibleIssueDeleteQuery(w, r)
	if !ok {
		return
	}
	if !compatibleIssueDeleteBody(w, r) {
		return
	}
	if err := s.db.DeleteCompatibleIssues(r.Context(), access, r.PathValue("project"), filter); err != nil {
		reportCompatibleIssueDelete(w, err)
		return
	}
	if r.PathValue("project") != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
