package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"goodsentry/internal/event"
	"goodsentry/internal/protocol"
	"goodsentry/internal/store"
)

// Validate every report field and parent relationship before ingestion writes.
// This also handles report-only envelopes whose target has not arrived yet.
func envelopeUserReport(env protocol.Envelope, envelopeID string, parent *event.Event) (*store.UserReportInput, error) {
	var report *store.UserReportInput
	for _, item := range env.Items {
		if item.Type != "user_report" {
			continue
		}
		if report != nil || !utf8.Valid(item.Payload) {
			return nil, store.ErrInvalidUserReport
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(item.Payload, &fields) != nil || fields == nil {
			return nil, store.ErrInvalidUserReport
		}
		for _, key := range []string{"event_id", "name", "email", "comments"} {
			var value string
			if len(fields[key]) == 0 || string(fields[key]) == "null" || json.Unmarshal(fields[key], &value) != nil {
				return nil, store.ErrInvalidUserReport
			}
		}
		var input store.UserReportInput
		if json.Unmarshal(item.Payload, &input) != nil {
			return nil, store.ErrInvalidUserReport
		}
		input, err := store.NormalizeUserReport(input)
		if err != nil {
			return nil, err
		}
		if envelopeID != "" {
			header := input
			header.EventID = envelopeID
			normalized, err := store.NormalizeUserReport(header)
			if err != nil || normalized.EventID != input.EventID {
				return nil, store.ErrInvalidUserReport
			}
		}
		if parent != nil && (parent.ID != input.EventID || parent.Type != "error" && parent.Type != "message") {
			return nil, store.ErrInvalidUserReport
		}
		report = &input
	}
	if report != nil && parent == nil {
		for _, item := range env.Items {
			if item.Type == "attachment" {
				return nil, store.ErrInvalidUserReport
			}
		}
	}
	return report, nil
}

func reportUserReport(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidUserReport):
		failure(w, 400, "Invalid legacy user report or event identity")
	case errors.Is(err, store.ErrConflict):
		failure(w, 409, "User feedback is outside its submission or overwrite window")
	default:
		report(w, err)
	}
}
