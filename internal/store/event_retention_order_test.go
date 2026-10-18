//go:build integration

package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Observe real blockers at the existing gates. These cases protect re-reading
// owners after merge, atomic capture/count progress and triage baselines when
// the final payload expires. Provider rollback tests cannot create these cuts.
func TestEventRetentionOrdersFreshAndDuplicateCapture(t *testing.T) {
	for _, mode := range []string{"capture-first", "cleanup-first", "duplicate-after-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			f := resolutionStore(t)
			old := groupEvent(t, 1, "retention-order", "Expired event", time.Now())
			root, _, err := f.db.Ingest(f.ctx, f.project.ID, old, deleteOrderParts("old"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=now()-interval '2 days'`); err != nil {
				t.Fatal(err)
			}
			barrier, barrierPID := captureItemsBarrier(t, f)
			table := "envelope_items"
			if mode == "capture-first" {
				table = "attachments"
			}
			if _, err := barrier.Exec(f.ctx, "LOCK TABLE "+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			capturer, cleaner := captureItemsOther(t, f), captureItemsOther(t, f)
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			incoming := groupEvent(t, 2, "retention-order", "Fresh event", old.Timestamp.Add(time.Second))
			if mode == "duplicate-after-cleanup" {
				incoming = old
			}
			captured := make(chan captureItemsResult, 1)
			cleaned := make(chan error, 1)
			capture := func() {
				id, fresh, err := capturer.Ingest(ctx, f.project.ID, incoming, deleteOrderParts("new"))
				captured <- captureItemsResult{id, fresh, err}
			}
			clean := func() {
				n, err := cleaner.CleanupEvents(ctx, 1)
				if err == nil && n != 1 {
					err = fmt.Errorf("cleanup committed %d want1", n)
				}
				cleaned <- err
			}
			if mode == "capture-first" {
				go capture()
				pid := groupWaitBlocked(t, ctx, barrier, barrierPID, 1)[0]
				go clean()
				deleteOrderGateWait(t, f, barrier, pid, f.project.ID, "ExclusiveLock")
			} else {
				go clean()
				pid := groupWaitBlocked(t, ctx, barrier, barrierPID, 1)[0]
				go capture()
				deleteOrderGateWait(t, f, barrier, pid, f.project.ID, "ShareLock")
			}
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, cleaned)
			result := captureItemsAwait(t, captured)
			if result.err != nil || result.issue != root {
				t.Fatal("capture lost its retained route", result)
			}
			if mode == "duplicate-after-cleanup" {
				if result.fresh {
					t.Fatal("in-flight duplicate became a fresh generation")
				}
				deleteOrderRows(t, f, 1, 0, 0, 0)
				if triageIssue(t, ctx, f.db, f.owner.ID, root).Count != 0 || captureProgressCounter(t, ctx, f.db, root) != 1 {
					t.Fatal("stale duplicate children advanced an empty issue")
				}
				id, fresh, err := capturer.Ingest(ctx, f.project.ID, incoming, deleteOrderParts("new"))
				result = captureItemsResult{id, fresh, err}
			}
			if result.err != nil || !result.fresh || result.issue != root {
				t.Fatal("fresh capture did not retain group identity", result)
			}
			deleteOrderRows(t, f, 1, 1, 1, 1)
			i := triageIssue(t, ctx, f.db, f.owner.ID, root)
			if i.Count != 1 || i.Title != incoming.Title || captureProgressCounter(t, ctx, f.db, root) != 2 {
				t.Fatal("cleanup overwrote committed capture summary/progress", i)
			}
		})
	}
}

func TestEventRetentionRereadsCurrentOwnerAfterInFlightMerge(t *testing.T) {
	f := resolutionStore(t)
	a := groupEvent(t, 1, "retention-owner-a", "Retained owner A", time.Now())
	b := groupEvent(t, 2, "retention-owner-b", "Expired owner B", time.Now())
	aID := groupCapture(t, f.ctx, f.db, f.project, a)
	bID := groupCapture(t, f.ctx, f.db, f.project, b)
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=$1`, b.ID); err != nil {
		t.Fatal(err)
	}
	barrier, barrierPID := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", aID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	merger, cleaner := captureItemsOther(t, f), captureItemsOther(t, f)
	merged := make(chan error, 1)
	go func() {
		r, err := merger.MergeIssues(ctx, f.owner.ID, f.org.Slug, []string{aID, bID})
		if err == nil && (!r.Changed || r.Issue.ID != aID) {
			err = fmt.Errorf("merge selected unexpected owner %+v", r)
		}
		merged <- err
	}()
	mergePID := groupWaitBlocked(t, ctx, barrier, barrierPID, 1)[0]
	cleaned := make(chan error, 1)
	go func() {
		n, err := cleaner.CleanupEvents(ctx, 1)
		if err == nil && n != 1 {
			err = fmt.Errorf("cleanup committed %d want1", n)
		}
		cleaned <- err
	}()
	deleteOrderGateWait(t, f, barrier, mergePID, f.project.ID, "ExclusiveLock")
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, merged)
	deleteOrderAwait(t, ctx, cleaned)
	root := triageIssue(t, ctx, f.db, f.owner.ID, aID)
	if root.Count != 1 || root.Title != a.Title || root.GroupSize != 2 || captureProgressCounter(t, ctx, f.db, aID) != 1 || captureProgressCounter(t, ctx, f.db, bID) != 1 {
		t.Fatal("cleanup recounted the pre-gate owner or changed progress", root)
	}
}

func TestEventRetentionPreservesTriageAfterLastPayloadExpires(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "retention-triage", "Empty triage", time.Now())
	root := groupCapture(t, f.ctx, f.db, f.project, ev)
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	barrier, barrierPID := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	cleaner, triager := captureItemsOther(t, f), captureItemsOther(t, f)
	cleaned := make(chan error, 1)
	go func() {
		n, err := cleaner.CleanupEvents(ctx, 1)
		if err == nil && n != 1 {
			err = fmt.Errorf("cleanup committed %d want1", n)
		}
		cleaned <- err
	}()
	cleanPID := groupWaitBlocked(t, ctx, barrier, barrierPID, 1)[0]
	triaged := make(chan error, 1)
	go func() {
		_, err := triager.UpdateIssue(ctx, f.owner.ID, root, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 3)})
		triaged <- err
	}()
	deleteOrderGateWait(t, f, barrier, cleanPID, f.project.ID, "ShareLock")
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, cleaned)
	deleteOrderAwait(t, ctx, triaged)
	issue := triageIssue(t, ctx, f.db, f.owner.ID, root)
	if issue.Count != 0 || issue.Status != "ignored" || issue.Ignore == nil || *issue.Ignore.Remaining != 3 || captureProgressCounter(t, ctx, f.db, root) != 1 {
		t.Fatal("empty payload altered later triage baseline", issue)
	}
	if _, fresh, err := f.db.Ingest(ctx, f.project.ID, ev); err != nil || !fresh {
		t.Fatal("expired identity could not return", fresh, err)
	}
	issue = triageIssue(t, ctx, f.db, f.owner.ID, root)
	if issue.Count != 1 || issue.Ignore == nil || *issue.Ignore.Remaining != 2 || captureProgressCounter(t, ctx, f.db, root) != 2 {
		t.Fatal("new receipt did not advance the retained mute once", issue)
	}
}

func TestEventRetentionKeepsExactFrozenCutoffWhileWaiting(t *testing.T) {
	f := resolutionStore(t)
	id := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "retention-boundary", "Exact boundary", time.Now()))
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	barrier, barrierPID := captureItemsBarrier(t, f)
	if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		n, err := f.db.CleanupEvents(ctx, 1)
		if err == nil && n != 0 {
			err = fmt.Errorf("exact frozen boundary was deleted: %d", n)
		}
		done <- err
	}()
	pid := groupWaitBlocked(t, ctx, barrier, barrierPID, 1)[0]
	// Own fixture only: the initial cohort is frozen and waiting at its project
	// gate. Use the actual cleanup transaction's PG clock, with no injected clock.
	if tag, err := barrier.Exec(ctx, `UPDATE events SET created_at=(SELECT xact_start-interval '24 hours' FROM pg_stat_activity WHERE pid=$1)`, pid); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("could not set the owned exact boundary", tag, err)
	}
	if err := barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, done)
	if triageIssue(t, ctx, f.db, f.owner.ID, id).Count != 1 {
		t.Fatal("boundary retention changed group count")
	}
}
