//go:build integration

package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// Real persistence owns report identity, first/update clocks, reversed arrival
// and generation cascades. Opaque storage or an external-ID-only association
// cannot satisfy these assertions. Existing event tests have no report inputs;
// no production test seam, mock store or clock injection is required.
func TestUserReportsBindReceiptWithoutMutatingError(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "report", "Reported error", time.Now().Add(-365*24*time.Hour))
	id := groupCapture(t, f.ctx, f.db, f.project, ev)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	input := UserReportInput{EventID: ev.ID, Name: "<script>name</script>", Email: "person@example.test", Comments: "  Broken\nwith detail  "}
	report, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input)
	if err != nil || report.ID == "" || report.EventID != ev.ID || report.Name != input.Name || report.Comments != input.Comments {
		t.Fatalf("durable report=%+v err=%v", report, err)
	}
	input.Comments = "Updated feedback"
	updated, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input)
	if err != nil || updated.ID != report.ID || !updated.DateCreated.Equal(report.DateCreated) || updated.Comments != input.Comments {
		t.Fatalf("retry extended identity or first receipt: %+v %v", updated, err)
	}
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	for table, rows := range before {
		if table != "user_reports" && after[table] != rows {
			t.Fatalf("report mutated existing %s", table)
		}
	}
	var receipt int64
	if err := f.db.pool.QueryRow(f.ctx, `SELECT r.event_receipt_id FROM user_reports r JOIN events e ON e.id=r.event_receipt_id WHERE r.id::text=$1 AND e.issue_id::text=$2`, report.ID, id).Scan(&receipt); err != nil || receipt == 0 {
		t.Fatal("report did not pin a real error receipt", receipt, err)
	}
	other := groupEvent(t, 2, "other-report", "Other error", time.Now())
	otherID := groupCapture(t, f.ctx, f.db, f.project, other)
	merged, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{id, otherID})
	if err != nil {
		t.Fatal(err)
	}
	var root string
	if err := f.db.pool.QueryRow(f.ctx, `SELECT e.issue_id::text FROM user_reports r JOIN events e ON e.id=r.event_receipt_id WHERE r.id::text=$1`, report.ID).Scan(&root); err != nil || root != merged.Issue.ID {
		t.Fatal("report lost current merged membership", root, err)
	}
	if _, err := f.db.UnmergeIssue(f.ctx, f.owner.ID, merged.Issue.ID, []string{id}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.pool.QueryRow(f.ctx, `SELECT e.issue_id::text FROM user_reports r JOIN events e ON e.id=r.event_receipt_id WHERE r.id::text=$1`, report.ID).Scan(&root); err != nil || root != id {
		t.Fatal("report lost immutable event origin", root, err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=clock_timestamp()-interval '2 days' WHERE id=$1`, receipt); err != nil {
		t.Fatal(err)
	}
	if n, err := f.db.CleanupEvents(f.ctx, 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var count int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM user_reports`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired parent retained a report", count, err)
	}
	groupCapture(t, f.ctx, f.db, f.project, ev)
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM user_reports`).Scan(&count); err != nil || count != 0 {
		t.Fatal("fresh same SDK identity inherited old feedback", count, err)
	}
}

func TestUserReportsReversedArrivalAndAtomicCapture(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "pending", "Later error", time.Now())
	input := UserReportInput{EventID: ev.ID, Comments: "Feedback arrived first"}
	report, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err := f.db.pool.QueryRow(f.ctx, `SELECT event_receipt_id IS NULL FROM user_reports WHERE id::text=$1`, report.ID).Scan(&pending); err != nil || !pending {
		t.Fatal(pending, err)
	}
	groupCapture(t, f.ctx, f.db, f.project, ev)
	var first time.Time
	if err := f.db.pool.QueryRow(f.ctx, `SELECT created_at,event_receipt_id IS NULL FROM user_reports WHERE id::text=$1`, report.ID).Scan(&first, &pending); err != nil || pending || !first.Equal(report.DateCreated) {
		t.Fatal("reversed arrival failed or reset age", first, pending, err)
	}
	expired := groupEvent(t, 2, "expired-pending", "Too late", time.Now())
	old, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: expired.ID, Comments: "Expired pending"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE user_reports SET created_at=clock_timestamp()-interval '31 minutes' WHERE id::text=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	groupCapture(t, f.ctx, f.db, f.project, expired)
	var count int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM user_reports WHERE id::text=$1`, old.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("late capture resurrected expired pending feedback", count, err)
	}
	atomic := groupEvent(t, 3, "atomic-report", "Atomic error", time.Now())
	parts := CaptureItems{UserReports: []UserReportInput{{EventID: atomic.ID, Comments: "Same envelope"}}}
	id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, atomic, parts)
	if err != nil || !fresh || id == "" {
		t.Fatal(id, fresh, err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, atomic, parts); err != nil {
		t.Fatal(err)
	}
	var duplicates int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM user_reports WHERE event_id=$1`, atomic.ID).Scan(&duplicates); err != nil || duplicates != 1 {
		t.Fatal("duplicate capture duplicated report", duplicates, err)
	}
	bad := groupEvent(t, 4, "invalid-child", "Must roll back", time.Now())
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, bad, CaptureItems{UserReports: []UserReportInput{{EventID: atomic.ID, Comments: "Foreign parent"}}}); !errors.Is(err, ErrInvalidUserReport) {
		t.Fatal("mismatched report was accepted", err)
	}
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	for table, rows := range before {
		if table != "user_reports" && rows != after[table] {
			t.Fatalf("rejected parent/duplicate changed %s", table)
		}
	}
	// A real late SQL failure after event insertion must undo its grouping,
	// release observation, children and report in the same transaction.
	if _, err := f.db.pool.Exec(f.ctx, `ALTER TABLE user_reports ADD CONSTRAINT reject_specific_report CHECK(comments<>'Reject after insert')`); err != nil {
		t.Fatal(err)
	}
	rollback := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, bad, CaptureItems{UserReports: []UserReportInput{{EventID: bad.ID, Comments: "Reject after insert"}}}); err == nil {
		t.Fatal("declared SQL failure acknowledged capture")
	}
	if got := groupDatabaseSnapshot(t, f.ctx, f.db, 29); !reflect.DeepEqual(got, rollback) {
		t.Fatal("late report failure changed durable rows")
	}
}

func TestUserReportsMutabilityUsesServerReceipts(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "old-producer", "Old producer clock", time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC))
	groupCapture(t, f.ctx, f.db, f.project, ev)
	input := UserReportInput{EventID: ev.ID, Comments: "Fresh server receipt"}
	report, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input)
	if err != nil {
		t.Fatal("producer timestamp controlled mutability", err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE user_reports SET created_at=clock_timestamp()-interval '6 minutes' WHERE id::text=$1`, report.ID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input); !errors.Is(err, ErrConflict) {
		t.Fatal("report overwrite extended five-minute lifetime", err)
	}
	if got := groupDatabaseSnapshot(t, f.ctx, f.db, 29); !reflect.DeepEqual(got, before) {
		t.Fatal("rejected overwrite changed durable rows")
	}
	old := groupEvent(t, 2, "old-receipt", "Fresh producer clock", time.Now().Add(24*time.Hour))
	groupCapture(t, f.ctx, f.db, f.project, old)
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=clock_timestamp()-interval '31 minutes' WHERE event_id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: old.ID, Comments: "Too late"}); !errors.Is(err, ErrConflict) {
		t.Fatal("producer timestamp bypassed event mutability", err)
	}
	for n, mutate := range []func(UserReportInput) UserReportInput{
		func(x UserReportInput) UserReportInput { x.Comments = " \n"; return x },
		func(x UserReportInput) UserReportInput { x.Name = string([]byte{255}); return x },
		func(x UserReportInput) UserReportInput { x.Email = "bad\x00email"; return x },
		func(x UserReportInput) UserReportInput { x.EventID = "bad"; return x },
	} {
		if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, mutate(input)); !errors.Is(err, ErrInvalidUserReport) {
			t.Fatalf("invalid input%d accepted: %v", n, err)
		}
	}
}
