package server

import (
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strings"

	"goodsentry/internal/store"
)

func canonicalTraceID(trace string) (string, bool) {
	if len(trace) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(trace); err != nil {
		return "", false
	}
	return strings.ToLower(trace), true
}

func traceRequest(w http.ResponseWriter, r *http.Request) (string, string, int, bool) {
	trace, validTrace := canonicalTraceID(r.PathValue("trace"))
	if !validTrace {
		failure(w, 400, "Trace ID must contain exactly 32 hexadecimal digits")
		return "", "", 0, false
	}
	query := r.URL.Query()
	project := query.Get("project")
	if project != "" {
		if _, valid := occurrenceNumber(project, math.MaxInt64); !valid {
			failure(w, 400, "Project must be a positive decimal int64")
			return "", "", 0, false
		}
	}
	page := int64(1)
	if query.Has("page") {
		var valid bool
		page, valid = occurrenceNumber(query.Get("page"), 1000000)
		if !valid {
			failure(w, 400, "Page must be a positive decimal integer up to 1000000")
			return "", "", 0, false
		}
	}
	return trace, project, int(page), true
}

func validTraceNode(node string) bool {
	parts := strings.Split(node, "-")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if parts[0] != "event" && parts[0] != "span" {
		return false
	}
	if _, valid := occurrenceNumber(parts[1], math.MaxInt64); !valid {
		return false
	}
	if len(parts) == 3 {
		_, valid := occurrenceNumber(parts[2], math.MaxInt32)
		return valid && parts[0] == "event"
	}
	return true
}

func reportTrace(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrSpanProjectionPending) {
		w.Header().Set("Retry-After", "2")
		failure(w, http.StatusServiceUnavailable, "Historical trace metadata is still being indexed; retry shortly")
		return
	}
	report(w, err)
}

func (s *Server) trace(w http.ResponseWriter, r *http.Request) {
	trace, project, page, valid := traceRequest(w, r)
	if !valid {
		return
	}
	detail, err := s.db.Trace(r.Context(), current(r).User.ID, r.PathValue("org"), trace, project, page, s.cfg.Logs)
	if err != nil {
		reportTrace(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) traceNode(w http.ResponseWriter, r *http.Request) {
	trace, project, _, valid := traceRequest(w, r)
	if !valid {
		return
	}
	node := r.PathValue("node")
	if !validTraceNode(node) {
		failure(w, 400, "Node must be event-ID, event-ID-ordinal or span-ID with canonical positive decimal identifiers")
		return
	}
	detail, err := s.db.TraceNodeData(r.Context(), current(r).User.ID, r.PathValue("org"), trace, node, project)
	if err != nil {
		reportTrace(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) traceErrors(w http.ResponseWriter, r *http.Request) {
	trace, project, page, valid := traceRequest(w, r)
	if !valid {
		return
	}
	detail, err := s.db.TraceErrors(r.Context(), current(r).User.ID, r.PathValue("org"), trace, project, page)
	if err != nil {
		reportTrace(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
