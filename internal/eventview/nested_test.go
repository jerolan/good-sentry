package eventview_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"goodsentry/internal/eventview"
)

func entryData(t *testing.T, view eventview.Event, kind string) map[string]any {
	t.Helper()
	for _, item := range eventObject(t, view)["entries"].([]any) {
		entry := item.(map[string]any)
		if entry["type"] == kind {
			return entry["data"].(map[string]any)
		}
	}
	t.Fatalf("missing %s entry", kind)
	return nil
}

func TestNestedRequestCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, request string
		headers       any
		query         any
		queryPresent  bool
		contentType   any
	}{
		{
			name:    "object headers and encoded query",
			request: `{"method":"POST","url":"https://example.test/pay?ignored=1","headers":{"Z":"last","content-type":"application/json; charset=utf-8","A":null,"bad":3},"query_string":"dup=one&dup=two+words&percent=%2520&bad=%ZZ&lonely&=empty","data":[1,null,{"kept":true}],"cookies":{"session":"x","bad":3,"empty":null},"bodySize":17,"env":{"REMOTE_ADDR":"127.0.0.1"},"protocol":3,"future":{"kept":true}}`,
			headers: []any{[]any{"A", nil}, []any{"Z", "last"}, []any{"content-type", "application/json; charset=utf-8"}},
			query:   []any{[]any{"dup", "one"}, []any{"dup", "two words"}, []any{"percent", "%20"}, []any{"lonely", ""}, []any{"", "empty"}}, queryPresent: true,
			contentType: "application/json",
		},
		{
			name:    "paired headers and explicit query win",
			request: `{"url":"https://example.test/?ignored=1","headers":[["Content-Type","invalid"],["CONTENT-TYPE","text/plain; charset=utf-8"],["Content-Type","application/json"],[null,"nullable"],["bad",3],["bad","length","three"]],"query":[["dup","one"],["dup",null],[null,"key"],["bad",4]],"query_string":"ignored=also"}`,
			headers: []any{[]any{"Content-Type", "invalid"}, []any{"CONTENT-TYPE", "text/plain; charset=utf-8"}, []any{"Content-Type", "application/json"}, []any{nil, "nullable"}},
			query:   []any{[]any{"dup", "one"}, []any{"dup", nil}, []any{nil, "key"}}, queryPresent: true,
			contentType: "text/plain",
		},
		{
			name:    "URL fallback and retained MIME",
			request: `{"url":"https://example.test/pay?x=first&x=second&escaped=%252B#kept","headers":[],"inferredContentType":"application/custom"}`,
			headers: []any{}, query: []any{[]any{"x", "first"}, []any{"x", "second"}, []any{"escaped", "%2B"}}, queryPresent: true,
			contentType: "application/custom",
		},
		{
			name:    "explicit malformed query blocks URL fallback",
			request: `{"url":"https://example.test/?must=notappear","query":7,"headers":[]}`,
			headers: []any{}, queryPresent: false, contentType: nil,
		},
		{
			name:    "explicit null and malformed known fields",
			request: `{"url":"https://example.test/?must=notappear","query":null,"headers":null,"inferredContentType":7,"method":false,"apiTarget":[],"bodySize":"17","env":[],"cookies":4,"fragment":null,"data":false}`,
			headers: nil, query: nil, queryPresent: true, contentType: nil,
		},
		{
			name:    "query object sorts keys and filters values",
			request: `{"query_string":{"z":"last","a":null,"bad":3},"headers":[],"inferredContentType":null}`,
			headers: []any{}, query: []any{[]any{"a", nil}, []any{"z", "last"}}, queryPresent: true, contentType: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"request":` + tc.request + `}`)
			before := append([]byte(nil), raw...)
			view, err := eventview.Project(eventview.Record{Data: raw}, true)
			if err != nil {
				t.Fatal(err)
			}
			request := entryData(t, view, "request")
			if !reflect.DeepEqual(request["headers"], tc.headers) {
				t.Errorf("headers = %#v, want %#v", request["headers"], tc.headers)
			}
			if query, present := request["query"]; present != tc.queryPresent || !reflect.DeepEqual(query, tc.query) {
				t.Errorf("query = %#v (present=%t), want %#v (present=%t)", query, present, tc.query, tc.queryPresent)
			}
			if value, present := request["inferredContentType"]; !present || value != tc.contentType {
				t.Errorf("inferredContentType = %#v (present=%t), want %#v", value, present, tc.contentType)
			}
			if tc.name == "object headers and encoded query" {
				if request["method"] != "POST" || request["bodySize"] != json.Number("17") || request["url"] != "https://example.test/pay?ignored=1" || !reflect.DeepEqual(request["data"], []any{json.Number("1"), nil, map[string]any{"kept": true}}) || request["future"].(map[string]any)["kept"] != true {
					t.Errorf("valid fields lost: %+v", request)
				}
				if _, present := request["protocol"]; present || !reflect.DeepEqual(request["cookies"], map[string]any{"session": "x", "empty": nil}) {
					t.Errorf("malformed protocol/cookies retained: %+v", request)
				}
			}
			if tc.name == "explicit null and malformed known fields" {
				for _, key := range []string{"method", "apiTarget", "bodySize", "env", "cookies"} {
					if _, present := request[key]; present {
						t.Errorf("malformed %s retained", key)
					}
				}
				if request["data"] != false || request["fragment"] != nil {
					t.Errorf("arbitrary body/null fragment lost: %+v", request)
				}
			}
			legacy, err := eventview.LegacyJSON(eventview.Record{Data: raw})
			if err != nil {
				t.Fatal(err)
			}
			var original, legacyObject map[string]json.RawMessage
			if json.Unmarshal(raw, &original) != nil || json.Unmarshal(legacy, &legacyObject) != nil {
				t.Fatal("invalid raw or legacy JSON")
			}
			var rawRequest, legacyRequest any
			_ = json.Unmarshal(original["request"], &rawRequest)
			_ = json.Unmarshal(legacyObject["request"], &legacyRequest)
			if !reflect.DeepEqual(rawRequest, legacyRequest) || !bytes.Equal(raw, before) {
				t.Fatal("nested conversion mutated native or legacy request")
			}
		})
	}
}

func TestNestedBreadcrumbUTCAndTypedFields(t *testing.T) {
	raw := json.RawMessage(`{"breadcrumbs":{"values":[{"timestamp":-0.25,"message":"before epoch","category":"test","data":{"number":9007199254740993}},{"timestamp":"2026-10-15T01:02:03.123456789-06:00","level":"warning","future":{"kept":true}},{"timestamp":"2026-10-15T07:02:03.5","type":"http","event_id":"known"},{"message":"timestamp absent"},{"timestamp":null},{"timestamp":"not a time"},{"timestamp":9999999999999999},{"timestamp":true,"category":3,"message":[],"type":false,"level":"unsupported","event_id":{},"data":[]},{"timestamp":1792067503.185},{"timestamp":1792067503.123456789},false],"outer":"omit"}}`)
	before := append([]byte(nil), raw...)
	view, err := eventview.Project(eventview.Record{Data: raw}, true)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := entryData(t, view, "breadcrumbs")
	if len(wrapper) != 1 {
		t.Errorf("breadcrumb wrapper retained non-values fields: %+v", wrapper)
	}
	crumbs := wrapper["values"].([]any)
	if len(crumbs) != 10 {
		t.Fatalf("breadcrumbs = %#v", crumbs)
	}
	for index, want := range map[int]string{0: "1969-12-31T23:59:59.75Z", 1: "2026-10-15T07:02:03.123456789Z", 2: "2026-10-15T07:02:03.5Z", 8: "2026-10-15T12:31:43.185Z", 9: "2026-10-15T12:31:43.123456789Z"} {
		if got := crumbs[index].(map[string]any)["timestamp"]; got != want {
			t.Errorf("breadcrumb %d timestamp = %#v, want %s", index, got, want)
		}
	}
	if _, present := crumbs[3].(map[string]any)["timestamp"]; present {
		t.Error("missing timestamp was invented")
	}
	for _, index := range []int{4, 5, 6, 7} {
		if value, present := crumbs[index].(map[string]any)["timestamp"]; !present || value != nil {
			t.Errorf("invalid/null timestamp %d = %#v, present=%t", index, value, present)
		}
	}
	if crumbs[0].(map[string]any)["data"].(map[string]any)["number"] != json.Number("9007199254740993") || crumbs[1].(map[string]any)["future"].(map[string]any)["kept"] != true || crumbs[2].(map[string]any)["event_id"] != "known" {
		t.Error("actual breadcrumb fields lost")
	}
	if len(crumbs[7].(map[string]any)) != 1 {
		t.Errorf("malformed breadcrumb fields retained: %+v", crumbs[7])
	}
	legacy, err := eventview.LegacyJSON(eventview.Record{Data: raw})
	if err != nil {
		t.Fatal(err)
	}
	var input, projected map[string]json.RawMessage
	_ = json.Unmarshal(raw, &input)
	_ = json.Unmarshal(legacy, &projected)
	if !bytes.Equal(input["breadcrumbs"], projected["breadcrumbs"]) || !bytes.Equal(raw, before) {
		t.Fatal("nested breadcrumb conversion changed stored or legacy payload")
	}
	// JSON decimal exponents and dates outside UnixNano's range exercise the
	// same public timestamp contract without a floating-point expected value.
	for _, tc := range []struct {
		raw  string
		want any
	}{
		{"1.792067503185e9", "2026-10-15T12:31:43.185Z"},
		{"-1e-9", "1969-12-31T23:59:59.999999999Z"},
		{"5e-10", "1970-01-01T00:00:00.000000001Z"},
		{"1e-999999999999999999999", "1970-01-01T00:00:00Z"},
		{"1e999999999999999999999", nil},
		{"253402300799.123456789", "9999-12-31T23:59:59.123456789Z"},
		{"253402300800", nil},
		{`"9999-12-31T23:59:59-01:00"`, nil},
		{`"0001-01-01T00:00:00+01:00"`, nil},
	} {
		view, err := eventview.Project(eventview.Record{Data: json.RawMessage(`{"breadcrumbs":[{"timestamp":` + tc.raw + `}]}`)}, true)
		if err != nil {
			t.Fatal(err)
		}
		crumb := entryData(t, view, "breadcrumbs")["values"].([]any)[0].(map[string]any)
		if got := crumb["timestamp"]; got != tc.want {
			t.Errorf("timestamp %s = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}
