package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Feedback is extracted metadata. Data retains the original SDK fields.
type Feedback struct {
	Message, Name, Email, URL, AssociatedEventID string
}

func feedbackIdentity(payloadID json.RawMessage, envelopeID, normalizedID string) error {
	if envelopeID == "" || payloadID == nil {
		return nil
	}
	var supplied string
	if json.Unmarshal(payloadID, &supplied) != nil || supplied == "" {
		return errors.New("feedback event_id must be a nonempty string")
	}
	canonical, err := normalizeID(supplied)
	if err != nil || canonical != normalizedID {
		return errors.New("feedback event_id must match its envelope")
	}
	return nil
}

func normalizeFeedback(raw, contexts json.RawMessage, ownID string) (*Feedback, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("feedback must contain valid UTF-8")
	}
	var objects map[string]json.RawMessage
	if json.Unmarshal(contexts, &objects) != nil || objects == nil {
		return nil, errors.New("feedback requires contexts.feedback")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(objects["feedback"], &fields) != nil || fields == nil {
		return nil, errors.New("feedback requires contexts.feedback object")
	}
	feedback := &Feedback{}
	for _, field := range []struct {
		name     string
		limit    int
		value    *string
		required bool
	}{
		{"message", 10000, &feedback.Message, true},
		{"name", 128, &feedback.Name, false},
		{"contact_email", 254, &feedback.Email, false},
		{"url", 2048, &feedback.URL, false},
		{"associated_event_id", 36, &feedback.AssociatedEventID, false},
	} {
		value, exists := fields[field.name]
		if !exists && !field.required {
			continue
		}
		if !exists || string(value) == "null" || json.Unmarshal(value, field.value) != nil || strings.ContainsRune(*field.value, 0) || !utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.limit {
			return nil, fmt.Errorf("invalid feedback %s", field.name)
		}
	}
	if strings.TrimSpace(feedback.Message) == "" {
		return nil, errors.New("feedback message must contain text")
	}
	if feedback.AssociatedEventID != "" {
		canonical, err := normalizeID(feedback.AssociatedEventID)
		if err != nil || canonical == ownID {
			return nil, errors.New("feedback associated_event_id must identify another event")
		}
		feedback.AssociatedEventID = canonical
	}
	return feedback, nil
}
