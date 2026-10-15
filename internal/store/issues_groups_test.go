//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

func groupEvent(t *testing.T, n int, fingerprint, title string, stamp time.Time) event.Event {
	t.Helper()
	payload := map[string]any{
		"event_id": fmt.Sprintf("%032x", n), "message": title, "level": "error", "platform": "javascript",
		"timestamp": stamp.UTC().Format(time.RFC3339Nano), "environment": "env-" + strconv.Itoa(n), "release": "app@" + strconv.Itoa(n),
		"tags": map[string]string{"group": "merge proof"}, "extra": map[string]int{"ordinal": n},
		"contexts": map[string]any{"trace": map[string]string{"trace_id": "0123456789abcdef0123456789abcdef", "span_id": "0123456789abcdef"}},
	}
	if fingerprint != "" {
		payload["fingerprint"] = []string{fingerprint}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func groupCapture(t *testing.T, ctx context.Context, db *Store, project Project, ev event.Event) string {
	t.Helper()
	id, fresh, err := db.Ingest(ctx, project.ID, ev)
	if err != nil || !fresh || id == "" {
		t.Fatalf("capture %s issue=%s fresh=%v err=%v", ev.ID, id, fresh, err)
	}
	return id
}

func groupSummary(t *testing.T, ctx context.Context, db *Store, user User, id string, count int64, size int, first time.Time, latest event.Event) Issue {
	t.Helper()
	i := triageIssue(t, ctx, db, user.ID, id)
	if i.ID != id || i.Count != count || i.GroupSize != size || !i.FirstSeen.Equal(first) || !i.LastSeen.Equal(latest.Timestamp) || i.Title != latest.Title || i.Environment != latest.Environment || i.Release != latest.Release {
		t.Fatalf("group summary id=%s want count=%d size=%d first=%s latest=%s got=%+v", id, count, size, first, latest.ID, i)
	}
	return i
}

func groupDatabaseSnapshot(t *testing.T, ctx context.Context, db *Store, throughVersion int) map[string]string {
	t.Helper()
	rows, err := db.pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string, len(tables))
	for _, table := range tables {
		filter := ""
		if table == "schema_migrations" && throughVersion > 0 {
			filter = " WHERE version<=" + strconv.Itoa(throughVersion)
		}
		query := "SELECT COALESCE(jsonb_agg(payload ORDER BY payload::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(v) AS payload FROM " + pgx.Identifier{table}.Sanitize() + " v" + filter + ") history"
		var snapshot string
		if err := db.pool.QueryRow(ctx, query).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		result[table] = snapshot
	}
	return result
}

func groupRawSnapshot(t *testing.T, ctx context.Context, db *Store) string {
	t.Helper()
	var snapshot string
	if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e)-'issue_id'-'original_issue_id' ORDER BY id),'[]'::jsonb)::text FROM events e`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func groupRequireUnchanged(t *testing.T, ctx context.Context, db *Store, before map[string]string) {
	t.Helper()
	if after := groupDatabaseSnapshot(t, ctx, db, 0); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected/no-op operation changed persisted state")
	}
}

func groupLegacy015(t *testing.T, ctx context.Context, db *Store) {
	t.Helper()
	// Retire019's dependent routing catalog before restoring016's catalog in
	// this owned schema. Every015 column, application row and receipt remains
	// untouched; Open will apply both migrations again.
	_, err := db.pool.Exec(ctx, `DROP TABLE issue_hash_routes;
DROP INDEX issues_project_fingerprint;
ALTER TABLE issues ADD CONSTRAINT issues_project_id_fingerprint_key UNIQUE(project_id,fingerprint);
DELETE FROM schema_migrations WHERE version=19;
ALTER TABLE issues DROP CONSTRAINT issues_group_root_fkey;
ALTER TABLE issues DROP CONSTRAINT issues_group_older_root;
ALTER TABLE issues DROP CONSTRAINT issues_project_id_id_key;
ALTER TABLE issues ADD CONSTRAINT issues_merged_into_fkey FOREIGN KEY(merged_into) REFERENCES issues ON DELETE SET NULL;
DROP INDEX issues_group_members;
DROP INDEX events_origin_date;
DELETE FROM schema_migrations WHERE version=16`)
	if err != nil {
		t.Fatal(err)
	}
}

func groupCatalogSnapshot(t *testing.T, ctx context.Context, db *Store) string {
	t.Helper()
	var snapshot string
	err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object(
'constraints',(SELECT jsonb_agg(jsonb_build_object('table',c.conrelid::regclass::text,'name',c.conname,'definition',pg_get_constraintdef(c.oid)) ORDER BY c.conname,c.conrelid) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname=current_schema()),
'indexes',(SELECT jsonb_agg(to_jsonb(i) ORDER BY indexname) FROM pg_indexes i WHERE schemaname=current_schema()))::text`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func groupWaitBlocked(t *testing.T, ctx context.Context, observer pgx.Tx, blockerPID, minimum int) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := observer.Exec(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
			t.Fatal(err)
		}
		var pids []int
		if err := observer.QueryRow(ctx, `SELECT ARRAY(SELECT a.pid FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)) ORDER BY a.pid)`, blockerPID).Scan(&pids); err != nil {
			t.Fatal(err)
		}
		if len(pids) >= minimum {
			return pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected%d real blocked operations behind PID%d, got%v", minimum, blockerPID, pids)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func groupAwaitCancellation(t *testing.T, done <-chan error, label string) {
	t.Helper()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("canceled %s acknowledged success", label)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("canceled %s did not return", label)
	}
}

func groupAliasFaultSnapshot(t *testing.T, ctx context.Context, db *Store, id string) string {
	t.Helper()
	var snapshot string
	err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object(
'issue',(SELECT to_jsonb(i) FROM issues i WHERE id=$1::bigint),
'ignore',(SELECT to_jsonb(g) FROM issue_ignores g WHERE issue_id=$1::bigint),
'resolution',(SELECT to_jsonb(r) FROM issue_resolutions r WHERE issue_id=$1::bigint),
'activity',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM issue_activity a WHERE issue_id=$1::bigint))::text`, id).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestIssueGroupsMergeRoutingAndOriginalPartitions(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	// Trace links are a separate consumer of current ownership. Existing trace
	// tests do not change topology; SQL ownership assertions alone cannot catch
	// a trace reader accidentally using original ownership for its issue link.
	traceRefs := func(want map[string]string) {
		t.Helper()
		page, err := db.TraceErrors(ctx, owner.ID, org.Slug, "0123456789abcdef0123456789abcdef", project.ID, 1)
		if err != nil || page.Total != int64(len(want)) || len(page.Data) != len(want) {
			t.Fatalf("group trace references total=%d rows=%d err=%v", page.Total, len(page.Data), err)
		}
		got := map[string]string{}
		for _, item := range page.Data {
			got[item.EventID] = item.IssueID
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("group trace issue links=%v want=%v", got, want)
		}
	}
	a1 := groupEvent(t, 1, "partition-a", "A first", base.Add(time.Second))
	a2 := groupEvent(t, 2, "partition-a", "A latest", base.Add(4*time.Second))
	b1 := groupEvent(t, 3, "", "Default B", base.Add(3*time.Second))
	c1 := groupEvent(t, 4, "partition-c", "C latest", base.Add(4*time.Second))
	a := groupCapture(t, ctx, db, project, a1)
	if got := groupCapture(t, ctx, db, project, a2); got != a {
		t.Fatalf("custom fingerprint did not group: %s != %s", got, a)
	}
	b := groupCapture(t, ctx, db, project, b1)
	c := groupCapture(t, ctx, db, project, c1)
	muted, err := db.UpdateIssue(ctx, owner.ID, a, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 6)})
	if err != nil {
		t.Fatal(err)
	}
	rawBefore := groupRawSnapshot(t, ctx, db)
	first, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{c, b})
	if err != nil || !first.Changed || first.Issue.ID != b {
		t.Fatalf("first merge=%+v err=%v", first, err)
	}
	groupSummary(t, ctx, db, owner, b, 2, 2, b1.Timestamp, c1)
	merged, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{c, a})
	if err != nil || !merged.Changed || merged.Issue.ID != a {
		t.Fatalf("flatten through alias=%+v err=%v", merged, err)
	}
	i := groupSummary(t, ctx, db, owner, a, 4, 3, a1.Timestamp, c1)
	if !reflect.DeepEqual(i.Ignore, muted.Ignore) || captureProgressCounter(t, ctx, db, a) != 2 || captureProgressCounter(t, ctx, db, b) != 1 || captureProgressCounter(t, ctx, db, c) != 1 || groupRawSnapshot(t, ctx, db) != rawBefore {
		t.Fatal("historical merge changed raw events or capture/mute progress")
	}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	if id, fresh, err := db.Ingest(ctx, project.ID, b1); err != nil || fresh || id != a {
		t.Fatalf("old SDK identity retry root=%s fresh=%v err=%v", id, fresh, err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	if noOp, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{b, c}); err != nil || noOp.Changed || noOp.Issue.ID != a {
		t.Fatalf("already combined merge=%+v err=%v", noOp, err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	b2 := groupEvent(t, 5, "", "Default B", base.Add(5*time.Second))
	lateC := groupEvent(t, 6, "partition-c", "C late arrival", base)
	for _, ev := range []event.Event{b2, lateC} {
		if id := groupCapture(t, ctx, db, project, ev); id != a {
			t.Fatalf("future original fingerprint routed to %s instead of %s", id, a)
		}
	}
	i = groupSummary(t, ctx, db, owner, a, 6, 3, lateC.Timestamp, b2)
	if i.Ignore == nil || i.Ignore.Remaining == nil || *i.Ignore.Remaining != 4 {
		t.Fatalf("fresh captures did not consume root mute only: %+v", i.Ignore)
	}
	alias, err := db.IssueDetail(ctx, owner.ID, c)
	if err != nil || alias.Issue.ID != a || len(alias.Events) != 6 || alias.Events[0].ID != b2.ID {
		t.Fatalf("alias detail=%+v err=%v", alias.Issue, err)
	}
	filtered, total, err := db.Issues(ctx, owner.ID, org.Slug, IssueFilter{Project: project.ID, Environment: c1.Environment, Release: c1.Release})
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != a || filtered[0].Count != 6 {
		t.Fatalf("historical filter exposed aliases/lost root: rows=%+v total=%d err=%v", filtered, total, err)
	}
	for eventID, origin := range map[string]string{a1.ID: a, a2.ID: a, b1.ID: b, b2.ID: b, c1.ID: c, lateC.ID: c} {
		var current, original string
		if err := db.pool.QueryRow(ctx, "SELECT issue_id::text,original_issue_id::text FROM events WHERE event_id=$1", eventID).Scan(&current, &original); err != nil || current != a || original != origin {
			t.Fatalf("event %s current=%s origin=%s want=%s err=%v", eventID, current, original, origin, err)
		}
	}
	traceRefs(map[string]string{a1.ID: a, a2.ID: a, b1.ID: a, b2.ID: a, c1.ID: a, lateC.ID: a})
	rawBefore = groupRawSnapshot(t, ctx, db)
	split, err := db.UnmergeIssue(ctx, owner.ID, a, []string{c})
	if err != nil || !split.Changed || !reflect.DeepEqual(split.DetachedIssueIDs, []string{c}) || split.RemainingIssueID == nil || *split.RemainingIssueID != a {
		t.Fatalf("subset split=%+v err=%v", split, err)
	}
	groupSummary(t, ctx, db, owner, a, 4, 2, a1.Timestamp, b2)
	groupSummary(t, ctx, db, owner, c, 2, 1, lateC.Timestamp, c1)
	before = groupDatabaseSnapshot(t, ctx, db, 0)
	if retry, err := db.UnmergeIssue(ctx, owner.ID, a, []string{c}); err != nil || retry.Changed || len(retry.DetachedIssueIDs) != 0 {
		t.Fatalf("lost split-response retry=%+v err=%v", retry, err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	split, err = db.UnmergeIssue(ctx, owner.ID, a, []string{a})
	if err != nil || !split.Changed || !reflect.DeepEqual(split.DetachedIssueIDs, []string{a}) || split.RemainingIssueID == nil || *split.RemainingIssueID != b || split.Issue.ID != a {
		t.Fatalf("root split=%+v err=%v", split, err)
	}
	i = groupSummary(t, ctx, db, owner, a, 2, 1, a1.Timestamp, a2)
	if i.Ignore == nil || i.Ignore.Remaining == nil || *i.Ignore.Remaining != 4 || captureProgressCounter(t, ctx, db, a) != 4 {
		t.Fatalf("root separation reset capture progress: %+v", i.Ignore)
	}
	groupSummary(t, ctx, db, owner, b, 2, 1, b1.Timestamp, b2)
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{c, b, a}); err != nil {
		t.Fatal(err)
	}
	split, err = db.UnmergeIssue(ctx, owner.ID, a, []string{c, a, b})
	if err != nil || !split.Changed || !reflect.DeepEqual(split.DetachedIssueIDs, []string{a, b, c}) || split.RemainingIssueID != nil || split.Issue.ID != a || split.Issue.GroupSize != 1 {
		t.Fatalf("all partitions split=%+v err=%v", split, err)
	}
	if groupRawSnapshot(t, ctx, db) != rawBefore {
		t.Fatal("merge/split copied, deleted or rewrote raw event columns")
	}
	for _, item := range []struct {
		id     string
		latest event.Event
		first  time.Time
	}{{a, a2, a1.Timestamp}, {b, b2, b1.Timestamp}, {c, c1, lateC.Timestamp}} {
		groupSummary(t, ctx, db, owner, item.id, 2, 1, item.first, item.latest)
	}
	traceRefs(map[string]string{a1.ID: a, a2.ID: a, b1.ID: b, b2.ID: b, c1.ID: c, lateC.ID: c})
}

func TestIssueGroupsEmptyPartitionFirstFreshSummary(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	a := groupCapture(t, ctx, db, project, groupEvent(t, 1, "empty-a", "Surviving root", base))
	old := groupEvent(t, 2, "empty-b", "Legacy retained title", base.Add(time.Hour))
	b := groupCapture(t, ctx, db, project, old)
	// This explicitly represents a valid legacy partition whose historical raw
	// occurrence was no longer retained. Its receipt and first_seen remain.
	if _, err := db.pool.Exec(ctx, "DELETE FROM events WHERE issue_id=$1::bigint", b); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UnmergeIssue(ctx, owner.ID, a, []string{b}); err != nil {
		t.Fatal(err)
	}
	empty := triageIssue(t, ctx, db, owner.ID, b)
	if empty.Count != 0 || empty.Title != old.Title || !empty.FirstSeen.Equal(old.Timestamp) || !empty.LastSeen.Equal(old.Timestamp) || captureProgressCounter(t, ctx, db, b) != 1 {
		t.Fatalf("empty partition lost retained identity/progress: %+v", empty)
	}
	origins, err := db.IssueOrigins(ctx, owner.ID, b, "", 1)
	if err != nil || origins.Total != 1 || len(origins.Data) != 1 || origins.Data[0].Count != 0 || origins.Data[0].FirstSeen != nil || origins.Data[0].LastSeen != nil || origins.Data[0].Title != old.Title {
		t.Fatalf("empty origin=%+v err=%v", origins, err)
	}
	fresh := groupEvent(t, 3, "empty-b", "Only retained occurrence", base.Add(-time.Hour))
	if got := groupCapture(t, ctx, db, project, fresh); got != b {
		t.Fatalf("empty partition capture root=%s want=%s", got, b)
	}
	groupSummary(t, ctx, db, owner, b, 1, 1, fresh.Timestamp, fresh)
	firstSeen := 0
	for _, activity := range triageActivities(t, ctx, db, owner.ID, b) {
		if activity.Type == "first_seen" {
			firstSeen++
		}
	}
	if firstSeen != 1 || captureProgressCounter(t, ctx, db, b) != 2 {
		t.Fatalf("first fresh raw repeated capture receipt: first_seen=%d", firstSeen)
	}
}

func TestIssueGroupsPreserveRootPolicyAndOriginalHistory(t *testing.T) {
	f := resolutionStore(t)
	ctx, db, owner, org, project := f.ctx, f.db, f.owner, f.org, f.project
	author := triageMember(t, ctx, db, org, "group-author", "member")
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	config := alertConfig("Grouping preserves queued delivery")
	config.Quantity = 1
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	a := groupCapture(t, ctx, db, project, groupEvent(t, 1, "history-a", "Root body", base))
	b := groupCapture(t, ctx, db, project, groupEvent(t, 2, "history-b", "Absorbed mute", base.Add(time.Second)))
	c := groupCapture(t, ctx, db, project, groupEvent(t, 3, "history-c", "Absorbed resolution", base.Add(2*time.Second)))
	f.release(t, "app@1000")
	rootBefore := f.resolve(t, a, "release", "app@1000")
	if _, err := db.UpdateIssue(ctx, owner.ID, a, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	rootBefore = triageIssue(t, ctx, db, owner.ID, a)
	mutedBefore, err := db.UpdateIssue(ctx, owner.ID, b, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 5), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: author.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	resolvedBefore := f.resolve(t, c, "release", "app@1000")
	if _, err := db.UpdateIssue(ctx, owner.ID, c, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: author.ID}}}); err != nil {
		t.Fatal(err)
	}
	resolvedBefore = triageIssue(t, ctx, db, owner.ID, c)
	rootComment, err := db.AddComment(ctx, owner.ID, a, "Root comment")
	if err != nil {
		t.Fatal(err)
	}
	memberComment, err := db.AddComment(ctx, author.ID, b, "Original member comment")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddComment(ctx, owner.ID, c, "Other original comment"); err != nil {
		t.Fatal(err)
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(deliveries) == 0 {
		t.Fatalf("queued delivery positive control rows=%d err=%v", len(deliveries), err)
	}
	var rootReceipt string
	if err := db.pool.QueryRow(ctx, "SELECT to_jsonb(r)::text FROM issue_resolutions r WHERE issue_id=$1::bigint", a).Scan(&rootReceipt); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{c, b, a}); err != nil {
		t.Fatal(err)
	}
	root := triageIssue(t, ctx, db, owner.ID, a)
	var afterReceipt string
	if err := db.pool.QueryRow(ctx, "SELECT to_jsonb(r)::text FROM issue_resolutions r WHERE issue_id=$1::bigint", a).Scan(&afterReceipt); err != nil || rootReceipt != afterReceipt || root.Status != rootBefore.Status || !reflect.DeepEqual(root.Resolution, rootBefore.Resolution) || !reflect.DeepEqual(root.ResolvedAt, rootBefore.ResolvedAt) || !reflect.DeepEqual(root.AssignedTo, rootBefore.AssignedTo) {
		t.Fatalf("merge changed retained root policy/receipt: %+v err=%v", root, err)
	}
	after := groupDatabaseSnapshot(t, ctx, db, 0)
	for table, snapshot := range before {
		switch table {
		case "issues", "events", "issue_activity", "issue_ignores", "issue_resolutions":
			continue
		}
		if after[table] != snapshot {
			t.Fatalf("historical grouping changed unrelated persisted table %s", table)
		}
	}
	for id, previous := range map[string]Issue{b: mutedBefore, c: resolvedBefore} {
		var status string
		var active bool
		if err := db.pool.QueryRow(ctx, `SELECT status, resolved_at IS NOT NULL OR regressed_at IS NOT NULL OR EXISTS(SELECT 1 FROM issue_ignores WHERE issue_id=issues.id) OR EXISTS(SELECT 1 FROM issue_resolutions WHERE issue_id=issues.id) FROM issues WHERE id=$1::bigint`, id).Scan(&status, &active); err != nil || status != "unresolved" || active {
			t.Fatalf("absorbed state id=%s status=%s active=%v err=%v", id, status, active, err)
		}
		found := 0
		for _, activity := range triageActivities(t, ctx, db, owner.ID, a) {
			if activity.Type != "merged" {
				continue
			}
			var receipt struct {
				SourceIssueID string `json:"sourceIssueId"`
				MergedInto    string `json:"mergedInto"`
				Previous      Issue  `json:"previous"`
			}
			if err := json.Unmarshal(activity.Data, &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.SourceIssueID == id {
				found++
				p := receipt.Previous
				var body map[string]json.RawMessage
				if err := json.Unmarshal(activity.Data, &body); err != nil {
					t.Fatal(err)
				}
				var prior map[string]json.RawMessage
				if err := json.Unmarshal(body["previous"], &prior); err != nil {
					t.Fatal(err)
				}
				if len(body) != 3 || len(prior) != 7 || activity.User == nil || activity.User.ID != owner.ID {
					t.Fatalf("absorbed activity contains unsafe/unexpected fields or actor: %s", activity.Data)
				}
				if receipt.MergedInto != a || p.Status != previous.Status || p.Count != previous.Count || !reflect.DeepEqual(p.Ignore, previous.Ignore) || !reflect.DeepEqual(p.Resolution, previous.Resolution) || !reflect.DeepEqual(p.AssignedTo, previous.AssignedTo) || !reflect.DeepEqual(p.ResolvedAt, previous.ResolvedAt) || !reflect.DeepEqual(p.RegressedAt, previous.RegressedAt) {
					t.Fatalf("absorbed audit lost previous state: %s", activity.Data)
				}
			}
		}
		if found != 1 {
			t.Fatalf("absorbed id=%s audit receipts=%d want=1", id, found)
		}
	}
	detail, err := db.IssueDetail(ctx, owner.ID, b)
	if err != nil || detail.Issue.ID != a || detail.CommentTotal != 3 || len(detail.Comments) != 3 {
		t.Fatalf("combined comment detail total=%d rows=%+v err=%v", detail.CommentTotal, detail.Comments, err)
	}
	comments, total, err := db.IssueComments(ctx, author.ID, a, 1)
	if err != nil || total != 3 || len(comments) != 3 {
		t.Fatalf("combined member comments=%+v total=%d err=%v", comments, total, err)
	}
	for _, comment := range comments {
		if comment.CanEdit != (comment.ID == memberComment.ID) {
			t.Fatalf("combined comment moderation changed author rights: %+v", comment)
		}
	}
	edited, err := db.EditComment(ctx, author.ID, a, memberComment.ID, "Edited through current root")
	if err != nil || edited.User.ID != author.ID || !edited.DateCreated.Equal(memberComment.DateCreated) {
		t.Fatalf("editing visible member comment lost ownership: %+v err=%v", edited, err)
	}
	combinedComment, err := db.AddComment(ctx, owner.ID, a, "Written while combined")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UnmergeIssue(ctx, owner.ID, a, []string{c, b}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{b, c} {
		i := triageIssue(t, ctx, db, owner.ID, id)
		if i.Status != "unresolved" || i.Ignore != nil || i.Resolution != nil || i.ResolvedAt != nil || i.RegressedAt != nil || i.AssignedTo == nil || i.AssignedTo.ID != author.ID {
			t.Fatalf("detached source resurrected policy/lost assignment: %+v", i)
		}
	}
	memberDetail, err := db.IssueDetail(ctx, owner.ID, b)
	if err != nil || memberDetail.CommentTotal != 1 || len(memberDetail.Comments) != 1 || memberDetail.Comments[0].ID != memberComment.ID || memberDetail.Comments[0].Text != edited.Text || !memberDetail.Comments[0].DateCreated.Equal(memberComment.DateCreated) {
		t.Fatalf("original comment did not return with its partition: %+v err=%v", memberDetail.Comments, err)
	}
	for _, activity := range triageActivities(t, ctx, db, owner.ID, b) {
		if activity.Type == "comment_edited" {
			t.Fatal("combined-root edit activity moved to detached original")
		}
	}
	rootDetail, err := db.IssueDetail(ctx, owner.ID, a)
	if err != nil || rootDetail.CommentTotal != 2 || len(rootDetail.Comments) != 2 || !reflect.DeepEqual(rootDetail.Issue.Resolution, rootBefore.Resolution) {
		t.Fatalf("root history/policy after split=%+v err=%v", rootDetail, err)
	}
	ids := map[string]bool{}
	for _, comment := range rootDetail.Comments {
		ids[comment.ID] = true
	}
	if !ids[rootComment.ID] || !ids[combinedComment.ID] {
		t.Fatalf("root comments changed ownership: %+v", rootDetail.Comments)
	}
}

func TestIssueGroupsSelectionIsAtomicAndMembershipCurrent(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	ids := make([]string, 3)
	for n := range ids {
		ids[n] = groupCapture(t, ctx, db, project, groupEvent(t, n+1, fmt.Sprintf("scope-%d", n), "Scope proof", base))
	}
	a, b, c := ids[0], ids[1], ids[2]
	otherProject, err := db.CreateProject(ctx, owner.ID, org.Slug, "Other group project", "go")
	if err != nil {
		t.Fatal(err)
	}
	other := groupCapture(t, ctx, db, otherProject, groupEvent(t, 20, "other-project", "Other project", base))
	foreignOrg := triageForeignOrganization(t, ctx, db, "group-foreign")
	foreignOwner := triageMember(t, ctx, db, foreignOrg, "group-foreign-owner", "owner")
	foreignProject, err := db.CreateProject(ctx, foreignOwner.ID, foreignOrg.Slug, "Foreign group", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreign := groupCapture(t, ctx, db, foreignProject, groupEvent(t, 30, "foreign", "Foreign issue", base))
	if _, err := db.IssueDetail(ctx, foreignOwner.ID, foreign); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ids  []string
		want error
	}{
		{"too few", []string{a}, ErrInvalidIssueChange}, {"duplicate", []string{a, a}, ErrInvalidIssueChange},
		{"noncanonical", []string{a, "01"}, ErrInvalidIssueChange}, {"mixed project", []string{a, b, other}, ErrInvalidIssueChange},
		{"missing", []string{a, b, "9223372036854775807"}, ErrNotFound}, {"foreign", []string{a, b, foreign}, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := groupDatabaseSnapshot(t, ctx, db, 0)
			_, err := db.MergeIssues(ctx, owner.ID, org.Slug, tc.ids)
			triageRequireError(t, err, tc.want)
			groupRequireUnchanged(t, ctx, db, before)
		})
	}
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{b, c}); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	_, err = db.UnmergeIssue(ctx, owner.ID, a, []string{a, c})
	triageRequireError(t, err, ErrInvalidIssueChange)
	groupRequireUnchanged(t, ctx, db, before)
	if result, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{a, c}); err != nil || !result.Changed || result.Issue.ID != a {
		t.Fatalf("positive flatten=%+v err=%v", result, err)
	}
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"triage", func() error { _, err := db.UpdateIssue(ctx, owner.ID, b, IssueUpdate{Status: "resolved"}); return err }},
		{"comment", func() error { _, err := db.AddComment(ctx, owner.ID, b, "Stale alias comment"); return err }},
		{"unmerge", func() error { _, err := db.UnmergeIssue(ctx, owner.ID, b, []string{b}); return err }},
	} {
		t.Run("stale "+operation.name, func(t *testing.T) {
			before := groupDatabaseSnapshot(t, ctx, db, 0)
			var merged *IssueMergedError
			if err := operation.run(); !errors.As(err, &merged) || merged.MergedInto != a {
				t.Fatalf("stale alias err=%v target=%+v", err, merged)
			}
			groupRequireUnchanged(t, ctx, db, before)
		})
	}
	before = groupDatabaseSnapshot(t, ctx, db, 0)
	_, err = db.UnmergeIssue(ctx, owner.ID, a, []string{b, "9223372036854775807"})
	triageRequireError(t, err, ErrNotFound)
	groupRequireUnchanged(t, ctx, db, before)
	if _, err := db.pool.Exec(ctx, "DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint", owner.ID, org.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, ctx, db, 0)
	_, err = db.MergeIssues(ctx, owner.ID, org.Slug, []string{a, c})
	triageRequireError(t, err, ErrNotFound)
	groupRequireUnchanged(t, ctx, db, before)
}

func TestIssueGroupsOriginsPagesAndLiteralSearch(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	ids := make([]string, 26)
	seeded := make([]event.Event, len(ids))
	for n := range ids {
		title := fmt.Sprintf("Origin %02d", n)
		if n == 1 {
			title = "Literal %_\\ needle"
		}
		if n == 2 {
			title = "Literal AX\\ needle"
		}
		seeded[n] = groupEvent(t, n+1, title, title, base.Add(time.Duration(n)*time.Second))
		ids[n] = groupCapture(t, ctx, db, project, seeded[n])
	}
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, ids); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ page, start, end int }{{1, 0, 25}, {2, 25, 26}, {100, 26, 26}} {
		page, err := db.IssueOrigins(ctx, owner.ID, ids[25], "", tc.page)
		if err != nil || page.Total != 26 || page.Page != tc.page || page.PageSize != 25 || page.IssueID != ids[0] || len(page.Data) != tc.end-tc.start {
			t.Fatalf("origins page%d=%+v err=%v", tc.page, page, err)
		}
		for n, row := range page.Data {
			index := tc.start + n
			stamp := base.Add(time.Duration(index) * time.Second)
			if row.ID != ids[index] || row.Count != 1 || row.IsRoot != (index == 0) || row.FirstSeen == nil || row.LastSeen == nil || !row.FirstSeen.Equal(stamp) || !row.LastSeen.Equal(stamp) || row.FirstSeen.Location() != time.UTC || row.LastSeen.Location() != time.UTC || row.Fingerprint == "" {
				t.Fatalf("origins row index%d=%+v", index, row)
			}
		}
	}
	literal, err := db.IssueOrigins(ctx, owner.ID, ids[0], "%_\\", 1)
	if err != nil || literal.Total != 1 || len(literal.Data) != 1 || literal.Data[0].ID != ids[1] {
		t.Fatalf("literal metacharacters=%+v err=%v", literal, err)
	}
	byID, err := db.IssueOrigins(ctx, owner.ID, ids[0], ids[25], 1)
	wantIDMatches := []string{}
	for n, ev := range seeded {
		if strings.Contains(ids[n], ids[25]) || strings.Contains(strings.ToLower(ev.Title), ids[25]) || strings.Contains(strings.ToLower(ev.Fingerprint), ids[25]) {
			wantIDMatches = append(wantIDMatches, ids[n])
		}
	}
	gotIDMatches := []string{}
	for _, row := range byID.Data {
		gotIDMatches = append(gotIDMatches, row.ID)
	}
	if err != nil || byID.Total != len(wantIDMatches) || !reflect.DeepEqual(gotIDMatches, wantIDMatches) {
		t.Fatalf("ID search=%+v err=%v", byID, err)
	}
	byFingerprint, err := db.IssueOrigins(ctx, owner.ID, ids[0], strings.ToUpper(literal.Data[0].Fingerprint), 1)
	if err != nil || byFingerprint.Total != 1 || len(byFingerprint.Data) != 1 || byFingerprint.Data[0].ID != ids[1] {
		t.Fatalf("case-insensitive fingerprint search=%+v err=%v", byFingerprint, err)
	}
	if page, err := db.IssueOrigins(ctx, owner.ID, ids[0], strings.Repeat("🐛", 50), 1); err != nil || page.Total != 0 || len(page.Data) != 0 {
		t.Fatalf("200-byte query=%+v err=%v", page, err)
	}
	for _, query := range []string{strings.Repeat("🐛", 50) + "x", "bad\x00query", string([]byte{0xff})} {
		_, err := db.IssueOrigins(ctx, owner.ID, ids[0], query, 1)
		triageRequireError(t, err, ErrInvalidIssueChange)
	}
}
