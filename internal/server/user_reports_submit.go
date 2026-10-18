package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"goodsentry/internal/protocol"
	"goodsentry/internal/store"
)

func (s *Server) submitUserReport(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Invalid feedback query")
		return
	}
	sdk := len(r.Header.Values("X-Sentry-Auth")) > 0
	for key := range query {
		sdk = sdk || strings.HasPrefix(key, "sentry_")
	}
	if sdk {
		ingestCORS(w)
	}
	if len(r.Header.Values("Authorization")) > 0 && sdk {
		failure(w, 403, "Choose one feedback credential source")
		return
	}
	if sdk {
		for key := range query {
			if key != "sentry_key" && key != "sentry_version" && key != "sentry_client" && key != "sentry_timestamp" && key != "sentry_secret" {
				failure(w, 400, "Unsupported feedback authentication query")
				return
			}
		}
		key, err := protocol.Authenticate(r, nil, r.PathValue("project"))
		if err != nil {
			failure(w, 403, "Invalid SDK authentication")
			return
		}
		input, ok := s.userReportBody(w, r)
		if !ok {
			return
		}
		report, err := s.db.SubmitDSNUserReport(r.Context(), r.PathValue("org"), r.PathValue("project"), key, input)
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 403, "Invalid SDK authentication or project")
			return
		}
		writeUserReportResult(w, report, err)
		return
	}
	s.compatible("project:write", s.submitManagedUserReport).ServeHTTP(w, r)
}

func (s *Server) submitManagedUserReport(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" {
		failure(w, 400, "Feedback submission accepts no management query parameters")
		return
	}
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok {
		access.ReadOnly = !slices.Contains(grant.Scopes, "project:write")
	}
	input, ok := s.userReportBody(w, r)
	if !ok {
		return
	}
	report, err := s.db.SubmitManagedUserReport(r.Context(), access, r.PathValue("project"), input)
	writeUserReportResult(w, report, err)
}

func (s *Server) userReportBody(w http.ResponseWriter, r *http.Request) (store.UserReportInput, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	encoding := strings.TrimSpace(r.Header.Get("Content-Encoding"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) > 1 || !strings.EqualFold(media, "application/json") || encoding != "" && !strings.EqualFold(encoding, "identity") {
		failure(w, 415, "User feedback requires uncompressed application/json")
		return store.UserReportInput{}, false
	}
	limit := s.cfg.MaxBodyBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			failure(w, 413, "Request exceeds the configured ingestion limit")
		} else {
			failure(w, 400, "Invalid feedback request body")
		}
		return store.UserReportInput{}, false
	}
	input, err := decodeUserReportPayload(raw, true)
	if err != nil {
		reportUserReport(w, err)
		return store.UserReportInput{}, false
	}
	return input, true
}

func writeUserReportResult(w http.ResponseWriter, report store.UserReport, err error) {
	if err != nil {
		reportUserReport(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, report)
}
