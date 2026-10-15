package server

import (
	"errors"
	"math"
	"net/http"

	"goodsentry/internal/store"
)

func (s *Server) transactionSpanGroups(w http.ResponseWriter, r *http.Request) {
	source, id, page, valid := transactionOccurrence(w, r)
	if !valid {
		return
	}
	query := r.URL.Query()
	project := query.Get("project")
	if project != "" {
		if _, valid := occurrenceNumber(project, math.MaxInt64); !valid {
			failure(w, 400, "Project must be a positive decimal int64")
			return
		}
	}
	window, err := queryTimeWindow(query)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	groups, err := s.db.TransactionSpanGroups(r.Context(), current(r).User.ID, r.PathValue("org"), source, id, project, window, page)
	if errors.Is(err, store.ErrSpanProjectionPending) {
		w.Header().Set("Retry-After", "2")
		failure(w, http.StatusServiceUnavailable, "Historical transaction spans are still being indexed; retry shortly")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}
