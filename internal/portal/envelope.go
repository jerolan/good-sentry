package portal

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	maxRequestBytes  = 2 << 20
	maxEnvelopeBytes = 4 << 20
)

var (
	errInvalidEnvelope = errors.New("invalid envelope")
	errEnvelopeTooBig  = errors.New("envelope is too large")
	errContentEncoding = errors.New("unsupported content encoding")
)

func ingestKey(r *http.Request) string {
	if key := r.URL.Query().Get("sentry_key"); key != "" {
		return key
	}
	auth, ok := strings.CutPrefix(r.Header.Get("X-Sentry-Auth"), "Sentry ")
	if !ok {
		return ""
	}
	for _, part := range strings.Split(auth, ",") {
		if key, ok := strings.CutPrefix(strings.TrimSpace(part), "sentry_key="); ok {
			return key
		}
	}
	return ""
}

func readEnvelope(w http.ResponseWriter, r *http.Request) (event, error) {
	var body io.Reader = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		reader, err := gzip.NewReader(body)
		if err != nil {
			return event{}, envelopeReadError(err)
		}
		defer reader.Close()
		body = reader
	default:
		return event{}, errContentEncoding
	}
	payload, err := io.ReadAll(io.LimitReader(body, maxEnvelopeBytes+1))
	if err != nil {
		return event{}, envelopeReadError(err)
	}
	if len(payload) > maxEnvelopeBytes {
		return event{}, errEnvelopeTooBig
	}
	return parseEnvelope(payload)
}

func envelopeReadError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return errEnvelopeTooBig
	}
	return errInvalidEnvelope
}

// parseEnvelope consumes length-delimited items as bytes; only lengthless items
// use newlines as payload delimiters.
func parseEnvelope(payload []byte) (event, error) {
	var header struct {
		ID string `json:"event_id"`
	}
	line, remaining, _ := cutLine(payload)
	if json.Unmarshal(line, &header) != nil || len(line) == 0 || line[0] != '{' {
		return event{}, errInvalidEnvelope
	}
	var captured event
	for len(remaining) > 0 {
		var item struct {
			Type   string `json:"type"`
			Length *int   `json:"length"`
		}
		var found bool
		line, remaining, found = cutLine(remaining)
		if !found || json.Unmarshal(line, &item) != nil || item.Type == "" {
			return event{}, errInvalidEnvelope
		}
		var content []byte
		if item.Length == nil {
			content, remaining, _ = cutLine(remaining)
		} else {
			n := *item.Length
			if n < 0 || n > len(remaining) {
				return event{}, errInvalidEnvelope
			}
			content, remaining = remaining[:n], remaining[n:]
			if len(remaining) > 0 {
				if remaining[0] != '\n' {
					return event{}, errInvalidEnvelope
				}
				remaining = remaining[1:]
			}
		}
		if item.Type != "event" {
			continue
		}
		if captured.ID != "" {
			return event{}, errInvalidEnvelope
		}
		var err error
		captured, err = parseEvent(content)
		if err != nil || (header.ID != "" && strings.ToLower(header.ID) != captured.ID) {
			return event{}, errInvalidEnvelope
		}
	}
	return captured, nil
}

func cutLine(data []byte) (line, remaining []byte, found bool) {
	return bytes.Cut(data, []byte{'\n'})
}

type sentryException struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Stacktrace struct {
		Frames []frame `json:"frames"`
	} `json:"stacktrace"`
}

func parseEvent(content []byte) (event, error) {
	var body struct {
		ID        string          `json:"event_id"`
		Exception json.RawMessage `json:"exception"`
	}
	if json.Unmarshal(content, &body) != nil || !validEventID(body.ID) {
		return event{}, errInvalidEnvelope
	}

	var exceptions []sentryException
	raw := bytes.TrimSpace(body.Exception)
	if len(raw) > 0 && raw[0] == '[' {
		// sentry-go sends the values as an array.
		if err := json.Unmarshal(raw, &exceptions); err != nil {
			return event{}, errInvalidEnvelope
		}
	} else {
		var wrapper struct {
			Values []sentryException `json:"values"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return event{}, errInvalidEnvelope
		}
		exceptions = wrapper.Values
	}
	if len(exceptions) == 0 {
		return event{}, errInvalidEnvelope
	}
	exception := exceptions[len(exceptions)-1]
	if exception.Value == "" {
		return event{}, errInvalidEnvelope
	}
	return event{
		ID:      strings.ToLower(body.ID),
		Type:    exception.Type,
		Message: exception.Value,
		Frames:  exception.Stacktrace.Frames,
	}, nil
}

func validEventID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
