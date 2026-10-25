package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleIssueWriteRoutes(mux *routeMux) {
	s.registerCompatibleIssueBulkRoutes(mux)
	s.registerCompatibleIssueHashRoutes(mux)
	for _, path := range []string{"/api/0/issues/{issue}/", "/api/0/organizations/{org}/issues/{issue}/"} {
		mux.Handle("PUT "+path+"{$}", s.compatibleAny([]string{"event:write", "event:admin"}, s.compatibleIssueUpdate))
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
	// Retain the credential's organization and project grant through Store's
	// access gate. The handler applies the narrower event:write rule to triage.
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
	fields, ok := issueBody(w, r, "discard", "status", "assignedTo", "statusDetails", "ignoreDuration", "ignoreCount", "ignoreUserCount", "ignoreWindow", "ignoreUserWindow")
	if !ok {
		return
	}
	discard := false
	if raw, present := fields["discard"]; present {
		if err := json.Unmarshal(raw, &discard); err != nil {
			failure(w, 400, "discard must be a boolean")
			return
		}
		if discard {
			if len(fields) != 1 {
				failure(w, 400, "discard cannot be combined with other issue changes")
				return
			}
			if grant, bearer := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); bearer && !slices.Contains(grant.Scopes, "event:write") && !slices.Contains(grant.Scopes, "event:admin") {
				failure(w, 403, "Credential requires event:write or event:admin for discard")
				return
			}
			if err := s.db.DiscardCompatibleIssue(r.Context(), access, r.PathValue("issue")); err != nil {
				reportCompatibleIssueChange(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		delete(fields, "discard")
	}
	if grant, bearer := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); bearer && !discard && !slices.Contains(grant.Scopes, "event:write") {
		failure(w, 403, "Credential requires event:write for issue changes")
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
