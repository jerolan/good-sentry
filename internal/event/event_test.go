package event_test

import (
	"encoding/json"
	"math"
	"regexp"
	"testing"
	"time"

	"goodsentry/internal/event"
)

// Official sentry-go 0.50.0 emits a bare exception array. Historical SDKs use
// an object with values. Both are the same event contract at the wire boundary.
func TestOfficialGoSDKExceptionArray(t *testing.T) {
	raw := json.RawMessage(`{"event_id":"0123456789abcdef0123456789abcdef","exception":[{"type":"*errors.errorString","value":"SDK array exception","stacktrace":{"frames":[{"filename":"main.go","function":"main"}]}}]}`)
	ev, err := event.Normalize(raw, "", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Title != "*errors.errorString: SDK array exception" {
		t.Fatalf("title=%q", ev.Title)
	}
}

func TestNormalizePreservesSDKContextAndEnvelopeIdentity(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	raw := json.RawMessage(`{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","timestamp":"2026-10-14T09:10:11.123456-06:00","platform":"python","level":"warning","environment":"production","release":"shop@2.0","exception":{"values":[{"type":"ValueError","value":"bad\n input","stacktrace":{"frames":[{"module":"checkout","function":"charge","filename":"/app/checkout.py","lineno":20}]}}]},"contexts":{"trace":{"trace_id":"0123456789abcdef0123456789abcdef"}},"tags":{"region":"mx"},"extra":{"cart":{"ids":[1,2]},"html":"<script>not executable</script>"}}`)
	got, err := event.Normalize(raw, "12345678-90AB-CDEF-1234-567890ABCDEF", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "1234567890abcdef1234567890abcdef" || got.Type != "error" || got.Title != "ValueError: bad input" || got.Level != "warning" || got.Platform != "python" || got.Environment != "production" || got.Release != "shop@2.0" || got.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("wrong normalized metadata: %+v", got)
	}
	if want := time.Date(2026, 10, 14, 15, 10, 11, 123456000, time.UTC); !got.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %v, want %v", got.Timestamp, want)
	}
	var stored struct {
		ID    string `json:"event_id"`
		Extra struct {
			Cart struct{ IDs []int }
			HTML string
		}
		Tags map[string]string
	}
	if err := json.Unmarshal(got.Data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != "1234567890abcdef1234567890abcdef" || len(stored.Extra.Cart.IDs) != 2 || stored.Extra.Cart.IDs[0] != 1 || stored.Extra.Cart.IDs[1] != 2 || stored.Extra.HTML != "<script>not executable</script>" || stored.Tags["region"] != "mx" {
		t.Fatalf("SDK data or normalized id lost: %s", got.Data)
	}
}

func TestNormalizeIDsAndTimestampContract(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		raw  string
		want time.Time
	}{
		{"server time", `{"message":"hello"}`, now},
		{"epoch", `{"timestamp":0,"message":"hello"}`, time.Unix(0, 0)},
		{"fractional unix", `{"timestamp":1791981000.125,"message":"hello"}`, time.Unix(1791981000, 125000000)},
		{"pre epoch", `{"timestamp":-0.25,"message":"hello"}`, time.Unix(-1, 750000000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := event.Normalize(json.RawMessage(tc.raw), "", now)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Timestamp.Equal(tc.want) || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(got.ID) {
				t.Fatalf("timestamp or generated id invalid: %+v", got)
			}
		})
	}
	for _, raw := range []string{
		`null`, `[]`, `{"type":"session"}`, `{"event_id":"not-an-id","message":"hello"}`,
		`{"event_id":123,"message":"hello"}`, `{"event_id":null,"message":"hello"}`, `{"timestamp":"yesterday","message":"hello"}`,
		`{"timestamp":null,"message":"hello"}`, `{"timestamp":true,"message":"hello"}`,
		`{"timestamp":1e100,"message":"hello"}`, `{"message":"hello","fingerprint":[42]}`,
	} {
		if _, err := event.Normalize(json.RawMessage(raw), "", now); err == nil {
			t.Errorf("accepted malformed event %s", raw)
		}
	}
	if _, err := event.Normalize(json.RawMessage(`{"message":"hello"}`), "bad-envelope-id", now); err == nil {
		t.Error("accepted malformed envelope id")
	}
	known, err := event.Normalize(json.RawMessage(`{"event_id":"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF","message":"known"}`), "", now)
	if err != nil || known.ID != "ffffffffffffffffffffffffffffffff" {
		t.Fatalf("payload UUID normalization failed: id %q, error %v", known.ID, err)
	}
}

func TestNormalizeMessageTitles(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		title string
	}{
		{`{"message":"one\n two"}`, "one two"},
		{`{"message":{"formatted":"User 42 failed","message":"User %s failed"}}`, "User 42 failed"},
		{`{"logentry":{"formatted":"formatted log","message":"raw %s"},"message":"fallback"}`, "formatted log"},
		{`{"message":{"message":"plain log"}}`, "plain log"},
		{`{"exception":{"values":[{"type":"RootError","value":"root"},{"type":"WrapperError","value":"wrapper"}]}}`, "WrapperError: wrapper"},
		{`{"exception":{"values":[{"type":"TimeoutError"}]}}`, "TimeoutError"},
		{`{}`, "Unknown error"},
		{`{"exception":{"values":[{"stacktrace":{"frames":[{"filename":"main.go","function":"main"}]}}]}}`, "Unknown error"},
	} {
		got, err := event.Normalize(json.RawMessage(tc.raw), "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != tc.title {
			t.Errorf("title = %q, want %q for %s", got.Title, tc.title, tc.raw)
		}
	}
}

func TestNormalizeTransactionTiming(t *testing.T) {
	got, err := event.Normalize(json.RawMessage(`{"type":"transaction","transaction":"GET /checkout","start_timestamp":1791981000.25,"timestamp":1791981001.5,"contexts":{"trace":{"trace_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","span_id":"1234567890abcdef","op":"http.server"}},"spans":[{"op":"db","start_timestamp":1791981000.5,"timestamp":1791981001}]}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "transaction" || got.Transaction != "GET /checkout" || got.Title != "GET /checkout" || got.Level != "info" || math.Abs(got.Duration-1250) > 0.001 {
		t.Fatalf("transaction fields incorrect: %+v", got)
	}
	unlabeled, err := event.Normalize(json.RawMessage(`{"type":"transaction","start_timestamp":1791981000,"timestamp":1791981000}`), "", time.Now())
	if err != nil || unlabeled.Title != "<unlabeled transaction>" || unlabeled.Duration != 0 {
		t.Fatalf("unlabeled zero-duration transaction failed: %+v, %v", unlabeled, err)
	}
	for _, raw := range []string{
		`{"type":"transaction","transaction":"name","timestamp":4}`,
		`{"type":"transaction","transaction":"name","start_timestamp":5,"timestamp":4}`,
		`{"type":"transaction","transaction":"name","start_timestamp":"broken","timestamp":4}`,
	} {
		if _, err := event.Normalize(json.RawMessage(raw), "", time.Now()); err == nil {
			t.Errorf("accepted invalid transaction timing: %s", raw)
		}
	}
}

func TestNormalizeDefaultAndCustomGrouping(t *testing.T) {
	// Fixed vectors describe GoodSentry's own v1 grouping contract. They are not
	// promises to reproduce another server's grouping implementation.
	const original = `{"exception":{"values":[{"type":"ValueError","value":"bad 1","stacktrace":{"frames":[{"module":" checkout ","function":"charge","filename":"https://example.com/app.js?v=1","lineno":20}]}}]}}`
	const repeated = `{"exception":{"values":[{"type":"IgnoredCause","value":"cause"},{"type":"ValueError","value":"bad 2","stacktrace":{"frames":[{"module":"checkout","function":"charge","filename":"https://example.com/app.js#build2","lineno":99,"colno":8}]}}]}}`
	a, err := event.Normalize(json.RawMessage(original), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := event.Normalize(json.RawMessage(repeated), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != b.Fingerprint || a.Fingerprint != "15b53bb5efc980676b2399d92aef34a5cf0dc0e01e6a9f876ff613ae1964e6bd" {
		t.Fatalf("default grouping changed: %s, %s", a.Fingerprint, b.Fingerprint)
	}
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`{"message":"one\n two"}`, "bf543ea1996c0f25b12f73d36d3233950166b21179a9ec8e7f005ac4fbcdc0ca"},
		{`{"message":"different body","fingerprint":["checkout","timeout"]}`, "464679fd8b6ca20c5c535fc488c715c9185dc6efa323aa3ed60a643f63851946"},
		{`{"message":"one two","fingerprint":["{{ default }}","tenant-1"]}`, "2b6573133bf8f5375e3961bc7a97b893e60c33dc3056413cc9b4dbdc69e02416"},
		{`{"message":"one two","fingerprint":[]}`, "bf543ea1996c0f25b12f73d36d3233950166b21179a9ec8e7f005ac4fbcdc0ca"},
		{`{"message":"one two","fingerprint":["{{ default }}"]}`, "bf543ea1996c0f25b12f73d36d3233950166b21179a9ec8e7f005ac4fbcdc0ca"},
	} {
		got, err := event.Normalize(json.RawMessage(tc.raw), "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got.Fingerprint != tc.want {
			t.Errorf("fingerprint = %s, want %s for %s", got.Fingerprint, tc.want, tc.raw)
		}
	}
}
