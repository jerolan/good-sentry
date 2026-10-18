//go:build integration

package store

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Actual PostgreSQL blockers establish capture/projection order. The ordinary
// retention tests cannot detect stale duplicate children after deletion, reversed
// capture gates, a cleanup that waits on a projector, or partial cancellation.
// No production hooks or timing assumptions supply the ordering being tested.
func TestTransactionRetentionOrdersDuplicateChildrenAroundDeletion(t *testing.T) {
	for _, captureFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("captureFirst=%v", captureFirst), func(t *testing.T) {
			f := resolutionStore(t)
			ev := operationEvent(1, time.Now(), `[{"op":"db","description":"retention order"}]`)
			if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, deleteOrderParts("old")); err != nil || !fresh {
				t.Fatal(fresh, err)
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE events SET created_at=now()-interval '2 days'`); err != nil {
				t.Fatal(err)
			}
			barrier, barrierPID := captureItemsBarrier(t, f)
			table := "envelope_items"
			if captureFirst {
				table = "attachments"
			}
			if _, err := barrier.Exec(f.ctx, "LOCK TABLE "+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			capturer, cleaner := captureItemsOther(t, f), captureItemsOther(t, f)
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			captured := make(chan captureItemsResult, 1)
			cleaned := make(chan error, 1)
			capture := func() {
				id, fresh, err := capturer.Ingest(ctx, f.project.ID, ev, deleteOrderParts("stale"))
				captured <- captureItemsResult{id, fresh, err}
			}
			clean := func() {
				n, spans, err := cleaner.CleanupTransactions(ctx, 1)
				if err == nil && (n != 1 || spans != 0) {
					err = fmt.Errorf("cleanup committed %d/%d instead of1/0", n, spans)
				}
				cleaned <- err
			}
			if captureFirst {
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
			if result := captureItemsAwait(t, captured); result.err != nil || result.fresh {
				t.Fatalf("duplicate capture changed its acknowledgment: %+v", result)
			}
			deleteOrderRows(t, f, 0, 0, 0, 0)
			if _, fresh, err := capturer.Ingest(ctx, f.project.ID, ev, deleteOrderParts("new")); err != nil || !fresh {
				t.Fatalf("fresh retry after physical expiry: %v %v", fresh, err)
			}
			deleteOrderRows(t, f, 0, 1, 1, 1)
			var filename, itemType string
			if err := f.db.pool.QueryRow(ctx, `SELECT a.filename,i.item_type FROM attachments a
 JOIN envelope_items i ON i.project_id=a.project_id AND i.event_id=a.event_id`).Scan(&filename, &itemType); err != nil || filename != "new.bin" || itemType != "new" {
				t.Fatalf("stale children attached to new generation: %q %q %v", filename, itemType, err)
			}
		})
	}
}

func TestTransactionRetentionSkipsActiveProjectorAndRemovesItsCommittedProjection(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	id := historicalOperationEvent(t, ctx, db, project.ID, operationEvent(1, time.Now(), `[{"op":"db","description":"active projection"}]`))
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	barrier, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(ctx)
	var pid int
	if err := barrier.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.Exec(ctx, "LOCK TABLE transaction_span_operations IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		processed, err := db.ProjectTransactionSpans(bounded)
		if err == nil && !processed {
			err = fmt.Errorf("projector never claimed historical receipt%s", id)
		}
		done <- err
	}()
	groupWaitBlocked(t, bounded, barrier, pid, 1)
	if n, spans, err := db.CleanupTransactions(bounded, 1); err != nil || n != 0 || spans != 0 {
		t.Fatalf("cleanup did not skip the active projector: %d/%d %v", n, spans, err)
	}
	if err := barrier.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, bounded, done)
	var projected int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM transaction_span_operations WHERE event_id::text=$1", id).Scan(&projected); err != nil || projected != 1 {
		t.Fatal("projector did not commit before retry", projected, err)
	}
	if n, spans, err := db.CleanupTransactions(bounded, 1); err != nil || n != 1 || spans != 0 {
		t.Fatalf("later cleanup failed: %d/%d %v", n, spans, err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM transaction_span_operations").Scan(&projected); err != nil || projected != 0 {
		t.Fatal("expired projection survived its receipt", projected, err)
	}
}

func TestTransactionRetentionCancellationRollsBackLegacyAndChildren(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, _, err := db.Ingest(ctx, project.ID, operationEvent(1, time.Now(), `[{"op":"db"}]`), deleteOrderParts("cancel")); err != nil {
		t.Fatal(err)
	}
	if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("1111111111111111", true, 1, 2)), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days';
 UPDATE spans SET created_at=now()-interval '2 days';
 CREATE FUNCTION wait_transaction_retention() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,8348)); RETURN OLD; END $$;
 CREATE TRIGGER wait_transaction_retention BEFORE DELETE ON spans FOR EACH ROW EXECUTE FUNCTION wait_transaction_retention()`); err != nil {
		t.Fatal(err)
	}
	checkpoint := groupDatabaseSnapshot(t, ctx, db, 23)
	barrier, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(ctx)
	var pid int
	if err := barrier.QueryRow(ctx, `SELECT pg_backend_pid() FROM (SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),8348))) gate`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		n, spans, err := db.CleanupTransactions(canceled, 1)
		if err == nil || n != 0 || spans != 0 {
			done <- fmt.Errorf("canceled cleanup acknowledged %d/%d err=%v", n, spans, err)
			return
		}
		done <- nil
	}()
	groupWaitBlocked(t, ctx, barrier, pid, 1)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup ignored cancellation after dependent deletion")
	}
	if actual := groupDatabaseSnapshot(t, ctx, db, 23); !reflect.DeepEqual(actual, checkpoint) {
		t.Fatal("canceled cleanup committed earlier receipt/child removal")
	}
}
