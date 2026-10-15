//go:build integration

package server

import (
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
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// HTTP owns URL admission, authenticated wiring and the canonical page response.
// Ignored malformed/duplicate parameters or an unconnected grammar consumer are
// credible regressions not reached by parser or PostgreSQL provider tests. The
// fixture uses real sessions, SDK HTTP ingestion and PostgreSQL, without seams.
func TestIssueQueryHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	schema := fmt.Sprintf("goodsentry_issue_query_http_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("b", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, db))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	request := func(c *http.Client, path string) (int, map[string]json.RawMessage) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+cfg.Prefix+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		if err = json.Unmarshal(data, &body); err != nil {
			t.Fatalf("HTTP%d invalid JSON %s: %v", response.StatusCode, data, err)
		}
		return response.StatusCode, body
	}
	path := "/api/v1/organizations/issue-query-http/issues"
	if code, _ := request(http.DefaultClient, path+"?sort=invalid"); code != 401 {
		t.Fatalf("anonymous query status=%d", code)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("issue-query-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "issue-query@example.test", hash, "Issue Owner", "Issue Query HTTP")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "Browser", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL+cfg.Prefix+"/api/v1/login", strings.NewReader(`{"email":"issue-query@example.test","password":"issue-query-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("login=%d", response.StatusCode)
	}
	path = "/api/v1/organizations/" + org.Slug + "/issues"
	capture := func(id, environment, release string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"event_id": id, "message": "Transport history failure", "fingerprint": []string{"issue-query-http"}, "environment": environment, "release": release, "tags": map[string]string{"region": "eu"}})
		endpoint := srv.URL + cfg.Prefix + "/api/" + project.ID + "/envelope/?sentry_version=7&sentry_key=" + project.PublicKey
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader("{}\n{\"type\":\"event\"}\n"+string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("capture=%d %s", response.StatusCode, data)
		}
	}
	capture("11111111111111111111111111111111", "production", "build@1")
	capture("22222222222222222222222222222222", "staging", "build@2")
	t.Run("invalid query admission", func(t *testing.T) {
		for _, query := range []string{"sort=bogus", "sort=date&sort=freq", "page=0", "page=-1", "page=01", "page=1.5", "page=1000001", "page=bad", "page=", "unknown=value", "project=01", "project=0", "project=-1", "project=9223372036854775808", "status=typo", "status=resolved&status=ignored", "query=%zz", "query=%FF", "query=%00", "query=" + url.QueryEscape(strings.Repeat("x", 4097)), "environment=" + url.QueryEscape(strings.Repeat("é", 129)), "release=" + url.QueryEscape(strings.Repeat("é", 256)), "query=" + url.QueryEscape(`region:[eu,]`)} {
			t.Run(query[:min(60, len(query))], func(t *testing.T) {
				code, body := request(client, path+"?"+query)
				if code != 400 || len(body["detail"]) == 0 {
					t.Fatalf("status=%d body=%s", code, body)
				}
			})
		}
	})
	t.Run("historical grammar is wired to native read", func(t *testing.T) {
		query := url.Values{"project": {project.ID}, "environment": {"production"}, "release": {"build@1"}, "query": {`region:"eu" Transport history`}, "status": {"all"}, "sort": {"new"}, "page": {"1"}}
		code, body := request(client, path+"?"+query.Encode())
		if code != 200 {
			t.Fatalf("query=%d %s", code, body)
		}
		var data []store.Issue
		if err := json.Unmarshal(body["data"], &data); err != nil {
			t.Fatal(err)
		}
		if len(data) != 1 || data[0].Count != 2 {
			t.Fatalf("historical lifetime results=%+v", data)
		}
		if string(body["total"]) != "1" || string(body["pageSize"]) != "25" || string(body["page"]) != "1" {
			t.Fatalf("page response=%s", body)
		}
		query.Set("page", "2")
		code, body = request(client, path+"?"+query.Encode())
		if code != 200 || string(body["total"]) != "1" || string(body["page"]) != "2" || string(body["data"]) != "[]" {
			t.Fatalf("past-end response=%d %s", code, body)
		}
	})
	t.Run("scope and maximum selector wiring", func(t *testing.T) {
		for _, query := range []string{"release=" + url.QueryEscape(strings.Repeat("é", 255)), "environment=" + url.QueryEscape(strings.Repeat("é", 128)), "page=1000000"} {
			if code, _ := request(client, path+"?"+query); code != 200 {
				t.Fatalf("valid boundary status=%d", code)
			}
		}
		if code, _ := request(client, path+"?project=9223372036854775807"); code != 404 {
			t.Fatalf("missing project=%d", code)
		}
		if code, _ := request(client, "/api/v1/organizations/hidden-organization/issues"); code != 404 {
			t.Fatalf("hidden organization=%d", code)
		}
	})
}
