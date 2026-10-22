package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/eventview"
	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleEventRoutes(mux *routeMux) {
	for _, route := range []struct{ root, scope string }{
		{"/api/0/projects/{org}/{project}/events/", "project:read"},
		{"/api/0/issues/{issue}/events/", "event:read"},
		{"/api/0/organizations/{org}/issues/{issue}/events/", "event:read"},
	} {
		scopes := []string{route.scope}
		if route.scope == "event:read" {
			scopes = append(scopes, "event:write")
		}
		mux.Handle("GET "+route.root+"{$}", s.compatibleAny(scopes, s.compatibleEvents))
		mux.Handle("GET "+route.root+"{event}/{$}", s.compatibleAny(scopes, s.compatibleEvent))
	}
	mux.Handle("GET /api/0/organizations/{org}/issues/{issue}/events/{event}/json/{$}", s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleEventJSON))
}

func (s *Server) compatibleEventAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	if r.PathValue("org") != "" {
		return s.managementAccess(w, r)
	}
	access := store.ManagementAccess{UserID: current(r).User.ID, AllProjects: true, ReadOnly: true}
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok {
		access.OrganizationID = grant.OrganizationID
		access.AllProjects = grant.AllProjects
		access.ProjectIDs = grant.ProjectIDs
	}
	return access, true
}

func compatibleEventTarget(r *http.Request) store.CompatibleEventTarget {
	if project := r.PathValue("project"); project != "" {
		return store.EventProjectTarget(project)
	}
	return store.EventIssueTarget(r.PathValue("issue"))
}

func reportCompatibleEvent(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidEventFilter) {
		failure(w, 400, "Invalid event reference or filters")
		return
	}
	report(w, err)
}

func (s *Server) compatibleEvents(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	paging, ok := compatibleEventListQuery(w, r)
	if !ok {
		return
	}
	records, err := s.db.CompatibleEvents(r.Context(), access, compatibleEventTarget(r), paging.filter)
	if err != nil {
		reportCompatibleEvent(w, err)
		return
	}
	next := len(records) > paging.filter.Limit
	if next {
		records = records[:paging.filter.Limit]
	}
	result := make([]eventview.Event, 0, len(records))
	for _, record := range records {
		item, err := eventview.Project(record, paging.full)
		if err != nil {
			report(w, err)
			return
		}
		result = append(result, item)
	}
	s.compatibleOffsetLinks(w, r, paging.query, paging.filter.Offset, paging.filter.Limit, next)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleEvent(w http.ResponseWriter, r *http.Request) {
	s.readCompatibleEvent(w, r, false)
}

func (s *Server) compatibleEventJSON(w http.ResponseWriter, r *http.Request) {
	s.readCompatibleEvent(w, r, true)
}

func (s *Server) readCompatibleEvent(w http.ResponseWriter, r *http.Request, legacyJSON bool) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	_, environments, ok := compatibleEventQuery(w, r, false)
	if !ok {
		return
	}
	record, err := s.db.CompatibleEvent(r.Context(), access, compatibleEventTarget(r), r.PathValue("event"), environments)
	if err != nil {
		reportCompatibleEvent(w, err)
		return
	}
	if legacyJSON {
		result, err := eventview.LegacyJSON(record)
		if err != nil {
			report(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	result, err := eventview.Project(record, true)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) compatibleOffsetLinks(w http.ResponseWriter, r *http.Request, query url.Values, offset, limit int, next bool) {
	links := make([]string, 0, 2)
	for _, link := range []struct {
		relation  string
		offset    int
		previous  bool
		hasResult bool
	}{{"previous", max(0, offset-limit), true, offset > 0}, {"next", min(maxProjectOffset, offset+limit), false, next && offset+limit <= maxProjectOffset}} {
		direction := "0"
		if link.previous {
			direction = "1"
		}
		cursor := "0:" + strconv.Itoa(link.offset) + ":" + direction
		query.Set("cursor", cursor)
		public := strings.TrimRight(s.cfg.Domain, "/") + r.URL.EscapedPath() + "?" + query.Encode()
		links = append(links, fmt.Sprintf(`<%s>; rel="%s"; results="%t"; cursor="%s"`, public, link.relation, link.hasResult, cursor))
	}
	w.Header().Set("Link", strings.Join(links, ", "))
}
