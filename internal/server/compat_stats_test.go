//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/store"
)

func statsCapture(t *testing.T, f *resolutionHTTPFixture, number int, at time.Time) {
	t.Helper()
	raw := json.RawMessage(fmt.Sprintf(`{"event_id":"%032x","message":"stats error","level":"error","timestamp":%q}`, number, at.UTC().Format(time.RFC3339Nano)))
	normalized, err := event.Normalize(raw, "", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.Ingest(f.ctx, f.project.ID, normalized); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleStatsV2HTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	statsCapture(t, f, 900, base.Add(5*time.Minute))
	statsCapture(t, f, 901, base.Add(10*time.Minute))
	statsCapture(t, f, 902, base.Add(65*time.Minute))
	mcpPerformanceTransaction(t, f, f.project.ID, 910, 920, "GET /stats", "http.server", base.Add(15*time.Minute), 10, 0)
	mcpPerformanceTransaction(t, f, f.project.ID, 911, 921, "GET /stats", "http.server", base.Add(20*time.Minute), 10, 0)
	mcpPerformanceTransaction(t, f, f.project.ID, 912, 922, "GET /stats", "http.server", base.Add(70*time.Minute), 10, 0)
	start, end := base.Format(time.RFC3339), base.Add(2*time.Hour).Format(time.RFC3339)
	path := "/api/0/organizations/" + f.org.Slug + "/stats_v2/?category=error&field=sum(times_seen)&interval=1h&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end)
	f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	var result store.StatsV2Response
	if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || len(result.Intervals) != 2 || result.Groups[0].By.Category != "error" || result.Groups[0].Totals.SumTimesSeen != 3 || result.Groups[0].Totals.SumQuantity != 3 || len(result.Groups[0].Series.SumTimesSeen) != 2 || result.Groups[0].Series.SumTimesSeen[0] != 2 || result.Groups[0].Series.SumTimesSeen[1] != 1 || result.Groups[0].Series.SumQuantity[1] != 1 {
		t.Fatalf("error stats=%+v", result)
	}
	transactionPath := "/api/0/organizations/" + f.org.Slug + "/stats_v2/?category=transaction&field=sum(quantity)&interval=1h&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end)
	var transactions store.StatsV2Response
	if err := json.Unmarshal(f.request(t, f.client, "GET", transactionPath, "", nil, 200), &transactions); err != nil || transactions.Groups[0].Totals.SumQuantity != 3 || transactions.Groups[0].Series.SumQuantity[0] != 2 || transactions.Groups[0].Series.SumQuantity[1] != 1 {
		t.Fatalf("transaction stats=%+v err=%v", result, err)
	}
	filteredPath := path + "&project=" + f.project.ID + "&project=-1"
	var filtered store.StatsV2Response
	if err := json.Unmarshal(f.request(t, f.client, "GET", filteredPath, "", nil, 200), &filtered); err != nil || filtered.Groups[0].Totals.SumTimesSeen != 3 {
		t.Fatalf("project -1 stats=%+v err=%v", result, err)
	}

	token := issueManagementToken(t, f, f.org.Slug, "Stats reader", []string{"event:read"}, []string{f.project.ID})
	bearer := http.Header{"Authorization": {"Bearer " + token}}
	f.request(t, http.DefaultClient, "GET", filteredPath, "", bearer, 200)
	foreign, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Stats hidden", "go")
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "GET", path+"&project="+foreign.ID, "", bearer, 404)

	for _, query := range []string{
		"category=error&field=sum(times_seen)&start=" + url.QueryEscape(start),
		"category=bad&field=sum(times_seen)&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end),
		"category=error&field=bad&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end),
		"category=error&field=sum(times_seen)&interval=bad&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end),
		"category=error&field=sum(times_seen)&outcome=accepted&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end),
		"category=error&field=sum(times_seen)&groupBy=outcome&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end),
		"category=error&field=sum(times_seen)&interval=1m&start=2026-01-01T00:00:00Z&end=2026-01-03T00:00:00Z",
		"category=error&field=sum(times_seen)&start=bad&end=bad",
	} {
		f.request(t, f.client, "GET", "/api/0/organizations/"+f.org.Slug+"/stats_v2/?"+query, "", nil, 400)
	}
	f.request(t, f.client, "GET", "/api/0/organizations/"+f.org.Slug+"/stats_v2/?category=error&field=sum(times_seen)&groupBy=category&start="+url.QueryEscape(start)+"&end="+url.QueryEscape(end), "", nil, 200)
}
