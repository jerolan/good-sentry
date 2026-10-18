//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Actual project/receipt waits and declared SQL failures own frozen generation,
// cancellation and atomic binding. Sequential state tests cannot reach these
// interleavings; all barriers are real PostgreSQL locks with no product seam.
func TestFeedbackBindingFreezesTargetAcrossProjectWait(t *testing.T) {
	f := resolutionStore(t)
	targetID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	feedbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernFeedbackEvent(t, feedbackID, "Before parent", targetID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, targetID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE feedback_events SET associated_event_receipt_id=NULL,association_pending=true`); err != nil {
		t.Fatal(err)
	}
	barrier, pid := captureItemsBarrier(t, f)
	if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
		t.Fatal(err)
	}
	other := captureItemsOther(t, f)
	type result struct {
		n   int64
		err error
	}
	done := make(chan result, 1)
	go func() { n, err := other.BindPendingFeedbackAssociations(f.ctx); done <- result{n, err} }()
	groupWaitBlocked(t, f.ctx, barrier, pid, 1)
	// The binder has frozen the old numeric receipt and waits at the project
	// gate. Replace that receipt inside the gate owner's transaction.
	var replacement int64
	if _, err := barrier.Exec(f.ctx, `DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, targetID); err != nil {
		t.Fatal(err)
	}
	if err := barrier.QueryRow(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
 VALUES($1::bigint,$2,'error','Replacement','error',clock_timestamp(),'{}') RETURNING id`, f.project.ID, targetID).Scan(&replacement); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.n != 0 {
			t.Fatal("frozen pass substituted a new target generation", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("binding did not finish after project release")
	}
	var bound *int64
	if err := f.db.pool.QueryRow(f.ctx, `SELECT associated_event_receipt_id FROM feedback_events`).Scan(&bound); err != nil || bound != nil {
		t.Fatal("frozen target replaced", bound, err)
	}
	// It was never bound, so a later independently selected pass can observe
	// this generation. This does not permit rebinding an established link.
	if n, err := other.BindPendingFeedbackAssociations(f.ctx); err != nil || n != 1 {
		t.Fatal("later pending pass", n, err)
	}
	if err := f.db.pool.QueryRow(f.ctx, `SELECT associated_event_receipt_id FROM feedback_events`).Scan(&bound); err != nil || bound == nil || *bound != replacement {
		t.Fatal("later pass selected wrong receipt", bound, replacement, err)
	}
}

func TestFeedbackBindingFailureAndCancellationAreAtomic(t *testing.T) {
	for _, mode := range []string{"statement", "commit", "cancel", "maintainer"} {
		t.Run(mode, func(t *testing.T) {
			f := resolutionStore(t)
			targetID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			for n := 1; n <= 3; n++ {
				if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernFeedbackEvent(t, fmt.Sprintf("%032x", n), "Atomic binding", targetID)); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, targetID)); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE feedback_events SET associated_event_receipt_id=NULL,association_pending=true`); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
			if mode == "statement" || mode == "commit" {
				trigger := `CREATE TRIGGER fail_feedback BEFORE UPDATE ON feedback_events FOR EACH ROW EXECUTE FUNCTION reject_feedback()`
				if mode == "commit" {
					trigger = `CREATE CONSTRAINT TRIGGER fail_feedback AFTER UPDATE ON feedback_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_feedback()`
				}
				_, err := f.db.pool.Exec(f.ctx, `CREATE FUNCTION reject_feedback() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.event_id='00000000000000000000000000000003' AND
 EXISTS(SELECT 1 FROM feedback_events WHERE event_id='00000000000000000000000000000001' AND associated_event_receipt_id IS NOT NULL)
 THEN RAISE EXCEPTION 'declared failure after prior feedback binding'; END IF; RETURN NEW; END $$;`+trigger)
				if err != nil {
					t.Fatal(err)
				}
				if n, err := f.db.BindPendingFeedbackAssociations(f.ctx); err == nil || n != 0 || !strings.Contains(err.Error(), "declared failure after prior feedback binding") {
					t.Fatal("failure acknowledged partial bindings", n, err)
				}
			} else {
				barrier, pid := captureItemsBarrier(t, f)
				if mode == "maintainer" {
					if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':feedback-associations',0))`); err != nil {
						t.Fatal(err)
					}
					if n, err := f.db.BindPendingFeedbackAssociations(f.ctx); err != nil || n != 0 {
						t.Fatal("busy maintainer", n, err)
					}
				} else {
					if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithCancel(f.ctx)
					done := make(chan error, 1)
					go func() {
						n, err := f.db.BindPendingFeedbackAssociations(ctx)
						if n != 0 {
							err = fmt.Errorf("canceled binder acknowledged%d", n)
						}
						done <- err
					}()
					groupWaitBlocked(t, f.ctx, barrier, pid, 1)
					cancel()
					groupAwaitCancellation(t, done, "feedback association")
				}
				if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			groupRequireUnchanged(t, f.ctx, f.db, before)
		})
	}
}

func TestFeedbackCaptureUsesBothReceiptGatesAndRollsBackChildren(t *testing.T) {
	f := resolutionStore(t)
	targetID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	feedbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ev := modernFeedbackEvent(t, feedbackID, "Serialized capture", targetID)
	for _, blockedID := range []string{feedbackID, targetID} {
		before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
		barrier, pid := captureItemsBarrier(t, f)
		if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, f.project.ID+":"+blockedID); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(f.ctx)
		done := make(chan error, 1)
		go func() { _, _, err := f.db.Ingest(ctx, f.project.ID, ev); done <- err }()
		groupWaitBlocked(t, f.ctx, barrier, pid, 1)
		cancel()
		groupAwaitCancellation(t, done, "feedback capture")
		if err := barrier.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	_, err := f.db.pool.Exec(f.ctx, `ALTER TABLE attachments ADD CONSTRAINT reject_feedback_child CHECK(filename<>'fail.bin')`)
	if err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "fail.bin", Data: []byte{0, 255}}}}); err == nil {
		t.Fatal("failed attachment acknowledged feedback")
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, ev); err != nil {
		t.Fatal("canceled/failed capture leaked a gate", err)
	}
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, feedbackID)); !errors.Is(err, ErrConflict) {
		t.Fatal("error reused feedback receipt", err)
	}
	if n, err := f.db.BindPendingFeedbackAssociations(f.ctx); err != nil || n != 0 {
		t.Fatal("absent target became bound", n, err)
	}
	var kind string
	if err := f.db.pool.QueryRow(f.ctx, `SELECT kind FROM events WHERE event_id=$1`, feedbackID).Scan(&kind); err != nil || kind != "feedback" {
		t.Fatal(kind, err)
	}
	// A failure after the error's reversed-arrival binding must roll back both
	// its parent and the already updated feedback association.
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, targetID), CaptureItems{Attachments: []CaptureAttachment{{Filename: "fail.bin", Data: []byte{1}}}}); err == nil {
		t.Fatal("failed error child acknowledged its parent and pending binding")
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, targetID)); err != nil {
		t.Fatal("rolled-back error leaked a lock", err)
	}
	var pending bool
	var target int64
	if err := f.db.pool.QueryRow(f.ctx, `SELECT f.association_pending,e.id FROM feedback_events f JOIN events e ON e.id=f.associated_event_receipt_id WHERE f.event_id=$1`, feedbackID).Scan(&pending, &target); err != nil || pending || target <= 0 {
		t.Fatal("subsequent error did not bind pending feedback", pending, target, err)
	}
}
