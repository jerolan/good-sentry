package server

import (
	"errors"
	"html/template"
	"net/http"
	"time"

	"goodsentry/internal/store"
	"goodsentry/internal/uptime"
)

var publicStatusTemplate = template.Must(template.New("public-status").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Name}} · Status</title><link rel="stylesheet" href="{{.Base}}/app.css"></head>
<body><main class="main"><div class="page-header"><div><p class="eyebrow">Service status</p><h1>{{.Name}}</h1><p class="muted">Last updated {{.Updated}}</p></div><span class="badge">{{.Overall}}</span></div>
{{if .Monitors}}<section class="panel"><div class="panel-header"><h2>Services</h2></div><div class="table-wrap"><table><thead><tr><th>Service</th><th>Status</th><th>Last checked</th></tr></thead><tbody>
{{range .Monitors}}<tr><td><strong>{{.Name}}</strong></td><td><span class="badge">{{.Status}}</span></td><td>{{.LastCheck}}</td></tr>{{end}}
</tbody></table></div></section>{{else}}<section class="panel"><div class="panel-body"><p class="muted">No services are published on this status page.</p></div></section>{{end}}
<p class="small muted">This page exposes only monitor names and current availability.</p></main></body></html>`))

type publicStatusMonitor struct {
	Name      string
	Status    string
	LastCheck string
}

type publicStatusView struct {
	Base, Name, Overall, Updated string
	Monitors                     []publicStatusMonitor
}

func (s *Server) registerStatusPageRoutes(mux *routeMux) {
	if !s.cfg.Uptime {
		return
	}
	mux.Handle("GET /api/v1/organizations/{org}/status-pages", s.auth(http.HandlerFunc(s.statusPages)))
	mux.Handle("POST /api/v1/organizations/{org}/status-pages", s.auth(http.HandlerFunc(s.saveStatusPage)))
	mux.Handle("PUT /api/v1/organizations/{org}/status-pages/{page}", s.auth(http.HandlerFunc(s.saveStatusPage)))
	mux.Handle("DELETE /api/v1/organizations/{org}/status-pages/{page}", s.auth(http.HandlerFunc(s.deleteStatusPage)))
	mux.HandleFunc("GET /api/v1/public/status-pages/{slug}", s.publicStatusPageJSON)
	mux.HandleFunc("GET /status/{slug}", s.publicStatusPage)
	mux.Handle("GET /api/0/organizations/{org}/status-pages/{$}", s.compatible("org:read", s.compatibleStatusPages))
	mux.Handle("POST /api/0/organizations/{org}/status-pages/{$}", s.compatible("org:write", s.compatibleCreateStatusPage))
}

func (s *Server) statusPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.db.StatusPages(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": pages})
}

func (s *Server) saveStatusPage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name       string   `json:"name"`
		IsPublic   bool     `json:"isPublic"`
		MonitorIDs []string `json:"monitorIds"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if len(input.MonitorIDs) > 100 {
		failure(w, http.StatusBadRequest, "A status page can contain at most 100 monitors")
		return
	}
	var page store.StatusPage
	var err error
	if r.Method == http.MethodPut {
		page, err = s.db.UpdateStatusPage(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("page"), input.Name, input.IsPublic, input.MonitorIDs)
	} else {
		page, err = s.db.CreateStatusPage(r.Context(), current(r).User.ID, r.PathValue("org"), input.Name, input.IsPublic, input.MonitorIDs)
	}
	if errors.Is(err, store.ErrInvalidStatusPage) {
		failure(w, http.StatusBadRequest, "Status page name must contain 1 to 200 characters without controls")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		failure(w, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		status = http.StatusOK
	}
	writeJSON(w, status, page)
}

func (s *Server) deleteStatusPage(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteStatusPage(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("page")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) compatibleStatusPages(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	pages, err := s.db.StatusPages(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	wire := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		wire = append(wire, compatibleStatusPage(page))
	}
	writeJSON(w, http.StatusOK, wire)
}

func (s *Server) compatibleCreateStatusPage(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	var input struct {
		Name     string `json:"name"`
		IsPublic bool   `json:"isPublic"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	page, err := s.db.CreateStatusPage(r.Context(), current(r).User.ID, r.PathValue("org"), input.Name, input.IsPublic, nil)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStatusPage) {
			failure(w, http.StatusBadRequest, "Status page name must contain 1 to 200 characters without controls")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, compatibleStatusPage(page))
}

func (s *Server) publicStatusPageJSON(w http.ResponseWriter, r *http.Request) {
	page, err := s.db.PublicStatusPage(r.Context(), r.PathValue("slug"))
	if err != nil {
		report(w, err)
		return
	}
	monitors := make([]map[string]any, 0, len(page.Monitors))
	for _, monitor := range page.Monitors {
		monitors = append(monitors, map[string]any{
			"id": monitor.ID, "name": monitor.Name, "monitorType": monitor.Type,
			"status": monitor.Status, "isUp": monitor.Status == uptime.Up,
			"lastCheck": monitor.LastCheck, "lastChange": monitor.LastChange,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": page.Name, "slug": page.Slug, "isPublic": true, "monitors": monitors,
	})
}

func (s *Server) publicStatusPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.db.PublicStatusPage(r.Context(), r.PathValue("slug"))
	if err != nil {
		report(w, err)
		return
	}
	view := publicStatusView{Base: s.cfg.Prefix, Name: page.Name, Overall: "unknown", Updated: time.Now().UTC().Format(time.RFC3339)}
	if len(page.Monitors) == 0 {
		view.Overall = "unknown"
	} else {
		view.Overall = "up"
	}
	for _, monitor := range page.Monitors {
		status := string(monitor.Status)
		if status == "" {
			status = "unknown"
		}
		if status == "down" {
			view.Overall = "down"
		} else if status == "unknown" && view.Overall == "up" {
			view.Overall = "unknown"
		}
		lastCheck := "Not checked yet"
		if monitor.LastCheck != nil {
			lastCheck = monitor.LastCheck.UTC().Format(time.RFC3339)
		}
		view.Monitors = append(view.Monitors, publicStatusMonitor{Name: monitor.Name, Status: status, LastCheck: lastCheck})
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := publicStatusTemplate.Execute(w, view); err != nil {
		report(w, err)
	}
}

func compatibleStatusPage(page store.StatusPage) map[string]any {
	monitors := make([]map[string]any, 0, len(page.Monitors))
	for _, monitor := range page.Monitors {
		monitors = append(monitors, map[string]any{
			"id": monitor.ID, "name": monitor.Name, "monitorType": monitor.Type,
			"url": monitor.URL, "interval": monitor.IntervalSeconds,
			"timeout": monitor.TimeoutSeconds, "expectedStatus": monitor.ExpectedStatus,
			"expectedBody": monitor.ExpectedBody, "isUp": monitor.Status == uptime.Up,
			"lastChange": monitor.LastChange, "checks": []any{},
		})
	}
	return map[string]any{
		"isPublic": page.IsPublic, "name": page.Name, "slug": page.Slug, "monitors": monitors,
	}
}
