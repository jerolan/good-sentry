//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func TestIssueIgnoreSuppressesNewFrequencyQueue(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	config := alertConfig("Ignored frequency")
	config.Quantity = 2
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	id := triageCapture(t, ctx, db, project)
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, triageEvent(t, "00000000000000000000000000000002", time.Now())); err != nil || !fresh {
		t.Fatalf("ignored capture fresh=%v err=%v", fresh, err)
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("ignored issue queued new frequency work: deliveries=%d err=%v", len(deliveries), err)
	}
	if got := triageIssue(t, ctx, db, owner.ID, id); got.Status != "ignored" || got.Count != 2 {
		t.Fatalf("mute discarded capture: %+v", got)
	}
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "unresolved"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, project.ID, triageEvent(t, "00000000000000000000000000000003", time.Now())); err != nil {
		t.Fatal(err)
	}
	deliveries, _, err = db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("threshold crossed while muted was replayed: %d %v", len(deliveries), err)
	}
}

func issueIgnoreChange(minutes, count int32) *IssueIgnoreChange {
	condition := &IssueIgnoreCondition{}
	if minutes != 0 {
		condition.Minutes = &minutes
	}
	if count != 0 {
		condition.Count = &count
	}
	return &IssueIgnoreChange{Condition: condition}
}

func ignoreExpiries(t *testing.T, ctx context.Context, db *Store, userID, issueID string) []IssueActivity {
	t.Helper()
	var rows []IssueActivity
	for _, activity := range triageActivities(t, ctx, db, userID, issueID) {
		if activity.Type == "unignored" {
			rows = append(rows, activity)
		}
	}
	return rows
}

func TestIssueIgnoreCountRetriesNoOpAndLateUniqueCaptures(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	change := IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 4)}
	muted, err := db.UpdateIssue(ctx, owner.ID, id, change)
	if err != nil || muted.Ignore == nil || muted.Ignore.Until == nil || muted.Ignore.Until.Location() != time.UTC || *muted.Ignore.Remaining != 4 {
		t.Fatalf("conditional mute: %+v err=%v", muted, err)
	}
	deadline := *muted.Ignore.Until
	duplicate := triageEvent(t, "00000000000000000000000000000002", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	fresh := make(chan bool, 8)
	for range 8 {
		wg.Go(func() { _, admitted, err := db.Ingest(ctx, project.ID, duplicate); errs <- err; fresh <- admitted })
	}
	wg.Wait()
	close(errs)
	close(fresh)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	unique := 0
	for admitted := range fresh {
		if admitted {
			unique++
		}
	}
	before := triageIssue(t, ctx, db, owner.ID, id)
	if unique != 1 || before.Count != 2 || before.Status != "ignored" || before.Ignore == nil || *before.Ignore.Remaining != 3 {
		t.Fatalf("duplicate retry consumed additional occurrences: unique=%d issue=%+v", unique, before)
	}
	history := triageActivities(t, ctx, db, owner.ID, id)
	for _, update := range []IssueUpdate{change, {Status: "ignored"}, {Assignment: &IssueAssignmentChange{}}} {
		got, err := db.UpdateIssue(ctx, owner.ID, id, update)
		if err != nil || got.Ignore == nil || !got.Ignore.Until.Equal(deadline) || *got.Ignore.Remaining != 3 || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
			t.Fatalf("retry/omission/assignment reset active policy: %+v err=%v", got, err)
		}
	}
	transaction := triageEvent(t, "00000000000000000000000000000003", time.Now())
	transaction.Type = "transaction"
	if _, _, err := db.Ingest(ctx, project.ID, transaction); err != nil {
		t.Fatal(err)
	}
	if got := triageIssue(t, ctx, db, owner.ID, id); got.Count != 2 || *got.Ignore.Remaining != 3 {
		t.Fatalf("transaction consumed mute: %+v", got)
	}
	errs = make(chan error, 3)
	for n := 4; n <= 6; n++ {
		ev := triageEvent(t, fmt.Sprintf("%032x", n), time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC))
		wg.Go(func() { _, _, err := db.Ingest(ctx, project.ID, ev); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := triageIssue(t, ctx, db, owner.ID, id)
	expiries := ignoreExpiries(t, ctx, db, owner.ID, id)
	if got.Status != "unresolved" || got.Count != 5 || got.Ignore != nil || got.ResolvedAt != nil || got.RegressedAt != nil || len(expiries) != 1 || expiries[0].User != nil {
		t.Fatalf("concurrent count expiry: issue=%+v activities=%+v", got, expiries)
	}
	var data struct {
		Reason  string
		Ignore  IssueIgnore
		EventID string
	}
	if err := json.Unmarshal(expiries[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	// The previous policy includes the fresh capture that reached its count.
	if data.Reason != "count" || data.EventID == "" || data.Ignore.Remaining == nil || *data.Ignore.Remaining != 0 || data.Ignore.Until == nil || !data.Ignore.Until.Equal(deadline) {
		t.Fatalf("count expiry snapshot: %s", expiries[0].Data)
	}
}

func TestIssueIgnoreDurationCatchUpWorkerORAndAtomicValidation(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	muted, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(1, 1)})
	if err != nil || muted.Ignore == nil {
		t.Fatalf("mute=%+v err=%v", muted, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 second' WHERE issue_id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	before := triageIssue(t, ctx, db, owner.ID, id)
	history := triageActivities(t, ctx, db, owner.ID, id)
	// A retry of the already acknowledged SDK identity cannot expire even a
	// due policy. Expiry belongs after fresh-event admission, not before it.
	duplicate := triageEvent(t, "00000000000000000000000000000001", time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	duplicateIssue, admitted, err := db.Ingest(ctx, project.ID, duplicate)
	if err != nil || admitted || duplicateIssue != id || !reflect.DeepEqual(before, triageIssue(t, ctx, db, owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatalf("duplicate SDK retry expired a due policy: issue=%s admitted=%v err=%v", duplicateIssue, admitted, err)
	}
	_, err = db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(1, 1), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: "99999999"}}})
	if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, triageIssue(t, ctx, db, owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatalf("invalid target partially expired policy: %v", err)
	}
	foreign := triageForeignOrganization(t, ctx, db, "ignore-foreign")
	outsider := triageMember(t, ctx, db, foreign, "ignore-outsider", "owner")
	_, err = db.UpdateIssue(ctx, outsider.ID, id, IssueUpdate{Status: "unresolved"})
	if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, triageIssue(t, ctx, db, owner.ID, id)) {
		t.Fatalf("unauthorized request expired policy: %v", err)
	}
	if _, _, err := db.Ingest(ctx, project.ID, triageEvent(t, "00000000000000000000000000000002", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	expiries := ignoreExpiries(t, ctx, db, owner.ID, id)
	var data struct {
		Reason  string
		Ignore  IssueIgnore
		EventID string
	}
	if len(expiries) != 1 {
		t.Fatalf("OR expiry count=%d", len(expiries))
	}
	if err := json.Unmarshal(expiries[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Reason != "duration" || data.Ignore.Remaining == nil || *data.Ignore.Remaining != 0 || data.EventID != "00000000000000000000000000000002" {
		t.Fatalf("duration did not win simultaneous OR: %s", expiries[0].Data)
	}
	// A worker catches a persisted historical deadline after a process restart,
	// without a capture or a clock override.
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(1, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 minute' WHERE issue_id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	url := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	var wg sync.WaitGroup
	results, errs := make(chan bool, 8), make(chan error, 8)
	for range 8 {
		wg.Go(func() { expired, err := reopened.ExpireIssueIgnore(ctx); results <- expired; errs <- err })
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	completed := 0
	for expired := range results {
		if expired {
			completed++
		}
	}
	got := triageIssue(t, ctx, reopened, owner.ID, id)
	expiries = ignoreExpiries(t, ctx, reopened, owner.ID, id)
	if completed != 1 || got.Status != "unresolved" || got.Ignore != nil || len(expiries) != 2 {
		t.Fatalf("restart/replicas expiry: completed=%d issue=%+v activities=%+v", completed, got, expiries)
	}
	if err := json.Unmarshal(expiries[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	var workerData map[string]json.RawMessage
	if err := json.Unmarshal(expiries[0].Data, &workerData); err != nil {
		t.Fatal(err)
	}
	if data.Reason != "duration" || data.Ignore.Count != nil || data.Ignore.Remaining != nil || workerData["eventID"] != nil || expiries[0].User != nil {
		t.Fatalf("worker fabricated capture/context: %s", expiries[0].Data)
	}
	if expired, err := reopened.ExpireIssueIgnore(ctx); err != nil || expired {
		t.Fatalf("expiry replay: %v %v", expired, err)
	}
}

func TestIssueIgnoreDeadlineRangeClearAndNewBaseline(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	var before, after time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	muted, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(2147483647, 2)})
	if err != nil || muted.Ignore == nil || muted.Ignore.Until == nil {
		t.Fatalf("max minutes mute: %+v %v", muted, err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	start := time.Unix(muted.Ignore.Until.Unix()-int64(2147483647)*60, int64(muted.Ignore.Until.Nanosecond())).UTC()
	if start.Before(before) || start.After(after) || muted.Ignore.Until.Location() != time.UTC {
		t.Fatalf("minutes overflowed or used civil time: before=%v start=%v after=%v until=%v", before, start, after, muted.Ignore.Until)
	}
	cleared, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: &IssueIgnoreChange{}})
	if err != nil || cleared.Status != "ignored" || cleared.Ignore != nil {
		t.Fatalf("explicit indefinite clear: %+v %v", cleared, err)
	}
	history := triageActivities(t, ctx, db, owner.ID, id)
	var activity map[string]json.RawMessage
	if err := json.Unmarshal(history[0].Data, &activity); err != nil {
		t.Fatal(err)
	}
	if history[0].Type != "ignored" || string(activity["ignore"]) != "null" {
		t.Fatalf("clear did not snapshot indefinite ignore: %s", history[0].Data)
	}
	for _, status := range []string{"resolved", "unresolved"} {
		if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 2)}); err != nil {
			t.Fatal(err)
		}
		got, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: status})
		if err != nil || got.Ignore != nil || got.Status != status {
			t.Fatalf("%s did not clear condition: %+v %v", status, got, err)
		}
		if _, _, err := db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", got.Count+10), time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	fresh, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 2)})
	if err != nil || fresh.Ignore == nil || *fresh.Ignore.Remaining != 2 {
		t.Fatalf("new mute reused old baseline: %+v %v", fresh, err)
	}
}

func TestIssueIgnoreUnmutingCaptureOrdinaryEligibilityAndQueuedHistory(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	config := alertConfig("Unmute crossing")
	config.Quantity = 3
	config.CooldownSeconds = 0
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	id := triageCapture(t, ctx, db, project)
	if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
		t.Fatal(err)
	}
	existing, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(existing) != 1 {
		t.Fatalf("queued fixture: %+v %v", existing, err)
	}
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 2)}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{2, 3} {
		if _, _, err := db.Ingest(ctx, project.ID, triageEvent(t, fmt.Sprintf("%032x", n), time.Now())); err != nil {
			t.Fatal(err)
		}
		deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
		if err != nil || len(deliveries) != n-1 {
			t.Fatalf("mute/unmute frequency eligibility for event%d: %d %v", n, len(deliveries), err)
		}
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deliveries[0].Payload.EventID != "00000000000000000000000000000003" {
		t.Fatalf("Nth fresh capture was not ordinarily eligible: %+v", deliveries[0])
	}
	for _, d := range deliveries {
		if d.ID == existing[0].ID && !reflect.DeepEqual(d, existing[0]) {
			t.Fatal("mute canceled or rewrote existing delivery")
		}
	}
	// A threshold crossed under an indefinite mute is not replayed on reopen.
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, project.ID, triageEvent(t, "00000000000000000000000000000004", time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "unresolved"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, project.ID, triageEvent(t, "00000000000000000000000000000005", time.Now())); err != nil {
		t.Fatal(err)
	}
	after, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(after) != 2 {
		t.Fatalf("unmute replayed old crossing: %+v %v", after, err)
	}
}

func TestIssueIgnoreLockWaitRechecksPolicyAndWorkerSkipsRenewal(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(1, 1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 minute' WHERE issue_id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var pid int
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid() FROM issues WHERE id=$1::bigint FOR UPDATE", id).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var renewedUntil time.Time
	if err := tx.QueryRow(ctx, "UPDATE issue_ignores SET count=8,until=clock_timestamp()+interval '1 hour' WHERE issue_id=$1::bigint RETURNING until", id).Scan(&renewedUntil); err != nil {
		t.Fatal(err)
	}
	ev := triageEvent(t, "00000000000000000000000000000002", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))
	captured := make(chan error, 1)
	go func() { _, _, err := db.Ingest(ctx, project.ID, ev); captured <- err }()
	// Observe the actual database lock dependency rather than assume goroutine
	// scheduling or inject a pause into production code.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := db.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if expired, err := db.ExpireIssueIgnore(ctx); err != nil || expired {
		t.Fatalf("worker did not skip the locked issue: %v %v", expired, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-captured; err != nil {
		t.Fatal(err)
	}
	got := triageIssue(t, ctx, db, owner.ID, id)
	if got.Status != "ignored" || got.Count != 2 || got.Ignore == nil || *got.Ignore.Count != 8 || *got.Ignore.Remaining != 7 || !got.Ignore.Until.Equal(renewedUntil) || len(ignoreExpiries(t, ctx, db, owner.ID, id)) != 0 {
		t.Fatalf("capture used policy from before its lock wait: %+v", got)
	}
	if expired, err := db.ExpireIssueIgnore(ctx); err != nil || expired {
		t.Fatalf("worker expired renewed policy: %v %v", expired, err)
	}
	// Valid assignment-only PATCH also catches up a due condition immediately.
	if _, err := db.pool.Exec(ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 minute' WHERE issue_id=$1::bigint", id); err != nil {
		t.Fatal(err)
	}
	updated, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}})
	if err != nil || updated.Status != "unresolved" || updated.Ignore != nil || updated.AssignedTo == nil || updated.AssignedTo.ID != owner.ID {
		t.Fatalf("valid PATCH did not atomically expire and assign: %+v %v", updated, err)
	}
	expiries := ignoreExpiries(t, ctx, db, owner.ID, id)
	var data map[string]json.RawMessage
	if len(expiries) != 1 {
		t.Fatalf("PATCH expiry activities=%+v", expiries)
	}
	if err := json.Unmarshal(expiries[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["eventID"] != nil || string(data["reason"]) != `"duration"` || expiries[0].User != nil {
		t.Fatalf("PATCH expiry fabricated capture: %s", expiries[0].Data)
	}
}

func TestIssueIgnoreMigrationPreservesLegacyRowsAndReceipts(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	// Reconstruct the pre-ignore schema in this isolated fixture by removing
	// both mute tables and their migration receipts, then seed legacy values.
	if _, err := db.pool.Exec(ctx, `DROP TABLE issue_ignore_users;DROP TABLE issue_ignores;DELETE FROM schema_migrations WHERE version IN (13,63)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE issues SET status='ignored',assignee_id=$2::bigint,first_seen='2000-01-01Z',last_seen='2001-01-01Z' WHERE id=$1::bigint`, id, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO issue_activity(issue_id,user_id,action,data,created_at) VALUES($1::bigint,NULL,'ignored','{"from":"unresolved","to":"ignored"}','2002-01-01Z')`, id); err != nil {
		t.Fatal(err)
	}
	const snapshot = `SELECT jsonb_build_object(
	 'issues',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM issues i),
	 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM events e),
	 'activity',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM issue_activity a),
	 'receipts',(SELECT jsonb_agg(to_jsonb(m) ORDER BY version) FROM schema_migrations m WHERE version<=12))::text`
	var before, after string
	if err := db.pool.QueryRow(ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	url := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err := reopened.pool.QueryRow(ctx, snapshot).Scan(&after); err != nil || before != after {
		t.Fatalf("013 rewrote historical state/001..012 receipts: equal=%v err=%v", before == after, err)
	}
	got := triageIssue(t, ctx, reopened, owner.ID, id)
	if got.Status != "ignored" || got.Ignore != nil || got.Count != 1 {
		t.Fatalf("legacy ignored issue gained a conditional mute: %+v", got)
	}
	if expired, err := reopened.ExpireIssueIgnore(ctx); err != nil || expired {
		t.Fatalf("legacy indefinite issue expired: %v %v", expired, err)
	}
	var receipt time.Time
	if err := reopened.pool.QueryRow(ctx, "SELECT applied_at FROM schema_migrations WHERE version=13").Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	second, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	var again time.Time
	if err := second.pool.QueryRow(ctx, "SELECT applied_at FROM schema_migrations WHERE version=13").Scan(&again); err != nil || !again.Equal(receipt) {
		t.Fatalf("013 reapplied on reopen: %v %v", again, err)
	}
	if err := second.pool.QueryRow(ctx, snapshot).Scan(&after); err != nil || before != after {
		t.Fatalf("restart rewrote history: equal=%v err=%v", before == after, err)
	}
}

func TestIssueIgnoreDrainContinuesAcrossConcurrentRenewal(t *testing.T) {
	fixtureCtx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, fixtureCtx, db, project)
	if _, err := db.UpdateIssue(fixtureCtx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(1, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(fixtureCtx, `WITH seeded AS (
	 INSERT INTO issues(project_id,fingerprint,title,level,platform,status,count,first_seen,last_seen)
	 SELECT $1::bigint,'fixed-due-'||n,'Fixed due backlog','error','go','ignored',1,'2000-01-01Z','2000-01-01Z'
	 FROM generate_series(1,4000) n RETURNING id
	) INSERT INTO issue_ignores(issue_id,minutes,until,baseline) SELECT id,1,'2000-01-01Z',1 FROM seeded`, project.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(fixtureCtx, 60*time.Second)
	done := make(chan error, 1)
	started := make(chan struct{})
	renew := func(historic bool) error {
		tx, err := db.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var locked string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM issues WHERE id=$1::bigint FOR UPDATE", id).Scan(&locked); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE issues SET status='ignored',resolved_at=NULL,regressed_at=NULL WHERE id=$1::bigint", id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO issue_ignores(issue_id,minutes,until,baseline)
		 VALUES($1::bigint,1,CASE WHEN $2::boolean THEN '1999-01-01Z'::timestamptz ELSE clock_timestamp()+interval '1 minute' END,1)
		 ON CONFLICT(issue_id) DO UPDATE SET minutes=1,until=EXCLUDED.until,baseline=1`, id, historic)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	go func() {
		close(started)
		for ctx.Err() == nil {
			for _, historic := range []bool{true, false} {
				if err := renew(historic); err != nil {
					done <- err
					return
				}
			}
		}
		done <- nil
	}()
	<-started
	defer func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("renewal failed: %v", err)
		}
	}()
	// The renewal race need not occur for success. Four thousand immutable,
	// unlocked other deadlines guarantee work for every one of these claims.
	// The old provider did return false with 3958 still due in the real race.
	for claim := 1; claim <= 500; claim++ {
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		processed, err := db.ExpireIssueIgnore(ctx)
		if err != nil {
			t.Fatalf("claim %d failed: %v", claim, err)
		}
		if !processed {
			var due int
			if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM issues i JOIN issue_ignores g ON g.issue_id=i.id
			 WHERE i.project_id=$1::bigint AND i.id<>$2::bigint AND i.status='ignored' AND g.until<clock_timestamp()`, project.ID, id).Scan(&due); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("drain stopped at claim %d with %d immutable other issues still due", claim, due)
		}
	}
}

func issueUserMuteEvent(t *testing.T, id, fingerprint string, userID any) event.Event {
	t.Helper()
	payload := map[string]any{
		"event_id": id, "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"message": "Issue user mute", "fingerprint": []string{fingerprint}, "level": "error",
	}
	if userID != nil {
		payload["user"] = map[string]any{"id": userID}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestIssueIgnoreUserCountLifecycle(t *testing.T) {
	ctx, db, owner, organization, project := telemetryStore(t)
	db.SetIssueIdentitySecret("issue-user-mute-integration-secret-0001")
	root, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, fmt.Sprintf("%032x", 1), "user-mute-root", "42"))
	if err != nil || !fresh {
		t.Fatalf("root capture=%s fresh=%v err=%v", root, fresh, err)
	}
	two := int32(2)
	muted, err := db.UpdateIssue(ctx, owner.ID, root, IssueUpdate{Status: "ignored", Ignore: &IssueIgnoreChange{Condition: &IssueIgnoreCondition{UserCount: &two}}})
	if err != nil || muted.Ignore == nil || muted.Ignore.UserCount == nil || *muted.Ignore.UserCount != two || muted.Ignore.UserRemaining == nil || *muted.Ignore.UserRemaining != 2 {
		t.Fatalf("user-count policy=%+v err=%v", muted.Ignore, err)
	}
	var baselineRows, allRows, digestLength int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE baseline),count(*),COALESCE(max(octet_length(identity_digest)),0)
 FROM issue_ignore_users WHERE issue_id=$1::bigint`, root).Scan(&baselineRows, &allRows, &digestLength); err != nil || baselineRows != 1 || allRows != 1 || digestLength != 32 {
		t.Fatalf("baseline digests baseline=%d all=%d maxBytes=%d err=%v", baselineRows, allRows, digestLength, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=clock_timestamp()-interval '2 days' WHERE event_id=$1`, fmt.Sprintf("%032x", 1)); err != nil {
		t.Fatal(err)
	}
	if removed, err := db.CleanupEvents(ctx, 1); err != nil || removed != 1 {
		t.Fatalf("retained baseline cleanup removed=%d err=%v", removed, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, fmt.Sprintf("%032x", 2), "user-mute-root", 42)); err != nil || !fresh {
		t.Fatalf("numeric ID equivalent capture fresh=%v err=%v", fresh, err)
	}
	current := triageIssue(t, ctx, db, owner.ID, root)
	if current.Ignore == nil || current.Ignore.UserRemaining == nil || *current.Ignore.UserRemaining != 2 {
		t.Fatalf("known user after event retention advanced mute: %+v", current.Ignore)
	}
	source, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, fmt.Sprintf("%032x", 3), "user-mute-source", "source-user"))
	if err != nil || !fresh {
		t.Fatalf("source capture=%s fresh=%v err=%v", source, fresh, err)
	}
	merged, err := db.MergeIssues(ctx, owner.ID, organization.Slug, []string{root, source})
	if err != nil || merged.Issue.ID != root || merged.Issue.Ignore == nil || merged.Issue.Ignore.UserRemaining == nil || *merged.Issue.Ignore.UserRemaining != 2 {
		t.Fatalf("merge did not seed existing users as baseline: %+v err=%v", merged.Issue.Ignore, err)
	}
	split, err := db.UnmergeIssue(ctx, owner.ID, root, []string{source})
	if err != nil || !split.Changed || split.Issue.ID != root || split.Issue.Ignore == nil || split.Issue.Ignore.UserRemaining == nil || *split.Issue.Ignore.UserRemaining != 2 {
		t.Fatalf("unmerge did not preserve root user mute: %+v err=%v", split.Issue.Ignore, err)
	}
	detached, err := db.IssueDetail(ctx, owner.ID, source)
	if err != nil || detached.Issue.Status == "ignored" || detached.Issue.Ignore != nil {
		t.Fatalf("unmerge copied mute to detached issue: %+v err=%v", detached.Issue, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, fmt.Sprintf("%032x", 4), "user-mute-root", "source-user")); err != nil || !fresh {
		t.Fatalf("existing merged user capture fresh=%v err=%v", fresh, err)
	}
	firstNewID := fmt.Sprintf("%032x", 5)
	newUser := issueUserMuteEvent(t, firstNewID, "user-mute-root", "new-user-a")
	if _, fresh, err := db.Ingest(ctx, project.ID, newUser); err != nil || !fresh {
		t.Fatalf("first new user capture fresh=%v err=%v", fresh, err)
	}
	current = triageIssue(t, ctx, db, owner.ID, root)
	if current.Status != "ignored" || current.Ignore == nil || current.Ignore.UserRemaining == nil || *current.Ignore.UserRemaining != 1 {
		t.Fatalf("first new user did not consume one: %+v", current.Ignore)
	}
	if same, err := db.UpdateIssue(ctx, owner.ID, root, IssueUpdate{Status: "ignored", Ignore: &IssueIgnoreChange{Condition: &IssueIgnoreCondition{UserCount: &two}}}); err != nil || same.Ignore == nil || same.Ignore.UserRemaining == nil || *same.Ignore.UserRemaining != 1 {
		t.Fatalf("identical policy reset user progress: %+v err=%v", same.Ignore, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, newUser); err != nil || fresh {
		t.Fatalf("duplicate event receipt fresh=%v err=%v", fresh, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, fmt.Sprintf("%032x", 6), "user-mute-root", "new-user-a")); err != nil || !fresh {
		t.Fatalf("repeat user capture fresh=%v err=%v", fresh, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, fmt.Sprintf("%032x", 7), "user-mute-root", nil)); err != nil || !fresh {
		t.Fatalf("anonymous capture fresh=%v err=%v", fresh, err)
	}
	current = triageIssue(t, ctx, db, owner.ID, root)
	if current.Status != "ignored" || current.Ignore == nil || current.Ignore.UserRemaining == nil || *current.Ignore.UserRemaining != 1 {
		t.Fatalf("retry, repeated or anonymous user advanced mute: %+v", current.Ignore)
	}
	lastID := fmt.Sprintf("%032x", 8)
	if _, fresh, err := db.Ingest(ctx, project.ID, issueUserMuteEvent(t, lastID, "user-mute-root", "new-user-b")); err != nil || !fresh {
		t.Fatalf("threshold user capture fresh=%v err=%v", fresh, err)
	}
	current = triageIssue(t, ctx, db, owner.ID, root)
	if current.Status != "unresolved" || current.Ignore != nil {
		t.Fatalf("second new user did not expire mute: %+v", current)
	}
	var remainingDigests int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM issue_ignore_users WHERE issue_id=$1::bigint", root).Scan(&remainingDigests); err != nil || remainingDigests != 0 {
		t.Fatalf("expired mute retained identity digests=%d err=%v", remainingDigests, err)
	}
	expiries := ignoreExpiries(t, ctx, db, owner.ID, root)
	if len(expiries) != 1 {
		t.Fatalf("automatic unignore activities=%d", len(expiries))
	}
	var receipt struct {
		Reason  string      `json:"reason"`
		EventID string      `json:"eventID"`
		Ignore  IssueIgnore `json:"ignore"`
	}
	if err := json.Unmarshal(expiries[0].Data, &receipt); err != nil || receipt.Reason != "user_count" || receipt.EventID != lastID || receipt.Ignore.UserRemaining == nil || *receipt.Ignore.UserRemaining != 0 {
		t.Fatalf("user-count expiry receipt=%s err=%v", expiries[0].Data, err)
	}
}
