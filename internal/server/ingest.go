package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/artifact"
	"goodsentry/internal/event"
	"goodsentry/internal/native"
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
	ok, eventThrottleRate, err := s.db.ValidKeyWithEventThrottle(r.Context(), project, key)
	if err != nil {
		report(w, err)
		return
	}
	if !ok {
		failure(w, 403, "Invalid SDK authentication")
		return
	}
	projectThrottled := shouldThrottleProjectEvent(eventThrottleRate)
	mayBeDiscarded := false
	for _, item := range env.Items {
		if item.Type == "event" {
			mayBeDiscarded = true
			break
		}
	}
	if projectThrottled && !mayBeDiscarded {
		rejectProjectThrottle(w)
		return
	}
	earlyFailure := func(status int, message string) {
		if projectThrottled {
			rejectProjectThrottle(w)
			return
		}
		failure(w, status, message)
	}
	earlyReport := func(err error) {
		if projectThrottled {
			rejectProjectThrottle(w)
			return
		}
		report(w, err)
	}
	earlyUserReportError := func(err error) {
		if projectThrottled {
			rejectProjectThrottle(w)
			return
		}
		reportUserReport(w, err)
	}
	var envelopeID string
	if raw, exists := env.Header["event_id"]; exists {
		if json.Unmarshal(raw, &envelopeID) != nil || envelopeID == "" {
			earlyFailure(400, "Invalid envelope event ID")
			return
		}
	}
	nativeMinidumpIndex := -1
	for index, item := range env.Items {
		if item.Type != "attachment" {
			continue
		}
		var attachmentType string
		if json.Unmarshal(item.Header["attachment_type"], &attachmentType) != nil || attachmentType != "event.minidump" {
			continue
		}
		if nativeMinidumpIndex >= 0 {
			earlyFailure(400, "An envelope may contain at most one native Minidump")
			return
		}
		nativeMinidumpIndex = index
	}
	if nativeMinidumpIndex >= 0 {
		nativeEvents := 0
		for _, item := range env.Items {
			switch item.Type {
			case "event":
				nativeEvents++
				var fields struct {
					Platform string `json:"platform"`
				}
				if json.Unmarshal(item.Payload, &fields) != nil || fields.Platform != "native" {
					earlyFailure(400, "A native Minidump attachment requires a native event")
					return
				}
			case "transaction", "feedback":
				earlyFailure(400, "A native Minidump attachment requires one event item")
				return
			}
		}
		if nativeEvents != 1 {
			earlyFailure(400, "A native Minidump attachment requires one event item")
			return
		}
	}
	var normalized *event.Event
	for _, item := range env.Items {
		if item.Type != "event" && item.Type != "transaction" && item.Type != "feedback" {
			continue
		}
		if normalized != nil {
			earlyFailure(400, "An envelope may contain one event, transaction or feedback")
			return
		}
		raw := json.RawMessage(item.Payload)
		if item.Type == "feedback" {
			var fields map[string]json.RawMessage
			if !utf8.Valid(raw) || json.Unmarshal(raw, &fields) != nil || fields == nil {
				earlyFailure(400, "Invalid feedback")
				return
			}
			if supplied, exists := fields["type"]; exists {
				var kind string
				if json.Unmarshal(supplied, &kind) != nil || kind != "feedback" {
					earlyFailure(400, "A feedback item requires feedback payload type")
					return
				}
			} else {
				fields["type"] = json.RawMessage(`"feedback"`)
				raw, _ = json.Marshal(fields)
			}
		}
		if item.Type == "transaction" {
			var value map[string]json.RawMessage
			if json.Unmarshal(raw, &value) != nil || value == nil {
				earlyFailure(400, "Invalid transaction")
				return
			}
			value["type"] = json.RawMessage(`"transaction"`)
			raw, _ = json.Marshal(value)
		}
		var payloadKind struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &payloadKind)
		if payloadKind.Type != "feedback" {
			raw, err = s.mapEvent(r.Context(), project, raw)
		}
		if err != nil {
			if errors.Is(err, store.ErrArtifactLimit) || errors.Is(err, artifact.ErrSourceMapLimit) {
				earlyFailure(413, "Selected source maps exceed the configured processing limit")
			} else {
				earlyReport(err)
			}
			return
		}
		if payloadKind.Type != "feedback" && native.PortableEventNeedsSymbols(raw) {
			symbolFiles, lookupErr := s.db.DebugSymbolFiles(r.Context(), project)
			if lookupErr != nil {
				earlyReport(lookupErr)
				return
			}
			files := make([]native.DebugSymbolFile, 0, len(symbolFiles))
			for _, file := range symbolFiles {
				files = append(files, native.DebugSymbolFile{Name: file.Name, DebugID: file.DebugID, Content: file.Content})
			}
			raw, _, err = native.SymbolicatePortableEvent(raw, files)
			if err != nil {
				earlyReport(err)
				return
			}
		}
		if nativeMinidumpIndex >= 0 && item.Type == "event" {
			crash, parseErr := native.ParseMinidump(env.Items[nativeMinidumpIndex].Payload)
			if parseErr != nil {
				earlyFailure(400, "Invalid native Minidump attachment")
				return
			}
			if err := s.symbolizeCrash(r.Context(), project, &crash); err != nil {
				earlyReport(err)
				return
			}
			raw, err = native.MergeMinidumpEvent(raw, crash, time.Now())
			if err != nil {
				earlyFailure(400, "Invalid native event Minidump data")
				return
			}
		}
		ev, err := event.Normalize(raw, envelopeID, time.Now())
		if err != nil {
			earlyFailure(400, err.Error())
			return
		}
		normalized = &ev
	}
	userReport, err := envelopeUserReport(env, envelopeID, normalized)
	if err != nil {
		earlyUserReportError(err)
		return
	}
	if userReport != nil && normalized == nil {
		envelopeID = userReport.EventID
	}
	if normalized == nil {
		if charge := ingestRateCharge(env); charge > 0 {
			allowed, retry, err := s.db.AdmitProjectKey(r.Context(), project, key, charge)
			if err != nil {
				report(w, err)
				return
			}
			if !allowed {
				rejectProjectKeyRateLimit(w, retry)
				return
			}
		}
	}
	if normalized != nil {
		var children store.CaptureItems
		if userReport != nil {
			children.UserReports = []store.UserReportInput{*userReport}
		}
		for index, item := range env.Items {
			if index == nativeMinidumpIndex {
				continue
			}
			switch item.Type {
			case "attachment":
				var filename, contentType, attachmentType string
				_ = json.Unmarshal(item.Header["filename"], &filename)
				_ = json.Unmarshal(item.Header["content_type"], &contentType)
				_ = json.Unmarshal(item.Header["attachment_type"], &attachmentType)
				filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
				if filename == "." || filename == "/" || filename == "" {
					filename = "attachment"
				}
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				children.Attachments = append(children.Attachments, store.CaptureAttachment{Filename: filename, ContentType: contentType, Type: attachmentType, Data: item.Payload})
			case "event", "transaction", "feedback", "span", "log", "user_report":
			default:
				header, _ := json.Marshal(item.Header)
				children.EnvelopeItems = append(children.EnvelopeItems, store.CaptureEnvelopeItem{Type: item.Type, Header: header, Data: item.Payload})
			}
		}
		var parts []store.CaptureItems
		if len(children.Attachments) > 0 || len(children.EnvelopeItems) > 0 || len(children.UserReports) > 0 {
			parts = []store.CaptureItems{children}
		}
		charge := ingestRateCharge(env)
		if charge < 1 {
			failure(w, 400, "Event envelope has no billable capture item")
			return
		}
		outcome, ingestErr := s.db.IngestWithAdmission(r.Context(), project, *normalized, store.CaptureAdmission{
			PublicKey: key, Charge: charge, IndependentCharge: charge - 1,
			Throttle: projectThrottled,
		}, parts...)
		if ingestErr != nil {
			reportUserReport(w, ingestErr)
			return
		}
		if outcome.ProjectThrottled {
			rejectProjectThrottle(w)
			return
		}
		if outcome.RetryAfter > 0 {
			rejectProjectKeyRateLimit(w, outcome.RetryAfter)
			return
		}
		envelopeID = normalized.ID
	}
	unsupported := []string{}
	for _, item := range env.Items {
		switch item.Type {
		case "event", "transaction", "feedback":
			continue
		case "user_report":
			if normalized == nil {
				if _, err := s.db.SubmitUserReport(r.Context(), project, *userReport); err != nil {
					reportUserReport(w, err)
					return
				}
			}
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

func rejectProjectKeyRateLimit(w http.ResponseWriter, retry int) {
	if retry < 1 {
		retry = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	w.Header().Set("X-Sentry-Rate-Limits", strconv.Itoa(retry)+":event:key:rate_limited")
	failure(w, http.StatusTooManyRequests, "SDK key rate limit exceeded")
}

func ingestRateCharge(env protocol.Envelope) int {
	charge := 0
	for _, item := range env.Items {
		switch item.Type {
		case "event", "transaction", "feedback", "log":
			charge++
		case "span":
			count := 1
			if json.Unmarshal(item.Header["item_count"], &count) != nil || count < 1 || count > 1000 {
				count = 1
			}
			charge += count
		}
	}
	return charge
}
