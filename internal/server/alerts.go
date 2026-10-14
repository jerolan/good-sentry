package server

import (
	"errors"
	"net/http"
	"strconv"

	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	rules, err := s.db.Alerts(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": rules})
}

func (s *Server) alert(w http.ResponseWriter, r *http.Request) {
	rule, err := s.db.Alert(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), r.PathValue("rule"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, rule)
}

func (s *Server) saveAlert(w http.ResponseWriter, r *http.Request) {
	c := notify.Defaults()
	if !bodyJSON(w, r, &c) {
		return
	}
	if err := c.Validate(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	var rule notify.Rule
	var err error
	status := 201
	if r.Method == http.MethodPut {
		rule, err = s.db.UpdateAlert(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), r.PathValue("rule"), c)
		status = 200
	} else {
		rule, err = s.db.CreateAlert(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), c)
	}
	if errors.Is(err, store.ErrConflict) {
		failure(w, 409, "Alert kind cannot change after creation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, status, rule)
}

func (s *Server) deleteAlert(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteAlert(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), r.PathValue("rule")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.TestAlert(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), r.PathValue("rule"))
	if errors.Is(err, store.ErrConflict) {
		failure(w, 409, "Enable this alert before queuing a test")
		return
	}
	if errors.Is(err, store.ErrNoRecipients) {
		failure(w, 400, "No current project team members match this alert")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 202, map[string]int{"queued": count})
}

func (s *Server) alertDeliveries(w http.ResponseWriter, r *http.Request) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 1000000 {
			failure(w, 400, "Page must be between 1 and 1000000")
			return
		}
	}
	items, more, err := s.db.AlertDeliveries(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), r.URL.Query().Get("rule"), page)
	if err != nil {
		report(w, err)
		return
	}
	for i := range items {
		items[i].Payload = items[i].Payload.Public(s.cfg.Domain)
	}
	writeJSON(w, 200, map[string]any{"data": items, "hasMore": more, "pageSize": 25})
}

func (s *Server) alertDelivery(w http.ResponseWriter, r *http.Request) {
	delivery, attempts, err := s.db.AlertDelivery(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), r.PathValue("delivery"))
	if err != nil {
		report(w, err)
		return
	}
	delivery.Payload = delivery.Payload.Public(s.cfg.Domain)
	writeJSON(w, 200, map[string]any{"delivery": delivery, "attempts": attempts})
}
