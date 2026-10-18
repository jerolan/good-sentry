//go:build integration

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/eventview"
)

// Real authorized readers own containment, current group membership, pending
// visibility and nonplaceholder detail data. Existing event readers have no
// report rows and cannot expose external-ID-only or unscoped lookups. No test
// seam is added; actual Store capture/read/merge/unmerge APIs exercise the model.
func TestUserReportReadersScopePagingAndCurrentOwnership(t *testing.T) {
	f := resolutionStore(t)
	a := groupEvent(t, 1, "report-a", "First report error", time.Now())
	b := groupEvent(t, 2, "report-b", "Second report error", time.Now())
	root := groupCapture(t, f.ctx, f.db, f.project, a)
	alias := groupCapture(t, f.ctx, f.db, f.project, b)
	for _, ev := range []string{a.ID, b.ID, fmt.Sprintf("%032x", 3)} {
		if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: ev, Comments: "Text for " + ev}); err != nil {
			t.Fatal(err)
		}
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Hidden reports", "go")
	if err != nil {
		t.Fatal(err)
	}
	groupCapture(t, f.ctx, f.db, other, a)
	if _, err := f.db.SubmitUserReport(f.ctx, other.ID, UserReportInput{EventID: a.ID, Comments: "Hidden same SDK ID"}); err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}, ReadOnly: true}
	check := func(target CompatibleEventTarget, offset, limit int, wantIDs []string, wantTotal int64) {
		t.Helper()
		got, total, err := f.db.UserReports(f.ctx, access, target, offset, limit)
		if err != nil || total != wantTotal || len(got) != len(wantIDs) {
			t.Fatalf("reports=%+v total=%d want=%v/%d err=%v", got, total, wantIDs, wantTotal, err)
		}
		for n, report := range got {
			if report.ID == "" || report.EventID != wantIDs[n] || report.Event.EventID != report.EventID || report.Comments != "Text for "+wantIDs[n] || report.Event.ID != nil || report.Issue != nil || report.User != nil {
				t.Fatalf("report lost its actual DTO or containment: %+v", report)
			}
		}
	}
	check(EventProjectTarget(f.project.Slug), 0, 2, []string{fmt.Sprintf("%032x", 3), b.ID, a.ID}, 3)
	check(EventProjectTarget(f.project.ID), 2, 2, []string{a.ID}, 3)
	check(EventIssueTarget(root), 0, 25, []string{a.ID}, 1)
	if _, _, err := f.db.UserReports(f.ctx, access, EventProjectTarget(other.ID), 0, 25); !errors.Is(err, ErrNotFound) {
		t.Fatal("project-restricted reader exposed hidden feedback", err)
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, alias}); err != nil {
		t.Fatal(err)
	}
	check(EventIssueTarget(alias), 0, 25, []string{b.ID, a.ID}, 2)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	detail, err := f.db.IssueDetail(f.ctx, f.owner.ID, alias)
	if err != nil || detail.UserReportCount != 2 || len(detail.UserReports) != 2 || detail.Issue.Count != 2 {
		t.Fatal("native report count/list is a placeholder or changed errors", detail, err)
	}
	compatible, err := f.db.CompatibleIssue(f.ctx, access, alias, CompatibleIssueDetailFilter{})
	if err != nil || compatible.UserReportCount != 2 {
		t.Fatal("compatible issue report count is a placeholder", compatible, err)
	}
	for _, target := range []CompatibleEventTarget{EventProjectTarget(f.project.ID), EventIssueTarget(alias)} {
		record, err := f.db.CompatibleEvent(f.ctx, access, target, a.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		view, err := eventview.Project(record, true)
		var report UserReport
		if err != nil || view.UserReport == nil || json.Unmarshal(*view.UserReport, &report) != nil || report.EventID != a.ID || report.Comments != "Text for "+a.ID {
			t.Fatal("compatible full event report is null or foreign", report, err)
		}
	}
	if after := groupDatabaseSnapshot(t, f.ctx, f.db, 0); !reflect.DeepEqual(before, after) {
		t.Fatal("report reads performed maintenance or other writes")
	}
	if _, err := f.db.UnmergeIssue(f.ctx, f.owner.ID, root, []string{alias}); err != nil {
		t.Fatal(err)
	}
	check(EventIssueTarget(root), 0, 25, []string{a.ID}, 1)
	check(EventIssueTarget(alias), 0, 25, []string{b.ID}, 1)
	if _, err := f.db.pool.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.UserReports(f.ctx, access, EventIssueTarget(root), 0, 25); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed membership still reads feedback", err)
	}
}

func TestUserReportNativeDetailUsesOneSnapshot(t *testing.T) {
	f := resolutionStore(t)
	first := groupEvent(t, 1, "report-snapshot", "Snapshot error", time.Now())
	root := groupCapture(t, f.ctx, f.db, f.project, first)
	if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: first.ID, Comments: "First report"}); err != nil {
		t.Fatal(err)
	}
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `LOCK TABLE user_reports IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		detail IssueDetail
		err    error
	}
	done := make(chan result, 1)
	go func() { d, err := f.db.IssueDetail(f.ctx, f.owner.ID, root); done <- result{d, err} }()
	groupWaitBlocked(t, f.ctx, barrier, pid, 1)
	// Publish a newer receipt/report while the actual detail is paused after
	// reading its issue and event snapshot. This is a snapshot fixture only.
	second := groupEvent(t, 2, "report-snapshot", "Later snapshot error", time.Now())
	// Ingest's pending lookup also touches this table, so use its already-tested
	// event SQL shape as a concurrent snapshot fixture, not ingestion evidence.
	if _, err := barrier.Exec(f.ctx, `INSERT INTO events(project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,data)
 VALUES($1::bigint,$2,$3::bigint,$3::bigint,'error','Later snapshot error','error',clock_timestamp(),$4)`, f.project.ID, second.ID, root, second.Data); err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.Exec(f.ctx, `UPDATE issues SET count=count+1 WHERE id=$1::bigint`, root); err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.Exec(f.ctx, `INSERT INTO user_reports(project_id,event_id,event_receipt_id,name,email,comments)
 SELECT project_id,event_id,id,'','','Later report' FROM events WHERE event_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.detail.Issue.Count != 1 || len(got.detail.Events) != 1 || got.detail.UserReportCount != 1 || len(got.detail.UserReports) != 1 || got.detail.UserReports[0].Comments != "First report" {
		t.Fatal("detail combined old issue with new feedback snapshot", got.detail, got.err)
	}
}
