//go:build integration

package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goodsentry/internal/config"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// HTTP owns path/header admission and durable acknowledgements. The codec and
// Store suites own full normalization, selection, SQL constraints and races.
func otlpHTTPConfig(prefix string) config.Config {
	return config.Config{Domain: "https://public.example.test" + prefix, Prefix: prefix, Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20, Logs: true, OTLPMaxRecords: 1000, OTLPMaxNodes: 100000}
}

func otlpHTTPHeaders(key, media string) http.Header {
	return http.Header{"Content-Type": {media}, "X-Goodsentry-Key": {key}}
}

func otlpHTTPCall(t *testing.T, handler http.Handler, method, target string, headers http.Header, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.Header = headers.Clone()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func otlpHTTPRequireHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("OTLP response cache/CORS headers: %v", w.Header())
	}
	exposed := strings.ToLower(w.Header().Get("Access-Control-Expose-Headers"))
	if !strings.Contains(exposed, "retry-after") {
		t.Fatalf("OTLP response does not expose Retry-After: %q", exposed)
	}
}

// Decode the public google.rpc.Status wire independently of the response
// encoder. No collector/gRPC package or expected-value production helper.
func otlpHTTPStatus(t *testing.T, w *httptest.ResponseRecorder, wantHTTP, wantCode int, binary bool) string {
	t.Helper()
	if w.Code != wantHTTP {
		t.Fatalf("OTLP HTTP status=%d want=%d body=%q", w.Code, wantHTTP, w.Body.Bytes())
	}
	otlpHTTPRequireHeaders(t, w)
	if w.Body.Len() == 0 || w.Body.Len() > 1024 {
		t.Fatalf("OTLP error length=%d", w.Body.Len())
	}
	var code int
	var message string
	if binary {
		if w.Header().Get("Content-Type") != "application/x-protobuf" {
			t.Fatalf("binary error media=%q", w.Header().Get("Content-Type"))
		}
		raw := w.Body.Bytes()
		seen := map[protowire.Number]bool{}
		for len(raw) > 0 {
			number, kind, n := protowire.ConsumeTag(raw)
			if n < 0 || seen[number] {
				t.Fatalf("malformed/duplicate Status field: %x", raw)
			}
			seen[number] = true
			raw = raw[n:]
			switch {
			case number == 1 && kind == protowire.VarintType:
				value, n := protowire.ConsumeVarint(raw)
				if n < 0 {
					t.Fatal("malformed Status.code")
				}
				code, raw = int(value), raw[n:]
			case number == 2 && kind == protowire.BytesType:
				value, n := protowire.ConsumeString(raw)
				if n < 0 {
					t.Fatal("malformed Status.message")
				}
				message, raw = value, raw[n:]
			default:
				t.Fatalf("unexpected Status field=%d kind=%d", number, kind)
			}
		}
	} else {
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("JSON error media=%q", w.Header().Get("Content-Type"))
		}
		var status struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		decoder := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&status); err != nil {
			t.Fatalf("google.rpc.Status JSON: %v", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("Status has trailing JSON: %v", err)
		}
		code, message = status.Code, status.Message
	}
	if code != wantCode || message == "" {
		t.Fatalf("OTLP Status code=%d message=%q want code=%d", code, message, wantCode)
	}
	if wantHTTP == 503 && w.Header().Get("Retry-After") != "1" {
		t.Fatalf("unavailable storage Retry-After=%q", w.Header().Get("Retry-After"))
	}
	return message
}

func otlpHTTPCounts(t *testing.T, f *resolutionHTTPFixture, batches, logs int) {
	t.Helper()
	var actualBatches, actualLogs int
	if err := f.sql.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM otlp_log_batches),(SELECT count(*) FROM logs)").Scan(&actualBatches, &actualLogs); err != nil {
		t.Fatal(err)
	}
	if actualBatches != batches || actualLogs != logs {
		t.Fatalf("committed OTLP batches/logs=%d/%d want=%d/%d", actualBatches, actualLogs, batches, logs)
	}
}

func otlpHTTPBinary(t *testing.T, bodies ...string) []byte {
	t.Helper()
	records := make([]*logspb.LogRecord, len(bodies))
	for i, body := range bodies {
		records[i] = &logspb.LogRecord{Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: body}}}
	}
	raw, err := proto.Marshal(&logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func otlpHTTPGzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	return otlpHTTPGzipLevel(t, raw, gzip.DefaultCompression)
}

func otlpHTTPGzipLevel(t *testing.T, raw []byte, level int) []byte {
	t.Helper()
	var compressed bytes.Buffer
	w, err := gzip.NewWriterLevel(&compressed, level)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func TestOTLPHTTPEmptyExportIsAcknowledged(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	for _, prefix := range []string{"/good", ""} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			handler := New(otlpHTTPConfig(prefix), f.db)
			for _, binary := range []bool{true, false} {
				media, body := "application/x-protobuf; charset=binary", ""
				if !binary {
					media, body = "application/json; charset=utf-8", "{}"
				}
				w := otlpHTTPCall(t, handler, "POST", prefix+"/v1/logs?", otlpHTTPHeaders(f.project.PublicKey, media), strings.NewReader(body))
				if w.Code != 200 {
					t.Fatalf("empty authenticated OTLP export: status=%d want=200 body=%q", w.Code, w.Body.String())
				}
				otlpHTTPRequireHeaders(t, w)
				if binary && (w.Body.Len() != 0 || w.Header().Get("Content-Type") != "application/x-protobuf") {
					t.Fatalf("binary empty success: media=%q body=%x", w.Header().Get("Content-Type"), w.Body.Bytes())
				}
				if !binary && (strings.TrimSpace(w.Body.String()) != "{}" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json")) {
					t.Fatalf("JSON empty success: media=%q body=%q", w.Header().Get("Content-Type"), w.Body.String())
				}
			}
		})
	}
	otlpHTTPCounts(t, f, 0, 0)
}

func TestOTLPHTTPExactRoutesAndMethodAdmission(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	for _, prefix := range []string{"/good", ""} {
		handler := New(otlpHTTPConfig(prefix), f.db)
		paths := []string{"/v1/logs/", "/v1/logs/child", "/v1//logs", "/v1/x/../logs", "/v1/./logs", "/%761/logs", "/v1/%6cogs", "/v1/logs%2f"}
		if prefix == "/good" {
			// A redirect target is interpreted again by the deployment-prefix
			// wrapper. These aliases can become the exact endpoint on a retry.
			paths = append(paths, "//good/v1/logs", "/good/v1//logs", "/good/v1/./logs", "/good/v1/logs/../logs")
		}
		for _, path := range paths {
			t.Run(prefix+path, func(t *testing.T) {
				w := otlpHTTPCall(t, handler, "POST", prefix+path, otlpHTTPHeaders(f.project.PublicKey, "application/x-protobuf"), bytes.NewReader(otlpHTTPBinary(t, "must not ingest alias")))
				if w.Code != 404 || w.Header().Get("Location") != "" {
					t.Fatalf("noncanonical POST=%d Location=%q want404 without redirect", w.Code, w.Header().Get("Location"))
				}
			})
		}
		for _, method := range []string{"GET", "HEAD", "PUT", "PATCH", "DELETE"} {
			t.Run(prefix+method, func(t *testing.T) {
				w := otlpHTTPCall(t, handler, method, prefix+"/v1/logs?invalid=yes", http.Header{"Content-Type": {"application/json"}, "Authorization": {""}}, strings.NewReader("not-json"))
				otlpHTTPStatus(t, w, 405, 12, false)
				if w.Header().Get("Allow") != "POST, OPTIONS" {
					t.Fatalf("method Allow=%q", w.Header().Get("Allow"))
				}
			})
		}
		// Representation selection precedes method admission as well as auth.
		w := otlpHTTPCall(t, handler, "GET", prefix+"/v1/logs", http.Header{"Content-Type": {"application/x-protobuf"}}, nil)
		otlpHTTPStatus(t, w, 405, 12, true)
		if w.Header().Get("Allow") != "POST, OPTIONS" {
			t.Fatalf("binary method Allow=%q", w.Header().Get("Allow"))
		}
	}
	w := otlpHTTPCall(t, New(otlpHTTPConfig("/good"), f.db), "POST", "/v1/logs", otlpHTTPHeaders(f.project.PublicKey, "application/json"), strings.NewReader("{}"))
	if w.Code != 404 || w.Header().Get("Location") != "" {
		t.Fatalf("deployment prefix bypass=%d Location=%q", w.Code, w.Header().Get("Location"))
	}
	unicode := New(otlpHTTPConfig("/héllo"), f.db)
	w = otlpHTTPCall(t, unicode, "POST", "/h%C3%A9llo/v1/logs", otlpHTTPHeaders(f.project.PublicKey, "application/x-protobuf"), nil)
	if w.Code != 200 {
		t.Fatalf("canonical escaped Unicode prefix=%d want200", w.Code)
	}
	// URL.EscapedPath can discard an invalid RawPath containing literal UTF-8.
	// It must not turn a percent-escaped endpoint alias into a canonical send.
	w = otlpHTTPCall(t, unicode, "POST", "/héllo/v1/%6cogs", otlpHTTPHeaders(f.project.PublicKey, "application/x-protobuf"), bytes.NewReader(otlpHTTPBinary(t, "must not ingest raw-path fallback")))
	if w.Code != 404 || w.Header().Get("Location") != "" {
		t.Fatalf("Unicode RawPath fallback alias=%d Location=%q", w.Code, w.Header().Get("Location"))
	}
	otlpHTTPCounts(t, f, 0, 0)
}

type otlpObservedBody struct {
	io.Reader
	reads int
}

func (b *otlpObservedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *otlpObservedBody) Close() error               { return nil }

func TestOTLPHTTPPreflightRequiresNoStore(t *testing.T) {
	handler := New(otlpHTTPConfig("/good"), nil)
	body := &otlpObservedBody{Reader: strings.NewReader("must remain unread")}
	w := otlpHTTPCall(t, handler, "OPTIONS", "/good/v1/logs?untrusted=yes", http.Header{"Authorization": {""}}, body)
	if w.Code != 204 || w.Body.Len() != 0 || body.reads != 0 {
		t.Fatalf("preflight status=%d bytes=%d reads=%d", w.Code, w.Body.Len(), body.reads)
	}
	otlpHTTPRequireHeaders(t, w)
	if w.Header().Get("Access-Control-Allow-Methods") != "POST, OPTIONS" {
		t.Fatalf("preflight methods=%q", w.Header().Get("Access-Control-Allow-Methods"))
	}
	allowed := strings.ToLower(w.Header().Get("Access-Control-Allow-Headers"))
	for _, name := range []string{"content-type", "content-encoding", "x-goodsentry-key"} {
		if !strings.Contains(allowed, name) {
			t.Fatalf("preflight omits %s: %q", name, allowed)
		}
	}
}

func TestOTLPHTTPKeyBoundaryPrecedesBodyAndMedia(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	handler := New(otlpHTTPConfig("/good"), f.db)
	cookieURL, err := url.Parse("https://" + strings.TrimPrefix(f.server.URL, "http://") + "/good/v1/logs")
	if err != nil {
		t.Fatal(err)
	}
	cookies := f.client.Jar.Cookies(cookieURL)
	if len(cookies) == 0 {
		t.Fatal("authenticated fixture did not retain its session cookie")
	}
	sessionHeaders := http.Header{}
	for _, cookie := range cookies {
		sessionHeaders.Add("Cookie", cookie.String())
	}
	if control := otlpHTTPCall(t, handler, "GET", "/good/api/v1/me", sessionHeaders, nil); control.Code != 200 {
		t.Fatalf("session positive control status=%d", control.Code)
	}
	invalid := []struct {
		name, target string
		mutate       func(http.Header)
		status, code int
	}{
		{"missing", "/good/v1/logs", func(h http.Header) { h.Del("X-GoodSentry-Key") }, 403, 7},
		{"empty", "/good/v1/logs", func(h http.Header) { h.Set("X-GoodSentry-Key", "") }, 403, 7},
		{"unknown", "/good/v1/logs", func(h http.Header) { h.Set("X-GoodSentry-Key", strings.Repeat("f", 32)) }, 403, 7},
		{"auth-before-media", "/good/v1/logs", func(h http.Header) {
			h.Set("X-GoodSentry-Key", strings.Repeat("f", 32))
			h.Set("Content-Type", "text/plain")
		}, 403, 7},
		{"duplicate", "/good/v1/logs", func(h http.Header) { h.Add("X-GoodSentry-Key", f.project.PublicKey) }, 403, 7},
		{"comma", "/good/v1/logs", func(h http.Header) { h.Set("X-GoodSentry-Key", f.project.PublicKey+","+f.project.PublicKey) }, 403, 7},
		{"space", "/good/v1/logs", func(h http.Header) { h.Set("X-GoodSentry-Key", " "+f.project.PublicKey) }, 403, 7},
		{"equals", "/good/v1/logs", func(h http.Header) { h.Set("X-GoodSentry-Key", "key=private-probe") }, 403, 7},
		{"invalid-utf8", "/good/v1/logs", func(h http.Header) { h.Set("X-GoodSentry-Key", string([]byte{0xff})) }, 403, 7},
		{"bearer", "/good/v1/logs", func(h http.Header) { h.Set("Authorization", "Bearer private-probe") }, 403, 7},
		{"empty-authorization", "/good/v1/logs", func(h http.Header) { h["Authorization"] = []string{""} }, 403, 7},
		{"query-key", "/good/v1/logs?x-goodsentry-key=" + f.project.PublicKey, nil, 400, 3},
		{"query-sdk", "/good/v1/logs?sentry_key=" + f.project.PublicKey, nil, 400, 3},
		{"malformed-query", "/good/v1/logs?%zz", nil, 400, 3},
		{"query-before-auth", "/good/v1/logs?unexpected=1", func(h http.Header) { h.Del("X-GoodSentry-Key") }, 400, 3},
		{"sdk-auth", "/good/v1/logs", func(h http.Header) {
			h.Del("X-GoodSentry-Key")
			h.Set("X-Sentry-Auth", "Sentry sentry_version=7,sentry_key="+f.project.PublicKey)
		}, 403, 7},
		{"session-cookie", "/good/v1/logs", func(h http.Header) {
			h.Del("X-GoodSentry-Key")
			for _, cookie := range cookies {
				h.Add("Cookie", cookie.String())
			}
		}, 403, 7},
		{"userinfo", "http://" + f.project.PublicKey + "@example.test/good/v1/logs", func(h http.Header) { h.Del("X-GoodSentry-Key") }, 403, 7},
	}
	var keyDiagnostic string
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			headers := otlpHTTPHeaders(f.project.PublicKey, "application/json")
			if test.mutate != nil {
				test.mutate(headers)
			}
			body := &otlpObservedBody{Reader: strings.NewReader("bad gzip private-probe")}
			headers.Set("Content-Encoding", "gzip")
			w := otlpHTTPCall(t, handler, "POST", test.target, headers, body)
			message := otlpHTTPStatus(t, w, test.status, test.code, false)
			if body.reads != 0 {
				t.Fatalf("body read %d times before denial", body.reads)
			}
			if strings.Contains(message, "private-probe") || strings.Contains(message, f.project.PublicKey) {
				t.Fatal("credential/body leaked into diagnostic")
			}
			if test.status == 403 {
				if keyDiagnostic == "" {
					keyDiagnostic = message
				} else if message != keyDiagnostic {
					t.Fatalf("invalid credential diagnostic=%q want same static=%q", message, keyDiagnostic)
				}
			}
		})
	}
	binaryDenied := otlpHTTPHeaders("", "application/x-protobuf")
	binaryDenied.Del("X-GoodSentry-Key")
	unreadBinary := &otlpObservedBody{Reader: bytes.NewReader([]byte{0xff})}
	if message := otlpHTTPStatus(t, otlpHTTPCall(t, handler, "POST", "/good/v1/logs", binaryDenied, unreadBinary), 403, 7, true); message != keyDiagnostic || unreadBinary.reads != 0 {
		t.Fatalf("binary auth diagnostic/read=%q/%d", message, unreadBinary.reads)
	}
	if _, err := f.sql.Exec(f.ctx, "UPDATE project_keys SET enabled=false WHERE public_key=$1", f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	body := &otlpObservedBody{Reader: strings.NewReader("{}")}
	w := otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, "application/json"), body)
	if message := otlpHTTPStatus(t, w, 403, 7, false); message != keyDiagnostic || body.reads != 0 {
		t.Fatalf("disabled credential diagnostic/read=%q/%d", message, body.reads)
	}
	if _, err := f.sql.Exec(f.ctx, "UPDATE project_keys SET enabled=true WHERE public_key=$1", f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	body = &otlpObservedBody{Reader: strings.NewReader("{}")}
	w = otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, "application/json"), body)
	if w.Code != 200 || body.reads == 0 {
		t.Fatalf("enabled credential positive control status=%d reads=%d", w.Code, body.reads)
	}
	otlpHTTPCounts(t, f, 0, 0)
}

func TestOTLPHTTPMediaBodiesAndConfiguredLimits(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	binary := otlpHTTPBinary(t, "binary-media-control")
	jsonRecord := []byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"json-media-control"}}]}]}]}`)
	nodeBodies := make([]string, 100)
	for i := range nodeBodies {
		nodeBodies[i] = "node-budget"
	}
	tests := []struct {
		name, media, encoding string
		body                  []byte
		mutate                func(http.Header)
		configure             func(*config.Config)
		status, code          int
		binary                bool
	}{
		{name: "missing media", media: "", body: []byte("{}"), status: 415, code: 12},
		{name: "unsupported media", media: "text/plain", body: jsonRecord, status: 415, code: 12},
		{name: "duplicate JSON media", media: "application/json", body: jsonRecord, mutate: func(h http.Header) { h.Add("Content-Type", "application/json") }, status: 415, code: 12},
		{name: "comma combined media", media: "application/json, application/json", body: jsonRecord, status: 415, code: 12},
		{name: "unsupported encoding binary response", media: "application/x-protobuf", encoding: "br", body: binary, status: 415, code: 12, binary: true},
		{name: "duplicate encoding", media: "application/x-protobuf", encoding: "identity", body: binary, mutate: func(h http.Header) { h.Add("Content-Encoding", "identity") }, status: 415, code: 12, binary: true},
		{name: "comma combined encoding", media: "application/json", encoding: "identity, gzip", body: jsonRecord, status: 415, code: 12},
		{name: "malformed binary", media: "application/x-protobuf", body: []byte{0xff}, status: 400, code: 3, binary: true},
		{name: "empty JSON", media: "application/json", status: 400, code: 3},
		{name: "JSON null", media: "application/json", body: []byte("null"), status: 400, code: 3},
		{name: "JSON NDJSON", media: "application/json", body: []byte("{}\n{}"), status: 400, code: 3},
		{name: "invalid gzip", media: "application/x-protobuf", encoding: "gzip", body: []byte("private-body-not-gzip"), status: 400, code: 3, binary: true},
		{name: "truncated gzip", media: "application/json", encoding: "gzip", body: otlpHTTPGzip(t, jsonRecord)[:8], status: 400, code: 3},
		{name: "compressed entity limit", media: "application/json", encoding: "gzip", body: otlpHTTPGzipLevel(t, []byte("{}"+strings.Repeat(" ", 1022)), gzip.NoCompression), configure: func(c *config.Config) { c.MaxBodyBytes = 1024 }, status: 413, code: 8},
		{name: "decompressed entity limit", media: "application/json", encoding: "gzip", body: otlpHTTPGzip(t, []byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"`+strings.Repeat("x", 2048)+`"}}]}]}]}`)), configure: func(c *config.Config) { c.MaxBodyBytes = 1024 }, status: 413, code: 8},
		{name: "configured record limit", media: "application/x-protobuf", body: otlpHTTPBinary(t, "one", "two"), configure: func(c *config.Config) { c.OTLPMaxRecords = 1 }, status: 413, code: 8, binary: true},
		{name: "configured node limit", media: "application/x-protobuf", body: otlpHTTPBinary(t, nodeBodies...), configure: func(c *config.Config) { c.OTLPMaxNodes = 100 }, status: 413, code: 8, binary: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := otlpHTTPConfig("/good")
			if test.configure != nil {
				test.configure(&cfg)
			}
			headers := otlpHTTPHeaders(f.project.PublicKey, test.media)
			if test.encoding != "" {
				headers.Set("Content-Encoding", test.encoding)
			}
			if test.mutate != nil {
				test.mutate(headers)
			}
			w := otlpHTTPCall(t, New(cfg, f.db), "POST", "/good/v1/logs", headers, bytes.NewReader(test.body))
			message := otlpHTTPStatus(t, w, test.status, test.code, test.binary)
			if strings.Contains(message, "private-body") {
				t.Fatal("input leaked into static error")
			}
		})
	}
	otlpHTTPCounts(t, f, 0, 0)
	for _, test := range []struct {
		name, media, encoding string
		body                  []byte
		binary                bool
	}{
		{"binary identity", "application/x-protobuf; charset=binary", "identity", binary, true},
		{"binary gzip", "application/x-protobuf", "gzip", otlpHTTPGzip(t, binary), true},
		{"JSON identity", "application/json; charset=utf-8", "identity", jsonRecord, false},
		{"JSON gzip", "application/json", "gzip", otlpHTTPGzip(t, jsonRecord), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := otlpHTTPHeaders(f.project.PublicKey, test.media)
			headers.Set("Content-Encoding", test.encoding)
			w := otlpHTTPCall(t, New(otlpHTTPConfig("/good"), f.db), "POST", "/good/v1/logs", headers, bytes.NewReader(test.body))
			if w.Code != 200 || (test.binary && w.Body.Len() != 0) || (!test.binary && strings.TrimSpace(w.Body.String()) != "{}") {
				t.Fatalf("valid export status=%d body=%q", w.Code, w.Body.Bytes())
			}
			otlpHTTPRequireHeaders(t, w)
		})
	}
	otlpHTTPCounts(t, f, 4, 4)
}

func TestOTLPHTTPDisabledFeatureAndUnavailableLookup(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := otlpHTTPConfig("/good")
	cfg.Logs = false
	handler := New(cfg, f.db)
	for _, media := range []string{"application/json", "application/x-protobuf"} {
		for _, empty := range []bool{true, false} {
			raw := "malformed-body"
			if empty {
				raw = ""
				if media == "application/json" {
					raw = "{}"
				}
			}
			body := &otlpObservedBody{Reader: strings.NewReader(raw)}
			w := otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, media), body)
			otlpHTTPStatus(t, w, 404, 12, media == "application/x-protobuf")
			if body.reads != 0 {
				t.Fatalf("disabled feature read body %d times", body.reads)
			}
		}
	}
	w := otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders("disabled-feature-unknown-key", "application/json"), strings.NewReader("{}"))
	otlpHTTPStatus(t, w, 403, 7, false)
	otlpHTTPCounts(t, f, 0, 0)
	f.db.Close()
	// A closed real pool makes lookup fail. No store replacement or success mock.
	body := &otlpObservedBody{Reader: strings.NewReader("{}")}
	w = otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, "application/json"), body)
	otlpHTTPStatus(t, w, 503, 14, false)
	if body.reads != 0 {
		t.Fatalf("failed key lookup read body %d times", body.reads)
	}
	options := otlpHTTPCall(t, handler, "OPTIONS", "/good/v1/logs", nil, nil)
	if options.Code != 204 {
		t.Fatalf("closed pool preflight=%d", options.Code)
	}
}

func TestOTLPHTTPAcknowledgesCommittedOccurrencesAndRejectsRollback(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	handler := New(otlpHTTPConfig("/good"), f.db)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	raw := otlpHTTPBinary(t, "same-occurrence", "same-occurrence")
	export := func(wantStatus, batches, logs int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, "POST", srv.URL+"/good/v1/logs", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header = otlpHTTPHeaders(f.project.PublicKey, "application/x-protobuf")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != wantStatus {
			t.Fatalf("real export status=%d want=%d body=%x", response.StatusCode, wantStatus, body)
		}
		if wantStatus == 200 && len(body) != 0 {
			t.Fatalf("full binary acknowledgment is not empty: %x", body)
		}
		if wantStatus == 503 {
			recorded := httptest.NewRecorder()
			recorded.HeaderMap = response.Header.Clone()
			recorded.Code = response.StatusCode
			_, _ = recorded.Body.Write(body)
			otlpHTTPStatus(t, recorded, 503, 14, true)
		}
		// This connection is independent of the HTTP Store pool. Checking after
		// the actual response proves the acknowledgment corresponds to commit.
		otlpHTTPCounts(t, f, batches, logs)
		var matchingBodies int
		if err := f.sql.QueryRow(f.ctx, "SELECT count(*) FROM logs WHERE body=$1", "same-occurrence").Scan(&matchingBodies); err != nil || matchingBodies != logs {
			t.Fatalf("HTTP body projection retained=%d want=%d err=%v", matchingBodies, logs, err)
		}
		return body
	}
	export(200, 1, 2)
	export(200, 2, 4)
	var projects, occurrenceHashes int
	if err := f.sql.QueryRow(f.ctx, "SELECT count(DISTINCT project_id),count(DISTINCT item_hash) FROM logs").Scan(&projects, &occurrenceHashes); err != nil {
		t.Fatal(err)
	}
	if projects != 1 || occurrenceHashes != 4 {
		t.Fatalf("same records/replay collapsed or routed across projects: projects=%d identities=%d", projects, occurrenceHashes)
	}
	var actualProject string
	if err := f.sql.QueryRow(f.ctx, "SELECT DISTINCT project_id::text FROM logs").Scan(&actualProject); err != nil || actualProject != f.project.ID {
		t.Fatalf("key routing project=%q want=%q err=%v", actualProject, f.project.ID, err)
	}
	_, err := f.sql.Exec(f.ctx, `CREATE SEQUENCE http_otlp_failure_observed;
CREATE FUNCTION reject_http_otlp_later_row() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
IF EXISTS(SELECT 1 FROM logs WHERE otlp_batch_id=NEW.otlp_batch_id)
 AND EXISTS(SELECT 1 FROM otlp_log_batches WHERE id=NEW.otlp_batch_id) THEN
 PERFORM nextval('http_otlp_failure_observed');
 RAISE EXCEPTION 'private-http-fixture-post-first-insert';
END IF;
RETURN NEW; END $$;
CREATE TRIGGER reject_http_otlp_later_row BEFORE INSERT ON logs FOR EACH ROW WHEN(NEW.otlp_batch_id IS NOT NULL) EXECUTE FUNCTION reject_http_otlp_later_row()`)
	if err != nil {
		t.Fatal(err)
	}
	body := export(503, 2, 4)
	if bytes.Contains(body, []byte("private-http-fixture")) {
		t.Fatal("SQL diagnostic leaked in unavailable response")
	}
	var observed bool
	if err := f.sql.QueryRow(f.ctx, "SELECT is_called FROM http_otlp_failure_observed").Scan(&observed); err != nil || !observed {
		t.Fatalf("SQL failure did not observe an actual earlier inserted row and batch: observed=%v err=%v", observed, err)
	}
	if _, err := f.sql.Exec(f.ctx, "DROP TRIGGER reject_http_otlp_later_row ON logs"); err != nil {
		t.Fatal(err)
	}
	export(200, 3, 6)
	expected := new(logspb.LogsData)
	if err := proto.Unmarshal(raw, expected); err != nil {
		t.Fatal(err)
	}
	rows, err := f.sql.Query(f.ctx, "SELECT payload FROM otlp_log_batches ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var canonical []byte
		if err := rows.Scan(&canonical); err != nil {
			t.Fatal(err)
		}
		actual := new(logspb.LogsData)
		if err := proto.Unmarshal(canonical, actual); err != nil || !proto.Equal(actual, expected) {
			t.Fatalf("acknowledged HTTP entity differs from committed canonical payload: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestOTLPHTTPProfilesWarningRetainsEveryOccurrence(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	handler := New(otlpHTTPConfig("/good"), f.db)
	for index, binary := range []bool{false, true} {
		media := "application/json"
		raw := []byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValueStrindex":0}}]}]}]}`)
		if binary {
			media = "application/x-protobuf"
			value := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValueStrindex{StringValueStrindex: 0}}}}}}}}}
			var err error
			raw, err = proto.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
		}
		w := otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, media), bytes.NewReader(raw))
		if w.Code != 200 || w.Body.Len() == 0 || w.Body.Len() > 1024 {
			t.Fatalf("Profiles warning status=%d bytes=%d", w.Code, w.Body.Len())
		}
		var message string
		if binary {
			number, kind, n := protowire.ConsumeTag(w.Body.Bytes())
			if n < 0 || number != 1 || kind != protowire.BytesType {
				t.Fatal("missing ExportLogsServiceResponse.partial_success")
			}
			partial, consumed := protowire.ConsumeBytes(w.Body.Bytes()[n:])
			if consumed < 0 || n+consumed != w.Body.Len() {
				t.Fatal("malformed partial_success")
			}
			for len(partial) > 0 {
				number, kind, n = protowire.ConsumeTag(partial)
				if n < 0 {
					t.Fatal("malformed partial success field")
				}
				partial = partial[n:]
				if number == 1 && kind == protowire.VarintType {
					value, n := protowire.ConsumeVarint(partial)
					if n < 0 || value != 0 {
						t.Fatal("Profiles warning rejected actual records")
					}
					partial = partial[n:]
				} else if number == 2 && kind == protowire.BytesType {
					value, n := protowire.ConsumeString(partial)
					if n < 0 {
						t.Fatal("malformed partial success message")
					}
					message, partial = value, partial[n:]
				} else {
					t.Fatalf("unexpected partial success field=%d kind=%d", number, kind)
				}
			}
		} else {
			var response struct {
				PartialSuccess struct {
					RejectedLogRecords string `json:"rejectedLogRecords"`
					ErrorMessage       string `json:"errorMessage"`
				} `json:"partialSuccess"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.PartialSuccess.RejectedLogRecords != "0" {
				t.Fatalf("partial success must report zero rejected records: %v", err)
			}
			message = response.PartialSuccess.ErrorMessage
		}
		if message == "" {
			t.Fatal("Profiles warning omitted static explanation")
		}
		otlpHTTPCounts(t, f, index+1, index+1)
	}
}
