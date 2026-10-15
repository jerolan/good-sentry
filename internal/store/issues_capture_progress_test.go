//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/event"
)

func captureProgressCounter(t *testing.T, ctx context.Context, db *Store, id string) int64 {
	t.Helper()
	var value int64
	if err := db.pool.QueryRow(ctx, "SELECT receipt_count FROM issues WHERE id=$1::bigint", id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestIssueCaptureProgressSurvivesHistoricalCountChanges(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	policy := IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 3)}
	original, err := db.UpdateIssue(ctx, owner.ID, id, policy)
	if err != nil || original.Ignore == nil || original.Ignore.Until == nil {
		t.Fatalf("initial count mute: %+v %v", original.Ignore, err)
	}
	assertRemaining := func(t *testing.T, remaining int64) {
		t.Helper()
		i := triageIssue(t, ctx, db, owner.ID, id)
		if i.Status != "ignored" || i.Ignore == nil || i.Ignore.Remaining == nil || *i.Ignore.Remaining != remaining || i.Ignore.Until == nil || !i.Ignore.Until.Equal(*original.Ignore.Until) {
			var actual any = "null"
			if i.Ignore != nil && i.Ignore.Remaining != nil {
				actual = *i.Ignore.Remaining
			}
			t.Fatalf("historical count changed mute progress: count=%d status=%s remaining=%v want=%d", i.Count, i.Status, actual, remaining)
		}
	}
	// These SQL changes simulate summary recomputation by a future grouping
	// operation. This test proves capture progress, not merge/unmerge itself.
	if _, err := db.pool.Exec(ctx, "UPDATE issues SET count=100 WHERE id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	assertRemaining(t, 3) // Behavioral RED precedes any query of the new column.
	if got := captureProgressCounter(t, ctx, db, id); got != 1 {
		t.Fatalf("summary increase changed receipt_count=%d want=1", got)
	}
	history := triageActivities(t, ctx, db, owner.ID, id)
	for _, change := range []IssueUpdate{policy, {Status: "ignored"}} {
		if _, err := db.UpdateIssue(ctx, owner.ID, id, change); err != nil {
			t.Fatal(err)
		}
		assertRemaining(t, 3)
		if !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
			t.Fatal("same policy/status added activity")
		}
	}
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	assertRemaining(t, 3)
	fresh := triageEvent(t, fmt.Sprintf("%032x", 2), time.Now())
	if _, admitted, err := db.Ingest(ctx, project.ID, fresh); err != nil || !admitted {
		t.Fatalf("first new capture admitted=%v err=%v", admitted, err)
	}
	assertRemaining(t, 2)
	history = triageActivities(t, ctx, db, owner.ID, id)
	if _, admitted, err := db.Ingest(ctx, project.ID, fresh); err != nil || admitted {
		t.Fatalf("duplicate capture admitted=%v err=%v", admitted, err)
	}
	transaction, err := event.Normalize(json.RawMessage(`{"event_id":"00000000000000000000000000000003","type":"transaction","transaction":"/capture-progress","start_timestamp":"2020-01-01T00:00:00Z","timestamp":"2020-01-01T00:00:01Z","contexts":{"trace":{"trace_id":"0123456789abcdef0123456789abcdef","span_id":"0123456789abcdef","op":"http.server"}}}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if issue, admitted, err := db.Ingest(ctx, project.ID, transaction); err != nil || !admitted || issue != "" {
		t.Fatalf("transaction issue=%s admitted=%v err=%v", issue, admitted, err)
	}
	assertRemaining(t, 2)
	if got := captureProgressCounter(t, ctx, db, id); got != 2 || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatalf("retry/transaction advanced progress or history: receipt_count=%d", got)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE issues SET count=0 WHERE id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	assertRemaining(t, 2)
	if _, err := db.UpdateIssue(ctx, owner.ID, id, policy); err != nil {
		t.Fatal(err)
	}
	assertRemaining(t, 2)
	if _, admitted, err := db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", 4), time.Now())); err != nil || !admitted {
		t.Fatalf("second new capture admitted=%v err=%v", admitted, err)
	}
	assertRemaining(t, 1)
	last := triageEvent(t, fmt.Sprintf("%032x", 5), time.Now())
	if _, admitted, err := db.Ingest(ctx, project.ID, last); err != nil || !admitted {
		t.Fatalf("Nth new capture admitted=%v err=%v", admitted, err)
	}
	ended := triageIssue(t, ctx, db, owner.ID, id)
	if ended.Status != "unresolved" || ended.Ignore != nil || ended.AssignedTo == nil || ended.AssignedTo.ID != owner.ID || captureProgressCounter(t, ctx, db, id) != 4 {
		t.Fatalf("Nth additional capture did not end mute exactly: %+v", ended)
	}
	if _, admitted, err := db.Ingest(ctx, project.ID, last); err != nil || admitted {
		t.Fatalf("Nth capture retry admitted=%v err=%v", admitted, err)
	}
	if _, admitted, err := db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", 6), time.Now())); err != nil || !admitted {
		t.Fatalf("post-mute capture admitted=%v err=%v", admitted, err)
	}
	firstSeen, unignored := 0, []IssueActivity{}
	for _, activity := range triageActivities(t, ctx, db, owner.ID, id) {
		if activity.Type == "first_seen" {
			firstSeen++
		}
		if activity.Type == "unignored" {
			unignored = append(unignored, activity)
		}
	}
	if firstSeen != 1 || len(unignored) != 1 || captureProgressCounter(t, ctx, db, id) != 5 {
		t.Fatalf("summary decrease repeated receipt activity: first_seen=%d unignored=%d", firstSeen, len(unignored))
	}
	var expiry struct {
		Reason, EventID string
		Ignore          IssueIgnore
	}
	if err := json.Unmarshal(unignored[0].Data, &expiry); err != nil || expiry.Reason != "count" || expiry.EventID != last.ID || expiry.Ignore.Remaining == nil || *expiry.Ignore.Remaining != 0 {
		t.Fatalf("count expiry receipt=%s err=%v", unignored[0].Data, err)
	}
	beforeNewPolicy := triageIssue(t, ctx, db, owner.ID, id)
	if beforeNewPolicy.Count != 3 || captureProgressCounter(t, ctx, db, id) != 5 {
		t.Fatalf("new-policy fixture must have historical count=3 and receipts=5: count=%d", beforeNewPolicy.Count)
	}
	remuted, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 2)})
	if err != nil || remuted.Status != "ignored" || remuted.Ignore == nil || remuted.Ignore.Remaining == nil || *remuted.Ignore.Remaining != 2 {
		t.Fatalf("new policy used historical count instead of receipt baseline: %+v err=%v", remuted.Ignore, err)
	}
	if _, admitted, err := db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", 7), time.Now())); err != nil || !admitted {
		t.Fatalf("new-policy first capture admitted=%v err=%v", admitted, err)
	}
	progress := triageIssue(t, ctx, db, owner.ID, id)
	if progress.Status != "ignored" || progress.Ignore == nil || progress.Ignore.Remaining == nil || *progress.Ignore.Remaining != 1 || captureProgressCounter(t, ctx, db, id) != 6 {
		t.Fatalf("new-policy first capture did not consume exactly one: %+v", progress.Ignore)
	}
	newLast := triageEvent(t, fmt.Sprintf("%032x", 8), time.Now())
	if _, admitted, err := db.Ingest(ctx, project.ID, newLast); err != nil || !admitted {
		t.Fatalf("new-policy final capture admitted=%v err=%v", admitted, err)
	}
	ended = triageIssue(t, ctx, db, owner.ID, id)
	if ended.Status != "unresolved" || ended.Ignore != nil || captureProgressCounter(t, ctx, db, id) != 7 {
		t.Fatalf("new policy did not expire on its second capture: %+v", ended)
	}
	firstSeen, unignored = 0, nil
	for _, activity := range triageActivities(t, ctx, db, owner.ID, id) {
		if activity.Type == "first_seen" {
			firstSeen++
		}
		if activity.Type == "unignored" {
			unignored = append(unignored, activity)
		}
	}
	if firstSeen != 1 || len(unignored) != 2 {
		t.Fatalf("new policy expiry must add one receipt: first_seen=%d unignored=%d", firstSeen, len(unignored))
	}
	newExpiries := 0
	for _, activity := range unignored {
		if err := json.Unmarshal(activity.Data, &expiry); err != nil {
			t.Fatal(err)
		}
		if expiry.EventID == newLast.ID {
			newExpiries++
			if expiry.Reason != "count" || expiry.Ignore.Remaining == nil || *expiry.Ignore.Remaining != 0 {
				t.Fatalf("new-policy count expiry receipt=%s", activity.Data)
			}
		}
	}
	if newExpiries != 1 {
		t.Fatalf("new policy expiry receipts=%d want=1", newExpiries)
	}
}

// Snapshot every column and row in the isolated application schema, excluding
// only the new counter and receipt015. This is independent of migration SQL.
func captureProgressHistory(t *testing.T, ctx context.Context, db *Store) map[string]string {
	t.Helper()
	rows, err := db.pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	snapshot := make(map[string]string, len(tables))
	for _, table := range tables {
		value, filter := "to_jsonb(v)", ""
		if table == "issues" {
			value += "-'receipt_count'"
		}
		if table == "schema_migrations" {
			filter = " WHERE version<=14"
		}
		query := "SELECT COALESCE(jsonb_agg(payload ORDER BY payload::text),'[]'::jsonb)::text FROM (SELECT " + value + " AS payload FROM " + pgx.Identifier{table}.Sanitize() + " v" + filter + ") historical"
		var data string
		if err := db.pool.QueryRow(ctx, query).Scan(&data); err != nil {
			t.Fatal(err)
		}
		snapshot[table] = data
	}
	return snapshot
}

func TestIssueCaptureProgressMigration015BackfillsOnceAndReopens(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	var id string
	for n := 1; n <= 7; n++ {
		var fresh bool
		var err error
		id, fresh, err = db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", n), time.Now()))
		if err != nil || !fresh {
			t.Fatalf("legacy capture%d fresh=%v err=%v", n, fresh, err)
		}
	}
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(120, 8), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddComment(ctx, owner.ID, id, "Receipt progress upgrade preserves this comment."); err != nil {
		t.Fatal(err)
	}
	for n := 8; n <= 10; n++ {
		if _, fresh, err := db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", n), time.Now())); err != nil || !fresh {
			t.Fatalf("legacy muted capture%d fresh=%v err=%v", n, fresh, err)
		}
	}
	legacy := triageIssue(t, ctx, db, owner.ID, id)
	var baseline int64
	if err := db.pool.QueryRow(ctx, "SELECT baseline FROM issue_ignores WHERE issue_id=$1::bigint", id).Scan(&baseline); err != nil || baseline != 7 || legacy.Count != 10 || legacy.Ignore == nil || legacy.Ignore.Remaining == nil || *legacy.Ignore.Remaining != 5 {
		t.Fatalf("014 fixture baseline=%d issue=%+v err=%v", baseline, legacy, err)
	}
	// Reconstruct only the absent015 column/receipt in this isolated schema.
	// All history above came from real capture/triage APIs and remains untouched.
	if _, err := db.pool.Exec(ctx, "ALTER TABLE issues DROP COLUMN IF EXISTS receipt_count; DELETE FROM schema_migrations WHERE version=15"); err != nil {
		t.Fatal(err)
	}
	before := captureProgressHistory(t, ctx, db)
	url := db.pool.Config().ConnString()
	db.Close()
	upgraded, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	if after := captureProgressHistory(t, ctx, upgraded); !reflect.DeepEqual(before, after) {
		t.Fatal("015 changed historical columns/rows or001..014 migration receipts")
	}
	if got := captureProgressCounter(t, ctx, upgraded, id); got != 10 {
		t.Fatalf("015 backfill=%d want=10", got)
	}
	if got := triageIssue(t, ctx, upgraded, owner.ID, id); !reflect.DeepEqual(got, legacy) {
		t.Fatalf("015 changed exposed mute/assignment/progress: %+v", got)
	}
	var receipt string
	if err := upgraded.pool.QueryRow(ctx, "SELECT to_jsonb(m)::text FROM schema_migrations m WHERE version=15").Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := upgraded.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", 11), time.Now())); err != nil || !fresh {
		t.Fatalf("upgraded capture fresh=%v err=%v", fresh, err)
	}
	got := triageIssue(t, ctx, upgraded, owner.ID, id)
	if got.Count != 11 || got.Ignore == nil || got.Ignore.Remaining == nil || *got.Ignore.Remaining != 4 || captureProgressCounter(t, ctx, upgraded, id) != 11 {
		t.Fatalf("post-upgrade capture advanced incorrectly: %+v", got)
	}
	// Diverge the historical summary before restart so a repeated backfill would
	// visibly reset progress. This remains a grouping-summary simulation.
	if _, err := upgraded.pool.Exec(ctx, "UPDATE issues SET count=0 WHERE id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	beforeRestart := captureProgressHistory(t, ctx, upgraded)
	upgraded.Close()
	reopened, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	var again string
	if err := reopened.pool.QueryRow(ctx, "SELECT to_jsonb(m)::text FROM schema_migrations m WHERE version=15").Scan(&again); err != nil || receipt != again {
		t.Fatalf("015 reapplied on reopen: %v", err)
	}
	if after := captureProgressHistory(t, ctx, reopened); !reflect.DeepEqual(beforeRestart, after) {
		t.Fatal("reopen rewrote historical columns/rows or001..014 receipts")
	}
	got = triageIssue(t, ctx, reopened, owner.ID, id)
	if got.Count != 0 || got.Status != "ignored" || got.Ignore == nil || got.Ignore.Remaining == nil || *got.Ignore.Remaining != 4 || captureProgressCounter(t, ctx, reopened, id) != 11 {
		t.Fatalf("reopen reset monotonic capture progress: %+v", got)
	}
}
