package server

import (
	"errors"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/otlp"
	"goodsentry/internal/protocol"
	"goodsentry/internal/store"
)

// Route OTLP before either StripPrefix or ServeMux can redirect an alias into
// ingestion. Other endpoints keep their existing routing and normalization.
func (s *Server) otlpRoutes(next http.Handler) http.Handler {
	endpoint := s.cfg.Prefix + "/v1/logs"
	escaped := (&url.URL{Path: endpoint}).EscapedPath()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		candidate := r.URL.Path == endpoint || strings.HasPrefix(r.URL.Path, endpoint+"/") || clean == endpoint || strings.HasPrefix(clean, endpoint+"/")
		if s.cfg.Prefix != "" && strings.HasPrefix(r.URL.Path, s.cfg.Prefix+"/") {
			// ServeMux redirects the stripped path without restoring the prefix.
			// A repeated prefix in that tail can otherwise redirect into OTLP.
			tail := path.Clean(strings.TrimPrefix(r.URL.Path, s.cfg.Prefix))
			candidate = candidate || tail == endpoint || strings.HasPrefix(tail, endpoint+"/")
		}
		if candidate {
			if r.URL.Path != endpoint || r.URL.EscapedPath() != escaped || (r.URL.RawPath != "" && r.URL.RawPath != escaped) {
				failure(w, http.StatusNotFound, "Not found")
				return
			}
			s.otlpLogs(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Count all supplied field values, including differently cased names on direct
// Handler requests. No credential/media ambiguity is resolved by taking a first.
func otlpHeader(header http.Header, name string) (string, int) {
	var value string
	count := 0
	for field, values := range header {
		if strings.EqualFold(field, name) {
			count += len(values)
			if len(values) > 0 {
				value = values[0]
			}
		}
	}
	return value, count
}

func (s *Server) otlpLimits() otlp.Limits {
	return otlp.Limits{MaxBytes: s.cfg.MaxBodyBytes, MaxRecords: s.cfg.OTLPMaxRecords, MaxNodes: s.cfg.OTLPMaxNodes}
}

func writeOTLP(w http.ResponseWriter, encoding otlp.Encoding, status int, body []byte) {
	contentType := "application/json"
	if encoding == otlp.Protobuf {
		contentType = "application/x-protobuf"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func failOTLP(w http.ResponseWriter, encoding otlp.Encoding, status int) {
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	writeOTLP(w, encoding, status, otlp.ErrorResponse(encoding, status))
}

func (s *Server) otlpLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Encoding, X-GoodSentry-Key")
	w.Header().Set("Access-Control-Expose-Headers", "Retry-After")

	// Media recognition chooses the failure representation, but does not admit
	// a request or read its entity before project authentication.
	encoding := otlp.JSON
	contentType, fields := otlpHeader(r.Header, "Content-Type")
	mediaType, _, mediaErr := mime.ParseMediaType(contentType)
	supported := fields == 1 && mediaErr == nil && (mediaType == "application/json" || mediaType == "application/x-protobuf")
	if supported && mediaType == "application/x-protobuf" {
		encoding = otlp.Protobuf
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		failOTLP(w, encoding, http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" {
		failOTLP(w, encoding, http.StatusBadRequest)
		return
	}
	for field := range r.Header {
		if strings.EqualFold(field, "Authorization") {
			failOTLP(w, encoding, http.StatusForbidden)
			return
		}
	}
	key, fields := otlpHeader(r.Header, "X-GoodSentry-Key")
	if fields != 1 || !utf8.ValidString(key) || !protocol.ValidPublicKey(key) {
		failOTLP(w, encoding, http.StatusForbidden)
		return
	}
	projectID, err := s.db.ProjectForPublicKey(r.Context(), key)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusForbidden
		}
		failOTLP(w, encoding, status)
		return
	}
	if !s.cfg.Logs {
		failOTLP(w, encoding, http.StatusNotFound)
		return
	}
	contentEncoding, encodingFields := otlpHeader(r.Header, "Content-Encoding")
	if !supported || encodingFields > 1 {
		failOTLP(w, encoding, http.StatusUnsupportedMediaType)
		return
	}
	batch, err := otlp.Prepare(r.Body, encoding, contentEncoding, s.otlpLimits(), time.Now().UTC())
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, otlp.ErrTooLarge):
			status = http.StatusRequestEntityTooLarge
		case errors.Is(err, otlp.ErrEncoding):
			status = http.StatusUnsupportedMediaType
		}
		failOTLP(w, encoding, status)
		return
	}
	if _, err := s.db.IngestOTLP(r.Context(), projectID, batch); err != nil {
		failOTLP(w, encoding, http.StatusServiceUnavailable)
		return
	}
	writeOTLP(w, encoding, http.StatusOK, otlp.SuccessResponse(encoding, batch.ProfilesWarning))
}

func (s *Server) logDetail(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs {
		failure(w, http.StatusNotFound, "Log ingestion is disabled")
		return
	}
	id := r.PathValue("log")
	if _, valid := occurrenceNumber(id, math.MaxInt64); !valid || r.URL.RawQuery != "" {
		failure(w, http.StatusBadRequest, "A canonical positive log ID without query parameters is required")
		return
	}
	detail, err := s.db.Log(r.Context(), current(r).User.ID, r.PathValue("org"), id, s.otlpLimits())
	if err != nil {
		if errors.Is(err, otlp.ErrTooLarge) {
			failure(w, http.StatusRequestEntityTooLarge, "Log detail exceeds the current capacity")
		} else {
			report(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
