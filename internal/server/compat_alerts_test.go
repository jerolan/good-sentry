//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
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
