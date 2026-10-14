package server

import (
	"net/http"
	"strconv"

	"goodsentry/internal/store"
)

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
