//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// The owner is the real bulk selector across a native topology wait. A frozen
// root alone can retarget an explicit alias after a split. Existing merge-wait
// and containment tests do not detach the selected ID from that frozen root.
// Use independent pools and observed PostgreSQL blockers, without a seam.
func TestCompatibleIssueDeletionAndBulkPutDoNotRetargetDetachedExplicitAlias(t *testing.T) {
	for _, method := range []string{"DELETE", "PUT"} {
		t.Run(method, func(t *testing.T) {
			f := resolutionStore(t)
			stamp := time.Now().Add(-time.Hour)
			a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "delete-wait-a", "Unselected root", stamp))
			b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "delete-wait-b", "Selected alias", stamp))
			if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
				t.Fatal(err)
			}
			splitter := captureItemsOther(t, f)
			writer := captureItemsOther(t, f)
			barrier, pid := captureItemsBarrier(t, f)
			if _, err := barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", b); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			splitDone := make(chan error, 1)
			go func() {
				result, err := splitter.UnmergeIssue(ctx, f.owner.ID, a, []string{b})
				if err == nil && !result.Changed {
					err = ErrInvalidIssueChange
				}
				splitDone <- err
			}()
			// Unmerge holds the exclusive gate while its DESC row claim waits.
			splitPID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
			writeDone := make(chan error, 1)
			go func() {
				filter := CompatibleIssueBulkFilter{IDs: []string{b}, Limit: 1000}
				if method == "DELETE" {
					writeDone <- writer.DeleteCompatibleIssues(ctx, f.access, "", filter)
					return
				}
				result, err := writer.UpdateCompatibleIssues(ctx, f.access, "", filter, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
				if err == nil && result.First != nil {
					err = ErrInvalidCompatibleIssueChange
				}
				writeDone <- err
			}()
			// The bulk call has selected the old group and now awaits Unmerge.
			groupWaitBlocked(t, f.ctx, barrier, splitPID, 1)
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			for _, result := range []<-chan error{splitDone, writeDone} {
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("native split and bulk writer did not finish")
				}
			}
			var roots, unresolved, events int
			err := f.db.pool.QueryRow(f.ctx, `SELECT
 (SELECT count(*) FROM issues WHERE id=ANY($1::bigint[]) AND merged_into IS NULL),
 (SELECT count(*) FROM issues WHERE id=ANY($1::bigint[]) AND status='unresolved'),
 (SELECT count(*) FROM events WHERE issue_id=original_issue_id AND issue_id=ANY($1::bigint[]))`, issueNumericIDs([]string{a, b})).Scan(&roots, &unresolved, &events)
			if err != nil || roots != 2 || unresolved != 2 || events != 2 {
				t.Fatalf("%s retargeted the unselected remainder or backfilled the detached root: roots=%d unresolved=%d events=%d err=%v", method, roots, unresolved, events, err)
			}
		})
	}
}

// Exercise actual PostgreSQL failure after earlier destructive statements and
// cancellation during a cascading byte delete. Both must restore every table.
func TestCompatibleIssueDeletionFailureAndCancellationRollbackEveryTable(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "foreign-key-failure"
		if canceled {
			name = "canceled-cascading-attachment-delete"
		}
		t.Run(name, func(t *testing.T) {
			f := resolutionStore(t)
			config := alertConfig("Deletion rollback history")
			config.Quantity = 1
			if _, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config); err != nil {
				t.Fatal(err)
			}
			ev := groupEvent(t, 1, "delete-rollback", "Retained on failure", time.Now())
			parts := CaptureItems{Attachments: []CaptureAttachment{{Filename: "rollback.bin", ContentType: "application/octet-stream", Data: []byte{0, 255}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "future", Header: json.RawMessage(`{"type":"future"}`), Data: []byte{255, 0}}}}
			id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts)
			if err != nil || !fresh {
				t.Fatalf("capture id=%s fresh=%v err=%v", id, fresh, err)
			}
			captureItemsPopulatedOutbox(t, f)
			writer := captureItemsOther(t, f)
			if !canceled {
				// An isolated fixture FK forces the actual DELETE to fail. No
				// application hook or replacement deletion implementation is used.
				if _, err := f.db.pool.Exec(f.ctx, "CREATE TABLE issue_delete_guard(issue_id bigint REFERENCES issues(id))"); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.pool.Exec(f.ctx, "INSERT INTO issue_delete_guard(issue_id) VALUES($1::bigint)", id); err != nil {
					t.Fatal(err)
				}
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
			if canceled {
				barrier, pid := captureItemsBarrier(t, f)
				if _, err := barrier.Exec(f.ctx, "LOCK TABLE attachments IN ACCESS EXCLUSIVE MODE"); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					done <- writer.DeleteCompatibleIssues(ctx, f.access, "", CompatibleIssueBulkFilter{IDs: []string{id}, Limit: 1000})
				}()
				deletePID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
				for _, table := range []string{"issue_hash_routes", "envelope_items"} {
					var holds bool
					if err := barrier.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation=$2::regclass AND mode='RowExclusiveLock' AND granted)", deletePID, table).Scan(&holds); err != nil || !holds {
						t.Fatalf("delete did not reach its prior %s writes: lock=%v err=%v", table, holds, err)
					}
				}
				cancel()
				groupAwaitCancellation(t, done, "deletion waiting on cascading attachment storage")
				if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				// Wait for the canceled connection to release its transaction.
				cut, _ := captureItemsBarrier(t, f)
				if err := lockProjectIssues(f.ctx, cut, f.project.ID, true); err != nil {
					t.Fatal(err)
				}
				if err := cut.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				err := writer.DeleteCompatibleIssue(f.ctx, f.access, id)
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
					t.Fatalf("expected the real foreign-key failure, got %v", err)
				}
			}
			groupRequireUnchanged(t, f.ctx, f.db, before)
			if !canceled {
				if _, err := f.db.pool.Exec(f.ctx, "DROP TABLE issue_delete_guard"); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.DeleteCompatibleIssue(f.ctx, f.access, id); err != nil {
				t.Fatalf("retry after failure did not delete the retained group: %v", err)
			}
		})
	}
}

// A selected ID beyond the applied limit is still part of admission. The real
// competing DELETE removes that ID while both bulk writers wait for its gate.
func TestCompatibleIssueDeletionAndBulkPutRecheckEveryIDAfterDeletionWait(t *testing.T) {
	for _, method := range []string{"DELETE", "PUT"} {
		t.Run(method, func(t *testing.T) {
			f := resolutionStore(t)
			stamp := time.Now().Add(-time.Hour)
			a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "deleted-outside-limit", "Older outside limit", stamp))
			b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "retained-limit-candidate", "Newest candidate", stamp.Add(time.Minute)))
			deleter, writer := captureItemsOther(t, f), captureItemsOther(t, f)
			barrier, pid := captureItemsBarrier(t, f)
			if _, err := barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", a); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			deleteDone := make(chan error, 1)
			go func() { deleteDone <- deleter.DeleteCompatibleIssue(ctx, f.access, a) }()
			deletePID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
			writeDone := make(chan error, 1)
			go func() {
				filter := CompatibleIssueBulkFilter{IDs: []string{a, b}, Limit: 1}
				if method == "DELETE" {
					writeDone <- writer.DeleteCompatibleIssues(ctx, f.access, "", filter)
					return
				}
				result, err := writer.UpdateCompatibleIssues(ctx, f.access, "", filter, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
				if err == nil && result.First != nil {
					err = ErrInvalidCompatibleIssueChange
				}
				writeDone <- err
			}()
			groupWaitBlocked(t, f.ctx, barrier, deletePID, 1)
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			for _, done := range []<-chan error{deleteDone, writeDone} {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("competing deletion and bulk writer did not finish")
				}
			}
			var rows int
			if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM issues WHERE id=$1::bigint AND status='unresolved'", b).Scan(&rows); err != nil || rows != 1 {
				t.Fatalf("%s changed the limited candidate after another admitted ID disappeared: retained=%d err=%v", method, rows, err)
			}
			if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM events WHERE event_id=$1", "00000000000000000000000000000001").Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("the actual competing deletion did not remove its own event: rows=%d err=%v", rows, err)
			}
		})
	}
}

func TestCompatibleIssueDeletionRechecksRowFilterWithoutBackfill(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "unselected-backfill", "Older control", stamp))
	a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "row-filter-candidate", "Newest candidate", stamp.Add(time.Minute)))
	writer := captureItemsOther(t, f)
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", a); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- writer.DeleteCompatibleIssues(ctx, f.access, "", CompatibleIssueBulkFilter{Query: "is:unresolved", Limit: 1})
	}()
	groupWaitBlocked(t, f.ctx, barrier, pid, 1)
	// An isolated row-only maintenance fixture changes the selected predicate.
	if _, err := barrier.Exec(f.ctx, "UPDATE issues SET status='resolved' WHERE id=$1::bigint", a); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("deletion did not finish after row predicate changed")
	}
	var rows int
	if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM issues WHERE id=ANY($1::bigint[])", issueNumericIDs([]string{a, b})).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("row recheck deleted an excluded candidate or backfilled another group: retained=%d err=%v", rows, err)
	}
}
