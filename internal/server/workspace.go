package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

func (s *Server) dsn(p store.Project) string {
	u, _ := url.Parse(s.cfg.Domain)
	u.User = url.User(p.PublicKey)
	u.Path = strings.TrimRight(u.Path, "/") + "/" + p.ID
	return u.String()
}
func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Projects(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	for i := range items {
		items[i].DSN = s.dsn(items[i])
		items[i].PublicKey = ""
	}
	writeJSON(w, 200, map[string]any{"data": items})
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Platform string }
	if !bodyJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Platform = strings.TrimSpace(in.Platform)
	if in.Name == "" || len(in.Name) > 120 || len(in.Platform) > 80 {
		failure(w, 400, "Project name is required and must be at most 120 bytes")
		return
	}
	if in.Platform == "" {
		in.Platform = "other"
	}
	p, err := s.db.CreateProject(r.Context(), current(r).User.ID, r.PathValue("org"), in.Name, in.Platform)
	if err != nil {
		report(w, err)
		return
	}
	p.DSN = s.dsn(p)
	p.PublicKey = ""
	writeJSON(w, 201, p)
}

func (s *Server) nativeProjectWriteAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	org, err := s.db.Organization(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return store.ManagementAccess{}, false
	}
	if org.Role != "owner" && org.Role != "admin" {
		failure(w, http.StatusForbidden, "Organization owner or admin permission required")
		return store.ManagementAccess{}, false
	}
	return store.ManagementAccess{UserID: current(r).User.ID, OrganizationID: org.ID, Role: org.Role, AllProjects: true}, true
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	access, valid := s.nativeProjectWriteAccess(w, r)
	if !valid {
		return
	}
	var input struct{ Name, Platform string }
	if !bodyJSON(w, r, &input) {
		return
	}
	project, err := s.db.UpdateManagementProject(r.Context(), access, r.PathValue("project"), input.Name, input.Platform)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	access, valid := s.nativeProjectWriteAccess(w, r)
	if !valid {
		return
	}
	if err := s.db.DeleteManagementProject(r.Context(), access, r.PathValue("project")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
func (s *Server) issues(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Issue query encoding is invalid")
		return
	}
	for name, values := range q {
		switch name {
		case "project", "query", "status", "category", "environment", "release", "sort", "page":
		default:
			failure(w, 400, "Unknown issue query parameter")
			return
		}
		if len(values) != 1 {
			failure(w, 400, "Issue query parameters must occur once")
			return
		}
	}
	page := 1
	if q.Has("page") {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 || page > 1000000 || strconv.Itoa(page) != q.Get("page") {
			failure(w, 400, "Page must be a canonical decimal integer from 1 to 1000000")
			return
		}
	}
	f := store.IssueFilter{Project: q.Get("project"), Query: q.Get("query"), Status: q.Get("status"), Category: q.Get("category"), Environment: q.Get("environment"), Release: q.Get("release"), Sort: q.Get("sort"), Page: page}
	items, total, err := s.db.Issues(r.Context(), current(r).User.ID, r.PathValue("org"), f)
	if err != nil {
		if errors.Is(err, store.ErrInvalidIssueFilter) {
			failure(w, 400, err.Error())
		} else {
			report(w, err)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "total": total, "pageSize": 25, "page": page})
}
func (s *Server) issue(w http.ResponseWriter, r *http.Request) {
	detail, err := s.db.IssueDetail(r.Context(), current(r).User.ID, r.PathValue("issue"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, detail)
}
