//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestCompatibleProjectAlertsContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	base := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/alerts/"
	var created struct {
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		TimespanMinutes int    `json:"timespanMinutes"`
		Quantity        int    `json:"quantity"`
		Uptime          bool   `json:"uptime"`
		Recipients      []struct {
			RecipientType string  `json:"recipientType"`
			URL           *string `json:"url"`
		} `json:"alertRecipients"`
	}
	body := `{"name":"Compatible alert","environments":["production"],"timespanMinutes":5,"quantity":2,"uptime":false,"alertRecipients":[{"recipientType":"email"},{"recipientType":"webhook","url":"https://hooks.example.test/alerts"}]}`
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, body, csrf, 201), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "Compatible alert" || created.TimespanMinutes != 5 || created.Quantity != 2 || created.Uptime || len(created.Recipients) != 2 {
		t.Fatalf("created alert=%+v", created)
	}
	var list []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", base+"?limit=1", "", nil, 200), &list); err != nil || len(list) != 1 {
		t.Fatalf("alert list=%+v err=%v", list, err)
	}
	detail := base + strconv.FormatInt(created.ID, 10) + "/"
	var updated map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "PUT", detail, `{"name":"Renamed alert","environments":["staging"]}`, csrf, 200), &updated); err != nil || updated["name"] != "Renamed alert" {
		t.Fatalf("updated alert=%+v err=%v", updated, err)
	}
	var tests []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "POST", detail+"test/", "", csrf, 200), &tests); err != nil || len(tests) != 2 {
		t.Fatalf("test result=%+v err=%v", tests, err)
	}
	f.request(t, f.client, "POST", base, `{"alertRecipients":[{"recipientType":"zulip","url":"https://zulip.example.test/hook","botEmail":"bot@example.test","apiKey":"secret","channel":"alerts"}]}`, csrf, 400)
	f.request(t, f.client, "DELETE", detail, "", csrf, 204)
	if err := json.Unmarshal(f.request(t, f.client, "GET", base, "", nil, 200), &list); err != nil || len(list) != 0 {
		t.Fatalf("deleted alerts=%+v err=%v", list, err)
	}
}

func TestCompatibleProjectAlertsPaginationValidationAndTestQueue(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	base := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/alerts/"
	body := `{"name":"Paged alert","environments":["production"],"timespanMinutes":5,"quantity":2,"uptime":false,"alertRecipients":[{"recipientType":"email"},{"recipientType":"webhook","url":"https://hooks.example.test/alerts"}]}`
	var first, second struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, body, csrf, 201), &first); err != nil || first.ID == 0 {
		t.Fatalf("first alert=%+v err=%v", first, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, strings.Replace(body, "Paged alert", "Second alert", 1), csrf, 201), &second); err != nil || second.ID == 0 {
		t.Fatalf("second alert=%+v err=%v", second, err)
	}

	req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+f.prefix()+base+"?per_page=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var page []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || len(page) != 1 {
		t.Fatalf("first page status=%d body=%+v", res.StatusCode, page)
	}
	link := res.Header.Get("Link")
	if !strings.Contains(link, `rel="next"`) || !strings.Contains(link, `results="true"`) {
		t.Fatalf("missing next link: %q", link)
	}
	start, end := strings.Index(link, "<"), strings.Index(link, ">")
	if start < 0 || end <= start {
		t.Fatalf("malformed link: %q", link)
	}
	target, err := url.Parse(link[start+1 : end])
	if err != nil {
		t.Fatal(err)
	}
	nextRequest, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+target.RequestURI(), nil)
	if err != nil {
		t.Fatal(err)
	}
	nextResponse, err := f.client.Do(nextRequest)
	if err != nil {
		t.Fatal(err)
	}
	var next []map[string]any
	if err := json.NewDecoder(nextResponse.Body).Decode(&next); err != nil {
		t.Fatal(err)
	}
	nextResponse.Body.Close()
	if nextResponse.StatusCode != 200 || len(next) != 1 || strings.Contains(nextResponse.Header.Get("Link"), `rel="next"`) {
		t.Fatalf("second page status=%d body=%+v link=%q", nextResponse.StatusCode, next, nextResponse.Header.Get("Link"))
	}

	f.request(t, f.client, "GET", base+"?cursor=not-a-cursor", "", nil, 400)
	f.request(t, f.client, "GET", base+"?unknown=1", "", nil, 400)
	f.request(t, f.client, "POST", base, `{"alertRecipients":[{"recipientType":"webhook","url":"https://hooks.example.test/alerts","tagsToAdd":["x"]}]}`, csrf, 400)
	f.request(t, f.client, "POST", base, `{"alertRecipients":[{"recipientType":"email","url":"mailto:team@example.test"}]}`, csrf, 400)
	f.request(t, f.client, "POST", base, `{"alertRecipients":[{"recipientType":"webhook"}]}`, csrf, 400)
	f.request(t, f.client, "POST", base, `{"alertRecipients":[{"recipientType":"carrier-pigeon"}]}`, csrf, 400)

	detail := base + strconv.FormatInt(first.ID, 10) + "/"
	var updated struct {
		TimespanMinutes int      `json:"timespanMinutes"`
		Quantity        int      `json:"quantity"`
		Environments    []string `json:"environments"`
		Uptime          bool     `json:"uptime"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", detail, `{"name":"Only renamed"}`, csrf, 200), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.TimespanMinutes != 5 || updated.Quantity != 2 || updated.Uptime || len(updated.Environments) != 1 || updated.Environments[0] != "production" {
		t.Fatalf("omitted fields were not preserved: %+v", updated)
	}
	f.request(t, f.client, "PUT", detail, `{"uptime":true}`, csrf, 409)

	var tests []struct {
		RecipientType string  `json:"recipientType"`
		Status        string  `json:"status"`
		Message       *string `json:"message"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", detail+"test/", "", csrf, 200), &tests); err != nil || len(tests) != 2 {
		t.Fatalf("test queue=%+v err=%v", tests, err)
	}
	for _, entry := range tests {
		if entry.RecipientType == "" || entry.Status != "queued" || entry.Message != nil {
			t.Fatalf("test entry=%+v", entry)
		}
	}
}

func TestCompatibleProjectAlertsTokenScopes(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	base := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/alerts/"
	body := `{"name":"Scoped alert","environments":["production"],"timespanMinutes":5,"quantity":2,"uptime":false,"alertRecipients":[{"recipientType":"email"}]}`
	reader := issueManagementToken(t, f, f.org.Slug, "Alert reader", []string{"project:read"}, []string{f.project.ID})
	writer := issueManagementToken(t, f, f.org.Slug, "Alert writer", []string{"project:write"}, []string{f.project.ID})
	wrong := issueManagementToken(t, f, f.org.Slug, "Alert wrong scope", []string{"event:read"}, []string{f.project.ID})
	auth := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	f.request(t, f.client, "GET", base, "", auth(reader), 200)
	f.request(t, f.client, "POST", base, body, auth(reader), 403)
	f.request(t, f.client, "GET", base, "", auth(wrong), 403)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", base, body, auth(writer), 201), &created); err != nil || created.ID == 0 {
		t.Fatalf("writer create=%+v err=%v", created, err)
	}
	f.request(t, f.client, "GET", base, "", auth(writer), 403)
	detail := base + strconv.FormatInt(created.ID, 10) + "/"
	f.request(t, f.client, "DELETE", detail, "", auth(reader), 403)
	f.request(t, f.client, "DELETE", detail, "", csrf, 204)
}
