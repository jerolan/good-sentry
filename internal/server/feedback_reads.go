package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"goodsentry/internal/store"
)

func (s *Server) registerFeedbackRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/issues/{issue}/feedback", s.auth(http.HandlerFunc(s.issueFeedback)))
	mux.Handle("GET /api/v1/issues/{issue}/feedback/{feedbackEventID}/associated-event", s.auth(http.HandlerFunc(s.feedbackAssociatedEvent)))
}

func reportFeedbackRead(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidEventFilter) {
		failure(w, 400, "Invalid feedback reference or page")
		return
	}
	report(w, err)
}

func (s *Server) issueFeedback(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Invalid feedback page query")
		return
	}
	page := 1
	for key, values := range query {
		if key != "page" || len(values) != 1 {
			failure(w, 400, "Only one feedback page parameter is supported")
			return
		}
		page, err = strconv.Atoi(values[0])
		if err != nil || page < 1 || page > 1000000 || strconv.Itoa(page) != values[0] {
			failure(w, 400, "Feedback page must be a canonical integer from 1 to 1000000")
			return
		}
	}
	items, total, more, err := s.db.IssueFeedback(r.Context(), current(r).User.ID, r.PathValue("issue"), page)
	if err != nil {
		reportFeedbackRead(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, map[string]any{"data": items, "total": total, "page": page, "pageSize": 25, "hasMore": more})
}

func (s *Server) feedbackAssociatedEvent(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		failure(w, 400, "Associated feedback events accept no query parameters")
		return
	}
	linked, err := s.db.FeedbackAssociatedEvent(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("feedbackEventID"))
	if err != nil {
		reportFeedbackRead(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, linked)
}
