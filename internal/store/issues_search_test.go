//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func issueSearchCapture(t *testing.T, ctx context.Context, db *Store, projectID string, id int, stamp time.Time, group, message, environment, release, level string, tags any) string {
	t.Helper()
	payload := map[string]any{"event_id": fmt.Sprintf("%032x", id), "timestamp": stamp.UTC().Format(time.RFC3339Nano), "fingerprint": []string{group}, "message": message, "environment": environment, "release": release, "level": level, "tags": tags}
	if tags == nil {
		delete(payload, "tags")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := event.Normalize(raw, "", stamp)
	if err != nil {
		t.Fatal(err)
	}
	issue, created, err := db.Ingest(ctx, projectID, normalized)
	if err != nil || !created {
		t.Fatalf("capture created=%v error=%v", created, err)
	}
	return issue
}

func TestIssueSearchHistoricalSameEvent(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	wanted := issueSearchCapture(t, ctx, db, project.ID, 1, stamp, "historical", "payment timeout", "production", "v1", "error", map[string]any{"region": "eu"})
	issueSearchCapture(t, ctx, db, project.ID, 2, stamp.Add(time.Minute), "historical", "latest checkout", "staging", "v2", "warning", [][]string{{"region", "us"}})
	issueSearchCapture(t, ctx, db, project.ID, 3, stamp, "split", "payment split", "production", "v2", "error", map[string]any{"region": "eu"})
	issueSearchCapture(t, ctx, db, project.ID, 4, stamp.Add(time.Minute), "split", "latest split", "staging", "v1", "warning", map[string]any{"region": "us"})
	rows, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Environment: "production", Release: "v1", Query: "payment"})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != wanted || rows[0].Count != 2 || rows[0].Environment != "staging" || rows[0].Release != "v2" || rows[0].Title != "latest checkout" {
		t.Fatalf("historical same-event filter/lifetime summary: rows=%+v total=%d error=%v", rows, total, err)
	}
}

func TestIssueSearchStructuredEventPredicates(t *testing.T) {
	ctx, db, user, org, p := telemetryStore(t)
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a := issueSearchCapture(t, ctx, db, p.ID, 1, stamp, "history", `PAYMENT literal%_\raw`, "production", "v1", "error", map[string]any{"region": "eu", "colour": "blue", "status": "tag-status", "environment": "tag-env", "raw": `%_\literal`, "literalStar": "*", "empty": "", "number": 7, "bool": true, "nothing": nil, `odd'"]`: `v' OR 1=1 --`})
	issueSearchCapture(t, ctx, db, p.ID, 2, stamp.Add(time.Minute), "history", "latest A", "staging", "v2", "warning", [][]string{{"region", "us"}})
	b := issueSearchCapture(t, ctx, db, p.ID, 3, stamp, "split", "payment split", "production", "v2", "error", map[string]any{"region": "eu"})
	issueSearchCapture(t, ctx, db, p.ID, 4, stamp.Add(time.Minute), "split", "latest B", "staging", "v1", "warning", map[string]any{"region": "us"})
	pairs := []any{[]any{"region", "eu"}, []any{"colour", "blue"}, []any{"status", "tag-status"}, []any{"number", 7}, []any{"empty", ""}, []any{"bad"}, "scalar", nil, []any{"bad", "extra", "third"}, []any{2, "not-key"}}
	c := issueSearchCapture(t, ctx, db, p.ID, 5, stamp, "pairs", "payment pairs", "production", "v1", "error", pairs)
	d := issueSearchCapture(t, ctx, db, p.ID, 6, stamp, "scalar", "scalar tags", "production", "v1", "info", true)
	e := issueSearchCapture(t, ctx, db, p.ID, 7, stamp, "missing", "missing tags", "production", "v1", "info", nil)
	f := issueSearchCapture(t, ctx, db, p.ID, 8, stamp, "nonstring", "nonstring tags", "production", "v1", "info", map[string]any{"region": 7, "raw": `AX\literal`})
	g := issueSearchCapture(t, ctx, db, p.ID, 9, stamp, "empty", "empty string", "production", "v1", "info", map[string]any{"region": ""})
	h := issueSearchCapture(t, ctx, db, p.ID, 10, stamp, "wildcard", "wildcard region", "production", "v1", "info", map[string]any{"region": "eu-West"})
	star := issueSearchCapture(t, ctx, db, p.ID, 11, stamp, "star", "literal star", "production", "v1", "info", map[string]any{"region": "*"})
	if _, err := db.SetIssueStatus(ctx, user.ID, a, "resolved"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetIssueStatus(ctx, user.ID, h, "ignored"); err != nil {
		t.Fatal(err)
	}
	quotedKey, _ := json.Marshal(`odd'"]`)
	quotedValue, _ := json.Marshal(`v' OR 1=1 --`)
	parameterizedTag := fmt.Sprintf("tags[%s]:%s", quotedKey, quotedValue)
	cases := []struct {
		name   string
		filter IssueFilter
		want   []string
	}{
		{"all predicates same historical occurrence", IssueFilter{Query: "payment region:eu level:[error,fatal]", Environment: "production", Release: "v1"}, []string{a, c}},
		{"split events cannot satisfy tag", IssueFilter{Query: "region:us", Environment: "production", Release: "v1"}, nil},
		{"intersected structured event selectors", IssueFilter{Query: "environment:production release:v1 region:eu"}, []string{a, c}},
		{"array OR values", IssueFilter{Query: "region:[eu,us]"}, []string{a, b, c}},
		{"case sensitive structured values", IssueFilter{Query: "region:EU"}, nil},
		{"bare wildcard", IssueFilter{Query: "region:eu*"}, []string{a, b, c, h}},
		{"quoted star is exact", IssueFilter{Query: `region:"*"`}, []string{star}},
		{"bare star includes empty strings not nonstrings", IssueFilter{Query: "region:*"}, []string{a, b, c, g, h, star}},
		{"quoted empty", IssueFilter{Query: `region:""`}, []string{g}},
		{"negative includes missing scalar and nonstring tags", IssueFilter{Query: "!region:*", Environment: "production", Release: "v1"}, []string{d, e, f}},
		{"negative terms stay in selected occurrence", IssueFilter{Query: "!region:eu", Environment: "production", Release: "v1"}, []string{d, e, f, g, h, star}},
		{"negative OR", IssueFilter{Query: "!region:[eu,us]", Environment: "production", Release: "v1"}, []string{d, e, f, g, h, star}},
		{"explicit colliding tag name", IssueFilter{Query: "tags[status]:tag-status"}, []string{a, c}},
		{"quoted tag name", IssueFilter{Query: `tags["environment"]:tag-env`}, []string{a}},
		{"arbitrary key and SQL-looking value remain literal", IssueFilter{Query: parameterizedTag}, []string{a}},
		{"tag scalar never coerces", IssueFilter{Query: "number:7"}, nil},
		{"malformed pair not a tag", IssueFilter{Query: "bad:*"}, nil},
		{"free text SQL wildcards literal", IssueFilter{Query: `literal%_\raw`}, []string{a}},
		{"tag wildcard escapes SQL percent underscore backslash", IssueFilter{Query: `raw:%_\*`}, []string{a}},
		{"status OR independent from matching event", IssueFilter{Query: "is:[resolved,ignored]"}, []string{a, h}},
		{"status selectors intersect", IssueFilter{Query: "is:resolved", Status: "unresolved"}, nil},
		{"contradictory status terms", IssueFilter{Query: "is:resolved status:ignored"}, nil},
		{"explicit all statuses", IssueFilter{Status: "all", Query: "region:eu"}, []string{a, b, c}},
		{"negated issue status", IssueFilter{Query: "!is:[unresolved,ignored] region:eu"}, []string{a}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := db.Issues(ctx, user.ID, org.Slug, tc.filter)
			got := make([]string, 0, len(rows))
			for _, row := range rows {
				got = append(got, row.ID)
			}
			expected := append([]string{}, tc.want...)
			sort.Strings(got)
			sort.Strings(expected)
			if err != nil || total != len(expected) || !reflect.DeepEqual(got, expected) {
				t.Fatalf("IDs=%v total=%d error=%v want=%v", got, total, err, expected)
			}
		})
	}
}

func TestIssueSearchPagesLifetimeOrdersAndReopen(t *testing.T) {
	ctx, db, user, org, p := telemetryStore(t)
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ids := []string{}
	for i := range 26 {
		first := stamp
		if i == 0 {
			first = stamp.Add(time.Minute)
		}
		ids = append(ids, issueSearchCapture(t, ctx, db, p.ID, i+1, first, fmt.Sprintf("page-%d", i), fmt.Sprintf("Page issue %d", i), "prod", "page-v1", "error", nil))
	}
	for i := range 26 {
		issueSearchCapture(t, ctx, db, p.ID, i+101, stamp.Add(time.Hour), fmt.Sprintf("page-%d", i), fmt.Sprintf("Latest issue %d", i), "stage", "page-v2", "error", nil)
	}
	issueSearchCapture(t, ctx, db, p.ID, 201, stamp.Add(time.Hour), "page-1", "Most frequent issue", "stage", "page-v2", "error", nil)
	hidden := issueSearchCapture(t, ctx, db, p.ID, 202, stamp, "hidden", "Hidden merged issue", "prod", "page-v1", "error", nil)
	if _, err := db.pool.Exec(ctx, `UPDATE issues SET merged_into=$2::bigint WHERE id=$1::bigint`, hidden, ids[0]); err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"date", "freq", "new"} {
		expected := []string{}
		first := ""
		if order == "freq" {
			first = ids[1]
		} else if order == "new" {
			first = ids[0]
		}
		if first != "" {
			expected = append(expected, first)
		}
		for i := len(ids) - 1; i >= 0; i-- {
			if ids[i] != first {
				expected = append(expected, ids[i])
			}
		}
		all := []string{}
		for page := 1; page <= 3; page++ {
			rows, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Project: p.ID, Sort: order, Page: page})
			wantCount := 25
			if page == 2 {
				wantCount = 1
			} else if page == 3 {
				wantCount = 0
			}
			if err != nil || total != 26 || len(rows) != wantCount {
				t.Fatalf("sort=%s page=%d count=%d total=%d error=%v", order, page, len(rows), total, err)
			}
			for _, row := range rows {
				all = append(all, row.ID)
				if row.Environment != "stage" || row.Release != "page-v2" {
					t.Fatalf("historical query changed lifetime summary: %+v", row)
				}
			}
		}
		if !reflect.DeepEqual(all, expected) {
			t.Fatalf("sort=%s IDs=%v want=%v", order, all, expected)
		}
	}
	before, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Project: p.ID, Environment: "prod", Release: "page-v1", Sort: "new", Page: 2})
	if err != nil || total != 26 || len(before) != 1 {
		t.Fatalf("before reopen=%+v total=%d error=%v", before, total, err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	after, total, err := reopened.Issues(ctx, user.ID, org.Slug, IssueFilter{Project: p.ID, Environment: "prod", Release: "page-v1", Sort: "new", Page: 2})
	if err != nil || total != 26 || !reflect.DeepEqual(after, before) {
		t.Fatalf("reopened filtered page changed: %+v total=%d error=%v", after, total, err)
	}
}

func TestIssueSearchValidationAndOrganizationProjectBoundary(t *testing.T) {
	ctx, db, user, org, p := telemetryStore(t)
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	wanted := issueSearchCapture(t, ctx, db, p.ID, 1, stamp, "own", "Owned issue", strings.Repeat("界", 128), strings.Repeat("界", 255), "error", nil)
	var foreignOrg string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('issue-search-other','Other') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, foreignOrg, user.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateProject(ctx, user.ID, "issue-search-other", "Foreign", "go")
	if err != nil {
		t.Fatal(err)
	}
	issueSearchCapture(t, ctx, db, foreign.ID, 2, stamp, "foreign", "Foreign issue", "prod", "v1", "error", nil)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other search project", "go")
	if err != nil {
		t.Fatal(err)
	}
	issueSearchCapture(t, ctx, db, other.ID, 3, stamp, "other", "Other same-organization issue", "prod", "v1", "error", nil)
	if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1 AND user_id=$2`, org.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{foreign.ID, "9223372036854775807"} {
		if _, _, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Project: project}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign/absent project %s error=%v", project, err)
		}
	}
	if _, _, err := db.Issues(ctx, "9223372036854775807", org.Slug, IssueFilter{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonmember error=%v", err)
	}
	if _, _, err := db.Issues(ctx, user.ID, org.Slug+"-absent", IssueFilter{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing org error=%v", err)
	}
	rows, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Project: p.ID, Environment: strings.Repeat("界", 128), Release: strings.Repeat("界", 255)})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != wanted {
		t.Fatalf("Unicode exact limits/cross-org exclusion rows=%+v total=%d err=%v", rows, total, err)
	}
	invalid := []IssueFilter{{Project: "0"}, {Project: "-1"}, {Project: "01"}, {Project: "9223372036854775808"}, {Status: "active"}, {Sort: "unknown"}, {Page: -1}, {Page: 1000001}, {Environment: strings.Repeat("界", 129)}, {Release: strings.Repeat("界", 256)}, {Query: strings.Repeat("x", 4097)}, {Query: "nul\x00"}, {Environment: "bad\xff"}, {Release: "nul\x00"}, {Status: "bad\xff"}, {Sort: "nul\x00"}, {Project: "bad\xff"}}
	for _, filter := range invalid {
		if _, _, err := db.Issues(ctx, user.ID, org.Slug, filter); !errors.Is(err, ErrInvalidIssueFilter) {
			t.Fatalf("invalid filter=%+v error=%v", filter, err)
		}
	}
	if rows, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Page: 1000000}); err != nil || total != 2 || len(rows) != 0 {
		t.Fatalf("maximum/past-end page rows=%v total=%d error=%v", rows, total, err)
	}
}

func TestIssueSearchSnapshotDuringRealTriage(t *testing.T) {
	ctx, db, user, org, p := telemetryStore(t)
	id := issueSearchCapture(t, ctx, db, p.ID, 1, time.Now(), "snapshot", "Snapshot triage", "prod", "v1", "error", nil)
	start := make(chan struct{})
	results := make(chan error, 4)
	var group sync.WaitGroup
	var present, absent atomic.Int64
	group.Go(func() {
		<-start
		for i := range 240 {
			status := "resolved"
			if i%2 != 0 {
				status = "unresolved"
			}
			if _, err := db.SetIssueStatus(ctx, user.ID, id, status); err != nil {
				results <- err
				return
			}
		}
		results <- nil
	})
	for range 3 {
		group.Go(func() {
			<-start
			for range 180 {
				rows, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{Status: "resolved"})
				if err != nil {
					results <- err
					return
				}
				if total != len(rows) || total > 1 {
					results <- fmt.Errorf("count/page came from different triage states: total=%d rows=%+v", total, rows)
					return
				}
				if total == 1 {
					present.Add(1)
					if rows[0].ID != id || rows[0].Status != "resolved" {
						results <- fmt.Errorf("wrong resolved snapshot: %+v", rows)
						return
					}
				} else {
					absent.Add(1)
				}
			}
			results <- nil
		})
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if present.Load() == 0 || absent.Load() == 0 {
		t.Fatalf("natural race did not observe both states: resolved=%d unresolved=%d", present.Load(), absent.Load())
	}
	t.Logf("natural triage snapshots resolved=%d unresolved=%d", present.Load(), absent.Load())
}
