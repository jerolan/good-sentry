//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func TestCompatibleUserDeletionHTTPBoundary(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	var memberID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('delete@user-http.test',$1,'Delete account') RETURNING id::text`, f.hash).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, f.org.ID, memberID); err != nil {
		t.Fatal(err)
	}

	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	f.request(t, f.client, "DELETE", "/api/0/users/"+memberID+"/", "", csrf, 404)
	f.request(t, f.client, "DELETE", "/api/0/users/"+memberID+"/", "", nil, 403)
	f.request(t, f.client, "DELETE", "/api/0/users/me/", "", csrf, 409)
	f.request(t, f.client, "GET", "/api/0/users/me/", "", nil, 200)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	member := &http.Client{Jar: jar}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(f.request(t, member, "POST", "/api/v1/login", `{"email":"delete@user-http.test","password":"resolution-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("member login=%+v err=%v", session, err)
	}
	memberCSRF := http.Header{"X-CSRF-Token": {session.CSRFToken}}
	f.request(t, member, "DELETE", "/api/0/users/"+f.owner.ID+"/", "", memberCSRF, 404)
	f.request(t, member, "DELETE", "/api/0/users/me/", "", memberCSRF, 204)
	f.request(t, member, "GET", "/api/v1/me", "", nil, 401)
	f.request(t, member, "POST", "/api/v1/login", `{"email":"delete@user-http.test","password":"resolution-http-password"}`, nil, 401)

	var rows int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM users WHERE id=$1::bigint`, memberID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("deleted user rows=%d err=%v", rows, err)
	}
	f.request(t, f.client, "GET", "/api/0/users/me/", "", nil, 200)
}
