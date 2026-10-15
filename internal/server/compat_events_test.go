//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Authoring gate: HTTP owns exact routes, authoritative Bearer/session admission,
// strict query encoding, serializer dispatch and prefixed pagination Links. A
// missing route, wrong scope, accepted malformed value or dropped filter is a
// credible failure that provider/pure projection tests cannot reach. Reuse the
// real PostgreSQL/session fixture; no productive seam or duplicated SQL matrix.
func TestCompatibleEventsHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	projectPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/events/"
	issuePath := "/api/0/issues/" + f.issueID + "/events/"
	orgIssuePath := "/api/0/organizations/" + f.org.Slug + "/issues/" + f.issueID + "/events/"
	const eventID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// This must first fail because the route is missing, before authentication.
	f.request(t, http.DefaultClient, "GET", projectPath, "", nil, 401)
	f.request(t, http.DefaultClient, "GET", issuePath, "", nil, 401)
	f.request(t, http.DefaultClient, "GET", orgIssuePath, "", nil, 401)
	type wireEvent struct {
		ID, EventID, GroupID string
		ProjectID            int64
		Entries              json.RawMessage
	}
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("event response JSON: %v body=%s", err, raw)
		}
	}
	var item wireEvent
	for _, path := range []string{projectPath + eventID + "/", issuePath + eventID + "/", orgIssuePath + "latest/"} {
		decode(f.request(t, f.client, "GET", path, "", nil, 200), &item)
		if item.ID != eventID || item.EventID != eventID || item.GroupID != f.issueID || fmt.Sprint(item.ProjectID) != f.project.ID || len(item.Entries) == 0 {
			t.Fatalf("detail serializer wiring: %+v", item)
		}
	}
	var legacy struct {
		EventID string `json:"event_id"`
		Project int64
	}
	decode(f.request(t, f.client, "GET", orgIssuePath+eventID+"/json/", "", nil, 200), &legacy)
	if legacy.EventID != eventID || fmt.Sprint(legacy.Project) != f.project.ID {
		t.Fatalf("JSON projection route: %+v", legacy)
	}
	for _, path := range []string{projectPath + eventID + "/unexpected/", issuePath + eventID + "/json/", orgIssuePath + eventID + "/json/extra/"} {
		f.request(t, f.client, "GET", path, "", nil, 404)
	}
	t.Run("Bearer route scopes and authoritative credentials", func(t *testing.T) {
		type credential struct{ ID, Token string }
		issueToken := func(name string, scopes []string, projects []string) credential {
			t.Helper()
			body := resolutionJSON(t, map[string]any{"name": name, "scopes": scopes, "projectIds": projects})
			var c credential
			decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &c)
			if c.Token == "" || c.ID == "" {
				t.Fatal("credential issuance omitted secret/id")
			}
			return c
		}
		header := func(c credential) http.Header { return http.Header{"Authorization": {"Bearer " + c.Token}} }
		projectToken := issueToken("Project event reader", []string{"project:read"}, []string{f.project.ID})
		eventToken := issueToken("Issue event reader", []string{"event:read"}, []string{f.project.ID})
		f.request(t, http.DefaultClient, "GET", projectPath+eventID+"/", "", header(projectToken), 200)
		f.request(t, http.DefaultClient, "GET", issuePath, "", header(projectToken), 403)
		for _, path := range []string{issuePath, orgIssuePath + eventID + "/", orgIssuePath + eventID + "/json/"} {
			f.request(t, http.DefaultClient, "GET", path, "", header(eventToken), 200)
		}
		f.request(t, http.DefaultClient, "GET", projectPath, "", header(eventToken), 403)
		f.request(t, http.DefaultClient, "GET", "/api/v1/issues/"+f.issueID, "", header(eventToken), 401)
		for _, bad := range []http.Header{{"Authorization": {"Bearer invalid"}}, {"Authorization": {"Basic ignored"}}, {"Authorization": {""}}, {"Authorization": {"Bearer " + eventToken.Token, "Bearer " + eventToken.Token}}} {
			f.request(t, f.client, "GET", issuePath, "", bad, 401)
		}
		other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Hidden event project", "go")
		if err != nil {
			t.Fatal(err)
		}
		hidden := issueToken("Other project only", []string{"event:read", "project:read"}, []string{other.ID})
		for _, path := range []string{projectPath, issuePath, orgIssuePath} {
			f.request(t, http.DefaultClient, "GET", path, "", header(hidden), 404)
		}
		f.request(t, http.DefaultClient, "GET", "/api/0/organizations/foreign-event-org/issues/"+f.issueID+"/events/", "", header(eventToken), 404)
		f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+eventToken.ID, "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
		f.request(t, http.DefaultClient, "GET", issuePath, "", header(eventToken), 401)
	})
	t.Run("strict list and detail query admission", func(t *testing.T) {
		invalid := []string{"unknown=x", "cursor=0:01:0", "cursor=0:1:2", "cursor=1:1:0", "cursor=0:100000001:0", "cursor=0:0:0&cursor=", "per_page=", "limit=01", "limit=101", "limit=2&per_page=3", "full=yes", "sample=", "full=true&full=false", "query=x&query=y", "start=", "start=2026-02-30T01:00:00Z", "start=2026-01-03T00:00:00Z&end=2026-01-02T00:00:00Z", "statsPeriod=0h", "statsPeriod=-1d", "statsPeriod=1.5h", "statsPeriod=999999999999999999999w", "query=%zz", "environment=%FF", "environment=%00", "environment=" + url.QueryEscape(strings.Repeat("é", 201)), "environment=" + strings.Repeat("prod&environment=", 100) + "prod"}
		for _, query := range invalid {
			f.request(t, f.client, "GET", issuePath+"?"+query, "", nil, 400)
		}
		for _, query := range []string{"?query=", "?limit=2&per_page=2&full=0&sample=1", "?cursor=&statsPeriod=", "?start=2026-01-01T00:00:00.123456789&end=2026-01-02T00:00:00Z", "?statsPeriod=1w&start=2020-01-01T00:00:00Z&end=2020-01-02T00:00:00Z", "?environment=" + url.QueryEscape(strings.Repeat("é", 200))} {
			f.request(t, f.client, "GET", projectPath+query, "", nil, 200)
		}
		for _, path := range []string{projectPath + eventID + "/", orgIssuePath + "latest/", orgIssuePath + eventID + "/json/"} {
			for _, query := range []string{"?full=true", "?limit=1", "?query=", "?environment=%zz", "?environment=%00"} {
				f.request(t, f.client, "GET", path+query, "", nil, 400)
			}
		}
	})
	t.Run("full cap and public prefixed Links preserve supplied spelling", func(t *testing.T) {
		for i := 1; i <= 11; i++ {
			body := "{}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{"event_id": fmt.Sprintf("%032x", i), "message": "Resolution HTTP fixture", "release": "app@1.0", "timestamp": fmt.Sprintf("2026-01-01T00:00:%02dZ", i)})
			f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+f.project.PublicKey, body, nil, 200)
		}
		request := func(path string) ([]wireEvent, string) {
			t.Helper()
			req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			res, err := f.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, err := io.ReadAll(res.Body)
			if err != nil || res.StatusCode != 200 {
				t.Fatalf("event list status=%d err=%v body=%s", res.StatusCode, err, raw)
			}
			var result []wireEvent
			decode(raw, &result)
			return result, res.Header.Get("Link")
		}
		values := url.Values{"limit": {"100"}, "full": {"true"}, "sample": {"false"}, "query": {"release:app@1.0"}, "statsPeriod": {""}}
		page, links := request(issuePath + "?" + values.Encode())
		if len(page) != 10 || len(page[0].Entries) == 0 {
			t.Fatalf("full list effective cap/serializer: count=%d", len(page))
		}
		matches := regexp.MustCompile(`<([^>]+)>; rel="([^"]+)"; results="([^"]+)"; cursor="([^"]+)"`).FindAllStringSubmatch(links, -1)
		if len(matches) != 2 || matches[0][2] != "previous" || matches[0][3] != "false" || matches[0][4] != "0:0:1" || matches[1][2] != "next" || matches[1][3] != "true" || matches[1][4] != "0:10:0" {
			t.Fatalf("pagination Link contract=%s", links)
		}
		next, err := url.Parse(matches[1][1])
		if err != nil || next.Scheme != "https" || next.Host != "public.example.test" || next.Path != "/good"+issuePath {
			t.Fatalf("configured domain/prefix lost: %s err=%v", links, err)
		}
		want, err := url.ParseQuery(values.Encode())
		if err != nil {
			t.Fatal(err)
		}
		want.Set("cursor", "0:10:0")
		if next.RawQuery != want.Encode() {
			t.Fatalf("pagination changed supplied filters/limit alias: %s want=%s", next.RawQuery, want.Encode())
		}
		page, links = request(strings.TrimPrefix(next.RequestURI(), "/good"))
		if len(page) != 2 || !strings.Contains(links, `rel="previous"; results="true"; cursor="0:0:1"`) || !strings.Contains(links, `rel="next"; results="false"; cursor="0:20:0"`) {
			t.Fatalf("followed event page count=%d links=%s", len(page), links)
		}
		_, links = request(issuePath + "?limit=100&cursor=0:100000000:0")
		if !strings.Contains(links, `rel="next"; results="false"; cursor="0:100000000:0"`) {
			t.Fatalf("disabled boundary cursor exceeds admission range: %s", links)
		}
	})
}
