package server

import (
	"math"
	"net/http"
	"strconv"
)

func occurrenceNumber(value string, limit int64) (int64, bool) {
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n > 0 && n <= limit && strconv.FormatInt(n, 10) == value
}

func transactionOccurrence(w http.ResponseWriter, r *http.Request) (string, string, int, bool) {
	source, id := r.PathValue("source"), r.PathValue("id")
	if source != "event" && source != "span" {
		failure(w, 400, "Transaction source must be event or span")
		return "", "", 0, false
	}
	if _, valid := occurrenceNumber(id, math.MaxInt64); !valid {
		failure(w, 400, "Transaction ID must be a positive decimal int64")
		return "", "", 0, false
	}
	page := int64(1)
	if query := r.URL.Query(); query.Has("page") {
		var valid bool
		page, valid = occurrenceNumber(query.Get("page"), 1000000)
		if !valid {
			failure(w, 400, "Page must be a positive decimal integer up to 1000000")
			return "", "", 0, false
		}
	}
	return source, id, int(page), true
}

func (s *Server) transactionDetail(w http.ResponseWriter, r *http.Request) {
	source, id, page, valid := transactionOccurrence(w, r)
	if !valid {
		return
	}
	detail, err := s.db.TransactionDetail(r.Context(), current(r).User.ID, r.PathValue("org"), source, id, page)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, detail)
}
