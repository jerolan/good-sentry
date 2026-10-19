package event_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"goodsentry/internal/event"
)

const feedbackID = "0123456789abcdef0123456789abcdef"
const associatedID = "abcdef0123456789abcdef0123456789"

var feedbackNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func feedbackPayload(fields string) json.RawMessage {
	return json.RawMessage(`{"type":"feedback","event_id":"` + feedbackID + `","contexts":{"feedback":` + fields + `}}`)
}

func TestFeedbackNormalizeValidAndPreservesPayload(t *testing.T) {
	raw := json.RawMessage(`{"type":"feedback","event_id":"` + feedbackID + `","level":"","platform":"node","environment":"prod","release":"r1","timestamp":"2026-10-18T12:00:00Z","contexts":{"trace":{"trace_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"feedback":{"message":"  hello\n  world ","name":"  SDK reporter  ","contact_email":"sdk@example.test","url":"https://example.test/source","associated_event_id":"` + strings.ToUpper(associatedID) + `","extension":{"safe":true}}},"tags":{"region":"mx"},"extra":{"n":9223372036854775807}}`)
	got, err := event.Normalize(raw, "", feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "feedback" || got.ID != feedbackID || got.Title != "hello world" || got.Level != "info" || got.Platform != "node" || got.Environment != "prod" || got.Release != "r1" || got.TraceID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected normalized metadata: %+v", got)
	}
	if got.Feedback == nil || got.Feedback.Message != "  hello\n  world " || got.Feedback.Name != "  SDK reporter  " || got.Feedback.Email != "sdk@example.test" || got.Feedback.URL != "https://example.test/source" || got.Feedback.AssociatedEventID != associatedID {
		t.Fatalf("unexpected feedback fields: %+v", got.Feedback)
	}
	var in, out map[string]any
	decode := json.NewDecoder(bytes.NewReader(raw))
	decode.UseNumber()
	if err := decode.Decode(&in); err != nil {
		t.Fatal(err)
	}
	decode = json.NewDecoder(bytes.NewReader(got.Data))
	decode.UseNumber()
	if err := decode.Decode(&out); err != nil {
		t.Fatal(err)
	}
	in["event_id"] = got.ID
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("payload changed beyond normalized event_id: %s", got.Data)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, exists := metadata["Feedback"]; exists {
		t.Fatalf("feedback leaked into event metadata JSON: %s", encoded)
	}
	if _, exists := metadata["feedback"]; exists {
		t.Fatalf("feedback leaked into event metadata JSON: %s", encoded)
	}
	if !strings.Contains(string(metadata["data"]), `"message":"  hello\n  world "`) {
		t.Fatalf("raw feedback message was not retained in event data: %s", metadata["data"])
	}
}

func TestFeedbackNormalizeDefaultsAndUUIDAssociation(t *testing.T) {
	minimal, err := event.Normalize(feedbackPayload(`{"message":"x","associated_event_id":"abcdef01-2345-6789-abcd-ef0123456789"}`), "", feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	if minimal.Feedback == nil || minimal.Feedback.AssociatedEventID != associatedID || minimal.Level != "info" || minimal.Platform != "other" || !minimal.Timestamp.Equal(feedbackNow) {
		t.Fatalf("feedback defaults or UUID association incorrect: %+v", minimal)
	}
	emptyAssociation, err := event.Normalize(feedbackPayload(`{"message":"x","associated_event_id":""}`), "", feedbackNow)
	if err != nil || emptyAssociation.Feedback == nil || emptyAssociation.Feedback.AssociatedEventID != "" {
		t.Fatalf("empty association should mean absent: %+v, %v", emptyAssociation.Feedback, err)
	}
	for _, level := range []string{"info", "warning"} {
		raw := json.RawMessage(`{"type":"feedback","event_id":"` + feedbackID + `","level":"` + level + `","platform":"go","contexts":{"feedback":{"message":"x"}}}`)
		got, err := event.Normalize(raw, "", feedbackNow)
		if err != nil || got.Level != level || got.Platform != "go" {
			t.Errorf("supplied level/platform not preserved (%q): %+v, %v", level, got, err)
		}
	}
}

func feedbackFieldPayload(field, value string) json.RawMessage {
	if field == "message" {
		return feedbackPayload(`{"message":"` + value + `"}`)
	}
	return feedbackPayload(`{"message":"x","` + field + `":"` + value + `"}`)
}

func TestFeedbackNormalizeOptionalFieldsAndUnicodeLimits(t *testing.T) {
	minimal, err := event.Normalize(feedbackPayload(`{"message":"x"}`), "", feedbackNow)
	if err != nil || minimal.Feedback == nil || minimal.Feedback.Message != "x" || minimal.Feedback.Name != "" || minimal.Feedback.Email != "" || minimal.Feedback.URL != "" || minimal.Feedback.AssociatedEventID != "" {
		t.Fatalf("minimal feedback: %+v, %v", minimal, err)
	}
	for _, tc := range []struct {
		field string
		max   int
	}{{"message", 10000}, {"name", 128}, {"contact_email", 254}, {"url", 2048}} {
		t.Run(tc.field, func(t *testing.T) {
			value := strings.Repeat("界", tc.max)
			raw := feedbackFieldPayload(tc.field, value)
			if _, err := event.Normalize(raw, "", feedbackNow); err != nil {
				t.Fatalf("accepted boundary length %d: %v", tc.max, err)
			}
			value += "界"
			raw = feedbackFieldPayload(tc.field, value)
			if _, err := event.Normalize(raw, "", feedbackNow); err == nil {
				t.Fatalf("accepted %d runes for %s", tc.max+1, tc.field)
			}
		})
	}
	long := strings.Repeat("界", 300)
	got, err := event.Normalize(feedbackPayload(`{"message":"`+long+`"}`), "", feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(got.Title) != 255 || got.Title != strings.Repeat("界", 255) {
		t.Fatalf("title rune count/content invalid: %d", utf8.RuneCountInString(got.Title))
	}
}

func TestFeedbackNormalizeRejectsMalformedFieldsAndReferences(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing context", `{"type":"feedback"}`},
		{"null context", `{"type":"feedback","contexts":{"feedback":null}}`},
		{"array context", `{"type":"feedback","contexts":{"feedback":[]}}`},
		{"primitive contexts", `{"type":"feedback","contexts":3}`},
		{"missing message", `{"type":"feedback","contexts":{"feedback":{}}}`},
		{"null message", `{"type":"feedback","contexts":{"feedback":{"message":null}}}`},
		{"nonstring message", `{"type":"feedback","contexts":{"feedback":{"message":42}}}`},
		{"blank message", `{"type":"feedback","contexts":{"feedback":{"message":" \n\t "}}}`},
		{"null name", `{"type":"feedback","contexts":{"feedback":{"message":"x","name":null}}}`},
		{"number name", `{"type":"feedback","contexts":{"feedback":{"message":"x","name":2}}}`},
		{"null email", `{"type":"feedback","contexts":{"feedback":{"message":"x","contact_email":null}}}`},
		{"object email", `{"type":"feedback","contexts":{"feedback":{"message":"x","contact_email":{}}}}`},
		{"array url", `{"type":"feedback","contexts":{"feedback":{"message":"x","url":[]}}}`},
		{"null url", `{"type":"feedback","contexts":{"feedback":{"message":"x","url":null}}}`},
		{"number association", `{"type":"feedback","contexts":{"feedback":{"message":"x","associated_event_id":1}}}`},
		{"null association", `{"type":"feedback","contexts":{"feedback":{"message":"x","associated_event_id":null}}}`},
		{"bad association", `{"type":"feedback","contexts":{"feedback":{"message":"x","associated_event_id":"no"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := event.Normalize(json.RawMessage(tc.body), "", feedbackNow); err == nil {
				t.Fatalf("accepted %s", tc.body)
			}
		})
	}
	for _, value := range []string{`"` + feedbackID + `"`, `"` + strings.ToUpper(feedbackID) + `"`, `"01234567-89AB-CDEF-0123-456789ABCDEF"`} {
		raw := feedbackPayload(`{"message":"x","associated_event_id":` + value + `}`)
		if _, err := event.Normalize(raw, "", feedbackNow); err == nil {
			t.Errorf("accepted self association %s", value)
		}
	}
	invalidUTF8 := append([]byte(`{"type":"feedback","event_id":"`+feedbackID+`","contexts":{"feedback":{"message":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}}}`)...)
	if _, err := event.Normalize(invalidUTF8, "", feedbackNow); err == nil {
		t.Error("accepted invalid UTF-8")
	}
	for _, field := range []string{"message", "name", "contact_email", "url"} {
		bad := `{"type":"feedback","event_id":"` + feedbackID + `","contexts":{"feedback":{"message":"x","` + field + `":"bad\u0000text"}}}`
		if _, err := event.Normalize(json.RawMessage(bad), "", feedbackNow); err == nil {
			t.Errorf("accepted NUL in %s", field)
		}
	}
}

func TestFeedbackNormalizeIdentityAndLegacyBehavior(t *testing.T) {
	const other = "11111111111111111111111111111111"
	base := feedbackPayload(`{"message":"same","associated_event_id":"` + associatedID + `"}`)
	custom := json.RawMessage(strings.Replace(string(base), `"type":"feedback"`, `"type":"feedback","fingerprint":["custom"]`, 1))
	a, err := event.Normalize(base, "", feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	b, err := event.Normalize(custom, "", feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	otherPayload := json.RawMessage(strings.Replace(string(feedbackPayload(`{"message":"same"}`)), feedbackID, other, 1))
	c, err := event.Normalize(otherPayload, "", feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != "feedback:"+feedbackID || b.Fingerprint != a.Fingerprint || c.Fingerprint != "feedback:"+other || a.Fingerprint == c.Fingerprint {
		t.Fatalf("feedback identity grouping: %q %q %q", a.Fingerprint, b.Fingerprint, c.Fingerprint)
	}
	for _, payloadID := range []string{other, "11111111-1111-1111-1111-111111111111"} {
		raw := json.RawMessage(strings.Replace(string(base), feedbackID, payloadID, 1))
		if _, err := event.Normalize(raw, feedbackID, feedbackNow); err == nil {
			t.Errorf("accepted conflicting modern IDs payload=%s envelope=%s", payloadID, feedbackID)
		}
	}
	legacy, err := event.Normalize(json.RawMessage(`{"message":"old"}`), feedbackID, feedbackNow)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ID != feedbackID || legacy.Type != "error" || legacy.Feedback != nil {
		t.Fatalf("legacy behavior changed: %+v", legacy)
	}
}
