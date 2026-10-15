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
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// HTTP owns strict JSON/query admission, session/CSRF and the public response
// contracts. Provider tests own moderation, atomicity and concurrent ingestion.
func TestIssueTriageHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	schema := fmt.Sprintf("goodsentry_issue_triage_http_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
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
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, db))
	defer srv.Close()
	res, err := http.Get(srv.URL + cfg.Prefix + "/api/v1/issues/1/assignees")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("anonymous assignees=%d want=401 body=%s", res.StatusCode, raw)
	}
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	type response struct {
		code int
		raw  []byte
	}
	request := func(client *http.Client, method, path string, body []byte, headers http.Header) response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+cfg.Prefix+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for name, values := range headers {
			req.Header[name] = values
		}
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response{r.StatusCode, data}
	}
	want := func(t *testing.T, label string, r response, code int) response {
		t.Helper()
		if r.code != code {
			t.Fatalf("%s=%d want=%d body=%s", label, r.code, code, r.raw)
		}
		return r
	}
	decode := func(t *testing.T, r response, target any) {
		t.Helper()
		if err := json.Unmarshal(r.raw, target); err != nil {
			t.Fatalf("HTTP response JSON: %v body=%s", err, r.raw)
		}
	}
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	const password = "triage-http-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@triage.test", hash, "Owner", "Triage HTTP")
	if err != nil {
		t.Fatal(err)
	}
	users := map[string]string{}
	for _, name := range []string{"author", "peer", "outsider"} {
		var id string
		if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text", name+"@triage.test", hash, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		users[name] = id
		if name != "outsider" {
			if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	var teamID, foreignOrgID, foreignTeamID string
	if err := fixture.QueryRow(ctx, "INSERT INTO teams(organization_id,slug,name) VALUES($1,'transport-team','Transport Team') RETURNING id::text", org.ID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('triage-foreign','Foreign') RETURNING id::text").Scan(&foreignOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrgID, users["outsider"]); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRow(ctx, "INSERT INTO teams(organization_id,slug,name) VALUES($1,'foreign-team','Foreign Team') RETURNING id::text", foreignOrgID).Scan(&foreignTeamID); err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "Native Triage", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	foreignProject, err := db.CreateProject(ctx, users["outsider"], "triage-foreign", "Foreign Triage", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	type actor struct {
		client *http.Client
		csrf   string
	}
	login := func(email string) actor {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		var result struct{ CSRFToken string }
		decode(t, want(t, "login", request(client, "POST", "/api/v1/login", encode(map[string]string{"email": email, "password": password}), nil), 200), &result)
		if result.CSRFToken == "" {
			t.Fatal("login omitted CSRF token")
		}
		return actor{client, result.CSRFToken}
	}
	ownerActor, authorActor, peerActor, outsiderActor := login(owner.Email), login("author@triage.test"), login("peer@triage.test"), login("outsider@triage.test")
	csrf := func(who actor) http.Header { return http.Header{"X-CSRF-Token": {who.csrf}} }
	capture := func(project store.Project, organization, message, eventID string, viewer actor) string {
		t.Helper()
		body := "{}\n{\"type\":\"event\"}\n" + string(encode(map[string]any{"event_id": eventID, "message": message, "fingerprint": []string{message}}))
		want(t, "capture fixture issue", request(http.DefaultClient, "POST", "/api/"+project.ID+"/envelope/?sentry_version=7&sentry_key="+project.PublicKey, []byte(body), nil), 200)
		var result struct{ Data []struct{ ID string } }
		decode(t, want(t, "find fixture issue", request(viewer.client, "GET", "/api/v1/organizations/"+organization+"/issues?project="+project.ID+"&query="+url.QueryEscape(message), nil, nil), 200), &result)
		if len(result.Data) != 1 {
			t.Fatalf("fixture issue list=%+v", result.Data)
		}
		return result.Data[0].ID
	}
	issueID := capture(project, org.Slug, "Native triage HTTP proof", "11111111111111111111111111111111", ownerActor)
	otherIssueID := capture(project, org.Slug, "Comment nesting HTTP control", "22222222222222222222222222222222", ownerActor)
	foreignIssueID := capture(foreignProject, "triage-foreign", "Foreign triage HTTP control", "33333333333333333333333333333333", outsiderActor)
	path := "/api/v1/issues/" + issueID
	type wireAssignee struct {
		Type, ID, Name, Email, Slug string
	}
	type wireIssue struct {
		ID, Status              string
		AssignedTo              *wireAssignee
		ResolvedAt, RegressedAt *time.Time
	}
	type wireComment struct {
		ID, Text    string
		DateCreated time.Time
		UpdatedAt   *time.Time
		CanEdit     bool
		User        struct{ ID, Name, Email string }
	}
	type wireActivity struct {
		ID, Type    string
		Data        map[string]json.RawMessage
		DateCreated time.Time
		User        *struct{ ID, Name, Email string }
	}
	utc := func(t *testing.T, stamp time.Time) {
		t.Helper()
		_, offset := stamp.Zone()
		if stamp.IsZero() || offset != 0 || stamp != stamp.Truncate(time.Microsecond) {
			t.Fatalf("expected microsecond UTC instant, got %s", stamp.Format(time.RFC3339Nano))
		}
	}
	patchIssue := func(t *testing.T, body []byte) wireIssue {
		t.Helper()
		var result wireIssue
		decode(t, want(t, "member partial issue update", request(authorActor.client, "PATCH", path, body, csrf(authorActor)), 200), &result)
		return result
	}
	t.Run("read routes queries and candidates", func(t *testing.T) {
		var initialActivity struct {
			Data  []wireActivity
			Total int
		}
		initialResponse := want(t, "system activity response", request(authorActor.client, "GET", path+"/activity", nil, nil), 200)
		decode(t, initialResponse, &initialActivity)
		if initialActivity.Total != 1 || len(initialActivity.Data) != 1 || initialActivity.Data[0].ID == "" || initialActivity.Data[0].Type != "first_seen" || initialActivity.Data[0].Data == nil || initialActivity.Data[0].User != nil {
			t.Fatalf("fresh system activity=%+v", initialActivity)
		}
		var activityFields struct{ Data []map[string]json.RawMessage }
		decode(t, initialResponse, &activityFields)
		if string(activityFields.Data[0]["user"]) != "null" {
			t.Fatal("system activity user must be present null")
		}
		utc(t, initialActivity.Data[0].DateCreated)
		for _, suffix := range []string{"/assignees", "/comments", "/activity"} {
			want(t, "anonymous read", request(http.DefaultClient, "GET", path+suffix, nil, nil), 401)
			want(t, "foreign issue read", request(ownerActor.client, "GET", "/api/v1/issues/"+foreignIssueID+suffix, nil, nil), 404)
			want(t, "absent issue read", request(ownerActor.client, "GET", "/api/v1/issues/9223372036854775807"+suffix, nil, nil), 404)
			for _, query := range []string{"page=", "page=0", "page=01", "page=-1", "page=1.5", "page=1000001", "page=1&page=2", "page=%FF", "page=%00", "page=%zz", "unknown=x"} {
				want(t, "invalid page/query "+suffix+"?"+query, request(ownerActor.client, "GET", path+suffix+"?"+query, nil, nil), 400)
			}
			if suffix != "/assignees" {
				want(t, "query is assignees-only", request(ownerActor.client, "GET", path+suffix+"?query=author", nil, nil), 400)
			}
		}
		for _, query := range []string{"query=a&query=b", "query=%FF", "query=%00", "query=%zz", "query=" + url.QueryEscape(strings.Repeat("é", 101))} {
			want(t, "invalid assignee query", request(ownerActor.client, "GET", path+"/assignees?"+query, nil, nil), 400)
		}
		want(t, "maximum assignee query byte boundary", request(ownerActor.client, "GET", path+"/assignees?query="+url.QueryEscape(strings.Repeat("é", 100)), nil, nil), 200)
		var candidates struct {
			Data                  []wireAssignee
			Total, Page, PageSize int
		}
		decode(t, want(t, "member candidate read", request(authorActor.client, "GET", path+"/assignees", nil, nil), 200), &candidates)
		if candidates.Total != 5 || candidates.Page != 1 || candidates.PageSize != 25 || len(candidates.Data) != 5 {
			t.Fatalf("candidate page=%+v", candidates)
		}
		for _, query := range []struct{ value, kind, id string }{{"AUTHOR@TRIAGE.TEST", "user", users["author"]}, {"transport-team", "team", teamID}} {
			var result struct {
				Data  []wireAssignee
				Total int
			}
			decode(t, want(t, "candidate query", request(authorActor.client, "GET", path+"/assignees?query="+url.QueryEscape(query.value), nil, nil), 200), &result)
			if result.Total != 1 || len(result.Data) != 1 || result.Data[0].Type != query.kind || result.Data[0].ID != query.id || result.Data[0].Name == "" {
				t.Fatalf("filtered candidates=%+v", result)
			}
		}
		decode(t, want(t, "candidate past end", request(authorActor.client, "GET", path+"/assignees?page=1000000", nil, nil), 200), &candidates)
		if candidates.Total != 5 || len(candidates.Data) != 0 || candidates.Page != 1000000 {
			t.Fatalf("candidate past-end page=%+v", candidates)
		}
	})
	t.Run("partial assignment and status JSON", func(t *testing.T) {
		valid := encode(map[string]any{"assignedTo": map[string]string{"type": "user", "id": users["author"]}})
		want(t, "anonymous update", request(http.DefaultClient, "PATCH", path, valid, nil), 401)
		want(t, "update without CSRF", request(authorActor.client, "PATCH", path, valid, nil), 403)
		crossOrigin := csrf(authorActor)
		crossOrigin.Set("Origin", "https://untrusted.example")
		want(t, "cross-origin update", request(authorActor.client, "PATCH", path, valid, crossOrigin), 403)
		for _, body := range []string{
			`null`, `[]`, `{}`, `{"unknown":true}`, `{"status":null}`, `{"status":123}`, `{"status":""}`, `{"status":"muted"}`,
			`{"assignedTo":false}`, `{"assignedTo":[]}`, `{"assignedTo":"1"}`, `{"assignedTo":{}}`,
			`{"assignedTo":{"type":"user"}}`, `{"assignedTo":{"id":"1"}}`, `{"assignedTo":{"type":"other","id":"1"}}`,
			`{"assignedTo":{"type":null,"id":"1"}}`, `{"assignedTo":{"type":123,"id":"1"}}`,
			`{"assignedTo":{"type":"user","id":1}}`, `{"assignedTo":{"type":"user","id":null}}`,
			`{"assignedTo":{"type":"user","id":"01"}}`, `{"assignedTo":{"type":"user","id":"0"}}`,
			`{"assignedTo":{"type":"user","id":"-1"}}`, `{"assignedTo":{"type":"user","id":"+1"}}`,
			`{"assignedTo":{"type":"user","id":"1.0"}}`, `{"assignedTo":{"type":"user","id":"1 "}}`,
			`{"assignedTo":{"type":"user","id":"9223372036854775808"}}`,
			`{"assignedTo":{"type":"user","id":"1","name":"ignored"}}`, `{"status":"resolved","unknown":true}`, `{"status":"resolved"}{}`,
		} {
			want(t, "invalid partial update "+body, request(authorActor.client, "PATCH", path, []byte(body), csrf(authorActor)), 400)
		}
		for _, target := range []map[string]string{{"type": "user", "id": users["outsider"]}, {"type": "team", "id": foreignTeamID}, {"type": "user", "id": "9223372036854775807"}} {
			want(t, "hidden/absent assignment target", request(authorActor.client, "PATCH", path, encode(map[string]any{"status": "resolved", "assignedTo": target}), csrf(authorActor)), 404)
		}
		var initial struct {
			Issue json.RawMessage
		}
		decode(t, want(t, "detail after rejected inputs", request(ownerActor.client, "GET", path, nil, nil), 200), &initial)
		var initialIssue wireIssue
		if err := json.Unmarshal(initial.Issue, &initialIssue); err != nil {
			t.Fatal(err)
		}
		if initialIssue.Status != "unresolved" || initialIssue.AssignedTo != nil {
			t.Fatalf("rejected request mutated issue=%+v", initialIssue)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(initial.Issue, &fields); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"assignedTo", "resolvedAt", "regressedAt"} {
			if string(fields[field]) != "null" {
				t.Fatalf("initial issue field %s must be present null: %s", field, fields[field])
			}
		}
		assigned := patchIssue(t, valid)
		if assigned.AssignedTo == nil || assigned.AssignedTo.Type != "user" || assigned.AssignedTo.ID != users["author"] || assigned.AssignedTo.Email != "author@triage.test" || assigned.AssignedTo.Name != "author" {
			t.Fatalf("assigned user DTO=%+v", assigned.AssignedTo)
		}
		resolved := patchIssue(t, []byte(`{"status":"resolved"}`))
		if resolved.Status != "resolved" || resolved.AssignedTo == nil || resolved.AssignedTo.ID != users["author"] || resolved.ResolvedAt == nil {
			t.Fatalf("status-only update did not preserve assignment=%+v", resolved)
		}
		utc(t, *resolved.ResolvedAt)
		team := patchIssue(t, encode(map[string]any{"status": "ignored", "assignedTo": map[string]string{"type": "team", "id": teamID}}))
		if team.Status != "ignored" || team.AssignedTo == nil || team.AssignedTo.Type != "team" || team.AssignedTo.ID != teamID || team.AssignedTo.Slug != "transport-team" || team.AssignedTo.Name != "Transport Team" {
			t.Fatalf("combined team/status update=%+v", team)
		}
		if cleared := patchIssue(t, []byte(`{"assignedTo":null}`)); cleared.AssignedTo != nil || cleared.Status != "ignored" {
			t.Fatalf("explicit assignment clear=%+v", cleared)
		}
	})
	t.Run("comments detail and recorded activity wire", func(t *testing.T) {
		body := []byte(`{"text":"  Native comment <safe>  "}`)
		want(t, "anonymous comment", request(http.DefaultClient, "POST", path+"/comments", body, nil), 401)
		want(t, "comment without CSRF", request(authorActor.client, "POST", path+"/comments", body, nil), 403)
		var comment wireComment
		created := want(t, "author adds comment", request(authorActor.client, "POST", path+"/comments", body, csrf(authorActor)), 201)
		decode(t, created, &comment)
		if comment.ID == "" || comment.Text != "Native comment <safe>" || comment.User.ID != users["author"] || !comment.CanEdit || comment.UpdatedAt != nil {
			t.Fatalf("created comment=%+v", comment)
		}
		utc(t, comment.DateCreated)
		var fields map[string]json.RawMessage
		decode(t, created, &fields)
		if string(fields["updatedAt"]) != "null" {
			t.Fatalf("updatedAt must be a present null field: %s", fields["updatedAt"])
		}
		commentPath := path + "/comments/" + comment.ID
		for _, method := range []string{"PATCH", "DELETE"} {
			want(t, "anonymous comment mutation", request(http.DefaultClient, method, commentPath, body, nil), 401)
			want(t, "comment mutation without CSRF", request(authorActor.client, method, commentPath, body, nil), 403)
			want(t, "ordinary peer cannot mutate", request(peerActor.client, method, commentPath, body, csrf(peerActor)), 403)
			want(t, "comment cannot cross issue path", request(authorActor.client, method, "/api/v1/issues/"+otherIssueID+"/comments/"+comment.ID, body, csrf(authorActor)), 404)
			want(t, "absent comment", request(authorActor.client, method, path+"/comments/9223372036854775807", body, csrf(authorActor)), 404)
		}
		for _, text := range []string{`null`, `{}`, `{"text":null}`, `{"text":123}`, `{"text":" "}`, `{"text":"a\u0000b"}`, `{"text":"okay","unknown":1}`, `{"text":"okay"}{}`} {
			for _, method := range []string{"POST", "PATCH"} {
				endpoint := path + "/comments"
				if method == "PATCH" {
					endpoint = commentPath
				}
				want(t, "invalid comment JSON", request(authorActor.client, method, endpoint, []byte(text), csrf(authorActor)), 400)
			}
		}
		invalidUTF8 := append([]byte(`{"text":"`), 0xff)
		invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
		for _, method := range []string{"POST", "PATCH"} {
			endpoint := path + "/comments"
			if method == "PATCH" {
				endpoint = commentPath
			}
			want(t, "invalid UTF-8 comment body", request(authorActor.client, method, endpoint, invalidUTF8, csrf(authorActor)), 400)
			want(t, "comment UTF-8 byte budget", request(authorActor.client, method, endpoint, encode(map[string]string{"text": strings.Repeat("é", 5001)}), csrf(authorActor)), 400)
		}
		var edited wireComment
		decode(t, want(t, "author edits comment", request(authorActor.client, "PATCH", commentPath, []byte(`{"text":"  Edited native comment  "}`), csrf(authorActor)), 200), &edited)
		if edited.ID != comment.ID || edited.Text != "Edited native comment" || !edited.CanEdit || edited.UpdatedAt == nil || !edited.DateCreated.Equal(comment.DateCreated) {
			t.Fatalf("edited comment=%+v", edited)
		}
		utc(t, *edited.UpdatedAt)
		// Twenty-six real POSTs are needed here to distinguish the new bounded
		// detail and page wiring from the legacy unbounded Comments call.
		for index := 0; index < 25; index++ {
			text := fmt.Sprintf("HTTP page comment %02d", index)
			if index == 0 {
				text = strings.Repeat("é", 5000)
			}
			want(t, "comment page seed", request(authorActor.client, "POST", path+"/comments", encode(map[string]string{"text": text}), csrf(authorActor)), 201)
		}
		var page struct {
			Data                  []wireComment
			Total, Page, PageSize int
		}
		decode(t, want(t, "peer comment page", request(peerActor.client, "GET", path+"/comments", nil, nil), 200), &page)
		if page.Total != 26 || page.Page != 1 || page.PageSize != 25 || len(page.Data) != 25 || page.Data[0].CanEdit {
			t.Fatalf("comment first page=%+v", page)
		}
		decode(t, want(t, "author comment second page", request(authorActor.client, "GET", path+"/comments?page=2", nil, nil), 200), &page)
		if page.Total != 26 || page.Page != 2 || len(page.Data) != 1 || page.Data[0].ID != comment.ID || !page.Data[0].CanEdit {
			t.Fatalf("comment second page=%+v", page)
		}
		var detail struct {
			CommentTotal int
			Comments     []wireComment
		}
		decode(t, want(t, "bounded detail comments", request(authorActor.client, "GET", path, nil, nil), 200), &detail)
		if detail.CommentTotal != 26 || len(detail.Comments) != 25 {
			t.Fatalf("detail must expose total and page1 only: %+v", detail)
		}
		deleted := want(t, "author deletes comment", request(authorActor.client, "DELETE", commentPath, nil, csrf(authorActor)), 204)
		if len(deleted.raw) != 0 {
			t.Fatalf("DELETE204 must have an empty body: %s", deleted.raw)
		}
		decode(t, want(t, "comment past end retains total", request(authorActor.client, "GET", path+"/comments?page=2", nil, nil), 200), &page)
		if page.Total != 25 || page.Page != 2 || len(page.Data) != 0 {
			t.Fatalf("comment past end=%+v", page)
		}
		var activity struct {
			Data                  []wireActivity
			Total, Page, PageSize int
		}
		activityResponse := want(t, "activity first page", request(authorActor.client, "GET", path+"/activity", nil, nil), 200)
		decode(t, activityResponse, &activity)
		if activity.Total <= 25 || activity.Page != 1 || activity.PageSize != 25 || len(activity.Data) != 25 || activity.Data[0].Type != "comment_deleted" || activity.Data[0].User == nil || activity.Data[0].User.ID != users["author"] || string(activity.Data[0].Data["commentID"]) != string(encode(comment.ID)) {
			t.Fatalf("activity page=%+v", activity)
		}
		if bytes.Contains(activityResponse.raw, []byte("Edited native comment")) {
			t.Fatal("activity must not duplicate deleted comment content")
		}
		utc(t, activity.Data[0].DateCreated)
		decode(t, want(t, "activity past end", request(authorActor.client, "GET", path+"/activity?page=1000000", nil, nil), 200), &activity)
		if activity.Total <= 25 || activity.Page != 1000000 || len(activity.Data) != 0 {
			t.Fatalf("activity past end=%+v", activity)
		}
	})
}
