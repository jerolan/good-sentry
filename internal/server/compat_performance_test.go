//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCompatibleSpanGroupsAndNPlusOneHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Performance other", "go")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	for index := 0; index < 5; index++ {
		mcpPerformanceTransaction(t, f, f.project.ID, 300+index, 400+index, "GET /api/performance", "http.server", base.Add(time.Duration(index)*time.Minute), 100, 6)
	}
	mcpPerformanceTransaction(t, f, f.project.ID, 310, 410, "GET /api/performance", "background", base.Add(10*time.Minute), 50, 2)
	mcpPerformanceTransaction(t, f, other.ID, 311, 411, "GET /api/performance", "http.server", base.Add(11*time.Minute), 100, 7)
	start, end := base.Add(-time.Minute).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339)
	path := "/api/0/organizations/" + f.org.Slug + "/span-groups/?start=" + start + "&end=" + end + "&op=db.query&sort=-count&limit=50"
	f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	var spanGroups []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &spanGroups); err != nil {
		t.Fatal(err)
	}
	if len(spanGroups) != 1 || spanGroups[0]["op"] != "db.query" || spanGroups[0]["description"] != "SELECT mcp_performance" || spanGroups[0]["count"] != float64(39) {
		t.Fatalf("span groups=%+v", spanGroups)
	}
	for _, key := range []string{"op", "description", "count", "avgDuration", "p95Duration", "totalTime"} {
		if _, ok := spanGroups[0][key]; !ok {
			t.Fatalf("span group omitted %s: %+v", key, spanGroups[0])
		}
	}
	if len(spanGroups[0]) != 6 {
		t.Fatalf("span group exposed extra fields: %+v", spanGroups[0])
	}

	nPlusPath := "/api/0/organizations/" + f.org.Slug + "/n-plus-one/?start=" + start + "&end=" + end + "&project=" + f.project.ID + "&op=db&threshold=5&limit=50"
	var patterns []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", nPlusPath, "", nil, 200), &patterns); err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 || patterns[0]["transactionName"] != "GET /api/performance" || patterns[0]["op"] != "db.query" || patterns[0]["description"] != "SELECT mcp_performance" || patterns[0]["totalSpans"] != float64(30) || patterns[0]["transactionCount"] != float64(5) || patterns[0]["spansPerTxn"] != float64(6) {
		t.Fatalf("N+1 patterns=%+v", patterns)
	}
	if len(patterns[0]) != 8 {
		t.Fatalf("N+1 pattern exposed extra fields: %+v", patterns[0])
	}

	token := issueManagementToken(t, f, f.org.Slug, "Performance reader", []string{"event:read"}, []string{f.project.ID})
	bearer := http.Header{"Authorization": {"Bearer " + token}}
	var scoped []map[string]any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", path, "", bearer, 200), &scoped); err != nil || len(scoped) != 1 || scoped[0]["count"] != float64(32) {
		t.Fatalf("scoped span groups=%+v err=%v", scoped, err)
	}
	f.request(t, http.DefaultClient, "GET", path+"&project="+other.ID, "", bearer, 404)
	f.request(t, http.DefaultClient, "GET", path+"&project="+f.project.ID+"&project="+other.ID, "", bearer, 404)

	for _, query := range []string{"unknown=1", "sort=bad", "limit=0", "limit=101", "project=0", "start=x&end=x", "start=" + start + "&start=" + start + "&end=" + end} {
		f.request(t, f.client, "GET", "/api/0/organizations/"+f.org.Slug+"/span-groups/?"+query, "", nil, 400)
	}
	for _, query := range []string{"threshold=1", "threshold=NaN", "threshold=101", "op=", "limit=0", "unknown=1"} {
		f.request(t, f.client, "GET", "/api/0/organizations/"+f.org.Slug+"/n-plus-one/?"+query, "", nil, 400)
	}
}
