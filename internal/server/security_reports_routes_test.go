package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodsentry/internal/config"
)

// Real routing owns the prefix, preflight and credential-free rejection. A
// parser test cannot catch browsers posting to an unregistered security URL.
func TestSecurityReportRoutesAndPreflight(t *testing.T) {
	handler := New(config.Config{Domain: "https://example.test/good", Prefix: "/good"}, nil)
	for _, route := range []string{"security", "csp-report"} {
		for _, method := range []string{http.MethodOptions, http.MethodPost, http.MethodGet} {
			req := httptest.NewRequest(method, "/good/api/1/"+route+"/", strings.NewReader(`{"csp-report":{}}`))
			req.Header.Set("Origin", "https://website.example")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			want := http.StatusNoContent
			if method == http.MethodPost {
				want = http.StatusForbidden
			} else if method == http.MethodGet {
				want = http.StatusMethodNotAllowed
			}
			if response.Code != want {
				t.Fatalf("%s %s: HTTP %d, want %d: %s", method, route, response.Code, want, response.Body)
			}
			if method != http.MethodGet && (response.Header().Get("Access-Control-Allow-Origin") != "*" || response.Header().Get("Access-Control-Allow-Credentials") != "") {
				t.Fatal("browser report CORS changed", response.Header())
			}
		}
	}
	for _, route := range []string{"/api/1/security/", "/good/api/1/security/extra"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, nil))
		if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
			t.Fatal("unexpected security route accepted", route, response.Code)
		}
	}
}
