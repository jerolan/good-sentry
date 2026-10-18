//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// This owner protects actual late-recount rollback, receipt/maintenance lock
// contention and additive upgrade preservation. Ordinary capture/group tests
// cannot observe a cleanup transaction after its bytes have been deleted.
func TestEventRetentionLateRecountFailureAndCancellationRollBack(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", canceled), func(t *testing.T) {
			ctx, db, _, _, project := telemetryStore(t)
			if _, _, err := db.Ingest(ctx, project.ID, groupEvent(t, 1, "late-recount", "Rollback history", time.Now()), deleteOrderParts("rollback")); err != nil {
				t.Fatal(err)
			}
			function := `BEGIN RAISE EXCEPTION 'declared late recount failure'; END`
			if canceled {
				function = `BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,8349)); RETURN NEW; END`
			}
			if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days';
 CREATE FUNCTION wait_error_recount() RETURNS trigger LANGUAGE plpgsql AS $$ `+function+` $$;
 CREATE TRIGGER wait_error_recount BEFORE UPDATE ON issues FOR EACH ROW EXECUTE FUNCTION wait_error_recount()`); err != nil {
				t.Fatal(err)
			}
			checkpoint := groupDatabaseSnapshot(t, ctx, db, 24)
			if !canceled {
				if n, err := db.CleanupEvents(ctx, 1); err == nil || n != 0 {
					t.Fatalf("failed recount acknowledged deletion: %d %v", n, err)
				}
			} else {
				barrier, err := db.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer barrier.Rollback(ctx)
				var pid int
				if err := barrier.QueryRow(ctx, `SELECT pg_backend_pid() FROM (SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),8349))) gate`).Scan(&pid); err != nil {
					t.Fatal(err)
				}
				bounded, cancel := context.WithCancel(ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					n, err := db.CleanupEvents(bounded, 1)
					if err == nil || n != 0 {
						done <- fmt.Errorf("canceled recount acknowledged deletion: %d %v", n, err)
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
					t.Fatal("cleanup ignored cancellation during recount")
				}
			}
			if actual := groupDatabaseSnapshot(t, ctx, db, 24); !reflect.DeepEqual(actual, checkpoint) {
				t.Fatal("late recount failure/cancellation committed receipt or dependent deletion")
			}
		})
	}
}

func TestEventRetentionSkipsLockedReceiptAndBusyMaintainer(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := groupCapture(t, ctx, db, project, groupEvent(t, 1, "locked-retention", "Locked receipt", time.Now()))
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 24)
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "SELECT id FROM events FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CleanupEvents(ctx, 1); err != nil || n != 0 {
		t.Fatal("locked receipt not skipped", n, err)
	}
	if actual := groupDatabaseSnapshot(t, ctx, db, 24); !reflect.DeepEqual(actual, before) {
		t.Fatal("skipped event caused a spurious owner recount")
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':event-retention',0))`); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if n, err := other.CleanupEvents(ctx, 1); err != nil || n != 0 {
		t.Fatal("busy maintainer did not return idle", n, err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := other.CleanupEvents(ctx, 1); err != nil || n != 1 || triageIssue(t, ctx, other, owner.ID, id).Count != 0 {
		t.Fatal("unlocked receipt/owner not reclaimed", n, err)
	}
}

func TestEventRetentionMigration024PreservesRowsAndReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, _, err := db.Ingest(ctx, project.ID, groupEvent(t, 1, "before024", "Before upgrade", time.Now()), deleteOrderParts("before024")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP INDEX events_error_receipt_retention; DELETE FROM schema_migrations WHERE version=24`); err != nil {
		t.Fatal(err)
	}
	checkpoint := groupDatabaseSnapshot(t, ctx, db, 23)
	for range 2 {
		reopened, err := Open(ctx, db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if actual := groupDatabaseSnapshot(t, ctx, reopened, 23); !reflect.DeepEqual(actual, checkpoint) {
			t.Fatal("024 rewrote prior rows or receipts")
		}
		var receipt, index string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum,to_regclass('events_error_receipt_retention')::text FROM schema_migrations WHERE version=24`).Scan(&receipt, &index); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/024_event_retention.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) || index != "events_error_receipt_retention" {
			t.Fatal("024 index/receipt absent or changed", err)
		}
		reopened.Close()
	}
}
