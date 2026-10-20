//go:build integration

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOTLPProjectKeyRateLimit429(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	if _, err := f.sql.Exec(f.ctx, `UPDATE project_keys SET rate_limit_window=60,rate_limit_count=1 WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	handler := New(otlpHTTPConfig("/good"), f.db)
	media := "application/x-protobuf"
	call := func(body []byte) *httptest.ResponseRecorder {
		t.Helper()
		return otlpHTTPCall(t, handler, "POST", "/good/v1/logs", otlpHTTPHeaders(f.project.PublicKey, media), bytes.NewReader(body))
	}
	first := call(otlpHTTPBinary(t, "rate one"))
	if first.Code != http.StatusOK {
		t.Fatalf("first OTLP export=%d body=%q", first.Code, first.Body.Bytes())
	}
	second := call(otlpHTTPBinary(t, "rate two"))
	otlpHTTPStatus(t, second, http.StatusTooManyRequests, 8, true)
	if second.Header().Get("Retry-After") == "" || !strings.Contains(second.Header().Get("X-Sentry-Rate-Limits"), ":log_item:key:rate_limited") {
		t.Fatalf("OTLP rate headers: %v", second.Header())
	}
	empty := call([]byte{})
	if empty.Code != http.StatusOK {
		t.Fatalf("empty OTLP export after quota=%d body=%q", empty.Code, empty.Body.Bytes())
	}
}
