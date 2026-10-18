package eventview_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/eventview"
)

func eventObject(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestProjectActualPayloadAndDetachedResults(t *testing.T) {
	raw := json.RawMessage(`{"event_id":"untrusted","level":"warning","platform":"javascript","dist":"2","culprit":"checkout","logentry":{"formatted":"charge failed","message":"charge %s","params":[42]},"exception":{"values":[{"type":"Cause","value":"first"},{"type":"CheckoutError","value":"declined","stacktrace":{"frames":[{"filename":"charge.ts","lineno":27,"function":"charge","context_line":"throw error"}]},"raw_stacktrace":{"frames":[{"filename":"app.min.js","lineno":1,"colno":934}]}},{}]},"tags":[["region","mx"],["region","us"],["bad",3],["empty",""],["extra","ignored","shape"]],"user":{"id":"42","email":"a@example.com","data":{"paid":true}},"request":{"method":"POST","url":"https://example.com/pay","data":{"cart":42}},"breadcrumbs":[{"timestamp":123,"message":"clicked"}],"contexts":{"trace":{"trace_id":"abc","span_id":"def"}},"extra":{"cart":{"id":42}},"sdk":{"name":"sentry.javascript.browser","version":"11.7.0"},"modules":{"checkout":"2.0","bad":7,"unset":null},"errors":[{"type":"js_no_source","data":{"url":"app.js"}}],"environment":"production","release":"shop@2.0"}`)
	before := append([]byte(nil), raw...)
	report := json.RawMessage(`{"name":"Ada Lovelace","email":"ada@example.com","comments":"The page failed after checkout."}`)
	reportBefore := append([]byte(nil), report...)
	next := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	released := time.Date(2026, 10, 15, 2, 3, 4, 0, time.FixedZone("offset", -6*3600))
	record := eventview.Record{
		ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GroupID: "15", ProjectID: 7,
		Kind: "error", Title: strings.Repeat("界", 254) + "🙂end", Level: "warning",
		Timestamp:   time.Date(2026, 10, 15, 1, 2, 3, 123456789, time.FixedZone("offset", -6*3600)),
		Received:    time.Date(2026, 10, 15, 1, 2, 4, 987654321, time.FixedZone("offset", -6*3600)),
		Environment: "production", Release: "shop@2.0", Data: raw, UserReport: report, SiblingsKnown: true, NextID: &next,
		ReleaseInfo: &eventview.ReleaseReference{ID: "9", Version: "shop@2.0", DateCreated: released.Add(-time.Hour), DateReleased: &released},
	}
	summary, err := eventview.Project(record, false)
	if err != nil {
		t.Fatal(err)
	}
	s := eventObject(t, summary)
	if s["id"] != record.ID || s["eventID"] != record.ID || s["projectID"] != json.Number("7") || s["groupID"] != "15" || s["type"] != "error" || s["message"] != "charge failed" || s["level"] != "warning" || s["platform"] != "javascript" || s["dist"] != "2" || s["culprit"] != "checkout" {
		t.Fatalf("wrong summary identity or payload: %+v", s)
	}
	if s["dateCreated"] != "2026-10-15T07:02:03.123456789Z" || s["dateReceived"] != "2026-10-15T07:02:04.987654321Z" || s["title"] != strings.Repeat("界", 254)+"🙂" {
		t.Fatalf("wrong dates or Unicode title: %+v", s)
	}
	if !reflect.DeepEqual(s["metadata"], map[string]any{"type": "CheckoutError", "value": "declined"}) || len(s["tags"].([]any)) != 3 || s["user"].(map[string]any)["id"] != "42" {
		t.Fatalf("lost metadata/tags/user: %+v", s)
	}
	for _, field := range []string{"entries", "contexts", "context", "sdk", "packages", "errors", "release", "userReport"} {
		if _, exists := s[field]; exists {
			t.Fatalf("summary exposed full-only %s", field)
		}
	}
	full, err := eventview.Project(record, true)
	if err != nil {
		t.Fatal(err)
	}
	f := eventObject(t, full)
	if full.UserReport == nil || !bytes.Equal(*full.UserReport, reportBefore) {
		t.Fatalf("full userReport = %q, want exact report bytes %q", full.UserReport, reportBefore)
	}
	if value, exists := f["userReport"]; !exists || !reflect.DeepEqual(value, map[string]any{"name": "Ada Lovelace", "email": "ada@example.com", "comments": "The page failed after checkout."}) {
		t.Fatalf("full userReport projection = %#v, present=%t", value, exists)
	}
	if value, exists := f["previousEventID"]; !exists || value != nil || f["nextEventID"] != next {
		t.Fatalf("known nullable siblings = %+v", f)
	}
	entries := f["entries"].([]any)
	byType := map[string]map[string]any{}
	for _, entry := range entries {
		item := entry.(map[string]any)
		byType[item["type"].(string)] = item["data"].(map[string]any)
	}
	if len(byType) != 4 || byType["message"]["formatted"] != "charge failed" || byType["request"]["method"] != "POST" || len(byType["breadcrumbs"]["values"].([]any)) != 1 {
		t.Fatalf("actual entries = %+v", byType)
	}
	exception := byType["exception"]["values"].([]any)[1].(map[string]any)
	mapped := exception["stacktrace"].(map[string]any)["frames"].([]any)[0].(map[string]any)
	original := exception["raw_stacktrace"].(map[string]any)["frames"].([]any)[0].(map[string]any)
	if mapped["filename"] != "charge.ts" || mapped["context_line"] != "throw error" || original["filename"] != "app.min.js" || original["colno"] != json.Number("934") {
		t.Fatalf("stack fields lost: mapped=%+v original=%+v", mapped, original)
	}
	if f["context"].(map[string]any)["cart"].(map[string]any)["id"] != json.Number("42") || f["packages"].(map[string]any)["checkout"] != "2.0" || len(f["packages"].(map[string]any)) != 2 || f["packages"].(map[string]any)["unset"] != nil || len(f["errors"].([]any)) != 1 {
		t.Fatalf("full payload lost: %+v", f)
	}
	(*full.UserReport)[0] = '['
	reprojected, err := eventview.Project(record, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(report, reportBefore) || reprojected.UserReport == nil || !bytes.Equal(*reprojected.UserReport, reportBefore) {
		t.Fatalf("mutating projected userReport changed stored or fresh bytes: stored=%q fresh=%v", report, reprojected.UserReport)
	}
	copy(*full.UserReport, reportBefore)
	withoutReport := record
	withoutReport.UserReport = nil
	emptyReport, err := eventview.Project(withoutReport, true)
	if err != nil || emptyReport.UserReport == nil || string(*emptyReport.UserReport) != "null" {
		t.Fatal("missing report must remain an explicit null in full views", emptyReport.UserReport, err)
	}
	release := f["release"].(map[string]any)
	if release["id"] != "9" || release["version"] != "shop@2.0" || release["dateCreated"] != "2026-10-15T07:03:04Z" || release["dateReleased"] != "2026-10-15T08:03:04Z" {
		t.Fatalf("actual release metadata = %+v", release)
	}
	if !bytes.Equal(raw, before) || record.Title != strings.Repeat("界", 254)+"🙂end" {
		t.Fatal("projection mutated stored input")
	}
	// Mutating caller-owned source metadata after projection must not change its response.
	next = "changed"
	record.ReleaseInfo.Version = "changed"
	released = time.Time{}
	raw[0] = '['
	after := eventObject(t, full)
	if after["nextEventID"] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || after["release"].(map[string]any)["version"] != "shop@2.0" || after["release"].(map[string]any)["dateReleased"] != "2026-10-15T08:03:04Z" || after["user"].(map[string]any)["id"] != "42" {
		t.Fatal("projected response retained mutable source references")
	}
}

func TestProjectEvidenceAndMalformedOptionalShapes(t *testing.T) {
	for _, tc := range []struct {
		name, kind, raw, typ, message string
		metadata                      map[string]any
		entries                       int
	}{
		{"bare SDK exceptions", "error", `{"exception":[{"type":"First"},{"value":"last"},{}]}`, "error", "", map[string]any{"value": "last"}, 1},
		{"stack only", "error", `{"exception":{"values":[{"stacktrace":{"frames":[{"filename":"main.go"}]}}]}}`, "error", "", map[string]any{}, 1},
		{"message object", "error", `{"message":{"formatted":"actual  formatted","message":"template","params":["x"]}}`, "default", "actual  formatted", map[string]any{"title": "actual  formatted"}, 1},
		{"message string", "message", `{"message":"plain"}`, "default", "plain", map[string]any{"title": "plain"}, 1},
		{"transaction", "transaction", `{"type":"transaction","transaction":"GET /pay","exception":{"values":[{"type":"Error"}]}}`, "transaction", "", map[string]any{}, 1},
		{"CSP", "error", `{"csp":{"blocked_uri":"https://blocked.example"}}`, "csp", "", map[string]any{}, 1},
		{"malformed optionals", "error", `{"exception":"bad","message":{"formatted":3},"logentry":false,"tags":[["x",3],null,{"k":"v"}],"user":false,"sdk":[],"contexts":[],"extra":[],"modules":[],"request":[],"breadcrumbs":false,"errors":{},"dist":3,"platform":4,"culprit":false}`, "default", "", map[string]any{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := eventview.Record{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: tc.kind, Title: "stored title", Level: "error", Data: json.RawMessage(tc.raw), Release: "metadata is not associated"}
			got, err := eventview.Project(record, true)
			if err != nil {
				t.Fatal(err)
			}
			object := eventObject(t, got)
			if object["type"] != tc.typ || object["message"] != tc.message || !reflect.DeepEqual(object["metadata"], tc.metadata) || len(object["entries"].([]any)) != tc.entries {
				t.Fatalf("wrong evidence projection: %+v", object)
			}
			for _, key := range []string{"previousEventID", "nextEventID", "release"} {
				if _, exists := object[key]; exists {
					t.Fatalf("uncomputed %s exposed", key)
				}
			}
			if tc.name == "malformed optionals" {
				if len(object["tags"].([]any)) != 0 {
					t.Fatalf("invented malformed tags: %+v", object["tags"])
				}
				for _, key := range []string{"user", "sdk", "contexts", "context", "packages", "errors", "dist", "platform", "culprit"} {
					if _, exists := object[key]; exists {
						t.Fatalf("malformed %s retained", key)
					}
				}
			}
		})
	}
	object, err := eventview.Project(eventview.Record{Data: json.RawMessage(`{"tags":{"z":"last","a":"first","number":3,"null":null}}`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	tags := eventObject(t, object)["tags"].([]any)
	if !reflect.DeepEqual(tags, []any{map[string]any{"key": "a", "value": "first"}, map[string]any{"key": "z", "value": "last"}}) {
		t.Fatalf("string object tags = %+v", tags)
	}
	for _, raw := range []string{`null`, `[]`, `"text"`, `{"broken"`} {
		if _, err := eventview.Project(eventview.Record{Data: json.RawMessage(raw)}, true); err == nil {
			t.Errorf("invalid root accepted: %s", raw)
		}
		if _, err := eventview.LegacyJSON(eventview.Record{Data: json.RawMessage(raw)}); err == nil {
			t.Errorf("invalid legacy root accepted: %s", raw)
		}
	}
}

func TestProjectFeedbackUsesStoredContextMessage(t *testing.T) {
	contexts := json.RawMessage(`{ "feedback" : { "message" : "  Follow up  ", "score" : 9007199254740993, "answered" : true } }`)
	data := json.RawMessage(`{"type":"transaction","transaction":"GET /wrong","csp":{"blocked_uri":"https://wrong.example"},"exception":{"values":[{"type":"WrongError","value":"wrong"}]},"contexts":` + string(contexts) + `}`)
	dataBefore := append([]byte(nil), data...)
	report := json.RawMessage(`{"comments":"must not leak"}`)
	record := eventview.Record{Kind: "feedback", Title: strings.Repeat("界", 256), Data: data, UserReport: report}

	for _, full := range []bool{false, true} {
		got, err := eventview.Project(record, full)
		if err != nil {
			t.Fatal(err)
		}
		projected := eventObject(t, got)
		if projected["type"] != "feedback" || projected["message"] != "  Follow up  " || projected["title"] != strings.Repeat("界", 255) {
			t.Fatalf("feedback projection = %+v", projected)
		}
		if !reflect.DeepEqual(projected["metadata"], map[string]any{"title": strings.Repeat("界", 255)}) {
			t.Fatalf("feedback metadata = %#v", projected["metadata"])
		}
		if !full {
			for _, field := range []string{"contexts", "entries", "userReport"} {
				if _, exists := projected[field]; exists {
					t.Fatalf("summary exposed %s", field)
				}
			}
			continue
		}
		if !bytes.Equal(got.Contexts, contexts) {
			t.Fatalf("contexts bytes = %q, want %q", got.Contexts, contexts)
		}
		if got.UserReport == nil || string(*got.UserReport) != "null" || projected["userReport"] != nil {
			t.Fatalf("feedback userReport = %v / %#v", got.UserReport, projected["userReport"])
		}
		got.Contexts[0] = '['
		if !bytes.Equal(data, dataBefore) {
			t.Fatal("mutating projected contexts changed stored event data")
		}
		fresh, err := eventview.Project(record, true)
		if err != nil || !bytes.Equal(fresh.Contexts, contexts) {
			t.Fatalf("fresh contexts = %q, err=%v", fresh.Contexts, err)
		}
	}
}

func TestLegacyJSONRealIdentityExactUnixAndTagDictionary(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"before epoch fractional", time.Unix(-1, 750000000), "-0.25"},
		{"negative nanosecond", time.Unix(-2, 999999999), "-1.000000001"},
		{"positive nanosecond", time.Unix(0, 1), "0.000000001"},
		{"whole negative", time.Unix(-1, 0), "-1"},
		{"beyond UnixNano", time.Date(9999, 12, 31, 23, 59, 59, 123456789, time.UTC), "253402300799.123456789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"event_id":"old","timestamp":"not authoritative","project":99,"transaction":"GET /pay","tags":[["region","mx"],["region","us"],["region",false],["empty",""]],"extra":{"large":9007199254740993},"future":{"kept":true}}`)
			before := append([]byte(nil), raw...)
			record := eventview.Record{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: 7, Kind: "message", Title: "actual title", Timestamp: tc.at, Data: raw}
			out, err := eventview.LegacyJSON(record)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			decoder := json.NewDecoder(bytes.NewReader(out))
			decoder.UseNumber()
			if err := decoder.Decode(&object); err != nil {
				t.Fatal(err)
			}
			if object["event_id"] != record.ID || object["project"] != json.Number("7") || object["timestamp"] != json.Number(tc.want) || object["datetime"] != tc.at.UTC().Format(time.RFC3339Nano) || object["title"] != "actual title" || object["transaction"] != "GET /pay" {
				t.Fatalf("legacy identity/time = %s", out)
			}
			if !reflect.DeepEqual(object["tags"], map[string]any{"region": "us", "empty": ""}) || object["extra"].(map[string]any)["large"] != json.Number("9007199254740993") || object["future"].(map[string]any)["kept"] != true {
				t.Fatalf("legacy retained payload/tag dictionary = %s", out)
			}
			if _, exists := object["hashes"]; exists || !bytes.Equal(raw, before) {
				t.Fatal("legacy invented hashes or mutated stored input")
			}
		})
	}
	got, err := eventview.LegacyJSON(eventview.Record{Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var empty map[string]any
	if err := json.Unmarshal(got, &empty); err != nil || empty["transaction"] != "" || !reflect.DeepEqual(empty["tags"], map[string]any{}) {
		t.Fatalf("legacy required empty fields = %s (%v)", got, err)
	}
}
