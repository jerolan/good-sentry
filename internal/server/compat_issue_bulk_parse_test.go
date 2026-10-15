package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/store"
)

// Authoring gate: bulk alone admits the CLI duration alias and boolean/integer
// merge forms. Conflicting zero copies or treating merge:false as an operation
// would escape the individual parser's coverage. These tests own only that
// extension; native policy semantics remain with their existing owner.
func TestCompatibleIssueBulkChangeParsing(t *testing.T) {
	number := func(value int32) *int32 { return &value }
	for _, tc := range []struct {
		name, body string
		want       store.CompatibleIssueBulkChange
	}{
		{"CLI null is no policy", `{"status":"muted","snoozeDuration":null}`, store.CompatibleIssueBulkChange{Change: store.CompatibleIssueChange{Status: "muted"}}},
		{"null alias only is no command", `{"snoozeDuration":null}`, store.CompatibleIssueBulkChange{}},
		{"duration alias", `{"snoozeDuration":3}`, store.CompatibleIssueBulkChange{Change: store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: number(3)}}}},
		{"all duration copies agree", `{"snoozeDuration":3,"ignoreDuration":3,"statusDetails":{"ignoreDuration":3,"ignoreCount":4}}`, store.CompatibleIssueBulkChange{Change: store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: number(3), Count: number(4)}}}},
		{"explicit zero copies agree", `{"snoozeDuration":0,"ignoreDuration":0,"statusDetails":{"ignoreDuration":0}}`, store.CompatibleIssueBulkChange{Change: store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy}}}},
		{"largest duration", `{"snoozeDuration":2147483647}`, store.CompatibleIssueBulkChange{Change: store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: number(2147483647)}}}},
		{"boolean merge", `{"merge":true}`, store.CompatibleIssueBulkChange{Merge: true}},
		{"integer merge", `{"merge":1}`, store.CompatibleIssueBulkChange{Merge: true}},
		{"false is no merge", `{"merge":false}`, store.CompatibleIssueBulkChange{}},
		{"zero is no merge", `{"merge":0}`, store.CompatibleIssueBulkChange{}},
		{"null is no merge", `{"merge":null}`, store.CompatibleIssueBulkChange{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.body), &fields); err != nil {
				t.Fatal(err)
			}
			got, err := parseCompatibleIssueBulkChange(fields)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("change=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
	for _, body := range []string{
		`{"snoozeDuration":-1}`, `{"snoozeDuration":2147483648}`, `{"snoozeDuration":1.0}`,
		`{"snoozeDuration":"1"}`, `{"snoozeDuration":true}`, `{"snoozeDuration":[]}`,
		`{"snoozeDuration":0,"ignoreDuration":1}`, `{"snoozeDuration":1,"ignoreDuration":0}`,
		`{"snoozeDuration":0,"statusDetails":{"ignoreDuration":1}}`, `{"snoozeDuration":1,"statusDetails":{"ignoreDuration":0}}`,
		`{"snoozeDuration":0,"ignoreDuration":null}`, `{"snoozeDuration":0,"statusDetails":{"inRelease":null}}`,
		`{"merge":2}`, `{"merge":-1}`, `{"merge":1.0}`, `{"merge":"true"}`, `{"merge":[]}`, `{"merge":{}}`,
	} {
		t.Run(body, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &fields); err != nil {
				t.Fatal(err)
			}
			if _, err := parseCompatibleIssueBulkChange(fields); err == nil {
				t.Fatal("invalid bulk extension was accepted")
			}
		})
	}
}

// Authoring gate: write selection has a 1000-ID admission, different defaults
// and a relative-period meaning on both paths. Read pagination tests cannot
// detect accidental inheritance of their 100-ID, cutoff or cursor behavior.
// Real request/response objects exercise the boundary without a test-only seam.
func TestCompatibleIssueBulkQueryAdmission(t *testing.T) {
	parse := func(project, raw string) (store.CompatibleIssueBulkFilter, bool, int) {
		t.Helper()
		r := httptest.NewRequest("PUT", "/?"+raw, nil)
		r.SetPathValue("project", project)
		w := httptest.NewRecorder()
		f, ok := compatibleIssueBulkQuery(w, r)
		return f, ok, w.Code
	}
	for _, tc := range []struct{ name, project, raw, query string }{
		{"project missing query selects all", "app", "", ""},
		{"organization missing query defaults unresolved", "", "", "is:unresolved"},
		{"organization explicit IDs select all statuses", "", "id=1&id=1&id=2", ""},
		{"explicit empty query selects all", "", "query=", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, ok, status := parse(tc.project, tc.raw)
			if !ok || status != 200 || f.Query != tc.query || f.Limit != 1000 || f.Sort != "-last_seen" || f.Period != 0 {
				t.Fatalf("filter=%+v ok=%v status=%d", f, ok, status)
			}
			if strings.Contains(tc.raw, "id=") && !reflect.DeepEqual(f.IDs, []string{"1", "2"}) {
				t.Fatalf("IDs were not deduplicated: %v", f.IDs)
			}
		})
	}
	for _, project := range []string{"", "app"} {
		q := url.Values{"status": {"muted"}, "statsPeriod": {"30m"}, "limit": {"100"}, "project": {"-1"}, "environment": {"", " production "}, "start": {"2026-01-01T01:00:00+01:00"}, "end": {"2026-01-02T00:00:00Z"}}
		f, ok, status := parse(project, q.Encode())
		if !ok || status != 200 || f.Status != "ignored" || f.Period != 30*time.Minute || f.Limit != 100 || f.Start == nil || f.End == nil || f.Start.Format(time.RFC3339) != "2026-01-01T00:00:00Z" || !reflect.DeepEqual(f.Environments, []string{"", " production "}) {
			t.Fatalf("relative period on project=%q filter=%+v ok=%v status=%d", project, f, ok, status)
		}
	}
	q := url.Values{}
	for i := 1; i <= 1000; i++ {
		q.Add("id", fmt.Sprint(i))
	}
	if f, ok, _ := parse("", q.Encode()); !ok || len(f.IDs) != 1000 {
		t.Fatalf("1000 IDs rejected: len=%d ok=%v", len(f.IDs), ok)
	}
	q.Add("id", "1")
	invalid := []string{
		q.Encode(), strings.Repeat("project=app&", 100) + "project=app", strings.Repeat("environment=x&", 100) + "environment=x",
		"id=0", "id=01", "id=9223372036854775808", "id=bad", "query=a&query=b", "limit=1&limit=2",
		"limit=0", "limit=01", "limit=101", "limit=1000", "project=", "project=-1&project=app", "status=", "status=reprocessing",
		"cursor=", "per_page=1", "view=all", "shortIdLookup=1", "sort=", "sort=unsupported", "statsPeriod=0h", "statsPeriod=forever",
		"start=invalid&statsPeriod=1h", "start=2026-01-02T00%3A00%3A00Z&end=2026-01-01T00%3A00%3A00Z&statsPeriod=1h",
		"query=%00", "query=%ff", "environment=%00", "project=%ff", "query=%zz",
	}
	for _, raw := range invalid {
		if _, ok, status := parse("app", raw); ok || status != 400 {
			t.Errorf("query=%q ok=%v status=%d", raw, ok, status)
		}
	}
}
