package server

import (
	"errors"
	"net/http"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleIssueBulkRoutes(mux *routeMux) {
	for _, path := range []string{"/api/0/projects/{org}/{project}/issues/", "/api/0/organizations/{org}/issues/"} {
		mux.Handle("PUT "+path+"{$}", s.compatible("event:write", s.compatibleIssueBulkUpdate))
	}
}

func (s *Server) compatibleIssueBulkUpdate(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	access.ReadOnly = false
	filter, ok := compatibleIssueBulkQuery(w, r)
	if !ok {
		return
	}
	fields, ok := issueBody(w, r, "status", "assignedTo", "statusDetails", "ignoreDuration", "ignoreCount", "ignoreUserCount", "ignoreWindow", "ignoreUserWindow", "snoozeDuration", "merge")
	if !ok {
		return
	}
	change, err := parseCompatibleIssueBulkChange(fields)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	result, err := s.db.UpdateCompatibleIssues(r.Context(), access, r.PathValue("project"), filter, change)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCompatibleIssueFilter) {
			reportCompatibleIssue(w, err)
		} else {
			reportCompatibleIssueChange(w, err)
		}
		return
	}
	writeJSON(w, 200, compatibleIssueBulkAcknowledgement(result, change))
}

func compatibleIssueBulkAcknowledgement(result store.CompatibleIssueBulkResult, input store.CompatibleIssueBulkChange) map[string]any {
	ack := map[string]any{}
	i := result.First
	if i == nil {
		return ack
	}
	change := input.Change
	if change.Status != "" {
		ack["status"] = i.Status
	}
	if change.Assignment != nil {
		ack["assignedTo"] = i.AssignedTo
	}
	if change.Policy != nil || change.Status == "resolvedInNextRelease" {
		details := map[string]any{}
		if change.Status == "resolvedInNextRelease" {
			details["inNextRelease"] = true
		} else {
			switch policy := change.Policy; policy.Kind {
			case store.CompatibleIssueIgnorePolicy:
				minutes, count := int32(0), int32(0)
				users := int32(0)
				if policy.Minutes != nil {
					minutes = *policy.Minutes
				}
				if policy.Count != nil {
					count = *policy.Count
				}
				if policy.UserCount != nil {
					users = *policy.UserCount
				}
				details["ignoreDuration"], details["ignoreCount"], details["ignoreUserCount"] = minutes, count, users
				if policy.Window != nil {
					details["ignoreWindow"] = *policy.Window
				}
				if policy.UserWindow != nil {
					details["ignoreUserWindow"] = *policy.UserWindow
				}
			case store.CompatibleIssueReleasePolicy, store.CompatibleIssueLatestReleasePolicy:
				if i.Resolution != nil && i.Resolution.Release != nil {
					details["inRelease"] = string(i.Resolution.Release.Version)
				}
			case store.CompatibleIssueNextReleasePolicy:
				details["inNextRelease"] = true
			}
		}
		ack["statusDetails"] = details
	}
	if input.Merge && result.Merge != nil {
		ack["merge"] = result.Merge
	}
	return ack
}
