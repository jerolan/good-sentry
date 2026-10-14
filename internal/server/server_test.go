//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// The public HTTP boundary owns session/CSRF, browser SDK CORS and capture-to-
// query behavior. Parser/storage tests cannot prove routing or access controls.
// This test uses real PostgreSQL and ordinary HTTP clients, with no mocks/seams.
func TestAccountDSNCaptureAndTriage(t *testing.T) {
	ctx := context.Background()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	schema := fmt.Sprintf("goodsentry_http_%d", time.Now().UnixNano())
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Config{Domain: "http://example.test", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, db))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	request := func(method, path, body, csrf string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		m := map[string]any{}
		if len(b) > 0 && json.Unmarshal(b, &m) != nil {
			t.Fatalf("bad JSON %s", b)
		}
		return res.StatusCode, m
	}
	if code, _ := request("GET", "/api/v1/me", "", ""); code != 401 {
		t.Fatalf("anonymous me=%d", code)
	}
	code, me := request("POST", "/api/v1/setup", `{"email":"owner@example.test","password":"correct-long-password","name":"Owner","organization":"Test Org"}`, "")
	if code != 201 {
		t.Fatalf("setup=%d %+v", code, me)
	}
	csrf := me["csrfToken"].(string)
	org := me["organizations"].([]any)[0].(map[string]any)["slug"].(string)
	if code, _ = request("POST", "/api/v1/organizations/"+org+"/projects", `{"name":"No CSRF","platform":"go"}`, ""); code != 403 {
		t.Fatalf("CSRF=%d", code)
	}
	code, p := request("POST", "/api/v1/organizations/"+org+"/projects", `{"name":"Backend","platform":"go"}`, csrf)
	if code != 201 {
		t.Fatalf("project=%d %+v", code, p)
	}
	dsn, err := url.Parse(p["dsn"].(string))
	if err != nil {
		t.Fatal(err)
	}
	key := dsn.User.Username()
	project := p["id"].(string)
	endpoint := srv.URL + "/api/" + project + "/envelope/?sentry_version=7&sentry_key=" + key
	wire := []byte("{\"event_id\":\"0123456789abcdef0123456789abcdef\"}\n{\"type\":\"event\"}\n{\"message\":\"database failed\",\"level\":\"error\",\"tags\":{\"region\":\"test\"}}")
	for range 2 {
		req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(wire))
		req.Header.Set("Origin", "https://client.example.test")
		req.Header.Set("Content-Type", "text/plain")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("capture=%d %s", res.StatusCode, b)
		}
	}
	code, list := request("GET", "/api/v1/organizations/"+org+"/issues", "", "")
	if code != 200 {
		t.Fatalf("issues=%d %+v", code, list)
	}
	issues := list["data"].([]any)
	if len(issues) != 1 || issues[0].(map[string]any)["count"] != float64(1) {
		t.Fatalf("duplicate count %+v", list)
	}
	id := issues[0].(map[string]any)["id"].(string)
	code, detail := request("GET", "/api/v1/issues/"+id, "", "")
	if code != 200 || detail["event"].(map[string]any)["message"] != "database failed" {
		t.Fatalf("event=%d %+v", code, detail)
	}
	if code, _ = request("PATCH", "/api/v1/issues/"+id, `{"status":"resolved"}`, csrf); code != 200 {
		t.Fatalf("resolve=%d", code)
	}
	if code, _ = request("POST", "/api/v1/issues/"+id+"/comments", `{"text":"Investigating deployment"}`, csrf); code != 201 {
		t.Fatalf("comment=%d", code)
	}
	if code, _ = request("POST", "/api/v1/logout", "{}", csrf); code != 204 {
		t.Fatalf("logout=%d", code)
	}
	if code, _ = request("GET", "/api/v1/issues/"+id, "", ""); code != 401 {
		t.Fatalf("logged out access=%d", code)
	}
}
