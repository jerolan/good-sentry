package server

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/store"
)

// Authoring gate: DELETE owns wire framing and its explicit-ID filter-ignore
// rule. Reusing PUT parsing for IDs would reject ignored values or accidentally
// narrow deletion. Store receives typed filters and cannot prove byte encoding,
// duplicate scalars or supplied-count admission. No production seam is needed;
// provider/HTTP tests own containment, mutation, authorization and transactions.
func TestCompatibleIssueDeleteQuery(t *testing.T) {
	parse := func(project, query string) (store.CompatibleIssueBulkFilter, bool, int) {
		t.Helper()
		r := httptest.NewRequest("DELETE", "/?"+query, nil)
		r.SetPathValue("project", project)
		w := httptest.NewRecorder()
		filter, ok := compatibleIssueDeleteQuery(w, r)
		return filter, ok, w.Code
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	ignored := url.Values{
		"id": {"2", "1", "2"}, "project": {"", "-1", "nonexistent-project"},
		"environment": {strings.Repeat("é", 201)}, "query": {"release:[ unsupported:syntax"},
		"start": {"invalid"}, "end": {"also invalid"}, "statsPeriod": {"forever"},
		"status": {"reprocessing"}, "sort": {"not-implemented"}, "limit": {"2"},
	}
	for _, tc := range []struct {
		name, project, query string
		want                 store.CompatibleIssueBulkFilter
	}{
		{"organization defaults unresolved", "", "", store.CompatibleIssueBulkFilter{Limit: 1000, Sort: "-last_seen", Query: "is:unresolved"}},
		{"project defaults every state", "app", "", store.CompatibleIssueBulkFilter{Limit: 1000, Sort: "-last_seen"}},
		{"empty query explicitly selects every state", "", "query=", store.CompatibleIssueBulkFilter{Limit: 1000, Sort: "-last_seen"}},
		{"explicit IDs ignore every known selector value", "", ignored.Encode(), store.CompatibleIssueBulkFilter{IDs: []string{"2", "1"}, Limit: 2, Sort: "-last_seen"}},
		{"project IDs deduplicate", "app", "id=2&id=1&id=2", store.CompatibleIssueBulkFilter{IDs: []string{"2", "1"}, Limit: 1000, Sort: "-last_seen"}},
		{"int64 boundary and explicit limit", "", "id=9223372036854775807&limit=100", store.CompatibleIssueBulkFilter{IDs: []string{"9223372036854775807"}, Limit: 100, Sort: "-last_seen"}},
		{"ID-less filters retain existing normalization", "", "query=release%3Aapp&project=-1&environment=&environment=prod&status=muted&sort=freq&limit=100&statsPeriod=30m&start=2026-01-01T01%3A00%3A00%2B01%3A00&end=2026-01-02T00%3A00%3A00Z", store.CompatibleIssueBulkFilter{
			ProjectRefs: []string{"-1"}, Environments: []string{"", "prod"}, Query: "release:app", Status: "ignored", Sort: "freq", Limit: 100, Period: 30 * time.Minute, Start: &start, End: &end,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, status := parse(tc.project, tc.query)
			if !ok || status != 200 || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("filter=%+v want=%+v ok=%t status=%d", got, tc.want, ok, status)
			}
		})
	}
	for _, project := range []string{"", "app"} {
		query := url.Values{}
		for id := 1; id <= 1000; id++ {
			query.Add("id", fmt.Sprint(id))
		}
		if filter, ok, status := parse(project, query.Encode()); !ok || status != 200 || len(filter.IDs) != 1000 {
			t.Fatalf("1000 supplied IDs: len=%d ok=%t status=%d", len(filter.IDs), ok, status)
		}
		query.Add("id", "1")
		if _, ok, status := parse(project, query.Encode()); ok || status != 400 {
			t.Fatalf("1001 supplied IDs were admitted after dedup: ok=%t status=%d", ok, status)
		}
	}
	bounded := url.Values{"id": {"1"}}
	for i := 0; i < 100; i++ {
		bounded.Add("project", "")
		bounded.Add("environment", "")
	}
	if filter, ok, _ := parse("", bounded.Encode()); !ok || len(filter.ProjectRefs) != 0 || len(filter.Environments) != 0 {
		t.Fatalf("100 ignored selectors must retain admission but no selection effect: %+v ok=%t", filter, ok)
	}
	for _, query := range []string{
		"id=", "id=0", "id=01", "id=-1", "id=9223372036854775808",
		"id=1&limit=", "id=1&limit=0", "id=1&limit=01", "id=1&limit=101", "id=1&limit=1000", "id=1&limit=1&limit=1",
		"id=1&query=a&query=b", "id=1&status=resolved&status=resolved", "id=1&start=a&start=b", "id=1&sort=a&sort=b",
		"id=1&viewId=all", "id=1&unknown=x", "id=1&cursor=", "id=1&per_page=1",
		"id=1&query=%zz", "id=1&query=%ff", "id=1&project=%00", "id=1&environment=%ff", "id=1&sort=%00",
		"id=1&" + strings.Repeat("project=x&", 100) + "project=x", "id=1&" + strings.Repeat("environment=x&", 100) + "environment=x",
		"status=reprocessing", "sort=unsupported", "start=invalid&statsPeriod=1h", "environment=" + url.QueryEscape(strings.Repeat("é", 201)),
	} {
		if _, ok, status := parse("", query); ok || status != 400 {
			t.Errorf("invalid organization query=%q ok=%t status=%d", query, ok, status)
		}
	}
	for _, query := range []string{"limit=1", "query=", "status=resolved", "project=app", "environment=prod", "sort=date", "start=2026-01-01T00:00:00Z", "end=2026-01-02T00:00:00Z", "statsPeriod=1h", "viewId=all"} {
		for _, prefix := range []string{"", "id=1&"} {
			if _, ok, status := parse("app", prefix+query); ok || status != 400 {
				t.Errorf("project permits only id: query=%q ok=%t status=%d", prefix+query, ok, status)
			}
		}
	}
}

// Empty means zero bytes rather than an empty JSON value. The shared parser is
// called by both real deletion handlers; HTTP tests own routing/CSRF around it.
func TestCompatibleIssueDeleteBodyRequiresZeroBytes(t *testing.T) {
	for _, body := range []string{"", " ", "\n", "{}", "null", "[]", string([]byte{0}), string([]byte{0xff})} {
		w := httptest.NewRecorder()
		ok := compatibleIssueDeleteBody(w, httptest.NewRequest("DELETE", "/", strings.NewReader(body)))
		if ok != (body == "") || body != "" && w.Code != 400 {
			t.Errorf("body=%q admitted=%t status=%d", body, ok, w.Code)
		}
	}
}
