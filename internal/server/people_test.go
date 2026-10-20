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

func TestPeopleAndTeamHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	adminConn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer adminConn.Close(context.Background())
	schema := fmt.Sprintf("goodsentry_people_http_%d", time.Now().UnixNano())
	if _, err := adminConn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer adminConn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	srv := httptest.NewServer(New(config.Config{Domain: "http://example.test", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}, db))
	defer srv.Close()
	request := func(client *http.Client, method, path, body, csrf string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		data := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &data)
		}
		return res.StatusCode, data
	}
	login := func(email, password string) (*http.Client, string) {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		code, me := request(client, "POST", "/api/v1/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), "")
		if code != 200 {
			t.Fatalf("login %s=%d %+v", email, code, me)
		}
		return client, me["csrfToken"].(string)
	}
	ownerClient := &http.Client{Jar: func() *cookiejar.Jar { jar, _ := cookiejar.New(nil); return jar }()}
	code, setup := request(ownerClient, "POST", "/api/v1/setup", `{"email":"owner@people.test","password":"people-password","name":"Owner","organization":"People"}`, "")
	if code != 201 {
		t.Fatalf("setup=%d %+v", code, setup)
	}
	owner := setup["user"].(map[string]any)
	csrf := setup["csrfToken"].(string)
	org := setup["organizations"].([]any)[0].(map[string]any)["slug"].(string)
	hash, err := bcrypt.GenerateFromPassword([]byte("people-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	add := func(email, name, role string) string {
		t.Helper()
		var id string
		if err := fixture.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text`, email, hash, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) SELECT id,$2,$3 FROM organizations WHERE slug=$1`, org, id, role); err != nil {
			t.Fatal(err)
		}
		return id
	}
	adminID := add("admin@people.test", "Admin", "admin")
	memberID := add("member@people.test", "Member", "member")
	adminClient, adminCSRF := login("admin@people.test", "people-password")
	memberClient, memberCSRF := login("member@people.test", "people-password")
	basePath := "/api/v1/organizations/" + org
	if code, _ := request(http.DefaultClient, "GET", basePath+"/members", "", ""); code != 401 {
		t.Fatalf("anonymous member list=%d", code)
	}
	if code, members := request(memberClient, "GET", basePath+"/members", "", ""); code != 200 || len(members["data"].([]any)) != 3 {
		t.Fatalf("member list=%d %+v", code, members)
	}
	if code, _ := request(memberClient, "POST", basePath+"/teams", `{"name":"Forbidden team"}`, memberCSRF); code != 403 {
		t.Fatalf("member create team=%d", code)
	}
	if code, _ := request(ownerClient, "POST", basePath+"/teams", `{"name":"On call"}`, ""); code != 403 {
		t.Fatalf("team mutation without CSRF=%d", code)
	}
	if code, _ := request(ownerClient, "POST", basePath+"/teams", `{"name":"   "}`, csrf); code != 400 {
		t.Fatalf("empty team name=%d", code)
	}
	code, team := request(ownerClient, "POST", basePath+"/teams", `{"name":"On call"}`, csrf)
	if code != 201 || team["slug"] != "on-call" {
		t.Fatalf("create team=%d %+v", code, team)
	}
	teamID := team["id"].(string)
	if code, _ := request(ownerClient, "PATCH", basePath+"/members/"+memberID, `{"role":"admin"}`, csrf); code != 200 {
		t.Fatalf("promote member=%d", code)
	}
	if code, _ := request(adminClient, "PATCH", basePath+"/members/"+memberID, `{"role":"owner"}`, adminCSRF); code != 403 {
		t.Fatalf("admin promoted self=%d", code)
	}
	if code, _ := request(adminClient, "PATCH", basePath+"/members/"+owner["id"].(string), `{"role":"member"}`, adminCSRF); code != 403 {
		t.Fatalf("admin changed owner=%d", code)
	}
	if code, _ := request(ownerClient, "PATCH", basePath+"/members/"+owner["id"].(string), `{"role":"member"}`, csrf); code != 409 {
		t.Fatalf("last owner demotion=%d", code)
	}
	if code, _ := request(ownerClient, "POST", basePath+"/teams/"+teamID+"/members/"+memberID, "", csrf); code != 204 {
		t.Fatalf("add team member=%d", code)
	}
	if code, detail := request(memberClient, "GET", basePath+"/teams/"+teamID, "", ""); code != 200 || len(detail["members"].([]any)) != 1 {
		t.Fatalf("team detail=%d %+v", code, detail)
	}
	if code, _ := request(ownerClient, "DELETE", basePath+"/teams/"+teamID+"/members/"+memberID, "", csrf); code != 204 {
		t.Fatalf("remove team member=%d", code)
	}
	if code, _ := request(ownerClient, "PATCH", basePath+"/teams/01", `{"name":"Invalid"}`, csrf); code != 400 {
		t.Fatalf("noncanonical team ID=%d", code)
	}
	if code, _ := request(ownerClient, "DELETE", basePath+"/teams/"+teamID, "", csrf); code != 204 {
		t.Fatalf("delete team=%d", code)
	}
	code, teams := request(ownerClient, "GET", basePath+"/teams", "", "")
	if code != 200 {
		t.Fatalf("teams=%d", code)
	}
	defaultID := ""
	for _, raw := range teams["data"].([]any) {
		if raw.(map[string]any)["slug"] == "default" {
			defaultID = raw.(map[string]any)["id"].(string)
		}
	}
	protectedCode, _ := request(ownerClient, "DELETE", basePath+"/teams/"+defaultID, "", csrf)
	if defaultID == "" || protectedCode != 403 {
		t.Fatalf("default team protected id=%q", defaultID)
	}
	if code, _ := request(ownerClient, "DELETE", basePath+"/members/"+memberID, "", csrf); code != 204 {
		t.Fatalf("remove member=%d", code)
	}
	if code, members := request(ownerClient, "GET", basePath+"/members", "", ""); code != 200 || len(members["data"].([]any)) != 2 {
		t.Fatalf("members after removal=%d %+v", code, members)
	}
	if memberID == "" || adminID == "" {
		t.Fatal("fixture identity missing")
	}
}
