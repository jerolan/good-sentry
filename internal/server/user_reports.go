package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

func (s *Server) registerUserReportRoutes(mux *routeMux) {
	mux.Handle("GET /api/0/projects/{org}/{project}/user-feedback/{$}", s.compatible("project:read", s.compatibleUserReports))
	mux.HandleFunc("POST /api/0/projects/{org}/{project}/user-feedback/{$}", s.submitUserReport)
	mux.HandleFunc("OPTIONS /api/0/projects/{org}/{project}/user-feedback/{$}", s.preflight)
	for _, root := range []string{"/api/0/issues/{issue}/user-reports/", "/api/0/organizations/{org}/issues/{issue}/user-reports/"} {
		mux.Handle("GET "+root+"{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleUserReports))
	}
	mux.Handle("GET /api/v1/issues/{issue}/user-reports", s.auth(http.HandlerFunc(s.issueUserReports)))
}

func (s *Server) compatibleUserReports(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	q, ok := compatibleIssueQuery(w, r, []string{"cursor", "limit", "per_page"}, nil)
	if !ok {
		return
	}
	offset, limit := 0, 25
	if q.Has("limit") && q.Has("per_page") {
		failure(w, 400, "Use one report page size parameter")
		return
	}
	for _, key := range []string{"limit", "per_page"} {
		if q.Has(key) {
			value, valid := occurrenceNumber(q.Get(key), 100)
			if !valid {
				failure(w, 400, "Report page size must be a canonical integer from 1 to 100")
				return
			}
			limit = int(value)
		}
	}
	if cursor := q.Get("cursor"); cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 3 || parts[0] != "0" || parts[2] != "0" && parts[2] != "1" {
			failure(w, 400, "Report cursor is invalid")
			return
		}
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || value < 0 || value > maxProjectOffset || strconv.FormatInt(value, 10) != parts[1] {
			failure(w, 400, "Report cursor is invalid")
			return
		}
		offset = int(value)
	}
	reports, _, err := s.db.UserReports(r.Context(), access, compatibleEventTarget(r), offset, limit)
	if err != nil {
		reportCompatibleEvent(w, err)
		return
	}
	more := len(reports) > limit
	if more {
		reports = reports[:limit]
	}
	s.compatibleOffsetLinks(w, r, q, offset, limit, more)
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, reports)
}

func (s *Server) issueUserReports(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Invalid report page query")
		return
	}
	page := 1
	for key, values := range q {
		if key != "page" || len(values) != 1 {
			failure(w, 400, "Only one report page parameter is supported")
			return
		}
		page, err = strconv.Atoi(values[0])
		if err != nil || page < 1 || page > 1000000 || strconv.Itoa(page) != values[0] {
			failure(w, 400, "Report page must be a canonical integer from 1 to 1000000")
			return
		}
	}
	access := store.ManagementAccess{UserID: current(r).User.ID, AllProjects: true, ReadOnly: true}
	reports, total, err := s.db.UserReports(r.Context(), access, store.EventIssueTarget(r.PathValue("issue")), (page-1)*25, 25)
	if err != nil {
		reportCompatibleEvent(w, err)
		return
	}
	more := len(reports) > 25
	if more {
		reports = reports[:25]
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, map[string]any{"data": reports, "total": total, "page": page, "pageSize": 25, "hasMore": more})
}
