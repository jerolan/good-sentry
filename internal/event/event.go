// Package event turns SDK event payloads into durable, searchable metadata.
package event

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

type Event struct {
	ID          string          `json:"event_id"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Level       string          `json:"level"`
	Platform    string          `json:"platform"`
	Environment string          `json:"environment"`
	Release     string          `json:"release"`
	Fingerprint string          `json:"fingerprint"`
	Transaction string          `json:"transaction"`
	TraceID     string          `json:"trace_id"`
	Timestamp   time.Time       `json:"timestamp"`
	Duration    float64         `json:"duration"` // Milliseconds.
	Data        json.RawMessage `json:"data"`
}

type frame struct {
	Module   string `json:"module"`
	Function string `json:"function"`
	Filename string `json:"filename"`
}

type stacktrace struct {
	Frames []frame `json:"frames"`
}

type exception struct {
	Type       string     `json:"type"`
	Value      string     `json:"value"`
	Stacktrace stacktrace `json:"stacktrace"`
}

type exceptionCollection struct {
	Values []exception `json:"values"`
}

func (c *exceptionCollection) UnmarshalJSON(raw []byte) error {
	var values []exception
	if err := json.Unmarshal(raw, &values); err == nil {
		c.Values = values
		return nil
	}
	type collection exceptionCollection
	return json.Unmarshal(raw, (*collection)(c))
}

type logentry struct {
	Formatted string `json:"formatted"`
	Message   string `json:"message"`
}

type payload struct {
	Type           string              `json:"type"`
	Level          string              `json:"level"`
	Platform       string              `json:"platform"`
	Environment    string              `json:"environment"`
	Release        string              `json:"release"`
	Transaction    string              `json:"transaction"`
	Timestamp      json.RawMessage     `json:"timestamp"`
	StartTimestamp json.RawMessage     `json:"start_timestamp"`
	Message        json.RawMessage     `json:"message"`
	Logentry       logentry            `json:"logentry"`
	Fingerprint    []string            `json:"fingerprint"`
	Stacktrace     stacktrace          `json:"stacktrace"`
	Exception      exceptionCollection `json:"exception"`
	Contexts       struct {
		Trace struct {
			ID string `json:"trace_id"`
		} `json:"trace"`
	} `json:"contexts"`
}

// Normalize retains all structured SDK fields in Data and replaces event_id
// with its normalized value. The caller scopes Fingerprint to the project.
func Normalize(raw json.RawMessage, envelopeID string, now time.Time) (Event, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Event{}, errors.New("event must be a JSON object")
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Event{}, fmt.Errorf("decode event fields: %w", err)
	}
	var id string
	if envelopeID != "" {
		id = envelopeID
	} else if value, ok := fields["event_id"]; ok {
		if string(value) == "null" || json.Unmarshal(value, &id) != nil {
			return Event{}, errors.New("event_id must be a string")
		}
	}
	id, err := normalizeID(id)
	if err != nil {
		return Event{}, err
	}
	typ := p.Type
	if typ == "" || typ == "default" {
		typ = "error"
	}
	if typ != "error" && typ != "message" && typ != "transaction" {
		return Event{}, fmt.Errorf("unsupported event type %q", typ)
	}
	timestamp := now.UTC()
	if len(p.Timestamp) != 0 {
		timestamp, err = parseTimestamp(p.Timestamp)
		if err != nil {
			return Event{}, fmt.Errorf("timestamp: %w", err)
		}
	}
	var duration float64
	if typ == "transaction" {
		if len(p.Timestamp) == 0 || len(p.StartTimestamp) == 0 {
			return Event{}, errors.New("transaction requires start_timestamp and timestamp")
		}
		start, err := parseTimestamp(p.StartTimestamp)
		if err != nil {
			return Event{}, fmt.Errorf("start_timestamp: %w", err)
		}
		if timestamp.Before(start) {
			return Event{}, errors.New("transaction ends before it starts")
		}
		duration = float64(timestamp.Unix()-start.Unix())*1000 + float64(timestamp.Nanosecond()-start.Nanosecond())/1e6
	}
	exc := latestException(p.Exception.Values)
	title := eventTitle(p, exc, typ)
	if title == "" {
		title = "Unknown error"
	}
	level := p.Level
	if level == "" {
		level = "error"
		if typ == "transaction" || typ == "message" {
			level = "info"
		}
	}
	platform := p.Platform
	if platform == "" {
		platform = "other"
	}
	fields["event_id"], _ = json.Marshal(id)
	data, err := json.Marshal(fields)
	if err != nil {
		return Event{}, fmt.Errorf("encode normalized event: %w", err)
	}
	return Event{
		ID: id, Type: typ, Title: title, Level: level, Platform: platform,
		Environment: p.Environment, Release: p.Release, Transaction: p.Transaction,
		TraceID: p.Contexts.Trace.ID, Timestamp: timestamp, Duration: duration,
		Fingerprint: fingerprint(p, exc, title), Data: data,
	}, nil
}

func normalizeID(id string) (string, error) {
	if id == "" {
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			return "", fmt.Errorf("generate event id: %w", err)
		}
		return hex.EncodeToString(value[:]), nil
	}
	if len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-' {
		id = strings.ReplaceAll(id, "-", "")
	}
	if len(id) != 32 {
		return "", errors.New("event_id must contain 32 hexadecimal digits")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", errors.New("event_id must contain 32 hexadecimal digits")
	}
	return strings.ToLower(id), nil
}

func parseTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return time.Time{}, errors.New("invalid timestamp string")
		}
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, errors.New("timestamp string must use RFC3339")
		}
		t = t.UTC()
		if t.Year() < 1 || t.Year() > 9999 {
			return time.Time{}, errors.New("timestamp is outside supported range")
		}
		return t, nil
	}
	var seconds float64
	if string(raw) == "null" || json.Unmarshal(raw, &seconds) != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < -62135596800 || seconds >= 253402300800 {
		return time.Time{}, errors.New("timestamp must be finite Unix seconds or RFC3339")
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(math.Round(fraction*1e9))).UTC(), nil
}

func latestException(values []exception) *exception {
	for i := len(values) - 1; i >= 0; i-- {
		if text(values[i].Type) != "" || text(values[i].Value) != "" || len(values[i].Stacktrace.Frames) != 0 {
			return &values[i]
		}
	}
	return nil
}

func eventTitle(p payload, exc *exception, typ string) string {
	if typ == "transaction" {
		if name := text(p.Transaction); name != "" {
			return name
		}
		return "<unlabeled transaction>"
	}
	if exc != nil {
		typ, value := text(exc.Type), text(exc.Value)
		if typ != "" && value != "" {
			return typ + ": " + value
		}
		if typ != "" {
			return typ
		}
		if value != "" {
			return value
		}
	}
	if title := text(p.Logentry.Formatted); title != "" {
		return title
	}
	if title := text(p.Logentry.Message); title != "" {
		return title
	}
	var message string
	if json.Unmarshal(p.Message, &message) == nil {
		return text(message)
	}
	var entry logentry
	if json.Unmarshal(p.Message, &entry) == nil {
		if title := text(entry.Formatted); title != "" {
			return title
		}
		return text(entry.Message)
	}
	return ""
}

// Fingerprints hash unambiguous JSON arrays. The version prefixes make any
// future algorithm change explicit. Line numbers, columns and exception
// values do not affect stack-based grouping.
func fingerprint(p payload, exc *exception, title string) string {
	parts := []string{"goodsentry-group-v1"}
	stack := p.Stacktrace
	if exc != nil {
		parts = append(parts, "exception", text(exc.Type))
		stack = exc.Stacktrace
	}
	hasFrame := false
	for _, f := range stack.Frames {
		module, function, filename := text(f.Module), text(f.Function), normalizeFilename(f.Filename)
		if module == "" && function == "" && filename == "" {
			continue
		}
		hasFrame = true
		parts = append(parts, "frame", module, function, filename)
	}
	if !hasFrame {
		parts = append(parts, "message", title)
	}
	defaultKey := hashParts(parts)
	if len(p.Fingerprint) == 0 || len(p.Fingerprint) == 1 && p.Fingerprint[0] == "{{ default }}" {
		return defaultKey
	}
	custom := []string{"goodsentry-fingerprint-v1"}
	for _, component := range p.Fingerprint {
		if component == "{{ default }}" {
			component = defaultKey
		}
		custom = append(custom, component)
	}
	return hashParts(custom)
}

func hashParts(parts []string) string {
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func normalizeFilename(filename string) string {
	filename = strings.ReplaceAll(strings.TrimSpace(filename), `\`, "/")
	if parsed, err := url.Parse(filename); err == nil && parsed.Host != "" {
		parsed.RawQuery, parsed.Fragment, parsed.RawFragment = "", "", ""
		filename = parsed.String()
	}
	return filename
}

func text(value string) string { return strings.Join(strings.Fields(value), " ") }
