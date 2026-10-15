//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// Authoring gate: HTTP owns native grouping routes, strict JSON/query input,
// cookie/CSRF admission and canonical alias/409 response wiring. A missing
// route, ambiguous array input or dropped mergedInto field is a credible
// regression that provider topology tests cannot exercise. Use the existing
// real PostgreSQL/session fixture and public requests; add no production seams.
func TestIssueGroupsHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	mergePath := "/api/v1/organizations/" + f.org.Slug + "/issues/merge"
	rootPath := "/api/v1/issues/" + f.issueID
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	// An unregistered POST route fails before reaching the session middleware.
	f.request(t, http.DefaultClient, "POST", mergePath, `{"issueIds":["1","2"]}`, nil, 401)
	f.request(t, http.DefaultClient, "POST", rootPath+"/unmerge", `{"originIds":["1"]}`, nil, 401)
	f.request(t, http.DefaultClient, "GET", rootPath+"/origins", "", nil, 401)
	var memberID string
	if err := f.sql.QueryRow(f.ctx, "INSERT INTO users(email,password_hash,name) VALUES('member@groups.test',$1,'Grouping member') RETURNING id::text", f.hash).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", f.org.ID, memberID); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	member := &http.Client{Jar: jar}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(f.request(t, member, "POST", "/api/v1/login", `{"email":"member@groups.test","password":"resolution-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("member login csrf: %v", err)
	}
	memberCSRF := http.Header{"X-CSRF-Token": {session.CSRFToken}}
	capture := func(projectID, key, org, message, eventID string) string {
		t.Helper()
		body := "{}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{"event_id": eventID, "message": message, "fingerprint": []string{message}})
		f.request(t, http.DefaultClient, "POST", "/api/"+projectID+"/envelope/?sentry_version=7&sentry_key="+key, body, nil, 200)
		var list struct{ Data []struct{ ID string } }
		if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/v1/organizations/"+org+"/issues?project="+projectID+"&query="+url.QueryEscape(message), "", nil, 200), &list); err != nil || len(list.Data) != 1 {
			t.Fatalf("capture-to-query fixture: %+v err=%v", list, err)
		}
		return list.Data[0].ID
	}
	secondID := capture(f.project.ID, f.project.PublicKey, f.org.Slug, "Merge origin HTTP", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	otherProject, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other group project", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	otherID := capture(otherProject.ID, otherProject.PublicKey, f.org.Slug, "Mixed project HTTP", "cccccccccccccccccccccccccccccccc")
	var foreignOrg string
	if err := f.sql.QueryRow(f.ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-groups','Foreign groups') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := f.db.CreateProject(f.ctx, f.owner.ID, "foreign-groups", "Foreign grouping", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	foreignID := capture(foreignProject.ID, foreignProject.PublicKey, "foreign-groups", "Foreign origin HTTP", "dddddddddddddddddddddddddddddddd")
	body := func(field string, ids ...string) string { return resolutionJSON(t, map[string][]string{field: ids}) }
	validMerge := body("issueIds", f.issueID, secondID)
	validUnmerge := body("originIds", secondID)
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("HTTP JSON: %v body=%s", err, raw)
		}
	}
	// Analytics advances with the transaction clock; compare every prior field.
	mutationState := func(raw []byte) map[string]json.RawMessage {
		t.Helper()
		var fields map[string]json.RawMessage
		decode(raw, &fields)
		delete(fields, "analytics")
		return fields
	}
	type wireIssue struct {
		ID        string
		GroupSize int
		Count     int
	}
	type mergeResult struct {
		Issue   wireIssue
		Changed bool
	}
	type originPage struct {
		Data []struct {
			ID, Fingerprint, Title string
			Count                  int
			IsRoot                 bool
		}
		Total, Page, PageSize int
		IssueID               string
	}
	t.Run("session CSRF and mutation query", func(t *testing.T) {
		for _, op := range []struct{ path, body string }{{mergePath, validMerge}, {rootPath + "/unmerge", validUnmerge}} {
			f.request(t, member, "POST", op.path, op.body, nil, 403)
			f.request(t, member, "POST", op.path, op.body, http.Header{"X-CSRF-Token": {session.CSRFToken}, "Origin": {"https://foreign.example"}}, 403)
			for _, query := range []string{"page=1", "unknown=x", "query=", "broken=%zz", "unknown=1&unknown=2"} {
				f.request(t, member, "POST", op.path+"?"+query, op.body, memberCSRF, 400)
			}
		}
	})
	t.Run("strict distinct canonical ID string arrays", func(t *testing.T) {
		before := f.request(t, f.client, "GET", rootPath, "", nil, 200)
		for _, op := range []struct {
			path, field string
			minimum     int
		}{{mergePath, "issueIds", 2}, {rootPath + "/unmerge", "originIds", 1}} {
			invalid := []string{`null`, `[]`, `{}`, `{"unknown":[]}`, fmt.Sprintf(`{%q:null}`, op.field), fmt.Sprintf(`{%q:1}`, op.field), fmt.Sprintf(`{%q:"1"}`, op.field), fmt.Sprintf(`{%q:{}}`, op.field), fmt.Sprintf(`{%q:[]}`, op.field), fmt.Sprintf(`{%q:[1,2]}`, op.field), fmt.Sprintf(`{%q:["1",null]}`, op.field), fmt.Sprintf(`{%q:["1","1"]}`, op.field), fmt.Sprintf(`{%q:["1","2"],"unknown":true}`, op.field), fmt.Sprintf(`{%q:["1","2"],%q:["1","2"]}`, op.field, op.field), fmt.Sprintf(`{%q:["1","2"]}{}`, op.field)}
			if op.minimum == 2 {
				invalid = append(invalid, body(op.field, "1"))
			}
			for _, id := range []string{"", "0", "-1", "+1", "01", "1.0", "9223372036854775808", "not-id"} {
				invalid = append(invalid, body(op.field, "2", id))
			}
			tooMany := make([]string, 101)
			for i := range tooMany {
				tooMany[i] = fmt.Sprint(i + 1)
			}
			invalid = append(invalid, body(op.field, tooMany...))
			invalid = append(invalid, fmt.Sprintf(`{%q:["1","%s"]}`, op.field, string([]byte{0xff})))
			for _, input := range invalid {
				f.request(t, member, "POST", op.path, input, memberCSRF, 400)
			}
		}
		if after := f.request(t, f.client, "GET", rootPath, "", nil, 200); !reflect.DeepEqual(mutationState(before), mutationState(after)) {
			t.Fatalf("malformed transport mutated issue: before=%s after=%s", before, after)
		}
	})
	t.Run("origins query admission and scoped provider errors", func(t *testing.T) {
		for _, query := range []string{"unknown=1", "query=a&query=b", "page=1&page=2", "query=%zz", "query=%FF", "query=%00", "query=" + url.QueryEscape(strings.Repeat("é", 101)), "page=", "page=0", "page=-1", "page=%2B1", "page=01", "page=1.0", "page=1000001", "page=9223372036854775808"} {
			f.request(t, member, "GET", rootPath+"/origins?"+query, "", nil, 400)
		}
		f.request(t, member, "GET", rootPath+"/origins?query="+url.QueryEscape(strings.Repeat("é", 100)), "", nil, 200)
		for _, ids := range [][]string{{f.issueID, foreignID}, {f.issueID, "9223372036854775807"}} {
			f.request(t, member, "POST", mergePath, body("issueIds", ids...), memberCSRF, 404)
		}
		f.request(t, member, "POST", mergePath, body("issueIds", f.issueID, otherID), memberCSRF, 400)
		f.request(t, member, "POST", rootPath+"/unmerge", body("originIds", foreignID), memberCSRF, 404)
		f.request(t, member, "GET", "/api/v1/issues/"+foreignID+"/origins", "", nil, 404)
	})
	var merged mergeResult
	decode(f.request(t, member, "POST", mergePath, validMerge, memberCSRF, 200), &merged)
	if !merged.Changed || merged.Issue.ID != f.issueID || merged.Issue.GroupSize != 2 {
		t.Fatalf("merge response=%+v", merged)
	}
	var retry mergeResult
	decode(f.request(t, member, "POST", mergePath, validMerge, memberCSRF, 200), &retry)
	if retry.Changed || retry.Issue.ID != f.issueID || retry.Issue.GroupSize != 2 {
		t.Fatalf("merge retry wire=%+v", retry)
	}
	aliasPath := "/api/v1/issues/" + secondID
	var aliasDetail struct{ Issue wireIssue }
	decode(f.request(t, member, "GET", aliasPath, "", nil, 200), &aliasDetail)
	if aliasDetail.Issue.ID != f.issueID || aliasDetail.Issue.GroupSize != 2 {
		t.Fatalf("alias detail did not canonicalize: %+v", aliasDetail)
	}
	var origins originPage
	decode(f.request(t, member, "GET", aliasPath+"/origins", "", nil, 200), &origins)
	if origins.IssueID != f.issueID || origins.Total != 2 || origins.Page != 1 || origins.PageSize != 25 || len(origins.Data) != 2 || origins.Data[0].ID != f.issueID || !origins.Data[0].IsRoot || origins.Data[1].ID != secondID || origins.Data[1].IsRoot || origins.Data[1].Fingerprint == "" {
		t.Fatalf("origins wire=%+v", origins)
	}
	decode(f.request(t, member, "GET", aliasPath+"/origins?query="+url.QueryEscape("mErGe OrIgIn HTTP"), "", nil, 200), &origins)
	if origins.Total != 1 || len(origins.Data) != 1 || origins.Data[0].ID != secondID {
		t.Fatalf("origin query lost through HTTP: %+v", origins)
	}
	decode(f.request(t, member, "GET", aliasPath+"/origins?page=1000000", "", nil, 200), &origins)
	if origins.Total != 2 || origins.Page != 1000000 || origins.PageSize != 25 || len(origins.Data) != 0 {
		t.Fatalf("past-end metadata lost: %+v", origins)
	}
	var comment struct{ ID string }
	decode(f.request(t, f.client, "POST", rootPath+"/comments", `{"text":"Existing group comment"}`, csrf, 201), &comment)
	before := f.request(t, member, "GET", rootPath, "", nil, 200)
	for _, op := range []struct{ method, path, body string }{{"PATCH", aliasPath, `{"status":"resolved"}`}, {"POST", aliasPath + "/comments", `{"text":"Stale draft"}`}, {"PATCH", aliasPath + "/comments/" + comment.ID, `{"text":"Stale edit"}`}, {"DELETE", aliasPath + "/comments/" + comment.ID, ""}, {"POST", aliasPath + "/unmerge", validUnmerge}} {
		var conflict struct{ Detail, MergedInto string }
		decode(f.request(t, f.client, op.method, op.path, op.body, csrf, 409), &conflict)
		if conflict.Detail == "" || conflict.MergedInto != f.issueID {
			t.Fatalf("stale mutation lost current group: %+v", conflict)
		}
	}
	if after := f.request(t, member, "GET", rootPath, "", nil, 200); !reflect.DeepEqual(mutationState(before), mutationState(after)) {
		t.Fatalf("stale mutation changed canonical issue: before=%s after=%s", before, after)
	}
	var split struct {
		Issue            wireIssue
		Changed          bool
		DetachedIssueIDs []string
		RemainingIssueID *string
	}
	decode(f.request(t, member, "POST", rootPath+"/unmerge", validUnmerge, memberCSRF, 200), &split)
	if !split.Changed || split.Issue.ID != f.issueID || split.Issue.GroupSize != 1 || len(split.DetachedIssueIDs) != 1 || split.DetachedIssueIDs[0] != secondID || split.RemainingIssueID == nil || *split.RemainingIssueID != f.issueID {
		t.Fatalf("unmerge response=%+v", split)
	}
	decode(f.request(t, member, "POST", rootPath+"/unmerge", validUnmerge, memberCSRF, 200), &split)
	if split.Changed || split.DetachedIssueIDs == nil || len(split.DetachedIssueIDs) != 0 {
		t.Fatalf("unmerge retry response=%+v", split)
	}
}
