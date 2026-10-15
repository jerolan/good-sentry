package server

import (
	"encoding/json"
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
func (s *Server) issues(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Issue query encoding is invalid")
		return
	}
	for name, values := range q {
		switch name {
		case "project", "query", "status", "environment", "release", "sort", "page":
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
	f := store.IssueFilter{Project: q.Get("project"), Query: q.Get("query"), Status: q.Get("status"), Environment: q.Get("environment"), Release: q.Get("release"), Sort: q.Get("sort"), Page: page}
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
	uid := current(r).User.ID
	id := r.PathValue("issue")
	issue, err := s.db.Issue(r.Context(), uid, id)
	if err != nil {
		report(w, err)
		return
	}
	events, err := s.db.Events(r.Context(), uid, id)
	if err != nil {
		report(w, err)
		return
	}
	comments, err := s.db.Comments(r.Context(), uid, id)
	if err != nil {
		report(w, err)
		return
	}
	raw := json.RawMessage(`{}`)
	if len(events) > 0 {
		raw = events[0].Data
	}
	writeJSON(w, 200, map[string]any{"issue": issue, "event": raw, "events": events, "comments": comments})
}
func (s *Server) triage(w http.ResponseWriter, r *http.Request) {
	var in struct{ Status string }
	if !bodyJSON(w, r, &in) {
		return
	}
	if in.Status != "unresolved" && in.Status != "resolved" && in.Status != "ignored" {
		failure(w, 400, "Status must be unresolved, resolved or ignored")
		return
	}
	item, err := s.db.SetIssueStatus(r.Context(), current(r).User.ID, r.PathValue("issue"), in.Status)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	var in struct{ Text string }
	if !bodyJSON(w, r, &in) {
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len(in.Text) > 10000 {
		failure(w, 400, "Comment must contain 1 to 10000 bytes")
		return
	}
	comment, err := s.db.AddComment(r.Context(), current(r).User.ID, r.PathValue("issue"), in.Text)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, comment)
}
