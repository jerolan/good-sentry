package server

import (
	"errors"
	"net/http"
	"strconv"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleIssueRoutes(mux *routeMux) {
	for _, root := range []string{"/api/0/organizations/{org}/issues/", "/api/0/projects/{org}/{project}/issues/"} {
		mux.Handle("GET "+root+"{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleIssues))
	}
	for _, root := range []string{"/api/0/issues/{issue}/", "/api/0/organizations/{org}/issues/{issue}/"} {
		mux.Handle("GET "+root+"{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleIssue))
		mux.Handle("GET "+root+"comments/{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleIssueComments))
		mux.Handle("POST "+root+"comments/{$}", s.compatibleAny([]string{"event:write"}, s.compatibleAddIssueComment))
		mux.Handle("PUT "+root+"comments/{comment}/{$}", s.compatibleAny([]string{"event:write"}, s.compatibleEditIssueComment))
		mux.Handle("DELETE "+root+"comments/{comment}/{$}", s.compatibleAny([]string{"event:write"}, s.compatibleDeleteIssueComment))
		mux.Handle("GET "+root+"tags/{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleIssueTags))
	}
	mux.Handle("GET /api/0/organizations/{org}/issues-stats/{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleIssueStats))
}

func reportCompatibleIssue(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidCompatibleIssueFilter) {
		failure(w, 400, "Invalid issue reference or filters")
		return
	}
	report(w, err)
}

func (s *Server) compatibleIssues(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	filter, query, ok := compatibleIssueListQuery(w, r)
	if !ok {
		return
	}
	records, err := s.db.CompatibleIssues(r.Context(), access, r.PathValue("project"), filter)
	if err != nil {
		reportCompatibleIssue(w, err)
		return
	}
	next := len(records) > filter.Limit
	if next {
		records = records[:filter.Limit]
	}
	result := make([]compatibleIssueView, 0, len(records))
	for _, record := range records {
		item, err := projectCompatibleIssue(record, s.cfg.Domain, false)
		if err != nil {
			report(w, err)
			return
		}
		result = append(result, item)
	}
	s.compatibleOffsetLinks(w, r, query, filter.Offset, filter.Limit, next)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleIssue(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	filter, ok := compatibleIssueDetailQuery(w, r)
	if !ok {
		return
	}
	record, err := s.db.CompatibleIssue(r.Context(), access, r.PathValue("issue"), filter)
	if err != nil {
		reportCompatibleIssue(w, err)
		return
	}
	result, err := projectCompatibleIssue(record, s.cfg.Domain, true)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) compatibleIssueStats(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	ids, period, ok := compatibleIssueStatsQuery(w, r)
	if !ok {
		return
	}
	records, err := s.db.CompatibleIssueStats(r.Context(), access, ids, period)
	if err != nil {
		reportCompatibleIssue(w, err)
		return
	}
	result := make([]compatibleIssueStatsView, 0, len(records))
	for _, record := range records {
		result = append(result, compatibleIssueStatsView{ID: record.ID, Count: strconv.FormatInt(record.Count, 10), UserCount: record.UserCount, FirstSeen: record.FirstSeen.UTC(), LastSeen: record.LastSeen.UTC(), IsUnhandled: record.IsUnhandled, Stats: issueWireStats(record.Stats)})
	}
	writeJSON(w, 200, result)
}
