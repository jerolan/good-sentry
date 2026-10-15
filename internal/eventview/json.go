package eventview

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LegacyJSON preserves retained payload fields while projecting the documented
// identity, timestamp and dictionary-tag fields. It is not an SDK transport copy.
func LegacyJSON(record Record) (json.RawMessage, error) {
	fields, err := payload(record.Data)
	if err != nil {
		return nil, err
	}
	view := project(record, fields, false)
	for key, value := range map[string]string{
		"event_id":    record.ID,
		"datetime":    record.Timestamp.UTC().Format(time.RFC3339Nano),
		"title":       view.Title,
		"transaction": stringValue(fields["transaction"]),
	} {
		fields[key], _ = json.Marshal(value)
	}
	fields["timestamp"] = json.RawMessage(unixDecimal(record.Timestamp))
	fields["project"] = json.RawMessage(strconv.FormatInt(record.ProjectID, 10))
	dictionary := make(map[string]string, len(view.Tags))
	for _, tag := range view.Tags {
		dictionary[tag.Key] = tag.Value
	}
	fields["tags"], _ = json.Marshal(dictionary)
	if _, exists := fields["level"]; !exists {
		fields["level"], _ = json.Marshal(view.Level)
	}
	if _, exists := fields["type"]; !exists {
		fields["type"], _ = json.Marshal(view.Type)
	}
	return json.Marshal(fields)
}

// Unix returns the floor second even before the epoch. Constructing the decimal
// explicitly avoids both float rounding and UnixNano's narrower date range.
func unixDecimal(at time.Time) string {
	seconds, nanos := at.Unix(), at.Nanosecond()
	if nanos == 0 {
		return strconv.FormatInt(seconds, 10)
	}
	var value string
	if seconds < 0 {
		value = fmt.Sprintf("-%d.%09d", -(seconds + 1), 1_000_000_000-nanos)
	} else {
		value = fmt.Sprintf("%d.%09d", seconds, nanos)
	}
	return strings.TrimRight(value, "0")
}
