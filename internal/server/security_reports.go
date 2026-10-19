package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/protocol"
)

// Public Sentry client-key URLs expose both names. These routes intentionally
// require an exact terminal slash, like the SDK's existing ingestion routes.
func (s *Server) registerSecurityReportRoutes(mux *http.ServeMux) {
	for _, route := range []string{"security", "csp-report"} {
		pattern := "/api/{project}/" + route + "/{$}"
		mux.HandleFunc("POST "+pattern, s.securityReports)
		mux.HandleFunc("OPTIONS "+pattern, s.preflight)
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", "POST, OPTIONS")
			failure(w, 405, "Security reports require POST")
		})
	}
}

func (s *Server) securityReports(w http.ResponseWriter, r *http.Request) {
	ingestCORS(w)
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Invalid security report query")
		return
	}
	for name, values := range query {
		switch name {
		case "sentry_key", "sentry_version", "sentry_client", "sentry_release", "sentry_environment":
		default:
			failure(w, 400, "Unsupported security report query parameter")
			return
		}
		if len(values) != 1 {
			failure(w, 400, "Security report query parameters must occur once")
			return
		}
	}
	key := query.Get("sentry_key")
	if !protocol.ValidPublicKey(key) || len(r.Header.Values("Authorization")) != 0 || len(r.Header.Values("X-Sentry-Auth")) != 0 || query.Has("sentry_version") && query.Get("sentry_version") != "7" {
		failure(w, 403, "Invalid security report public key authentication")
		return
	}
	if !credentialText(query.Get("sentry_release"), 255) || !credentialText(query.Get("sentry_environment"), 128) || !credentialText(query.Get("sentry_client"), 200) {
		failure(w, 400, "Invalid security report metadata")
		return
	}
	valid, err := s.db.ValidKey(r.Context(), r.PathValue("project"), key)
	if err != nil {
		report(w, err)
		return
	}
	if !valid {
		failure(w, 403, "Invalid security report public key authentication")
		return
	}
	media, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	encoding := strings.TrimSpace(r.Header.Get("Content-Encoding"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) > 1 || encoding != "" && !strings.EqualFold(encoding, "identity") || parameters["charset"] != "" && !strings.EqualFold(parameters["charset"], "utf-8") {
		failure(w, 415, "Security reports require uncompressed UTF-8 JSON")
		return
	}
	switch strings.ToLower(media) {
	case "application/json", "application/csp-report", "application/reports+json":
	default:
		failure(w, 415, "Unsupported security report content type")
		return
	}
	limit := s.cfg.MaxBodyBytes
	if limit <= 0 {
		limit = 32 << 20
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			failure(w, 413, "Request exceeds the configured ingestion limit")
		} else {
			failure(w, 400, "Invalid security report body")
		}
		return
	}
	events, err := event.NormalizeSecurityReports(raw, time.Now(), event.SecurityReportOptions{Release: query.Get("sentry_release"), Environment: query.Get("sentry_environment"), UserAgent: r.UserAgent()})
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	for _, ev := range events {
		if _, _, err := s.db.Ingest(r.Context(), r.PathValue("project"), ev); err != nil {
			report(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
