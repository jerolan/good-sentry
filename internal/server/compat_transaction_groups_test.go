//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCompatibleTransactionGroupsDetailTrendAndSpansHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Transaction groups other", "go")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	for index := 0; index < 5; index++ {
		mcpPerformanceTransaction(t, f, f.project.ID, 600+index, 800+index, "GET /compat/groups", "http.server", base.Add(time.Duration(index)*time.Minute), 100, 6)
	}
	mcpPerformanceTransaction(t, f, f.project.ID, 610, 810, "GET /compat/groups", "background", base.Add(10*time.Minute), 50, 2)
	mcpPerformanceTransaction(t, f, other.ID, 611, 811, "GET /compat/groups", "http.server", base.Add(11*time.Minute), 100, 7)
	start, end := base.Add(-time.Minute).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339)
	collection := "/api/0/organizations/" + f.org.Slug + "/transaction-groups/?start=" + start + "&end=" + end + "&sort=-count&limit=25"
	var groups []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", collection, "", nil, 200), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || groups[0]["transaction"] != "GET /compat/groups" || groups[0]["op"] != "http.server" || groups[0]["project"] != float64(1) || groups[0]["count"] != float64(5) {
		t.Fatalf("transaction groups=%+v", groups)
	}
	for _, key := range []string{"project", "id", "transaction", "op", "method", "count", "avgDuration", "p50", "p95", "errorCount", "firstSeen", "lastSeen", "errorRate", "throughput"} {
		if _, ok := groups[0][key]; !ok {
			t.Fatalf("transaction group omitted %s: %+v", key, groups[0])
		}
	}
	groupID := int64(groups[0]["id"].(float64))
	var secondID int64
	otherID, _ := strconv.ParseInt(other.ID, 10, 64)
	for _, group := range groups {
		if group["project"] == float64(otherID) {
			secondID = int64(group["id"].(float64))
		}
	}
	if groupID <= 0 || secondID <= 0 || secondID == groupID {
		t.Fatalf("unstable IDs=%+v", groups)
	}
	var repeated []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", collection, "", nil, 200), &repeated); err != nil || int64(repeated[0]["id"].(float64)) != groupID {
		t.Fatalf("IDs were not stable: %+v err=%v", repeated, err)
	}
	if link := keyLink(t, f, collection[:len(collection)-len("&sort=-count&limit=25")]+"&limit=1"); !strings.Contains(link, "cursor=") || !strings.Contains(link, `rel="next"`) {
		t.Fatalf("transaction group pagination Link=%q", link)
	}
	detailPath := fmt.Sprintf("/api/0/organizations/%s/transaction-groups/%d/", f.org.Slug, groupID)
	var detail map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", detailPath, "", nil, 200), &detail); err != nil || int64(detail["id"].(float64)) != groupID || detail["transaction"] != "GET /compat/groups" || detail["count"] != float64(5) {
		t.Fatalf("transaction detail=%+v err=%v", detail, err)
	}
	f.request(t, f.client, "GET", detailPath+"?unknown=1", "", nil, 400)

	trendPath := fmt.Sprintf("/api/0/organizations/%s/transaction-groups/%d/trend/?start=%s&end=%s", f.org.Slug, groupID, start, end)
	var trend []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", trendPath, "", nil, 200), &trend); err != nil || len(trend) != 1 || trend[0]["count"] != float64(5) || trend[0]["transactionCount"] != float64(5) || trend[0]["totalTime"] != float64(500) {
		t.Fatalf("trend=%+v err=%v", trend, err)
	}
	spansPath := fmt.Sprintf("/api/0/organizations/%s/transaction-groups/%d/spans/?start=%s&end=%s", f.org.Slug, groupID, start, end)
	var spans []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", spansPath, "", nil, 200), &spans); err != nil || len(spans) != 1 || spans[0]["op"] != "db.query" || spans[0]["count"] != float64(30) || len(spans[0]) != 6 {
		t.Fatalf("group spans=%+v err=%v", spans, err)
	}

	token := issueManagementToken(t, f, f.org.Slug, "Group reader", []string{"event:read"}, []string{f.project.ID})
	bearer := http.Header{"Authorization": {"Bearer " + token}}
	var scoped []map[string]any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", collection, "", bearer, 200), &scoped); err != nil || len(scoped) != 2 {
		t.Fatalf("scoped groups=%+v err=%v", scoped, err)
	}
	f.request(t, http.DefaultClient, "GET", fmt.Sprintf("/api/0/organizations/%s/transaction-groups/%d/", f.org.Slug, secondID), "", bearer, 404)
	f.request(t, f.client, "GET", trendPath+"&project="+f.project.ID, "", nil, 400)
	f.request(t, f.client, "GET", spansPath+"&project="+f.project.ID, "", nil, 400)
	for _, path := range []string{
		collection[:len(collection)-len("&sort=-count&limit=25")] + "&sort=bad",
		collection + "&unknown=1",
		collection + "&project=0",
		collection + "&cursor=bad",
		"/api/0/organizations/" + f.org.Slug + "/transaction-groups/01/",
	} {
		f.request(t, f.client, "GET", path, "", nil, 400)
	}
}
