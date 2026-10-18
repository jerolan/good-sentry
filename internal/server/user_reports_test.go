//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

// HTTP owns exact route admission, real DTO/count dispatch and prefixed paging.
// SQL ownership tests cannot detect a wrong scope, missing route, placeholder
// serializer or permissive query. Reuse the real session/PostgreSQL fixture;
// report seeding here is a provider call, not an SDK-ingestion claim.
func TestUserReportReadHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	const eventID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	input := store.UserReportInput{EventID: eventID, Name: "<script>name</script>", Email: "person@example.test", Comments: "Feedback with\nactual details"}
	report, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, store.UserReportInput{EventID: strings.Repeat("b", 32), Comments: "Pending project report"}); err != nil {
		t.Fatal(err)
	}
	project := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/user-feedback/"
	issue := "/api/0/issues/" + f.issueID + "/user-reports/"
	orgIssue := "/api/0/organizations/" + f.org.Slug + "/issues/" + f.issueID + "/user-reports/"
	native := "/api/v1/issues/" + f.issueID + "/user-reports"
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("report JSON: %v body=%s", err, raw)
		}
	}
	for _, path := range []string{project, issue, orgIssue, native} {
		f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	}
	for _, path := range []string{issue, orgIssue} {
		var reports []store.UserReport
		decode(f.request(t, f.client, "GET", path, "", nil, 200), &reports)
		if len(reports) != 1 || reports[0].ID != report.ID || reports[0].Name != input.Name || reports[0].Comments != input.Comments || reports[0].Event.ID != nil || reports[0].Event.EventID != eventID {
			t.Fatal("report route projected a placeholder or different data", reports)
		}
	}
	var page struct {
		Data           []store.UserReport
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode(f.request(t, f.client, "GET", native, "", nil, 200), &page)
	if len(page.Data) != 1 || page.Total != 1 || page.Page != 1 || page.PageSize != 25 || page.HasMore {
		t.Fatal("native report page contract", page)
	}
	if raw := f.request(t, f.client, "GET", native+"?page=2", "", nil, 200); !strings.Contains(string(raw), `"data":[]`) {
		t.Fatal("empty native report page must be an array", string(raw))
	}
	if raw := f.request(t, f.client, "GET", issue+"?cursor=0:100:0", "", nil, 200); strings.TrimSpace(string(raw)) != "[]" {
		t.Fatal("empty compatible report page must be an array", string(raw))
	}
	var detail struct{ UserReportCount int64 }
	decode(f.request(t, f.client, "GET", "/api/0/issues/"+f.issueID+"/", "", nil, 200), &detail)
	if detail.UserReportCount != 1 {
		t.Fatal("compatible detail still returns zero reports", detail)
	}
	var event struct{ UserReport *store.UserReport }
	decode(f.request(t, f.client, "GET", "/api/0/issues/"+f.issueID+"/events/"+eventID+"/", "", nil, 200), &event)
	if event.UserReport == nil || event.UserReport.ID != report.ID {
		t.Fatal("compatible full event still returns null report", event)
	}
	var events []struct{ UserReport *store.UserReport }
	decode(f.request(t, f.client, "GET", "/api/0/issues/"+f.issueID+"/events/?full=1", "", nil, 200), &events)
	if len(events) != 1 || events[0].UserReport == nil || events[0].UserReport.ID != report.ID {
		t.Fatal("full event list dropped its report", events)
	}
	for _, query := range []string{"unknown=1", "limit=01", "limit=0", "limit=101", "limit=1&per_page=1", "cursor=broken", "cursor=0:01:0", "cursor=0:0:2", "cursor=x&cursor=x", "limit=1&limit=1", "cursor=%ZZ"} {
		f.request(t, f.client, "GET", project+"?"+query, "", nil, 400)
	}
	for _, query := range []string{"page=0", "page=01", "page=1000001", "page=1&page=1", "limit=1"} {
		f.request(t, f.client, "GET", native+"?"+query, "", nil, 400)
	}
	for _, path := range []string{project + "extra/", issue + "extra/", orgIssue + "extra/"} {
		f.request(t, f.client, "GET", path, "", nil, 404)
	}
	issueToken := func(scope string) string {
		t.Helper()
		var credential struct{ Token string }
		body := resolutionJSON(t, map[string]any{"name": "Report " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
		decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &credential)
		return credential.Token
	}
	projectToken, eventToken := issueToken("project:read"), issueToken("event:read")
	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	f.request(t, http.DefaultClient, "GET", project, "", bearer(projectToken), 200)
	f.request(t, http.DefaultClient, "GET", issue, "", bearer(projectToken), 403)
	f.request(t, http.DefaultClient, "GET", issue, "", bearer(eventToken), 200)
	f.request(t, http.DefaultClient, "GET", orgIssue, "", bearer(eventToken), 200)
	f.request(t, http.DefaultClient, "GET", project, "", bearer(eventToken), 403)
	f.request(t, http.DefaultClient, "GET", native, "", bearer(eventToken), 401)
	f.request(t, f.client, "GET", issue, "", bearer("invalid"), 401)
	// The first page is the pending report; its Link must carry the public
	// deployment prefix and the caller's size, never the internal host.
	req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good"+project+"?limit=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || !strings.Contains(res.Header.Get("Link"), `https://public.example.test/good`+project+`?cursor=0%3A1%3A0&limit=1`) || !strings.Contains(res.Header.Get("Link"), `rel="next"; results="true"`) {
		t.Fatal("report pagination lost prefix/query", res.StatusCode, res.Header, err)
	}
	var reports []store.UserReport
	decode(raw, &reports)
	if len(reports) != 1 || reports[0].EventID != strings.Repeat("b", 32) {
		t.Fatal("project report ordering/pending visibility", reports)
	}
}
