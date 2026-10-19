//go:build integration

package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func feedbackQueryWithTags(t *testing.T, id, message string, tags map[string]string) (ev event.Event) {
	t.Helper()
	ev = modernFeedbackEvent(t, id, message, "")
	var payload map[string]any
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		t.Fatal(err)
	}
	payload["tags"] = tags
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ev.Data = raw
	return ev
}

func TestFeedbackCategorySearchAndCompatibleDeletion(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().UTC().Add(-time.Minute)
	errIssue := issueSearchCapture(t, f.ctx, f.db, f.project.ID, 801, stamp,
		"feedback-category-error", "Error with colliding tag", "production", "v1", "error",
		map[string]string{"issue.category": "feedback", "region": "east"})
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, errIssue, IssueUpdate{Status: "resolved"}); err != nil {
		t.Fatal(err)
	}
	fbID := "0123456789abcdef0123456789abcde1"
	fbWithEvent, fresh, err := f.db.Ingest(f.ctx, f.project.ID, feedbackQueryWithTags(t, fbID, "Tagged feedback", map[string]string{"region": "east"}))
	if err != nil || !fresh {
		t.Fatalf("feedback capture fresh=%v err=%v", fresh, err)
	}
	fbEmptyID := "0123456789abcdef0123456789abcde2"
	fbWithoutEvent, fresh, err := f.db.Ingest(f.ctx, f.project.ID, modernFeedbackEvent(t, fbEmptyID, "Retained feedback", ""))
	if err != nil || !fresh {
		t.Fatalf("empty-history feedback capture fresh=%v err=%v", fresh, err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, fbEmptyID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE issue_id=$1::bigint`, fbWithoutEvent).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("feedback issue retained %d event receipts, err=%v", remaining, err)
	}

	native := func(query string) []string {
		t.Helper()
		rows, _, err := f.db.Issues(f.ctx, f.owner.ID, f.org.Slug, IssueFilter{Project: f.project.ID, Query: query, Status: "all"})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		sort.Strings(ids)
		return ids
	}
	compatible := func(query string) []string {
		t.Helper()
		rows, err := f.db.CompatibleIssues(f.ctx, f.access, "", CompatibleIssueFilter{Query: query, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		ids := compatibleIssueIDs(rows)
		sort.Strings(ids)
		return ids
	}
	assertIDs := func(name string, got []string, want ...string) {
		t.Helper()
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s IDs=%v want=%v", name, got, want)
		}
	}

	assertIDs("native category independent of event retention", native("issue.category:feedback"), fbWithEvent, fbWithoutEvent)
	assertIDs("compatible category independent of event retention", compatible("issue.category:feedback"), fbWithEvent, fbWithoutEvent)
	assertIDs("native category, status and occurrence tag", native("issue.category:error is:resolved region:east"), errIssue)
	assertIDs("compatible category, status and occurrence tag", compatible("issue.category:error is:resolved region:east"), errIssue)
	assertIDs("literal colliding tag", native(`tags["issue.category"]:feedback`), errIssue)
	assertIDs("category excludes colliding tag", native("issue.category:feedback"), fbWithEvent, fbWithoutEvent)
	assertIDs("native omission includes both categories", native(""), errIssue, fbWithEvent, fbWithoutEvent)
	assertIDs("compatible omission includes both categories", compatible(""), errIssue, fbWithEvent, fbWithoutEvent)
	assertIDs("native negation", native("!issue.category:error"), fbWithEvent, fbWithoutEvent)
	assertIDs("compatible list values", compatible("issue.category:[error,feedback]"), errIssue, fbWithEvent, fbWithoutEvent)
	for category, want := range map[string]int{"feedback": 2, "error": 1} {
		rows, total, err := f.db.Issues(f.ctx, f.owner.ID, f.org.Slug, IssueFilter{Category: category})
		if err != nil || total != want || len(rows) != want {
			t.Fatal("native category selector", category, rows, total, err)
		}
		for _, row := range rows {
			if row.Category != category {
				t.Fatal("native category projection disagrees", row)
			}
		}
	}
	for _, category := range []string{"all", "Feedback", "*", "error\x00", "bad\xff"} {
		if _, _, err := f.db.Issues(f.ctx, f.owner.ID, f.org.Slug, IssueFilter{Category: category}); !errors.Is(err, ErrInvalidIssueFilter) {
			t.Fatal("invalid native category accepted", category, err)
		}
	}
	if _, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Query: "issue.category:feedback", Limit: 1000}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored"}}); err != nil {
		t.Fatal(err)
	}
	assertIDs("category-only bulk PUT includes empty feedback history", native("issue.category:feedback is:ignored"), fbWithEvent, fbWithoutEvent)
	assertIDs("category-only bulk PUT preserves error state", native("issue.category:error is:resolved"), errIssue)

	if err := f.db.DeleteCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{
		Query: "issue.category:feedback region:east", Status: "ignored", Limit: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	deleteStoreRequireIssues(t, f, false, fbWithEvent)
	deleteStoreRequireIssues(t, f, true, errIssue, fbWithoutEvent)
	if err := f.db.DeleteCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{
		Query: "issue.category:feedback", Status: "ignored", Limit: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	deleteStoreRequireIssues(t, f, false, fbWithoutEvent)
	deleteStoreRequireIssues(t, f, true, errIssue)
}
