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

// Authoring gate: HTTP owns exact deletion routes/statuses, issued scopes,
// session/CSRF/authoritative Bearer, and propagation of selection defaults and
// ignored selectors. Wrong admission or a decoder wired to the wrong provider
// cannot be caught by typed Store tests. Parser tests own the grammar matrix;
// Store owns group cleanup, rollback and concurrency. Reuse real credentials,
// DSN-authenticated manual captures and an isolated fixture without new seams.
func TestCompatibleIssueDeleteHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("delete response JSON: %v body=%s", err, raw)
		}
	}
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	orgPath := "/api/0/organizations/" + f.org.Slug + "/issues/"
	global := func(id string) string { return "/api/0/issues/" + id + "/" }
	scoped := func(id string) string { return orgPath + id + "/" }
	projectPath := func(slug string) string { return "/api/0/projects/" + f.org.Slug + "/" + slug + "/issues/" }
	paths := []string{global(f.issueID), scoped(f.issueID), orgPath, projectPath(f.project.Slug)}
	const missing = "9223372036854775807"
	management := "/api/v1/organizations/" + f.org.Slug + "/api-tokens"
	type actor struct {
		ID, CSRF string
		Client   *http.Client
	}
	owner := actor{f.owner.ID, f.csrf, f.client}
	type credential struct {
		ID, Token   string
		Scopes      []string
		AllProjects bool
		Projects    []struct{ ID string }
	}
	issueToken := func(who actor, scope string) credential {
		t.Helper()
		var issued credential
		body := resolutionJSON(t, map[string]any{"name": "Delete HTTP " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
		decode(f.request(t, who.Client, "POST", management, body, http.Header{"X-CSRF-Token": {who.CSRF}}, 201), &issued)
		if issued.ID == "" || issued.Token == "" || issued.AllProjects || !reflect.DeepEqual(issued.Scopes, []string{scope}) || len(issued.Projects) != 1 || issued.Projects[0].ID != f.project.ID {
			t.Fatal("issued credential omitted its requested scope or project restriction")
		}
		return issued
	}
	deleter := issueToken(owner, "event:admin")
	type project struct{ ID, Slug, DSN string }
	createProject := func(name string) project {
		t.Helper()
		var created project
		decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/projects", resolutionJSON(t, map[string]string{"name": name, "platform": "go"}), csrf, 201), &created)
		return created
	}
	var projects struct{ Data []project }
	decode(f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/projects", "", nil, 200), &projects)
	var primary project
	for _, candidate := range projects.Data {
		if candidate.ID == f.project.ID {
			primary = candidate
		}
	}
	if primary.DSN == "" {
		t.Fatal("native project list did not return the actual DSN")
	}
	sequence := 1000
	capture := func(p project, state string) string {
		t.Helper()
		sequence++
		eventID, title := fmt.Sprintf("%032x", sequence), fmt.Sprintf("Delete HTTP captured %d", sequence)
		dsn, err := url.Parse(p.DSN)
		if err != nil || dsn.User == nil || dsn.User.Username() == "" || dsn.Path != "/good/"+p.ID {
			t.Fatalf("issued DSN invalid for project%s: %v", p.ID, err)
		}
		payload := "{\"event_id\":\"" + eventID + "\"}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{"event_id": eventID, "message": title, "fingerprint": []string{title}})
		var ack struct{ ID string }
		decode(f.request(t, http.DefaultClient, "POST", "/api/"+p.ID+"/envelope/?sentry_version=7&sentry_key="+url.QueryEscape(dsn.User.Username()), payload, nil, 200), &ack)
		if ack.ID != eventID {
			t.Fatal("manual envelope acknowledgment did not contain the captured event ID")
		}
		var found struct{ Data []struct{ ID string } }
		decode(f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/issues?project="+p.ID+"&query="+url.QueryEscape(title), "", nil, 200), &found)
		if len(found.Data) != 1 {
			t.Fatalf("captured HTTP fixture did not produce exactly one issue: %+v", found)
		}
		id := found.Data[0].ID
		if state != "unresolved" {
			f.request(t, f.client, "PATCH", "/api/v1/issues/"+id, resolutionJSON(t, map[string]string{"status": state}), csrf, 200)
		}
		return id
	}
	nativeIssue := func(id string) json.RawMessage {
		t.Helper()
		var detail struct{ Issue json.RawMessage }
		decode(f.request(t, f.client, "GET", "/api/v1/issues/"+id, "", nil, 200), &detail)
		return detail.Issue
	}
	assertDeleted := func(ids ...string) {
		t.Helper()
		for _, id := range ids {
			f.request(t, f.client, "GET", "/api/v1/issues/"+id, "", nil, 404)
		}
	}
	acknowledge := func(client *http.Client, path string, header http.Header, status int) {
		t.Helper()
		raw := f.request(t, client, "DELETE", path, "", header, status)
		if status == 200 {
			var result map[string]json.RawMessage
			decode(raw, &result)
			if result == nil || len(result) != 0 {
				t.Fatalf("organization bulk must return only an empty object: %s", raw)
			}
		} else if len(raw) != 0 {
			t.Fatalf("DELETE%d must have an empty body: %s", status, raw)
		}
	}
	t.Run("all four routes require deletion scope or protected session", func(t *testing.T) {
		before := nativeIssue(f.issueID)
		for _, path := range paths {
			f.request(t, http.DefaultClient, "DELETE", path, "", nil, 401)
			f.request(t, f.client, "DELETE", path, "", nil, 403)
			f.request(t, f.client, "DELETE", path, "", http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, 403)
			for _, header := range []http.Header{{"Authorization": {"Bearer invalid"}}, {"Authorization": {"Basic ignored"}}, {"Authorization": {""}}, {"Authorization": {"Bearer " + deleter.Token, "Bearer " + deleter.Token}}} {
				header.Set("X-CSRF-Token", f.csrf)
				f.request(t, f.client, "DELETE", path, "", header, 401)
			}
		}
		for _, scope := range []string{"event:read", "event:write", "project:read", "project:releases", "org:read"} {
			other := issueToken(owner, scope)
			for _, path := range paths {
				header := bearer(other.Token)
				header.Set("X-CSRF-Token", f.csrf)
				f.request(t, f.client, "DELETE", path, "", header, 403)
			}
		}
		if !reflect.DeepEqual(before, nativeIssue(f.issueID)) {
			t.Fatal("denied deletion changed the anchor issue")
		}
	})
	t.Run("admin scope does not inherit other APIs", func(t *testing.T) {
		for _, request := range []struct {
			method, path, body string
			status             int
		}{
			{"GET", global(f.issueID), "", 403}, {"PUT", global(f.issueID), `{}`, 403},
			{"GET", orgPath, "", 403}, {"PUT", orgPath, `{}`, 403},
			{"GET", "/api/0/projects/" + f.org.Slug + "/" + primary.Slug + "/", "", 403},
			{"GET", "/api/0/projects/" + f.org.Slug + "/" + primary.Slug + "/events/", "", 403},
			{"GET", "/api/0/organizations/" + f.org.Slug + "/projects/", "", 403},
			{"PUT", "/api/0/organizations/" + f.org.Slug + "/releases/app%402.0/", `{}`, 403},
			{"GET", "/api/v1/issues/" + f.issueID, "", 401}, {"PATCH", "/api/v1/issues/" + f.issueID, `{"status":"resolved"}`, 401},
			{"DELETE", "/api/v1/issues/" + f.issueID, "", 405}, {"GET", management, "", 401},
		} {
			f.request(t, http.DefaultClient, request.method, request.path, request.body, bearer(deleter.Token), request.status)
		}
	})
	t.Run("strict body query ID and slash framing never invokes deletion", func(t *testing.T) {
		before := nativeIssue(f.issueID)
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for _, path := range paths {
			f.request(t, noRedirect, "DELETE", strings.TrimSuffix(path, "/"), "", bearer(deleter.Token), 307)
			f.request(t, http.DefaultClient, "DELETE", path+"unexpected/extra/", "", bearer(deleter.Token), 405)
			for _, body := range []string{" ", "{}", "null"} {
				f.request(t, http.DefaultClient, "DELETE", path, body, bearer(deleter.Token), 400)
			}
			f.request(t, http.DefaultClient, "DELETE", path+"?viewId=all", "", bearer(deleter.Token), 400)
		}
		for _, id := range []string{"0", "01", "9223372036854775808"} {
			for _, path := range []string{global(id), scoped(id), orgPath + "?id=" + id, projectPath(primary.Slug) + "?id=" + id} {
				f.request(t, http.DefaultClient, "DELETE", path, "", bearer(deleter.Token), 400)
			}
		}
		f.request(t, http.DefaultClient, "DELETE", global(f.issueID)+"?id="+f.issueID, "", bearer(deleter.Token), 400)
		f.request(t, http.DefaultClient, "DELETE", orgPath+"?id="+f.issueID+"&query=a&query=b", "", bearer(deleter.Token), 400)
		f.request(t, http.DefaultClient, "DELETE", orgPath+"?id="+f.issueID+"&query=%zz", "", bearer(deleter.Token), 400)
		f.request(t, http.DefaultClient, "DELETE", projectPath(primary.Slug)+"?id="+f.issueID+"&limit=1", "", bearer(deleter.Token), 400)
		if !reflect.DeepEqual(before, nativeIssue(f.issueID)) {
			t.Fatal("framing rejection or redirect changed the anchor issue")
		}
	})
	t.Run("individual routes and bulk serializers acknowledge actual deletion", func(t *testing.T) {
		for index, endpoint := range []func(string) string{global, scoped} {
			id := capture(primary, "unresolved")
			client, header := http.DefaultClient, bearer(deleter.Token)
			if index == 1 {
				client, header = f.client, csrf
			}
			acknowledge(client, endpoint(id), header, 204)
			assertDeleted(id)
			f.request(t, client, "DELETE", endpoint(id), "", header, 404)
		}
		for _, endpoint := range []func(string) string{global, scoped} {
			f.request(t, http.DefaultClient, "DELETE", endpoint(missing), "", bearer(deleter.Token), 404)
		}
		first, second := capture(primary, "resolved"), capture(primary, "ignored")
		acknowledge(http.DefaultClient, orgPath+"?id="+first+"&id="+first+"&id="+second, bearer(deleter.Token), 200)
		assertDeleted(first, second)
		id := capture(primary, "resolved")
		acknowledge(http.DefaultClient, projectPath(primary.Slug)+"?id="+id, bearer(deleter.Token), 204)
		assertDeleted(id)
	})
	t.Run("hidden containment precedes limit and known selectors are ignored for IDs", func(t *testing.T) {
		hidden := createProject("Delete hidden grant")
		hiddenID := capture(hidden, "unresolved")
		knownID := capture(primary, "resolved")
		for _, endpoint := range []string{global(hiddenID), scoped(hiddenID), projectPath(hidden.Slug), orgPath + "?project=" + hidden.ID} {
			f.request(t, http.DefaultClient, "DELETE", endpoint, "", bearer(deleter.Token), 404)
		}
		f.request(t, http.DefaultClient, "DELETE", "/api/0/organizations/absent-delete-org/issues/"+knownID+"/", "", bearer(deleter.Token), 404)
		acknowledge(http.DefaultClient, orgPath+"?id="+knownID+"&id="+hiddenID+"&limit=1", bearer(deleter.Token), 200)
		acknowledge(http.DefaultClient, projectPath(primary.Slug)+"?id="+knownID+"&id="+hiddenID, bearer(deleter.Token), 204)
		nativeIssue(knownID)
		nativeIssue(hiddenID)
		ignored := url.Values{"id": {knownID}, "limit": {"1"}, "project": {"nonexistent-project"}, "environment": {"no-match"}, "query": {"unsupported:selector release:["}, "status": {"reprocessing"}, "sort": {"unsupported"}, "start": {"invalid"}, "end": {"invalid"}, "statsPeriod": {"forever"}}
		acknowledge(http.DefaultClient, orgPath+"?"+ignored.Encode(), bearer(deleter.Token), 200)
		assertDeleted(knownID)
		nativeIssue(hiddenID)
	})
	t.Run("individual alias conflicts and bulk alias canonicalization retain meaning", func(t *testing.T) {
		root, alias := capture(primary, "unresolved"), capture(primary, "unresolved")
		f.request(t, f.client, "PUT", orgPath+"?id="+root+"&id="+alias, `{"merge":true}`, csrf, 200)
		for _, endpoint := range []func(string) string{global, scoped} {
			var conflict struct{ MergedInto string }
			decode(f.request(t, http.DefaultClient, "DELETE", endpoint(alias), "", bearer(deleter.Token), 409), &conflict)
			if conflict.MergedInto != root {
				t.Fatalf("alias response lost its current root: %+v", conflict)
			}
		}
		nativeIssue(root)
		acknowledge(http.DefaultClient, orgPath+"?id="+alias+"&id="+root, bearer(deleter.Token), 200)
		assertDeleted(root, alias)
	})
	t.Run("ID-less organization defaults differ from explicit empty query and project scope", func(t *testing.T) {
		selected := createProject("Delete organization defaults")
		unresolved, resolved, ignored := capture(selected, "unresolved"), capture(selected, "resolved"), capture(selected, "ignored")
		acknowledge(f.client, orgPath+"?project="+selected.ID, csrf, 200)
		assertDeleted(unresolved)
		nativeIssue(resolved)
		nativeIssue(ignored)
		acknowledge(f.client, orgPath+"?project="+selected.ID+"&query=", csrf, 200)
		assertDeleted(resolved, ignored)
		selected = createProject("Delete project defaults")
		unresolved, resolved, ignored = capture(selected, "unresolved"), capture(selected, "resolved"), capture(selected, "ignored")
		acknowledge(f.client, projectPath(selected.Slug), csrf, 204)
		assertDeleted(unresolved, resolved, ignored)
		nativeIssue(f.issueID)
	})
	t.Run("owner and admin manage credentials while current membership governs consumption", func(t *testing.T) {
		// This project has no membership-provisioning HTTP API. Prepare only the
		// actors in the isolated schema, then use real login and management HTTP.
		loginActor := func(role string) actor {
			t.Helper()
			email := role + "@delete-http.test"
			var id string
			if err := f.sql.QueryRow(f.ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text", email, f.hash, role).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if _, err := f.sql.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3)", f.org.ID, id, role); err != nil {
				t.Fatal(err)
			}
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Jar: jar}
			var me struct{ CSRFToken string }
			decode(f.request(t, client, "POST", "/api/v1/login", resolutionJSON(t, map[string]string{"email": email, "password": "resolution-http-password"}), nil, 200), &me)
			if me.CSRFToken == "" {
				t.Fatal("role fixture login did not return CSRF")
			}
			return actor{id, me.CSRFToken, client}
		}
		admin, member := loginActor("admin"), loginActor("member")
		adminToken := issueToken(admin, "event:admin")
		var metadata struct{ Data []credential }
		decode(f.request(t, admin.Client, "GET", management, "", nil, 200), &metadata)
		found := false
		for _, item := range metadata.Data {
			if item.ID == adminToken.ID {
				found = reflect.DeepEqual(item.Scopes, []string{"event:admin"}) && item.Token == ""
			}
		}
		if !found {
			t.Fatal("admin management omitted the new scope or redisclosed its secret")
		}
		memberCSRF := http.Header{"X-CSRF-Token": {member.CSRF}}
		f.request(t, member.Client, "GET", management, "", nil, 403)
		f.request(t, member.Client, "POST", management, resolutionJSON(t, map[string]any{"name": "Denied deletion credential", "scopes": []string{"event:admin"}, "projectIds": []string{primary.ID}}), memberCSRF, 403)
		f.request(t, member.Client, "DELETE", management+"/"+adminToken.ID, "", memberCSRF, 403)
		acknowledge(http.DefaultClient, orgPath+"?id="+missing, bearer(adminToken.Token), 200)
		spare := issueToken(owner, "event:admin")
		f.request(t, admin.Client, "DELETE", management+"/"+spare.ID, "", http.Header{"X-CSRF-Token": {admin.CSRF}}, 204)
		if _, err := f.sql.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, admin.ID); err != nil {
			t.Fatal(err)
		}
		expired := issueToken(owner, "event:admin")
		if _, err := f.sql.Exec(f.ctx, "UPDATE api_tokens SET expires_at=now()-interval '1 second' WHERE id=$1::bigint", expired.ID); err != nil {
			t.Fatal(err)
		}
		before := nativeIssue(f.issueID)
		for _, token := range []string{spare.Token, adminToken.Token, expired.Token} {
			for _, path := range paths {
				header := bearer(token)
				header.Set("X-CSRF-Token", f.csrf)
				f.request(t, f.client, "DELETE", path, "", header, 401)
			}
		}
		f.request(t, f.client, "DELETE", management+"/"+deleter.ID, "", csrf, 204)
		for _, path := range paths {
			header := bearer(deleter.Token)
			header.Set("X-CSRF-Token", f.csrf)
			f.request(t, f.client, "DELETE", path, "", header, 401)
		}
		if !reflect.DeepEqual(before, nativeIssue(f.issueID)) {
			t.Fatal("revoked/expired/nonmember Bearer fell back to an authorized cookie")
		}
	})
}
