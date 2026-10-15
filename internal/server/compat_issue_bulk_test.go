//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Authoring gate: HTTP owns exact bulk routing, issued-token/session admission,
// strict framing and the partial acknowledgement. A missing registration or
// wrong capability cannot be observed by parser or Store tests. Reuse the real
// HTTP fixture; Store owns selection, mutation and transaction invariants.
func TestCompatibleIssueBulkHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	paths := []string{
		"/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/issues/",
		"/api/0/organizations/" + f.org.Slug + "/issues/",
	}
	for _, path := range paths {
		f.request(t, http.DefaultClient, "PUT", path, `{}`, nil, 401)
	}
	decode := func(t *testing.T, raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	type credential struct{ ID, Token string }
	issueToken := func(scope string) credential {
		t.Helper()
		var result credential
		body := resolutionJSON(t, map[string]any{"name": "Bulk HTTP " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
		decode(t, f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, csrf, 201), &result)
		return result
	}
	writer := issueToken("event:write")
	reader := issueToken("event:read")
	nativeIssue := func(t *testing.T) map[string]json.RawMessage {
		t.Helper()
		var detail struct{ Issue map[string]json.RawMessage }
		decode(t, f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200), &detail)
		delete(detail.Issue, "analytics")
		return detail.Issue
	}
	acknowledge := func(t *testing.T, path, body string, want map[string]any) {
		t.Helper()
		var got map[string]any
		decode(t, f.request(t, http.DefaultClient, "PUT", path, body, bearer(writer.Token), 200), &got)
		// JSON round-tripping converts integer configuration to wire numbers.
		var wireWant map[string]any
		decode(t, []byte(resolutionJSON(t, want)), &wireWant)
		if !reflect.DeepEqual(got, wireWant) {
			t.Fatalf("ack=%v want=%v", got, wireWant)
		}
	}
	t.Run("both routes require the writer or a protected session", func(t *testing.T) {
		for _, path := range paths {
			f.request(t, http.DefaultClient, "PUT", path, `{}`, bearer(reader.Token), 403)
			f.request(t, f.client, "PUT", path, `{}`, nil, 403)
			f.request(t, f.client, "PUT", path, `{}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, 403)
			f.request(t, f.client, "PUT", path, `{}`, csrf, 200)
			acknowledge(t, path, `{}`, map[string]any{})
			h := bearer("invalid")
			h.Set("X-CSRF-Token", f.csrf)
			f.request(t, f.client, "PUT", path, `{}`, h, 401)
			h = bearer(reader.Token)
			h.Set("X-CSRF-Token", f.csrf)
			f.request(t, f.client, "PUT", path, `{}`, h, 403)
		}
	})
	t.Run("partial acknowledgement uses committed actor and configuration", func(t *testing.T) {
		project := paths[0] + "?id=" + f.issueID
		organization := paths[1] + "?id=" + f.issueID
		acknowledge(t, project, `{"status":"resolved","snoozeDuration":null}`, map[string]any{"status": "resolved"})
		if string(nativeIssue(t)["status"]) != `"resolved"` {
			t.Fatal("CLI-shaped request did not mutate native state")
		}
		acknowledge(t, organization, resolutionJSON(t, map[string]any{"status": "muted", "assignedTo": f.owner.Email, "snoozeDuration": 0, "ignoreCount": 2}), map[string]any{
			"status": "ignored", "assignedTo": map[string]any{"type": "user", "id": f.owner.ID, "name": f.owner.Name, "email": f.owner.Email},
			"statusDetails": map[string]any{"ignoreDuration": 0, "ignoreCount": 2},
		})
		if string(nativeIssue(t)["status"]) != `"ignored"` || string(nativeIssue(t)["assignedTo"]) == "null" {
			t.Fatal("acknowledged actor or ignored state is absent from native detail")
		}
		acknowledge(t, project, `{"assignedTo":null,"statusDetails":null}`, map[string]any{"assignedTo": nil, "statusDetails": map[string]any{}})
		acknowledge(t, organization, `{"status":"resolved","statusDetails":{"inRelease":"latest"}}`, map[string]any{"status": "resolved", "statusDetails": map[string]any{"inRelease": "app@2.0"}})
		acknowledge(t, project, `{"status":"resolvedInNextRelease"}`, map[string]any{"status": "resolved", "statusDetails": map[string]any{"inNextRelease": true}})
		acknowledge(t, organization, `{"status":"unresolved"}`, map[string]any{"status": "unresolved"})
		before := nativeIssue(t)
		acknowledge(t, project, `{"status":null,"snoozeDuration":null,"merge":false}`, map[string]any{})
		if !reflect.DeepEqual(before, nativeIssue(t)) {
			t.Fatal("null CLI duration or false merge changed native issue state")
		}
		acknowledge(t, paths[0]+"?id=9223372036854775807", `{"status":"resolved"}`, map[string]any{})
	})
	t.Run("strict framing and exact collection routing", func(t *testing.T) {
		before := nativeIssue(t)
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for _, path := range paths {
			f.request(t, noRedirect, "PUT", strings.TrimSuffix(path, "/"), `{"status":"resolved"}`, bearer(writer.Token), http.StatusTemporaryRedirect)
			f.request(t, http.DefaultClient, "PUT", path+"unexpected/extra/", `{}`, bearer(writer.Token), 405)
			for _, body := range []string{
				``, `null`, `[]`, `{`, `{} {}`, `{"status":"resolved","status":"unresolved"}`,
				`{"snoozeDuration":1,"snoozeDuration":2}`, `{"merge":true,"merge":false}`,
				`{"priority":"high"}`, `{"statusDetails":{"ignoreWindow":1}}`,
				`{"assignedTo":"` + string([]byte{0xff}) + `"}`, `{"assignedTo":"` + strings.Repeat("x", 1<<20) + `"}`,
			} {
				f.request(t, http.DefaultClient, "PUT", path+"?id="+f.issueID, body, bearer(writer.Token), 400)
			}
			for _, query := range []string{"cursor=", "limit=101", "query=a&query=b", "query=%zz", "id=01", "statsPeriod=1h&start=invalid"} {
				f.request(t, http.DefaultClient, "PUT", path+"?"+query, `{}`, bearer(writer.Token), 400)
			}
			// These errors originate in the typed provider, so the transport must
			// classify them as client errors instead of a storage failure.
			f.request(t, http.DefaultClient, "PUT", path, `{"assignedTo":null}`, bearer(writer.Token), 400)
			f.request(t, http.DefaultClient, "PUT", path, `{"statusDetails":{}}`, bearer(writer.Token), 400)
			f.request(t, http.DefaultClient, "PUT", path+"?id="+f.issueID, `{"merge":true}`, bearer(writer.Token), 400)
			f.request(t, http.DefaultClient, "PUT", path+"?id="+f.issueID+"&query=release%3A%5B", `{}`, bearer(writer.Token), 400)
		}
		if !reflect.DeepEqual(before, nativeIssue(t)) {
			t.Fatal("rejected request or collection redirect mutated native state")
		}
	})
	t.Run("merge acknowledgement contains actual topology rather than detail", func(t *testing.T) {
		envelope := "{}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{
			"event_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "message": "Bulk HTTP merge source", "fingerprint": []string{"Bulk HTTP merge source"},
		})
		f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+f.project.PublicKey, envelope, nil, 200)
		var sources []struct{ ID string }
		decode(t, f.request(t, http.DefaultClient, "GET", paths[1]+"?query=Bulk%20HTTP%20merge%20source", "", bearer(writer.Token), 200), &sources)
		if len(sources) != 1 || sources[0].ID == f.issueID {
			t.Fatalf("merge fixture must contain a distinct real issue: %+v", sources)
		}
		selection := "?id=" + f.issueID + "&id=" + sources[0].ID
		acknowledge(t, paths[0]+selection, `{"merge":1,"status":"resolved"}`, map[string]any{
			"status": "resolved", "merge": map[string]any{"parent": f.issueID, "children": []string{sources[0].ID}},
		})
		var count int64
		var size int
		issue := nativeIssue(t)
		decode(t, issue["count"], &count)
		decode(t, issue["groupSize"], &size)
		if count != 2 || size != 2 || string(issue["status"]) != `"resolved"` {
			t.Fatalf("merge acknowledgement disagrees with native group count=%d size=%d status=%s", count, size, issue["status"])
		}
		acknowledge(t, paths[1]+selection, `{"merge":true}`, map[string]any{
			"merge": map[string]any{"parent": f.issueID, "children": []string{}},
		})
		before := nativeIssue(t)
		acknowledge(t, paths[1]+selection, `{"merge":0}`, map[string]any{})
		if !reflect.DeepEqual(before, nativeIssue(t)) {
			t.Fatal("merge:0 split or otherwise changed the merged issue")
		}
	})
	t.Run("hidden targets and revoked Bearer retain their meaning", func(t *testing.T) {
		var hidden struct{ ID, Slug string }
		decode(t, f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/projects", `{"name":"Bulk hidden project","platform":"go"}`, csrf, 201), &hidden)
		f.request(t, http.DefaultClient, "PUT", "/api/0/projects/"+f.org.Slug+"/"+hidden.Slug+"/issues/", `{}`, bearer(writer.Token), 404)
		f.request(t, http.DefaultClient, "PUT", paths[1]+"?project="+hidden.ID, `{"status":"resolved"}`, bearer(writer.Token), 404)
		f.request(t, http.DefaultClient, "PUT", "/api/0/organizations/absent-bulk-org/issues/", `{}`, bearer(writer.Token), 404)
		f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+writer.ID, "", csrf, 204)
		for _, path := range paths {
			h := bearer(writer.Token)
			h.Set("X-CSRF-Token", f.csrf)
			f.request(t, f.client, "PUT", path, `{"status":"resolved"}`, h, 401)
		}
	})
}
