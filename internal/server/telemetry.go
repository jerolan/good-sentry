package server

import (
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

func (s *Server) spans(w http.ResponseWriter, r *http.Request) {
	project, trace := r.URL.Query().Get("project"), r.URL.Query().Get("trace")
	_, err := hex.DecodeString(trace)
	if project == "" || len(trace) != 32 || strings.ToLower(trace) != trace || err != nil {
		failure(w, 400, "A project and a 32-character hexadecimal trace ID are required")
		return
	}
	items, err := s.db.Spans(r.Context(), current(r).User.ID, r.PathValue("org"), project, trace)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "limit": 1000})
}

func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Transactions(r.Context(), current(r).User.ID, r.PathValue("org"), r.URL.Query().Get("project"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs {
		failure(w, 404, "Log ingestion is disabled")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page > 1000000 {
		failure(w, 400, "Page is too large")
		return
	}
	items, err := s.db.Logs(r.Context(), current(r).User.ID, r.PathValue("org"), store.LogFilter{Project: q.Get("project"), Query: q.Get("query"), Level: q.Get("level"), Environment: q.Get("environment"), TraceID: q.Get("trace"), Page: page})
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "pageSize": 25})
}
