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

// Authoring gate: transport owns five exact routes, authoritative event:read
// admission, differential query defaults and filter-preserving prefixed Links.
// Missing routes, a project:read substitution, or silently accepted parameters
// are credible regressions outside SQL/pure projection coverage. Reuse real
// PostgreSQL/session requests, with no productive seams or provider matrix.
func TestCompatibleIssuesHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	orgPath := "/api/0/organizations/" + f.org.Slug + "/issues/"
	projectPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/issues/"
	globalPath := "/api/0/issues/" + f.issueID + "/"
	detailPath := orgPath + f.issueID + "/"
	statsPath := "/api/0/organizations/" + f.org.Slug + "/issues-stats/"
	// A missing route must fail here, before a supplied credential is involved.
	f.request(t, http.DefaultClient, "GET", orgPath, "", nil, 401)
	for _, path := range []string{projectPath, globalPath, detailPath, statsPath + "?groups=" + f.issueID} {
		f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	}
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("compatible issue JSON: %v body=%s", err, raw)
		}
	}
	var issue struct {
		ID, Count, Permalink string
		UserReportCount      *int64
	}
	decode(f.request(t, f.client, "GET", detailPath, "", nil, 200), &issue)
	if issue.ID != f.issueID || issue.Count != "1" || issue.UserReportCount == nil || *issue.UserReportCount != 0 || issue.Permalink != "https://public.example.test/good/?issue="+f.issueID+"&org="+f.org.Slug {
		t.Fatalf("issue detail wire=%+v", issue)
	}
	for _, path := range []string{globalPath, statsPath + "?groups=" + f.issueID, projectPath} {
		f.request(t, f.client, "GET", path, "", nil, 200)
	}
	var stats []struct{ ID, Count string }
	decode(f.request(t, f.client, "GET", statsPath+"?groups="+f.issueID, "", nil, 200), &stats)
	if len(stats) != 1 || stats[0].ID != f.issueID || stats[0].Count != "1" {
		t.Fatalf("issue-stats must expose actual string count: %+v", stats)
	}
	for _, path := range []string{globalPath + "unexpected/", detailPath + "unexpected/", projectPath + f.issueID + "/"} {
		f.request(t, f.client, "GET", path, "", nil, 404)
	}
	t.Run("event scope on every route and authoritative Bearer", func(t *testing.T) {
		issueToken := func(scope string) struct{ ID, Token string } {
			t.Helper()
			var result struct{ ID, Token string }
			body := resolutionJSON(t, map[string]any{"name": "Issue transport " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
			decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &result)
			return result
		}
		reader, projectReader := issueToken("event:read"), issueToken("project:read")
		header := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
		for _, path := range []string{orgPath, projectPath, globalPath, detailPath, statsPath + "?groups=" + f.issueID} {
			f.request(t, http.DefaultClient, "GET", path, "", header(reader.Token), 200)
			f.request(t, http.DefaultClient, "GET", path, "", header(projectReader.Token), 403)
		}
		f.request(t, f.client, "GET", globalPath, "", header("invalid"), 401)
		f.request(t, http.DefaultClient, "GET", "/api/v1/issues/"+f.issueID, "", header(reader.Token), 401)
		f.request(t, http.DefaultClient, "GET", "/api/0/organizations/foreign-issue-org/issues/"+f.issueID+"/", "", header(reader.Token), 404)
		f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+reader.ID, "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
		f.request(t, f.client, "GET", globalPath, "", header(reader.Token), 401)
	})
	t.Run("strict route-specific query parsing", func(t *testing.T) {
		for _, query := range []string{"unknown=x", "query=%zz", "query=x&query=y", "id=01", "id=0", "id=9223372036854775808", "project=-1&project=other", "project=", "environment=%FF", "environment=%00", "environment=" + url.QueryEscape(strings.Repeat("é", 201)), "sort=trends", "sort=inbox", "sort=recommended", "hashes=abc", "expand=owners", "collapse=stats", "viewId=1", "limit=", "per_page=01", "limit=101", "limit=1&per_page=2", "shortIdLookup=yes", "cursor=0:01:0", "cursor=0:0:2", "start=", "start=2026-02-30T00:00:00Z", "start=2026-02-02T00:00:00Z&end=2026-02-01T00:00:00Z&statsPeriod=1h", "statsPeriod=0h", "groupStatsPeriod=1h"} {
			f.request(t, f.client, "GET", orgPath+"?"+query, "", nil, 400)
		}
		for _, query := range []string{"?query=", "?shortIdLookup=true", "?shortIdLookup=false", "?groupStatsPeriod=auto&start=2026-01-01T00:00:00&end=2026-01-02T00:00:00Z", "?statsPeriod=1h&groupStatsPeriod=", "?project=-1", "?environment="} {
			f.request(t, f.client, "GET", orgPath+query, "", nil, 200)
		}
		f.request(t, f.client, "GET", projectPath+"?statsPeriod=1h", "", nil, 400)
		for _, query := range []string{"?query=", "?start=2026-01-01T00:00:00Z", "?statsPeriod=1h", "?statsPeriod=24h&statsPeriod=14d"} {
			f.request(t, f.client, "GET", globalPath+query, "", nil, 400)
		}
		for _, query := range []string{"", "?groups=01", "?groups=" + f.issueID + "&query=", "?groups=" + f.issueID + "&statsPeriod=", "?groups=" + f.issueID + "&statsPeriod=1h"} {
			f.request(t, f.client, "GET", statsPath+query, "", nil, 400)
		}
	})
	t.Run("wire defaults are distinct from explicit empty values", func(t *testing.T) {
		var list []struct {
			ID    string
			Stats map[string]json.RawMessage
		}
		for _, path := range []string{orgPath, projectPath} {
			list = nil
			decode(f.request(t, f.client, "GET", path, "", nil, 200), &list)
			if len(list) != 1 || list[0].Stats["24h"] == nil {
				t.Fatalf("default issue query/chart=%s result=%+v", path, list)
			}
		}
		list = nil
		decode(f.request(t, f.client, "GET", projectPath+"?statsPeriod=", "", nil, 200), &list)
		if len(list) != 1 || len(list[0].Stats) != 0 {
			t.Fatalf("empty project statsPeriod must disable series: %+v", list)
		}
		list = nil
		decode(f.request(t, f.client, "GET", orgPath+"?statsPeriod=", "", nil, 200), &list)
		if len(list) != 1 || list[0].Stats["24h"] == nil {
			t.Fatalf("empty org relative period must retain default series: %+v", list)
		}
		f.request(t, f.client, "PATCH", "/api/v1/issues/"+f.issueID, `{"status":"resolved"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
		list = nil
		decode(f.request(t, f.client, "GET", orgPath, "", nil, 200), &list)
		if len(list) != 0 {
			t.Fatalf("omitted query must request unresolved: %+v", list)
		}
		list = nil
		decode(f.request(t, f.client, "GET", orgPath+"?query=", "", nil, 200), &list)
		if len(list) != 1 {
			t.Fatalf("explicit empty query must include resolved: %+v", list)
		}
	})
	t.Run("prefixed Links preserve filters and follow cursor", func(t *testing.T) {
		for i := 1; i <= 2; i++ {
			body := "{}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{"event_id": fmt.Sprintf("%032x", i), "message": fmt.Sprintf("Issue HTTP page %d", i)})
			f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+f.project.PublicKey, body, nil, 200)
		}
		request := func(path string) ([]json.RawMessage, string) {
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
				t.Fatalf("issue page status=%d err=%v body=%s", res.StatusCode, err, raw)
			}
			var list []json.RawMessage
			decode(raw, &list)
			return list, res.Header.Get("Link")
		}
		q := url.Values{"limit": {"1"}, "query": {""}, "project": {f.project.ID}, "groupStatsPeriod": {""}, "sort": {"date"}}
		list, links := request(orgPath + "?" + q.Encode())
		matches := regexp.MustCompile(`<([^>]+)>; rel="([^"]+)"; results="([^"]+)"; cursor="([^"]+)"`).FindAllStringSubmatch(links, -1)
		if len(list) != 1 || len(matches) != 2 || matches[0][3] != "false" || matches[0][4] != "0:0:1" || matches[1][3] != "true" || matches[1][4] != "0:1:0" {
			t.Fatalf("issue Link lookahead=%s size=%d", links, len(list))
		}
		next, err := url.Parse(matches[1][1])
		if err != nil || next.Scheme != "https" || next.Host != "public.example.test" || next.Path != "/good"+orgPath {
			t.Fatalf("issue public prefix/domain=%s err=%v", links, err)
		}
		q.Set("cursor", "0:1:0")
		if next.RawQuery != q.Encode() {
			t.Fatalf("issue Link dropped selectors/limit spelling: %s want=%s", next.RawQuery, q.Encode())
		}
		list, links = request(strings.TrimPrefix(next.RequestURI(), "/good"))
		if len(list) != 1 || !strings.Contains(links, `rel="previous"; results="true"; cursor="0:0:1"`) {
			t.Fatalf("followed issue page size=%d links=%s", len(list), links)
		}
	})
}
