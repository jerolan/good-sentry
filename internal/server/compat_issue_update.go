package server

import (
	"errors"
	"net/http"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleIssueWriteRoutes(mux *routeMux) {
	s.registerCompatibleIssueBulkRoutes(mux)
	s.registerCompatibleIssueHashRoutes(mux)
	for _, path := range []string{"/api/0/issues/{issue}/", "/api/0/organizations/{org}/issues/{issue}/"} {
		mux.Handle("PUT "+path+"{$}", s.compatible("event:write", s.compatibleIssueUpdate))
	}
}

func reportCompatibleIssueChange(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidCompatibleIssueChange) || errors.Is(err, store.ErrInvalidIssueChange) {
		failure(w, 400, err.Error())
		return
	}
	report(w, err)
}

func (s *Server) compatibleIssueUpdate(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	// The event:write middleware admitted this request. Retain its organization
	// and project grant while allowing this write through Store's access gate.
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
		failure(w, 400, "Individual issue changes do not accept query parameters")
		return
	}
	fields, ok := issueBody(w, r, "status", "assignedTo", "statusDetails", "ignoreDuration", "ignoreCount", "ignoreUserCount", "ignoreWindow")
	if !ok {
		return
	}
	change, err := parseCompatibleIssueChange(fields)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	record, err := s.db.UpdateCompatibleIssue(r.Context(), access, r.PathValue("issue"), change)
	if err != nil {
		reportCompatibleIssueChange(w, err)
		return
	}
	result, err := projectCompatibleIssue(record, s.cfg.Domain, true)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, result)
}
