package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"goodsentry/internal/store"
	"goodsentry/internal/uptime"
)

func (s *Server) monitorURL(m *uptime.Monitor) {
	if m.Type == uptime.Heartbeat {
		m.HeartbeatURL = s.cfg.Domain + "/api/v1/heartbeat/" + m.HeartbeatKey
	}
}

func (s *Server) monitors(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Monitors(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	for i := range items {
		s.monitorURL(&items[i])
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (s *Server) monitor(w http.ResponseWriter, r *http.Request) {
	m, err := s.db.Monitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"))
	if err != nil {
		report(w, err)
		return
	}
	s.monitorURL(&m)
	writeJSON(w, 200, m)
}

func (s *Server) saveMonitor(w http.ResponseWriter, r *http.Request) {
	in := uptime.Defaults()
	if !bodyJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	var m uptime.Monitor
	var err error
	status := 201
	if r.Method == http.MethodPut {
		m, err = s.db.UpdateMonitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"), in)
		status = 200
	} else {
		m, err = s.db.CreateMonitor(r.Context(), current(r).User.ID, r.PathValue("org"), in)
	}
	if errors.Is(err, store.ErrConflict) {
		failure(w, 409, "Monitor type cannot change after creation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	s.monitorURL(&m)
	writeJSON(w, status, m)
}

func (s *Server) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteMonitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) monitorChecks(w http.ResponseWriter, r *http.Request) {
	changes, page := true, 1
	var err error
	if raw := r.URL.Query().Get("changes"); raw != "" {
		changes, err = strconv.ParseBool(raw)
		if err != nil {
			failure(w, 400, "Changes must be true or false")
			return
		}
	}
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 1000000 {
			failure(w, 400, "Page must be between 1 and 1000000")
			return
		}
	}
	items, more, err := s.db.MonitorChecks(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"), changes, page)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "hasMore": more, "pageSize": 25})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	// The URL capability grants only a bounded heartbeat write. It never uses
	// the browser session or acts as a management credential.
	if _, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1024)); err != nil {
		failure(w, 413, "Heartbeat body must be at most 1024 bytes")
		return
	}
	check, err := s.db.Heartbeat(r.Context(), r.PathValue("key"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, check)
}
