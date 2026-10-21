//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/config"
)

// HTTP owns admission, the published wire shape, cursor links and scope
// boundaries. Storage owns filter containment and aggregation.
type compatibleLogHTTPFixture struct {
	*resolutionHTTPFixture
	base time.Time
}

func newCompatibleLogHTTPFixture(t *testing.T) *compatibleLogHTTPFixture {
	t.Helper()
	f := newResolutionHTTPFixtureWithConfig(t, func(cfg *config.Config) { cfg.Logs = true })
	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	items := []map[string]any{
		logHTTPItem("boot complete", "info", "api", "production", "web-1", base),
		logHTTPItem("slow query", "warn", "api", "production", "web-1", base.Add(time.Hour)),
		logHTTPItem("timeout", "error", "worker", "production", "worker-1", base.Add(2*time.Hour)),
	}
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := f.db.IngestLogs(f.ctx, f.project.ID, payload); err != nil || count != len(items) {
		t.Fatalf("ingest count=%d err=%v", count, err)
	}
	return &compatibleLogHTTPFixture{resolutionHTTPFixture: f, base: base}
}

func logHTTPItem(body, level, service, environment, host string, at time.Time) map[string]any {
	return map[string]any{
		"timestamp": at.Format(time.RFC3339Nano),
		"level":     level,
		"body":      body,
		"attributes": map[string]any{
			"service.name": map[string]any{"type": "string", "value": service},
			"environment":  map[string]any{"type": "string", "value": environment},
			"host.name":    map[string]any{"type": "string", "value": host},
			"attempt":      map[string]any{"type": "integer", "value": 2},
		},
	}
}

// requestResponse performs one request and returns its response so a test can
// inspect headers such as the cursor Link.
func (f *compatibleLogHTTPFixture) requestResponse(t *testing.T, client *http.Client, method, path, body string, headers http.Header, want int) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(res.Body); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != want {
		t.Fatalf("%s %s=%d want=%d", method, path, res.StatusCode, want)
	}
	return res
}

func (f *compatibleLogHTTPFixture) logDetailPath(id string) string {
	return "/api/0/organizations/" + f.org.Slug + "/logs/" + id + "/"
}

func (f *compatibleLogHTTPFixture) logPath(suffix string) string {
	return "/api/0/organizations/" + f.org.Slug + "/logs/" + suffix
}

func (f *compatibleLogHTTPFixture) logsPath(query string) string {
	path := "/api/0/organizations/" + f.org.Slug + "/logs/"
	if query != "" {
		path += "?" + query
	}
	return path
}

func TestCompatibleLogListHTTP(t *testing.T) {
	f := newCompatibleLogHTTPFixture(t)
	path := f.logsPath("")
	f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	var logs []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("logs=%d", len(logs))
	}
	newest := logs[0]
	if newest["body"] != "timeout" || newest["level"] != "error" || newest["host"] != "worker-1" || newest["service"] != "worker" {
		t.Fatalf("newest=%v", newest)
	}
	if newest["projectId"] != f.project.ID {
		t.Fatalf("projectId=%v", newest["projectId"])
	}
	if newest["severityNumber"] != float64(17) {
		t.Fatalf("severityNumber=%v", newest["severityNumber"])
	}
	data, ok := newest["data"].(map[string]any)
	if !ok || data["attempt"] != float64(2) || data["host.name"] != "worker-1" {
		t.Fatalf("data=%v", newest["data"])
	}
	if _, exists := newest["traceID"]; !exists {
		t.Fatalf("traceID is missing from %v", newest)
	}

	// Published spelling, repeated filters and search text.
	filtered := f.logsPath("level=warning&level=info&project=" + f.project.ID)
	if err := json.Unmarshal(f.request(t, f.client, "GET", filtered, "", nil, 200), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0]["body"] != "slow query" || logs[0]["level"] != "warning" {
		t.Fatalf("filtered=%v", logs)
	}
	searched := f.logsPath("query=" + url.QueryEscape("quer"))
	if err := json.Unmarshal(f.request(t, f.client, "GET", searched, "", nil, 200), &logs); err != nil || len(logs) != 1 {
		t.Fatalf("search=%v err=%v", logs, err)
	}
	hostFiltered := f.logsPath("host=worker-1&service=worker&environment=production")
	if err := json.Unmarshal(f.request(t, f.client, "GET", hostFiltered, "", nil, 200), &logs); err != nil || len(logs) != 1 {
		t.Fatalf("host filter=%v err=%v", logs, err)
	}
	windowed := f.logsPath("start=" + url.QueryEscape(f.base.Format(time.RFC3339)) + "&end=" + url.QueryEscape(f.base.Add(time.Hour).Format(time.RFC3339)))
	if err := json.Unmarshal(f.request(t, f.client, "GET", windowed, "", nil, 200), &logs); err != nil || len(logs) != 1 || logs[0]["body"] != "boot complete" {
		t.Fatalf("window=%v err=%v", logs, err)
	}

	// A full page advertises its keyset cursor.
	response := f.requestResponse(t, f.client, "GET", f.logsPath("limit=2"), "", nil, 200)
	link := response.Header.Get("Link")
	if !strings.Contains(link, `rel="next"`) || !strings.Contains(link, "cursor=") {
		t.Fatalf("link=%q", link)
	}
	start := strings.Index(link, "?")
	end := strings.Index(link, ">")
	if start < 0 || end < 0 {
		t.Fatalf("link=%q", link)
	}
	next, err := url.Parse("?" + link[start+1:end])
	if err != nil {
		t.Fatal(err)
	}
	var second []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", f.logsPath(next.RawQuery), "", nil, 200), &second); err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0]["body"] != "boot complete" {
		t.Fatalf("second page=%v", second)
	}

	for _, query := range []string{
		"limit=0", "limit=201", "limit=two", "unknown=1", "service=api&service=web",
		"level=critical", "traceId=abc", "start=2026-10-21T12:00:00Z", "cursor=nope",
	} {
		f.request(t, f.client, "GET", f.logsPath(query), "", nil, 400)
	}
}

func TestCompatibleLogDetailResourcesAndStatsHTTP(t *testing.T) {
	f := newCompatibleLogHTTPFixture(t)
	var logs []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", f.logsPath("level=error"), "", nil, 200), &logs); err != nil || len(logs) != 1 {
		t.Fatalf("logs=%v err=%v", logs, err)
	}
	id, _ := logs[0]["id"].(string)
	if id == "" {
		t.Fatalf("id=%v", logs[0]["id"])
	}
	var detail map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", f.logDetailPath(id), "", nil, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if detail["body"] != "timeout" || detail["spanID"] == nil {
		t.Fatalf("detail=%v", detail)
	}
	f.request(t, http.DefaultClient, "GET", f.logDetailPath(id), "", nil, 401)
	f.request(t, f.client, "GET", f.logDetailPath("bad-id"), "", nil, 400)
	f.request(t, f.client, "GET", f.logDetailPath("999999"), "", nil, 404)

	var resources []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", f.logPath("resources/"), "", nil, 200), &resources); err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	for _, resource := range resources {
		types[resource["type"].(string)]++
		if resource["lastSeen"] == nil || resource["name"] == "" {
			t.Fatalf("resource=%v", resource)
		}
	}
	if types["service"] != 2 || types["environment"] != 1 || types["host"] != 2 {
		t.Fatalf("resources=%v", resources)
	}
	var hosts []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", f.logPath("resources/?resource_type=host"), "", nil, 200), &hosts); err != nil || len(hosts) != 2 {
		t.Fatalf("hosts=%v err=%v", hosts, err)
	}
	f.request(t, f.client, "GET", f.logPath("resources/?resource_type=session"), "", nil, 400)
	f.request(t, f.client, "GET", f.logPath("resources/?limit=1"), "", nil, 400)

	var stats struct {
		Intervals []time.Time `json:"intervals"`
		Series    []struct {
			Name string  `json:"name"`
			Data []int64 `json:"data"`
		} `json:"series"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", f.logPath("stats/"), "", nil, 200), &stats); err != nil {
		t.Fatal(err)
	}
	// Seven days of hourly buckets plus the two partially covered edge hours,
	// because buckets are floored rather than dropped.
	if len(stats.Intervals) < 7*24 || len(stats.Intervals) > 7*24+1 {
		t.Fatalf("default statistics intervals=%d", len(stats.Intervals))
	}
	if !stats.Intervals[0].Equal(stats.Intervals[0].Truncate(time.Hour)) || !stats.Intervals[1].Equal(stats.Intervals[0].Add(time.Hour)) {
		t.Fatalf("intervals are not hourly: %v", stats.Intervals[:2])
	}
	if len(stats.Series) != 3 {
		t.Fatalf("series=%+v", stats.Series)
	}
	total := int64(0)
	for _, series := range stats.Series {
		if len(series.Data) != len(stats.Intervals) {
			t.Fatalf("series %s has %d points", series.Name, len(series.Data))
		}
		for _, value := range series.Data {
			total += value
		}
	}
	if total != 3 {
		t.Fatalf("statistics total=%d", total)
	}
	explicit := f.logPath("stats/?level=error&start=" + url.QueryEscape(f.base.Format(time.RFC3339)) + "&end=" + url.QueryEscape(f.base.Add(4*time.Hour).Format(time.RFC3339)))
	if err := json.Unmarshal(f.request(t, f.client, "GET", explicit, "", nil, 200), &stats); err != nil {
		t.Fatal(err)
	}
	// The explicit window starts mid-hour, so its floored buckets span five hours
	// and the single error log lands in the third of them.
	if len(stats.Intervals) != 5 || len(stats.Series) != 1 || stats.Series[0].Name != "error" {
		t.Fatalf("explicit statistics=%+v", stats)
	}
	if got := stats.Series[0].Data; len(got) != 5 || got[2] != 1 || got[0]+got[1]+got[3]+got[4] != 0 {
		t.Fatalf("explicit error series=%v", got)
	}
	f.request(t, f.client, "GET", f.logPath("stats/?start=2026-01-01T00:00:00Z&end=2026-06-01T00:00:00Z"), "", nil, 400)
}

func TestCompatibleLogScopeAndDisabledFeatureHTTP(t *testing.T) {
	f := newCompatibleLogHTTPFixture(t)
	readToken := issueManagementToken(t, f.resolutionHTTPFixture, f.org.Slug, "Logs reader", []string{"event:read"}, []string{f.project.ID})
	bearer := http.Header{"Authorization": {"Bearer " + readToken}}
	var logs []map[string]any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", f.logsPath("limit=1"), "", bearer, 200), &logs); err != nil || len(logs) != 1 {
		t.Fatalf("bearer logs=%v err=%v", logs, err)
	}
	foreign, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Logs hidden", "go")
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "GET", f.logsPath("project="+foreign.ID), "", bearer, 404)
	projectToken := issueManagementToken(t, f.resolutionHTTPFixture, f.org.Slug, "Project reader", []string{"project:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "GET", f.logsPath(""), "", http.Header{"Authorization": {"Bearer " + projectToken}}, 403)
	f.request(t, http.DefaultClient, "GET", "/api/0/organizations/absent-org/logs/", "", bearer, 404)

	// A deployment without log ingestion answers 404 for every log route.
	disabled := newResolutionHTTPFixture(t)
	for _, path := range []string{"/logs/", "/logs/1/", "/logs/resources/", "/logs/stats/"} {
		disabled.request(t, disabled.client, "GET", "/api/0/organizations/"+disabled.org.Slug+path, "", nil, 404)
	}
}
