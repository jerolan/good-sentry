package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"goodsentry/internal/artifact"
	"goodsentry/internal/event"
	"goodsentry/internal/protocol"
	"goodsentry/internal/store"
)

func ingestCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Sentry-Auth, Content-Encoding, Authorization, X-Requested-With")
	w.Header().Set("Access-Control-Expose-Headers", "X-Sentry-Rate-Limits, Retry-After, X-Sentry-Error")
	w.Header().Set("Access-Control-Max-Age", "86400")
}
func (s *Server) preflight(w http.ResponseWriter, r *http.Request) { ingestCORS(w); w.WriteHeader(204) }
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	ingestCORS(w)
	env, err := protocol.Decode(r.Body, r.Header.Get("Content-Encoding"), strings.HasSuffix(r.URL.Path, "/store/"), protocol.Limits{MaxCompressed: s.cfg.MaxBodyBytes, MaxDecompressed: s.cfg.MaxBodyBytes, MaxItems: 1000, MaxItemBytes: s.cfg.MaxBodyBytes})
	if err != nil {
		switch {
		case errors.Is(err, protocol.ErrTooLarge):
			failure(w, 413, "Request exceeds the configured ingestion limit")
		case errors.Is(err, protocol.ErrEncoding):
			failure(w, 415, "Unsupported content encoding")
		default:
			failure(w, 400, "Malformed Sentry payload")
		}
		return
	}
	project := r.PathValue("project")
	key, err := protocol.Authenticate(r, env.Header, project)
	if err != nil {
		failure(w, 403, "Invalid SDK authentication")
		return
	}
	ok, err := s.db.ValidKey(r.Context(), project, key)
	if err != nil {
		report(w, err)
		return
	}
	if !ok {
		failure(w, 403, "Invalid SDK authentication")
		return
	}
	var envelopeID string
	if raw, exists := env.Header["event_id"]; exists {
		if json.Unmarshal(raw, &envelopeID) != nil || envelopeID == "" {
			failure(w, 400, "Invalid envelope event ID")
			return
		}
	}
	var normalized *event.Event
	for _, item := range env.Items {
		if item.Type != "event" && item.Type != "transaction" {
			continue
		}
		if normalized != nil {
			failure(w, 400, "An envelope may contain one event or transaction")
			return
		}
		raw := json.RawMessage(item.Payload)
		if item.Type == "transaction" {
			var value map[string]json.RawMessage
			if json.Unmarshal(raw, &value) != nil || value == nil {
				failure(w, 400, "Invalid transaction")
				return
			}
			value["type"] = json.RawMessage(`"transaction"`)
			raw, _ = json.Marshal(value)
		}
		raw, err = s.mapEvent(r.Context(), project, raw)
		if err != nil {
			if errors.Is(err, store.ErrArtifactLimit) || errors.Is(err, artifact.ErrSourceMapLimit) {
				failure(w, 413, "Selected source maps exceed the configured processing limit")
			} else {
				report(w, err)
			}
			return
		}
		ev, err := event.Normalize(raw, envelopeID, time.Now())
		if err != nil {
			failure(w, 400, err.Error())
			return
		}
		normalized = &ev
	}
	if normalized != nil {
		var children store.CaptureItems
		for _, item := range env.Items {
			switch item.Type {
			case "attachment":
				var filename, contentType string
				_ = json.Unmarshal(item.Header["filename"], &filename)
				_ = json.Unmarshal(item.Header["content_type"], &contentType)
				filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
				if filename == "." || filename == "/" || filename == "" {
					filename = "attachment"
				}
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				children.Attachments = append(children.Attachments, store.CaptureAttachment{Filename: filename, ContentType: contentType, Data: item.Payload})
			case "event", "transaction", "span", "log":
			default:
				header, _ := json.Marshal(item.Header)
				children.EnvelopeItems = append(children.EnvelopeItems, store.CaptureEnvelopeItem{Type: item.Type, Header: header, Data: item.Payload})
			}
		}
		var parts []store.CaptureItems
		if len(children.Attachments) > 0 || len(children.EnvelopeItems) > 0 {
			parts = []store.CaptureItems{children}
		}
		if _, _, err = s.db.Ingest(r.Context(), project, *normalized, parts...); err != nil {
			report(w, err)
			return
		}
		envelopeID = normalized.ID
	}
	unsupported := []string{}
	for _, item := range env.Items {
		switch item.Type {
		case "event", "transaction":
			continue
		case "span":
			var contentType string
			_ = json.Unmarshal(item.Header["content_type"], &contentType)
			if contentType != "application/vnd.sentry.items.span.v2+json" {
				failure(w, 415, "Unsupported span content type")
				return
			}
			var count int
			if json.Unmarshal(item.Header["item_count"], &count) != nil || count < 1 || count > 1000 {
				failure(w, 400, "Invalid span item count")
				return
			}
			if _, err = s.db.IngestSpans(r.Context(), project, item.Payload, count); err != nil {
				if errors.Is(err, store.ErrInvalidSpan) {
					failure(w, 400, err.Error())
				} else {
					report(w, err)
				}
				return
			}
		case "log":
			if !s.cfg.Logs {
				w.Header().Set("X-Sentry-Rate-Limits", "60:log_item;log_byte:organization:feature_disabled")
				unsupported = append(unsupported, "log:disabled")
				continue
			}
			if _, err = s.db.IngestLogs(r.Context(), project, item.Payload); err != nil {
				if errors.Is(err, store.ErrInvalidLog) {
					failure(w, 400, err.Error())
				} else {
					report(w, err)
				}
				return
			}
		case "attachment":
			if normalized == nil {
				failure(w, 400, "An attachment requires an event in this envelope")
				return
			}
		default:
			if normalized == nil {
				header, _ := json.Marshal(item.Header)
				if err = s.db.SaveEnvelopeItem(r.Context(), project, envelopeID, item.Type, header, item.Payload); err != nil {
					report(w, err)
					return
				}
			}
			unsupported = append(unsupported, item.Type)
		}
	}
	writeJSON(w, 200, map[string]any{"id": envelopeID, "unsupportedItems": unsupported})
}
