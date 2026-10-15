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

// Authoring gate: HTTP owns exact route registration, issued credential/session
// admission, projection and cursor Links. Wrong scopes, missing CSRF or a dropped
// empty202 response are transport failures that Store tests cannot exercise.
// The real fixture needs no production seam; Store owns reset/history atomicity.
func TestCompatibleIssueHashesHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/0/organizations/" + f.org.Slug + "/issues/" + f.issueID + "/hashes/"
	for _, method := range []string{"GET", "DELETE"} {
		f.request(t, http.DefaultClient, method, path, "", nil, 401)
	}
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("hash response JSON: %v body=%s", err, raw)
		}
	}
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	type credential struct{ ID, Token string }
	issueToken := func(scope string, projects []string) credential {
		t.Helper()
		var result credential
		body := resolutionJSON(t, map[string]any{"name": "Hash HTTP " + scope, "scopes": []string{scope}, "projectIds": projects})
		decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, csrf, 201), &result)
		if result.ID == "" || result.Token == "" {
			t.Fatal("issued credential is incomplete")
		}
		return result
	}
	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	writer := issueToken("event:write", []string{f.project.ID})
	reader := issueToken("event:read", []string{f.project.ID})
	const unknown = "ffffffffffffffffffffffffffffffff"
	t.Run("scopes protected session and authoritative Bearer", func(t *testing.T) {
		f.request(t, http.DefaultClient, "GET", path, "", bearer(reader.Token), 200)
		f.request(t, http.DefaultClient, "GET", path, "", bearer(writer.Token), 200)
		f.request(t, f.client, "GET", path, "", nil, 200)
		f.request(t, http.DefaultClient, "DELETE", path+"?id="+unknown, "", bearer(reader.Token), 403)
		other := issueToken("project:read", []string{f.project.ID})
		for _, method := range []string{"GET", "DELETE"} {
			f.request(t, http.DefaultClient, method, path+"?id="+unknown, "", bearer(other.Token), 403)
			h := bearer("invalid")
			h.Set("X-CSRF-Token", f.csrf)
			f.request(t, f.client, method, path+"?id="+unknown, "", h, 401)
		}
		f.request(t, f.client, "DELETE", path+"?id="+unknown, "", nil, 403)
		f.request(t, f.client, "DELETE", path+"?id="+unknown, "", http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, 403)
		if raw := f.request(t, f.client, "DELETE", path+"?id="+unknown, "", csrf, 202); len(raw) != 0 {
			t.Fatalf("session202 must have an empty body: %s", raw)
		}
	})
	t.Run("strict handler framing and exact routes", func(t *testing.T) {
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for _, method := range []string{"GET", "DELETE"} {
			f.request(t, noRedirect, method, strings.TrimSuffix(path, "/"), "", bearer(writer.Token), 307)
			for _, id := range []string{"0", "01", "9223372036854775808"} {
				f.request(t, http.DefaultClient, method, "/api/0/organizations/"+f.org.Slug+"/issues/"+id+"/hashes/", "", bearer(writer.Token), 400)
			}
		}
		f.request(t, http.DefaultClient, "GET", path+"unexpected/", "", bearer(writer.Token), 404)
		f.request(t, http.DefaultClient, "DELETE", path+"unexpected/", "", bearer(writer.Token), 405)
		for _, query := range []string{"full=%zz", "limit=2&limit=2", "id=" + unknown} {
			f.request(t, http.DefaultClient, "GET", path+"?"+query, "", bearer(writer.Token), 400)
		}
		for _, query := range []string{"", "id=invalid", "id=" + unknown + "&full=false"} {
			f.request(t, http.DefaultClient, "DELETE", path+"?"+query, "", bearer(writer.Token), 400)
		}
		f.request(t, http.DefaultClient, "DELETE", path+"?id="+unknown, "{}", bearer(writer.Token), 400)
	})
	type wireEvent struct {
		ID, EventID, GroupID string
		ProjectID            int64
		Entries              json.RawMessage
		PreviousEventID      json.RawMessage
		NextEventID          json.RawMessage
	}
	type wireHash struct {
		ID                string
		LatestEvent       *wireEvent
		MergedBySeer      *bool
		SeerMatchDistance json.RawMessage
	}
	var original []wireHash
	decode(f.request(t, http.DefaultClient, "GET", path, "", bearer(reader.Token), 200), &original)
	if len(original) != 1 || original[0].LatestEvent == nil {
		t.Fatalf("real initial hash/sample missing: %+v", original)
	}
	sample := original[0].LatestEvent
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(original[0].ID) || sample.ID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || sample.EventID != sample.ID || sample.GroupID != f.issueID || fmt.Sprint(sample.ProjectID) != f.project.ID || len(sample.Entries) == 0 || len(sample.PreviousEventID) != 0 || len(sample.NextEventID) != 0 || original[0].MergedBySeer == nil || *original[0].MergedBySeer || string(original[0].SeerMatchDistance) != "null" {
		t.Fatalf("actual full sample/Seer projection: hash=%+v sample=%+v", original[0], sample)
	}
	var alias string
	t.Run("default full cap and prefixed filter preserving Links", func(t *testing.T) {
		for i := 1; i <= 10; i++ {
			body := "{}\n{\"type\":\"event\"}\n" + resolutionJSON(t, map[string]any{"event_id": fmt.Sprintf("%032x", i), "message": fmt.Sprintf("Hash HTTP source %02d", i), "fingerprint": []string{fmt.Sprintf("Hash HTTP source %02d", i)}})
			f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+f.project.PublicKey, body, nil, 200)
		}
		var sources []struct{ ID string }
		collection := "/api/0/organizations/" + f.org.Slug + "/issues/"
		decode(f.request(t, http.DefaultClient, "GET", collection+"?query=Hash%20HTTP%20source", "", bearer(reader.Token), 200), &sources)
		if len(sources) != 10 {
			t.Fatalf("pagination requires ten distinct source issues: %+v", sources)
		}
		alias = sources[0].ID
		selection := url.Values{"id": {f.issueID}}
		for _, source := range sources {
			selection.Add("id", source.ID)
		}
		f.request(t, http.DefaultClient, "PUT", collection+"?"+selection.Encode(), `{"merge":true}`, bearer(writer.Token), 200)
		request := func(path string) ([]wireHash, string) {
			t.Helper()
			req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+reader.Token)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, err := io.ReadAll(res.Body)
			if err != nil || res.StatusCode != 200 {
				t.Fatalf("hash page status=%d error=%v body=%s", res.StatusCode, err, raw)
			}
			var list []wireHash
			decode(raw, &list)
			return list, res.Header.Get("Link")
		}
		query := url.Values{"limit": {"100"}, "full": {"1"}}
		page, links := request(path + "?" + query.Encode())
		matches := regexp.MustCompile(`<([^>]+)>; rel="([^"]+)"; results="([^"]+)"; cursor="([^"]+)"`).FindAllStringSubmatch(links, -1)
		if len(page) != 10 || len(matches) != 2 || matches[0][3] != "false" || matches[1][3] != "true" || matches[1][4] != "0:10:0" {
			t.Fatalf("full cap/lookahead page=%d links=%s", len(page), links)
		}
		next, err := url.Parse(matches[1][1])
		if err != nil || next.Scheme != "https" || next.Host != "public.example.test" || next.Path != "/good"+path {
			t.Fatalf("public hash Link prefix=%s error=%v", links, err)
		}
		query.Set("cursor", "0:10:0")
		if next.RawQuery != query.Encode() {
			t.Fatalf("hash Link lost supplied limit/full spelling: %s want=%s", next.RawQuery, query.Encode())
		}
		tail, tailLinks := request(strings.TrimPrefix(next.RequestURI(), "/good"))
		if len(tail) != 1 || !strings.Contains(tailLinks, `rel="previous"; results="true"; cursor="0:0:1"`) || !strings.Contains(tailLinks, `rel="next"; results="false"`) {
			t.Fatalf("followed hash page=%d links=%s", len(tail), tailLinks)
		}
		all, _ := request(path + "?full=0")
		if len(all) != 11 {
			t.Fatalf("summary should retain default100 rather than full cap: %d", len(all))
		}
		for _, hash := range all {
			if hash.LatestEvent == nil || len(hash.LatestEvent.Entries) != 0 || hash.LatestEvent.GroupID != f.issueID {
				t.Fatalf("summary projector dispatch/current ownership: %+v", hash)
			}
		}
	})
	t.Run("aliases preserve mergedInto and reset acknowledges actual commit", func(t *testing.T) {
		if alias == "" {
			t.Fatal("merged alias fixture was not established")
		}
		aliasPath := "/api/0/organizations/" + f.org.Slug + "/issues/" + alias + "/hashes/"
		f.request(t, http.DefaultClient, "GET", aliasPath, "", bearer(reader.Token), 200)
		var conflict struct{ MergedInto string }
		decode(f.request(t, http.DefaultClient, "DELETE", aliasPath+"?id="+original[0].ID, "", bearer(writer.Token), 409), &conflict)
		if conflict.MergedInto != f.issueID {
			t.Fatalf("alias lost current root: %+v", conflict)
		}
		for _, headers := range []http.Header{bearer(writer.Token), csrf} {
			client := http.DefaultClient
			if headers.Get("Authorization") == "" {
				client = f.client
			}
			if raw := f.request(t, client, "DELETE", path+"?id="+original[0].ID+"&id="+unknown, "", headers, 202); len(raw) != 0 {
				t.Fatalf("committed202 must have an empty body: %s", raw)
			}
		}
		var current []wireHash
		decode(f.request(t, http.DefaultClient, "GET", path+"?full=false", "", bearer(reader.Token), 200), &current)
		if len(current) != 10 {
			t.Fatalf("202 did not reflect the committed reset: routes=%d", len(current))
		}
		for _, hash := range current {
			if hash.ID == original[0].ID {
				t.Fatal("acknowledged reset route is still active")
			}
		}
	})
	t.Run("hidden grants and revocation remain authoritative", func(t *testing.T) {
		var hidden struct{ ID string }
		decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/projects", `{"name":"Hidden hash project","platform":"go"}`, csrf, 201), &hidden)
		limited := issueToken("event:write", []string{hidden.ID})
		for _, method := range []string{"GET", "DELETE"} {
			query := ""
			if method == "DELETE" {
				query = "?id=" + unknown
			}
			f.request(t, http.DefaultClient, method, path+query, "", bearer(limited.Token), 404)
			f.request(t, http.DefaultClient, method, "/api/0/organizations/foreign-hash-org/issues/"+f.issueID+"/hashes/"+query, "", bearer(writer.Token), 404)
		}
		f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+writer.ID, "", csrf, 204)
		for _, method := range []string{"GET", "DELETE"} {
			h := bearer(writer.Token)
			h.Set("X-CSRF-Token", f.csrf)
			f.request(t, f.client, method, path, "", h, 401)
		}
	})
}
