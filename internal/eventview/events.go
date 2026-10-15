// Package eventview projects retained SDK events into compatible public views.
package eventview

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// Record is populated directly by an authorized event query. Data remains the
// durable SDK payload; current ownership and receipt metadata come from storage.
type Record struct {
	ID, GroupID                              string
	ProjectID                                int64
	Kind, Title, Level, Environment, Release string
	Timestamp, Received                      time.Time
	Data                                     json.RawMessage
	SiblingsKnown                            bool
	PreviousID, NextID                       *string
	ReleaseInfo                              *ReleaseReference
}

type ReleaseReference struct {
	ID           string     `json:"id"`
	Version      string     `json:"version"`
	DateCreated  time.Time  `json:"dateCreated"`
	DateReleased *time.Time `json:"dateReleased"`
}

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Entry struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// SiblingIDs is present only when the provider queried neighboring events in
// the same authorized snapshot. Each known boundary is represented by null.
type SiblingIDs struct {
	PreviousEventID *string `json:"previousEventID"`
	NextEventID     *string `json:"nextEventID"`
}

type Event struct {
	ID           string            `json:"id"`
	EventID      string            `json:"eventID"`
	ProjectID    int64             `json:"projectID"`
	GroupID      string            `json:"groupID"`
	DateCreated  time.Time         `json:"dateCreated"`
	DateReceived time.Time         `json:"dateReceived"`
	Type         string            `json:"type"`
	Title        string            `json:"title"`
	Message      string            `json:"message"`
	Level        string            `json:"level"`
	Platform     *string           `json:"platform,omitempty"`
	Dist         *string           `json:"dist,omitempty"`
	Culprit      *string           `json:"culprit,omitempty"`
	Logger       *string           `json:"logger,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	Tags         []Tag             `json:"tags"`
	User         json.RawMessage   `json:"user,omitempty"`
	Entries      *[]Entry          `json:"entries,omitempty"`
	Contexts     json.RawMessage   `json:"contexts,omitempty"`
	Context      json.RawMessage   `json:"context,omitempty"`
	SDK          json.RawMessage   `json:"sdk,omitempty"`
	Packages     json.RawMessage   `json:"packages,omitempty"`
	Errors       json.RawMessage   `json:"errors,omitempty"`
	Release      *ReleaseReference `json:"release,omitempty"`
	UserReport   *json.RawMessage  `json:"userReport,omitempty"`
	*SiblingIDs
}

// Project never mutates or borrows mutable values from record.
func Project(record Record, full bool) (Event, error) {
	fields, err := payload(record.Data)
	if err != nil {
		return Event{}, err
	}
	return project(record, fields, full), nil
}

func project(record Record, fields map[string]json.RawMessage, full bool) Event {
	exceptions, values := collection(fields["exception"])
	selected := selectedException(values)
	message, messageData := eventMessage(fields)
	typ := "default"
	switch {
	case record.Kind == "transaction" || stringValue(fields["type"]) == "transaction":
		typ = "transaction"
	case record.Kind == "csp" || stringValue(fields["type"]) == "csp" || object(fields["csp"]) != nil:
		typ = "csp"
	case selected != nil || stringValue(fields["type"]) == "error":
		typ = "error"
	}
	metadata := map[string]string{}
	if typ == "error" && selected != nil {
		for _, key := range []string{"type", "value"} {
			if value := stringValue(selected[key]); value != "" {
				metadata[key] = value
			}
		}
	} else if typ == "default" && message != "" {
		metadata["title"] = message
	}
	level := record.Level
	if value := stringPointer(fields["level"]); value != nil {
		level = *value
	}
	result := Event{
		ID: record.ID, EventID: record.ID, ProjectID: record.ProjectID, GroupID: record.GroupID,
		DateCreated: record.Timestamp.UTC(), DateReceived: record.Received.UTC(), Type: typ,
		Title: responseTitle(record.Title), Message: message, Level: level,
		Platform: stringPointer(fields["platform"]), Dist: stringPointer(fields["dist"]), Culprit: stringPointer(fields["culprit"]), Logger: stringPointer(fields["logger"]),
		Metadata: metadata, Tags: tags(fields["tags"]), User: objectRaw(fields["user"]),
	}
	if record.SiblingsKnown {
		result.SiblingIDs = &SiblingIDs{PreviousEventID: copyString(record.PreviousID), NextEventID: copyString(record.NextID)}
	}
	if !full {
		return result
	}
	entries := []Entry{}
	for _, entry := range []Entry{
		{Type: "exception", Data: exceptions},
		{Type: "message", Data: messageData},
		{Type: "request", Data: requestView(fields["request"])},
		{Type: "csp", Data: objectRaw(fields["csp"])},
	} {
		if len(entry.Data) > 0 {
			entries = append(entries, entry)
		}
	}
	if breadcrumbs := breadcrumbView(fields["breadcrumbs"]); len(breadcrumbs) > 0 {
		entries = append(entries, Entry{Type: "breadcrumbs", Data: breadcrumbs})
	}
	result.Entries = &entries
	result.Contexts = objectRaw(fields["contexts"])
	result.Context = objectRaw(fields["extra"])
	result.SDK = objectRaw(fields["sdk"])
	if modules := object(fields["modules"]); modules != nil {
		valid := map[string]json.RawMessage{}
		for name, version := range modules {
			if stringPointer(version) != nil || string(version) == "null" {
				valid[name] = version
			}
		}
		result.Packages, _ = json.Marshal(valid)
	}
	if array := array(fields["errors"]); array != nil {
		valid := []json.RawMessage{}
		for _, item := range array {
			if itemFields := object(item); itemFields != nil && stringPointer(itemFields["type"]) != nil {
				valid = append(valid, item)
			}
		}
		result.Errors, _ = json.Marshal(valid)
	}
	if record.ReleaseInfo != nil {
		release := *record.ReleaseInfo
		release.DateCreated = release.DateCreated.UTC()
		if release.DateReleased != nil {
			at := release.DateReleased.UTC()
			release.DateReleased = &at
		}
		result.Release = &release
	}
	null := json.RawMessage("null")
	result.UserReport = &null
	return result
}

func payload(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("stored event must be a JSON object")
	}
	return fields, nil
}

func object(raw json.RawMessage) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	return fields
}

func objectRaw(raw json.RawMessage) json.RawMessage {
	if object(raw) == nil {
		return nil
	}
	return raw
}

func array(raw json.RawMessage) []json.RawMessage {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	return items
}

func stringPointer(raw json.RawMessage) *string {
	if len(raw) == 0 || raw[0] != '"' {
		return nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func stringValue(raw json.RawMessage) string {
	if value := stringPointer(raw); value != nil {
		return *value
	}
	return ""
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func responseTitle(title string) string {
	characters := []rune(title)
	if len(characters) > 255 {
		return string(characters[:255])
	}
	return title
}

func tags(raw json.RawMessage) []Tag {
	result := []Tag{}
	if fields := object(raw); fields != nil {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if value := stringPointer(fields[key]); value != nil {
				result = append(result, Tag{Key: key, Value: *value})
			}
		}
		return result
	}
	for _, item := range array(raw) {
		pair := array(item)
		if len(pair) != 2 {
			continue
		}
		key, value := stringPointer(pair[0]), stringPointer(pair[1])
		if key != nil && value != nil {
			result = append(result, Tag{Key: *key, Value: *value})
		}
	}
	return result
}

func collection(raw json.RawMessage) (json.RawMessage, []map[string]json.RawMessage) {
	fields := object(raw)
	items := array(raw)
	if fields != nil {
		items = array(fields["values"])
	}
	if items == nil {
		return nil, nil
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	valid := []json.RawMessage{}
	values := []map[string]json.RawMessage{}
	for _, item := range items {
		if value := object(item); value != nil {
			valid = append(valid, item)
			values = append(values, value)
		}
	}
	fields["values"], _ = json.Marshal(valid)
	encoded, _ := json.Marshal(fields)
	return encoded, values
}

func selectedException(values []map[string]json.RawMessage) map[string]json.RawMessage {
	for i := len(values) - 1; i >= 0; i-- {
		value := values[i]
		stack := object(value["stacktrace"])
		if strings.TrimSpace(stringValue(value["type"])) != "" || strings.TrimSpace(stringValue(value["value"])) != "" || len(array(stack["frames"])) > 0 {
			return value
		}
	}
	return nil
}

func eventMessage(fields map[string]json.RawMessage) (string, json.RawMessage) {
	for _, raw := range []json.RawMessage{fields["logentry"], fields["message"]} {
		if value := stringPointer(raw); value != nil {
			data, _ := json.Marshal(map[string]string{"formatted": *value})
			return *value, data
		}
		entry := object(raw)
		for _, key := range []string{"formatted", "message"} {
			if value := stringPointer(entry[key]); value != nil && *value != "" {
				return *value, raw
			}
		}
	}
	return "", nil
}
