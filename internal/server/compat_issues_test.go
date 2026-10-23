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
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/store"
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

// Authoring gate: this route is a distinct public contract with a wrapper
// shape and path identity that list-query coverage cannot reach. A real merged
// alias and resolved root catch loss of alias canonicalization or an accidental
// unresolved-only default; invalid, hidden and scoped-token requests exercise
// transport behavior. Existing store tests own the SQL grant/alias matrix, so
// this test adds no duplicated provider assertions or production seam.
func TestCompatibleShortIDLookupHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	aliasEvent, err := event.Normalize(json.RawMessage(fmt.Sprintf(`{"event_id":"%032x","message":"Short ID alias"}`, 2)), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	aliasID, _, err := f.db.Ingest(f.ctx, f.project.ID, aliasEvent)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{f.issueID, aliasID})
	if err != nil || !merged.Changed || merged.Issue.ID != f.issueID {
		t.Fatalf("merge fixture root=%+v err=%v", merged, err)
	}
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, f.issueID, store.IssueUpdate{Status: "resolved"}); err != nil {
		t.Fatal(err)
	}

	path := "/api/0/organizations/" + f.org.Slug + "/shortids/"
	lookup := path + strings.ToLower(f.project.Slug) + "-" + aliasID + "/"
	f.request(t, http.DefaultClient, "GET", lookup, "", nil, 401)
	for _, shortID := range []string{"bad", f.project.Slug + "-0", f.project.Slug + "-01", f.project.Slug + "-9223372036854775808", "bad_slug-1"} {
		f.request(t, f.client, "GET", path+shortID+"/", "", nil, 400)
	}
	f.request(t, f.client, "GET", lookup+"?unknown=value", "", nil, 400)
	f.request(t, f.client, "GET", path+f.project.Slug+"-9223372036854775807/", "", nil, 404)
	f.request(t, f.client, "GET", strings.Replace(lookup, f.org.Slug, "foreign-org", 1), "", nil, 404)

	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("short-ID lookup JSON: %v body=%s", err, raw)
		}
	}
	var result struct {
		OrganizationSlug string `json:"organizationSlug"`
		ProjectSlug      string `json:"projectSlug"`
		GroupID          string `json:"groupId"`
		ShortID          string `json:"shortId"`
		Group            struct {
			ID      string `json:"id"`
			ShortID string `json:"shortId"`
			Status  string `json:"status"`
			Project struct {
				Slug string `json:"slug"`
			} `json:"project"`
		} `json:"group"`
	}
	decode(f.request(t, f.client, "GET", lookup, "", nil, 200), &result)
	canonicalShortID := strings.ToUpper(f.project.Slug) + "-" + f.issueID
	if result.OrganizationSlug != f.org.Slug || result.ProjectSlug != f.project.Slug || result.GroupID != f.issueID || result.ShortID != canonicalShortID ||
		result.Group.ID != f.issueID || result.Group.ShortID != canonicalShortID || result.Group.Status != "resolved" || result.Group.Project.Slug != f.project.Slug {
		t.Fatalf("short-ID lookup failed to return canonical resolved root: %+v", result)
	}

	issueToken := func(scope string) string {
		t.Helper()
		var token struct {
			Token string `json:"token"`
		}
		body := resolutionJSON(t, map[string]any{"name": "Short-ID " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
		if raw := f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201); json.Unmarshal(raw, &token) != nil || token.Token == "" {
			t.Fatal("API token response missing credential")
		}
		return token.Token
	}
	readToken, projectToken := issueToken("event:read"), issueToken("project:read")
	f.request(t, http.DefaultClient, "GET", lookup, "", http.Header{"Authorization": {"Bearer " + readToken}}, 200)
	f.request(t, http.DefaultClient, "GET", lookup, "", http.Header{"Authorization": {"Bearer " + projectToken}}, 403)
	f.request(t, http.DefaultClient, "GET", lookup, "", http.Header{"Authorization": {"Bearer invalid"}}, 401)
}

// Authoring gate: the two issue commit paths, their five-field array shape,
// event:read admission and strict query behavior are independent public HTTP
// contracts. The store owner covers first-release selection/order; this real
// HTTP test catches route, scope and projection regressions those SQL checks
// cannot reach. It uses persisted release metadata and no production seam.
func TestCompatibleIssueCommitsHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	firstAt := time.Date(2024, 1, 2, 3, 4, 5, 123000000, time.UTC)
	secondAt := firstAt.Add(time.Minute)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	_, err := f.db.ReplaceReleaseCommits(f.ctx, access, "app@1.0", []store.ReleaseCommitInput{
		{ID: "first-commit", Message: "First commit", AuthorName: "Ada", AuthorEmail: "ada@example.invalid", Timestamp: &firstAt, PatchSet: []store.CommitPatch{{Path: "private.js", Type: "M"}}, Data: json.RawMessage(`{"id":"first-commit","extra":"private"}`)},
		{ID: "second-commit", Message: "Second commit", AuthorName: "Lin", AuthorEmail: "lin@example.invalid", Timestamp: &secondAt, Data: json.RawMessage(`{"id":"second-commit"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	aliasEvent, err := event.Normalize(json.RawMessage(`{"event_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","message":"Commit alias"}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	aliasID, _, err := f.db.Ingest(f.ctx, f.project.ID, aliasEvent)
	if err != nil {
		t.Fatal(err)
	}
	if merged, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{f.issueID, aliasID}); err != nil || merged.Issue.ID != f.issueID {
		t.Fatalf("commit issue merge=%+v err=%v", merged, err)
	}
	global := "/api/0/issues/" + f.issueID + "/commits/"
	org := "/api/0/organizations/" + f.org.Slug + "/issues/"
	for _, path := range []string{global, org + f.issueID + "/commits/", org + aliasID + "/commits/"} {
		f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	}
	for _, path := range []string{"/api/0/issues/01/commits/", "/api/0/organizations/" + f.org.Slug + "/issues/0/commits/", org + "9223372036854775808/commits/"} {
		f.request(t, f.client, "GET", path, "", nil, 400)
	}
	f.request(t, f.client, "GET", global+"?unknown=1", "", nil, 400)
	f.request(t, f.client, "GET", global+"?unknown=1&unknown=2", "", nil, 400)
	f.request(t, f.client, "GET", "/api/0/issues/9223372036854775807/commits/", "", nil, 404)
	f.request(t, f.client, "GET", "/api/0/organizations/foreign-org/issues/"+f.issueID+"/commits/", "", nil, 404)

	decode := func(raw []byte) []map[string]json.RawMessage {
		t.Helper()
		var result []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("issue commits JSON: %v body=%s", err, raw)
		}
		return result
	}
	for _, path := range []string{global, org + f.issueID + "/commits/", org + aliasID + "/commits/"} {
		commits := decode(f.request(t, f.client, "GET", path, "", nil, 200))
		if len(commits) != 2 {
			t.Fatalf("first-release commits=%v", commits)
		}
		for _, commit := range commits {
			if len(commit) != 5 {
				t.Fatalf("issue commit exposed fields outside its public projection: %v", commit)
			}
		}
		var id, message, author, email string
		var date time.Time
		if json.Unmarshal(commits[0]["id"], &id) != nil || id != "first-commit" || json.Unmarshal(commits[0]["message"], &message) != nil || message != "First commit" ||
			json.Unmarshal(commits[0]["authorName"], &author) != nil || author != "Ada" || json.Unmarshal(commits[0]["authorEmail"], &email) != nil || email != "ada@example.invalid" ||
			json.Unmarshal(commits[0]["dateCreated"], &date) != nil || !date.Equal(firstAt) {
			t.Fatalf("first issue commit=%v", commits[0])
		}
		if json.Unmarshal(commits[1]["id"], &id) != nil || id != "second-commit" {
			t.Fatalf("issue commit order=%v", commits)
		}
	}
	noReleaseEvent, err := event.Normalize(json.RawMessage(`{"event_id":"cccccccccccccccccccccccccccccccc","message":"No release issue"}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	noReleaseID, _, err := f.db.Ingest(f.ctx, f.project.ID, noReleaseEvent)
	if err != nil {
		t.Fatal(err)
	}
	if raw := f.request(t, f.client, "GET", "/api/0/issues/"+noReleaseID+"/commits/", "", nil, 200); strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("issue with no observed release commits=%s", raw)
	}
	noCommitsEvent, err := event.Normalize(json.RawMessage(`{"event_id":"dddddddddddddddddddddddddddddddd","message":"Release without commits","release":"app@uncommitted"}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	noCommitsID, _, err := f.db.Ingest(f.ctx, f.project.ID, noCommitsEvent)
	if err != nil {
		t.Fatal(err)
	}
	if raw := f.request(t, f.client, "GET", "/api/0/issues/"+noCommitsID+"/commits/", "", nil, 200); strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("issue with a release without commits=%s", raw)
	}

	var token struct {
		Token string `json:"token"`
	}
	body := resolutionJSON(t, map[string]any{"name": "Issue commits read", "scopes": []string{"event:read"}, "projectIds": []string{f.project.ID}})
	if raw := f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201); json.Unmarshal(raw, &token) != nil || token.Token == "" {
		t.Fatal("event:read token response missing credential")
	}
	read := http.Header{"Authorization": {"Bearer " + token.Token}}
	for _, path := range []string{global, org + f.issueID + "/commits/"} {
		f.request(t, http.DefaultClient, "GET", path, "", read, 200)
	}
	projectTokenBody := resolutionJSON(t, map[string]any{"name": "Issue commits wrong scope", "scopes": []string{"project:read"}, "projectIds": []string{f.project.ID}})
	var projectToken struct {
		Token string `json:"token"`
	}
	if raw := f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", projectTokenBody, http.Header{"X-CSRF-Token": {f.csrf}}, 201); json.Unmarshal(raw, &projectToken) != nil || projectToken.Token == "" {
		t.Fatal("project:read token response missing credential")
	}
	f.request(t, http.DefaultClient, "GET", global, "", http.Header{"Authorization": {"Bearer " + projectToken.Token}}, 403)
}
