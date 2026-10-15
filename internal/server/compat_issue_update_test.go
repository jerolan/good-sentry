//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Authoring gate: HTTP owns the two exact PUTs, authoritative event:write
// admission, session/CSRF, strict request framing and compatible detail output.
// Missing routes, a read-scope write, or a dropped mergedInto error cannot be
// caught by parser/Store tests. Reuse the real fixture and public requests;
// Store owns the identity, policy lifecycle and concurrent transaction matrix.
func TestCompatibleIssueUpdateHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	global := "/api/0/issues/" + f.issueID + "/"
	organization := "/api/0/organizations/" + f.org.Slug + "/issues/"
	paths := []string{global, organization + f.issueID + "/"}
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	// Record missing-route RED here before the registration is integrated.
	for _, path := range paths {
		f.request(t, http.DefaultClient, "PUT", path, `{}`, nil, 401)
	}
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("compatible update JSON=%s error=%v", raw, err)
		}
	}
	nativeState := func(t *testing.T) map[string]json.RawMessage {
		t.Helper()
		var result map[string]json.RawMessage
		decode(f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200), &result)
		var analytics map[string]json.RawMessage
		decode(result["analytics"], &analytics)
		// Chart timestamps advance with the transaction clock. Preserve the
		// actual user count and every business field when checking for writes.
		delete(analytics, "stats")
		raw, err := json.Marshal(analytics)
		if err != nil {
			t.Fatal(err)
		}
		result["analytics"] = raw
		return result
	}
	type credential struct{ ID, Token string }
	issueToken := func(scope string) credential {
		t.Helper()
		var result credential
		body := resolutionJSON(t, map[string]any{"name": "Issue write HTTP " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
		decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, csrf, 201), &result)
		return result
	}
	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	writer := issueToken("event:write")
	reader := issueToken("event:read")
	t.Run("scopes sessions and authoritative Bearer", func(t *testing.T) {
		for _, scope := range []string{"org:read", "project:read", "project:releases"} {
			other := issueToken(scope)
			for _, path := range paths {
				f.request(t, http.DefaultClient, "PUT", path, `{}`, bearer(other.Token), 403)
			}
		}
		for _, path := range paths {
			f.request(t, http.DefaultClient, "PUT", path, `{}`, bearer(reader.Token), 403)
			f.request(t, f.client, "PUT", path, `{}`, nil, 403)
			f.request(t, f.client, "PUT", path, `{}`, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, 403)
			f.request(t, f.client, "PUT", path, `{}`, csrf, 200)
			f.request(t, http.DefaultClient, "PUT", path, `{}`, bearer(writer.Token), 200)
		}
		h := bearer("invalid")
		h.Set("X-CSRF-Token", f.csrf)
		f.request(t, f.client, "PUT", global, `{}`, h, 401)
		h = bearer(reader.Token)
		h.Set("X-CSRF-Token", f.csrf)
		f.request(t, f.client, "PUT", global, `{}`, h, 403)
		f.request(t, http.DefaultClient, "PATCH", "/api/v1/issues/"+f.issueID, `{"status":"resolved"}`, bearer(writer.Token), 401)
		f.request(t, http.DefaultClient, "PUT", "/api/0/organizations/"+f.org.Slug+"/releases/app%402.0/", `{}`, bearer(writer.Token), 403)
		f.request(t, http.DefaultClient, "GET", "/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/events/", "", bearer(writer.Token), 403)
	})
	t.Run("partial write projects real compatible detail", func(t *testing.T) {
		body := resolutionJSON(t, map[string]any{"status": "muted", "assignedTo": f.owner.Email, "ignoreDuration": 5, "ignoreCount": 2})
		var result struct {
			ID, Status, Count, Permalink string
			AssignedTo                   *struct{ Type, ID, Email string }
			StatusDetails                struct {
				IgnoreUntil *string
				IgnoreCount *int32
			}
			Stats           map[string]json.RawMessage
			UserReportCount *int64
		}
		decode(f.request(t, http.DefaultClient, "PUT", global, body, bearer(writer.Token), 200), &result)
		if result.ID != f.issueID || result.Status != "ignored" || result.Count != "1" || result.AssignedTo == nil || result.AssignedTo.Type != "user" || result.AssignedTo.ID != f.owner.ID || result.AssignedTo.Email != f.owner.Email || result.StatusDetails.IgnoreUntil == nil || result.StatusDetails.IgnoreCount == nil || *result.StatusDetails.IgnoreCount != 2 || result.Stats["24h"] == nil || result.UserReportCount == nil || result.Permalink != "https://public.example.test/good/?issue="+f.issueID+"&org="+f.org.Slug {
			t.Fatalf("PUT must return actual compatible detail: %+v", result)
		}
		var native struct {
			Issue struct {
				ID, Status string
				AssignedTo *struct{ ID string }
			}
		}
		decode(f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200), &native)
		if native.Issue.ID != f.issueID || native.Issue.Status != "ignored" || native.Issue.AssignedTo == nil || native.Issue.AssignedTo.ID != f.owner.ID {
			t.Fatalf("compatible write missing from native detail: %+v", native)
		}
		for _, path := range []string{
			global, organization + f.issueID + "/", organization + "?query=",
			"/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/issues/?query=",
			global + "events/", organization + f.issueID + "/events/latest/",
			organization + f.issueID + "/events/latest/json/",
			"/api/0/organizations/" + f.org.Slug + "/issues-stats/?groups=" + f.issueID,
		} {
			f.request(t, http.DefaultClient, "GET", path, "", bearer(writer.Token), 200)
		}
		f.request(t, http.DefaultClient, "PUT", paths[1], `{"status":"unresolved","assignedTo":null}`, bearer(writer.Token), 200)
	})
	// Preserve the explicit resolution-selector intent across HTTP parsing and
	// Store validation. Collapsing it into a generic clear silently removes a
	// current mute, a regression that direct Store intent tests cannot catch.
	t.Run("resolution clear selectors require resolved status", func(t *testing.T) {
		for _, tc := range []struct{ name, body string }{
			{"false next release", `{"statusDetails":{"inNextRelease":false}}`},
			{"empty release", `{"statusDetails":{"inRelease":""}}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var seeded struct {
					Status        string
					StatusDetails struct {
						IgnoreUntil *string
						IgnoreCount *int32
					}
				}
				decode(f.request(t, http.DefaultClient, "PUT", global, `{"status":"ignored","ignoreDuration":5,"ignoreCount":2}`, bearer(writer.Token), 200), &seeded)
				if seeded.Status != "ignored" || seeded.StatusDetails.IgnoreUntil == nil || seeded.StatusDetails.IgnoreCount == nil || *seeded.StatusDetails.IgnoreCount != 2 {
					t.Fatalf("regression requires a real active conditional mute: %+v", seeded)
				}
				before := nativeState(t)
				f.request(t, http.DefaultClient, "PUT", global, tc.body, bearer(writer.Token), 400)
				if !reflect.DeepEqual(before, nativeState(t)) {
					t.Fatal("rejected resolution selector changed native state")
				}
			})
		}
	})
	t.Run("strict path query and JSON admission", func(t *testing.T) {
		before := nativeState(t)
		noRedirect := &http.Client{Jar: f.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for _, path := range paths {
			req, err := http.NewRequestWithContext(f.ctx, "PUT", f.server.URL+"/good"+strings.TrimSuffix(path, "/"), strings.NewReader(`{"status":"resolved"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-CSRF-Token", f.csrf)
			response, err := noRedirect.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusTemporaryRedirect || response.Header.Get("Location") == "" {
				t.Fatalf("missing slash status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
			}
		}
		if !reflect.DeepEqual(before, nativeState(t)) {
			t.Fatal("noncanonical redirect invoked the issue mutator")
		}
		for _, id := range []string{"0", "01", "-1", "9223372036854775808", "not-an-id"} {
			f.request(t, http.DefaultClient, "PUT", "/api/0/issues/"+id+"/", `{}`, bearer(writer.Token), 400)
		}
		for _, query := range []string{"query=", "environment=production", "ignoreCount=1", "%zz"} {
			f.request(t, http.DefaultClient, "PUT", global+"?"+query, `{}`, bearer(writer.Token), 400)
		}
		for _, body := range []string{
			``, `null`, `[]`, `{`, `{} {}`, `{"status":"resolved","status":"unresolved"}`,
			`{"merge":true}`, `{"priority":"high"}`, `{"statusDetails":{"ignoreWindow":1}}`,
			`{"assignedTo":"` + string([]byte{0xff}) + `"}`,
			`{"assignedTo":"` + strings.Repeat("x", 1<<20) + `"}`,
		} {
			f.request(t, http.DefaultClient, "PUT", global, body, bearer(writer.Token), 400)
		}
		// Provider validation errors must be 400 rather than a storage 503.
		f.request(t, http.DefaultClient, "PUT", global, `{"statusDetails":{"inNextRelease":true}}`, bearer(writer.Token), 400)
		f.request(t, http.DefaultClient, "PUT", "/api/0/issues/9223372036854775807/", `{}`, bearer(writer.Token), 404)
		f.request(t, http.DefaultClient, "PUT", "/api/0/organizations/foreign-issue-write/issues/"+f.issueID+"/", `{}`, bearer(writer.Token), 404)
		f.request(t, http.DefaultClient, "PUT", global+"unexpected/", `{}`, bearer(writer.Token), 405)
	})
	t.Run("alias error and revoked credential keep transport meaning", func(t *testing.T) {
		body := "{}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{"event_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "message": "Compatible stale write alias", "fingerprint": []string{"Compatible stale write alias"}})
		f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+f.project.PublicKey, body, nil, 200)
		var list []struct{ ID string }
		decode(f.request(t, http.DefaultClient, "GET", organization+"?query=Compatible%20stale%20write%20alias", "", bearer(writer.Token), 200), &list)
		if len(list) != 1 {
			t.Fatalf("alias fixture issue count=%d", len(list))
		}
		merge := resolutionJSON(t, map[string][]string{"issueIds": {f.issueID, list[0].ID}})
		f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/issues/merge", merge, csrf, 200)
		for _, path := range []string{fmt.Sprintf("/api/0/issues/%s/", list[0].ID), organization + list[0].ID + "/"} {
			var conflict struct{ Detail, MergedInto string }
			decode(f.request(t, http.DefaultClient, "PUT", path, `{"status":"resolved"}`, bearer(writer.Token), 409), &conflict)
			if conflict.Detail == "" || conflict.MergedInto != f.issueID {
				t.Fatalf("alias conflict omitted current root: %+v", conflict)
			}
		}
		f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+writer.ID, "", csrf, 204)
		h := bearer(writer.Token)
		h.Set("X-CSRF-Token", f.csrf)
		f.request(t, f.client, "PUT", global, `{}`, h, 401)
	})
}
