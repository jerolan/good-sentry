package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const legacyCSPReport = `{"csp-report":{"document-uri":"https://app.example/account","blocked-uri":"https://blocked.example/script.js?nonce=old#part","effective-directive":"script-src-elem","violated-directive":"script-src-elem","original-policy":"script-src 'self'; report-uri https://collector.example/","source-file":"https://app.example/index.js","script-sample":"alert('blocked')","line-number":7,"column-number":4,"status-code":200,"disposition":"report","browser-extension":{"future":true}}}`

// These cases own translation from actual browser wire formats into searchable
// event fields, stable grouping and retained originals. They do not mirror SQL.
func TestSecurityReportBrowserFormatsAndGrouping(t *testing.T) {
	now := time.Date(2026, 10, 18, 13, 20, 21, 123456000, time.FixedZone("offset", -6*3600))
	options := SecurityReportOptions{Release: "web@1", Environment: "production", UserAgent: "Actual request browser"}
	legacy, err := NormalizeSecurityReports([]byte(legacyCSPReport), now, options)
	if err != nil || len(legacy) != 1 {
		t.Fatalf("legacy report rejected: %v %+v", err, legacy)
	}
	got := legacy[0]
	if got.Type != "csp" || got.Level != "warning" || got.Platform != "javascript" || got.Release != "web@1" || got.Environment != "production" || got.ID == "" || !got.Timestamp.Equal(now) || got.Timestamp.Location() != time.UTC || !strings.Contains(got.Title, "script-src-elem") {
		t.Fatalf("CSP event metadata lost: %+v", got)
	}
	var fields struct {
		Type string `json:"type"`
		CSP  struct {
			Blocked string `json:"blocked_uri"`
			Line    uint32 `json:"line_number"`
			Sample  string `json:"script_sample"`
		} `json:"csp"`
		Request struct {
			URL     string      `json:"url"`
			Headers [][2]string `json:"headers"`
		} `json:"request"`
		Tags  map[string]string `json:"tags"`
		Extra struct {
			Report json.RawMessage `json:"security_report"`
		} `json:"extra"`
	}
	if err := json.Unmarshal(got.Data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields.Type != "csp" || fields.CSP.Blocked != "https://blocked.example/script.js?nonce=old#part" || fields.CSP.Line != 7 || fields.CSP.Sample != "alert('blocked')" || fields.Request.URL != "https://app.example/account" || len(fields.Request.Headers) != 1 || fields.Request.Headers[0][1] != options.UserAgent || fields.Tags["blocked-host"] != "blocked.example" || !strings.Contains(string(fields.Extra.Report), `"browser-extension":{"future":true}`) {
		t.Fatalf("CSP body or original report lost: %s", got.Data)
	}
	modern := `[{"type":"csp-violation","url":"https://app.example/another-page","user_agent":"Reported browser","age":1200,"body":{"blockedURL":"https://blocked.example/script.js?nonce=new#other","effectiveDirective":"script-src-elem","originalPolicy":"script-src 'self'","sample":"alert('new')","lineNumber":99,"statusCode":200}},{"type":"csp-violation","url":"about:blank","body":{"documentURL":"https://explicit.example/","effectiveDirective":"img-src","blockedURL":"inline"}}]`
	events, err := NormalizeSecurityReports([]byte(modern), now, options)
	if err != nil || len(events) != 2 {
		t.Fatalf("Reporting API batch rejected: %v %+v", err, events)
	}
	if events[0].ID == events[1].ID || events[0].ID == got.ID || !events[0].Timestamp.Equal(now.Add(-1200*time.Millisecond)) || !events[1].Timestamp.Equal(now) || events[0].Fingerprint != got.Fingerprint || events[1].Fingerprint == got.Fingerprint {
		t.Fatalf("browser reports lost identity, age or grouping: %+v", events)
	}
	if !strings.Contains(string(events[0].Data), `"url":"https://app.example/another-page"`) || !strings.Contains(string(events[0].Data), "Reported browser") || !strings.Contains(string(events[1].Data), `"url":"https://explicit.example/"`) {
		t.Fatalf("modern URL/user-agent translation lost: %s %s", events[0].Data, events[1].Data)
	}
	repeated, err := NormalizeSecurityReports([]byte(legacyCSPReport), now, options)
	if err != nil || repeated[0].ID == got.ID || repeated[0].Fingerprint != got.Fingerprint {
		t.Fatal("browser delivery identity was conflated with issue grouping", repeated, err)
	}
	// Old browsers supply only violated-directive, including its source list.
	fallback, err := NormalizeSecurityReports([]byte(`{"csp-report":{"document-uri":"about:blank","violated-directive":"script-src 'self'","blocked-uri":"eval"}}`), now, options)
	if err != nil || len(fallback) != 1 || !strings.Contains(string(fallback[0].Data), `"effective_directive":"script-src"`) {
		t.Fatal("legacy effective directive fallback failed", fallback, err)
	}
}

// CSP3's modern report body permits null for these six fields. Keep null
// distinct from an omitted value or an invented empty string/zero, including
// inside the preserved original report.
func TestSecurityReportModernNullableFieldsRetainNull(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	const report = `{"type":"csp-violation","url":"https://app.example/account","age":0,"body":{"documentURL":"https://app.example/account","referrer":"https://app.example/","blockedURL":"https://blocked.example/script.js","effectiveDirective":"script-src-elem","originalPolicy":"script-src 'self'","sourceFile":"https://app.example/index.js","sample":"alert('blocked')","disposition":"enforce","statusCode":200,"lineNumber":7,"columnNumber":4}}`
	if events, err := NormalizeSecurityReports([]byte("["+report+"]"), now, SecurityReportOptions{}); err != nil || len(events) != 1 {
		t.Fatalf("non-null standards-shaped control rejected: %v %+v", err, events)
	}
	for _, field := range []struct{ modern, normalized string }{
		{"referrer", "referrer"},
		{"blockedURL", "blocked_uri"},
		{"sourceFile", "source_file"},
		{"sample", "script_sample"},
		{"lineNumber", "line_number"},
		{"columnNumber", "column_number"},
	} {
		t.Run(field.modern, func(t *testing.T) {
			var input struct {
				Type string                     `json:"type"`
				URL  string                     `json:"url"`
				Age  int                        `json:"age"`
				Body map[string]json.RawMessage `json:"body"`
			}
			if err := json.Unmarshal([]byte(report), &input); err != nil {
				t.Fatal(err)
			}
			input.Body[field.modern] = json.RawMessage("null")
			raw, err := json.Marshal([]any{input})
			if err != nil {
				t.Fatal(err)
			}
			events, err := NormalizeSecurityReports(raw, now, SecurityReportOptions{})
			if err != nil || len(events) != 1 {
				t.Fatalf("CSP3 nullable %s rejected: %v %+v", field.modern, err, events)
			}
			var payload struct {
				CSP   map[string]json.RawMessage `json:"csp"`
				Extra struct {
					Report struct {
						Body map[string]json.RawMessage `json:"body"`
					} `json:"security_report"`
				} `json:"extra"`
			}
			if err := json.Unmarshal(events[0].Data, &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload.CSP[field.normalized]) != "null" || string(payload.Extra.Report.Body[field.modern]) != "null" {
				t.Fatalf("nullable %s changed or disappeared in normalized/original report: %s", field.modern, events[0].Data)
			}
		})
	}
}

func TestSecurityReportModernNumericAgePreservesElapsedTimeAndOriginal(t *testing.T) {
	now := time.Date(2026, 10, 18, 13, 20, 21, 123456789, time.FixedZone("receipt", -6*3600))
	for _, test := range []struct {
		age     string
		elapsed time.Duration
	}{
		{"0.5", 500 * time.Microsecond},
		{"1e3", time.Second},
		{"9007199254740", 9007199254740 * time.Millisecond},
		{"0.0000009", 0},
	} {
		t.Run(test.age, func(t *testing.T) {
			raw := []byte(`[{"type":"csp-violation","url":"https://app.example/account","age":` + test.age + `,"body":{"effectiveDirective":"script-src"}}]`)
			events, err := NormalizeSecurityReports(raw, now, SecurityReportOptions{})
			if err != nil || len(events) != 1 {
				t.Fatalf("valid numeric age %s rejected: %v %+v", test.age, err, events)
			}
			if !events[0].Timestamp.Equal(now.Add(-test.elapsed)) || events[0].Timestamp.Location() != time.UTC {
				t.Fatalf("age %s produced %s, want elapsed %s before receipt in UTC", test.age, events[0].Timestamp, test.elapsed)
			}
			var payload struct {
				Extra struct {
					Report struct {
						Age json.RawMessage `json:"age"`
					} `json:"security_report"`
				} `json:"extra"`
			}
			if err := json.Unmarshal(events[0].Data, &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload.Extra.Report.Age) != test.age {
				t.Fatalf("original age %s changed to %s", test.age, payload.Extra.Report.Age)
			}
		})
	}
}

func TestSecurityReportRejectsMalformedWholeBatch(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, input := range []string{
		``, `null`, `{}`, `[]`, `{"csp-report":null}`, `{"csp-report":[]}`,
		`{"csp-report":{"document-uri":"https://app.example"}}`,
		`{"csp-report":{"document-uri":7,"effective-directive":"script-src"}}`,
		`{"csp-report":{"document-uri":"https://app.example","effective-directive":null}}`,
		`{"csp-report":{"document-uri":"https://app.example","effective-directive":"script-src","line-number":-1}}`,
		`{"csp-report":{"document-uri":"https://app.example","effective-directive":"script-src","status-code":200.5}}`,
		`{"csp-report":{"document-uri":"https://app.example","effective-directive":"script-src","blocked-uri":"bad\u0000url"}}`,
		`{"csp-report":{},"csp-report":{}}`,
		`{"csp-report":{"document-uri":"https://app.example","effective-directive":"script-src","effective-directive":"img-src"}}`,
		legacyCSPReport + ` {}`,
		`[{"type":"deprecation","url":"https://app.example","body":{}}]`,
		`[{"type":"csp-violation","url":"https://app.example","age":-1,"body":{"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","age":-0.5,"body":{"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","age":9223372036854775807,"body":{"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","age":9223372036854.778,"body":{"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","age":1e309,"body":{"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","age":null,"body":{"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","body":{"documentURL":null,"effectiveDirective":"script-src"}}]`,
		`[{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":null}}]`,
		`[{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":"script-src","originalPolicy":null}}]`,
		`[{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":"script-src","disposition":null}}]`,
		`[{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":"script-src","statusCode":null}}]`,
		`[{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":"script-src"}},{"type":"csp-violation","body":null}]`,
		`[` + strings.Repeat(`{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":"script-src"}},`, MaxSecurityReports) + `{"type":"csp-violation","url":"https://app.example","body":{"effectiveDirective":"script-src"}}]`,
		`{"csp-report":{"document-uri":"bad` + string([]byte{255}) + `","effective-directive":"script-src"}}`,
	} {
		t.Run(input[:min(60, len(input))], func(t *testing.T) {
			if events, err := NormalizeSecurityReports([]byte(input), now, SecurityReportOptions{}); err == nil || len(events) != 0 {
				t.Fatalf("invalid report partially accepted: %+v %v", events, err)
			}
		})
	}
}
