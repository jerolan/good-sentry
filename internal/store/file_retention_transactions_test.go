//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func fileRetentionFixture(t *testing.T) resolutionFixture {
	t.Helper()
	f := resolutionStore(t)
	if _, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/atomic.map", "atomic", "", "", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	ev := groupEvent(t, 1, "atomic-file", "Keep file parent", time.Now())
	ev.Release = "atomic"
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "atomic.bin", Data: []byte{0, 255, 1}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile"}`), Data: []byte("atomic opaque")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE source_maps SET created_at=now()-interval '2 days'; UPDATE attachments SET created_at=now()-interval '2 days'; UPDATE envelope_items SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	return f
}

// The transaction owns all three file removals: a later failure/cancellation
// must restore earlier byte deletion. Observe real triggers and lock release;
// sequential result checks or lifecycle tests cannot establish atomicity.
func TestFileRetentionLateFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"statement", "commit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := fileRetentionFixture(t)
			canceled := mode == "cancel"
			failure := `RAISE EXCEPTION 'declared late file deletion failure';`
			trigger := `CREATE TRIGGER fail_file_retention BEFORE DELETE ON envelope_items FOR EACH ROW EXECUTE FUNCTION fail_file_retention()`
			if mode == "commit" {
				trigger = `CREATE CONSTRAINT TRIGGER fail_file_retention AFTER DELETE ON envelope_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_file_retention()`
			}
			if canceled {
				failure = `PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,8352));`
			}
			if _, err := f.db.pool.Exec(f.ctx, `CREATE SEQUENCE file_retention_visits;
 CREATE FUNCTION visit_file_retention() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('file_retention_visits'); RETURN OLD; END $$;
 CREATE TRIGGER visit_file_retention AFTER DELETE ON attachments FOR EACH ROW EXECUTE FUNCTION visit_file_retention();
 CREATE FUNCTION fail_file_retention() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+failure+` RETURN OLD; END $$;
 `+trigger); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
			if !canceled {
				if maps, attachments, items, err := f.db.CleanupFiles(f.ctx, 1); err == nil || maps != 0 || attachments != 0 || items != 0 {
					t.Fatal("late failure acknowledged partial deletion", maps, attachments, items, err)
				}
			} else {
				barrier, pid := captureItemsBarrier(t, f)
				if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),8352))`); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					maps, attachments, items, err := f.db.CleanupFiles(ctx, 1)
					if err == nil || maps != 0 || attachments != 0 || items != 0 {
						done <- fmt.Errorf("canceled deletion acknowledged %d/%d/%d: %v", maps, attachments, items, err)
						return
					}
					done <- nil
				}()
				groupWaitBlocked(t, f.ctx, barrier, pid, 1)
				cancel()
				deleteOrderAwait(t, f.ctx, done)
				if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				guardCtx, stop := context.WithTimeout(f.ctx, 5*time.Second)
				defer stop()
				guard, err := f.db.pool.Begin(guardCtx)
				if err != nil {
					t.Fatal(err)
				}
				defer guard.Rollback(f.ctx)
				if _, err := guard.Exec(guardCtx, "LOCK TABLE source_maps,attachments,envelope_items IN ACCESS EXCLUSIVE MODE"); err != nil {
					t.Fatal("canceled transaction still owns file locks", err)
				}
				if err := guard.Commit(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			var visited bool
			if err := f.db.pool.QueryRow(f.ctx, "SELECT is_called FROM file_retention_visits").Scan(&visited); err != nil || !visited {
				t.Fatal("failure never followed an earlier file deletion", visited, err)
			}
			if after := groupDatabaseSnapshot(t, f.ctx, f.db, 26); !reflect.DeepEqual(after, before) {
				t.Fatal("later file failure committed partial removal or parent changes")
			}
		})
	}
}

func TestFileRetentionSkipsLockedRowsAndBusyMaintainer(t *testing.T) {
	f := fileRetentionFixture(t)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
	barrier, _ := captureItemsBarrier(t, f)
	for _, table := range []string{"source_maps", "attachments", "envelope_items"} {
		if _, err := barrier.Exec(f.ctx, "SELECT id FROM "+table+" FOR UPDATE"); err != nil {
			t.Fatal(err)
		}
	}
	if maps, attachments, items, err := f.db.CleanupFiles(f.ctx, 1); err != nil || maps != 0 || attachments != 0 || items != 0 {
		t.Fatal("locked file row not skipped", maps, attachments, items, err)
	}
	if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':file-retention',0))`); err != nil {
		t.Fatal(err)
	}
	other := captureItemsOther(t, f)
	if maps, attachments, items, err := other.CleanupFiles(f.ctx, 1); err != nil || maps != 0 || attachments != 0 || items != 0 {
		t.Fatal("busy maintainer not idle", maps, attachments, items, err)
	}
	if after := groupDatabaseSnapshot(t, f.ctx, f.db, 26); !reflect.DeepEqual(after, before) {
		t.Fatal("idle cleanup changed file or independent state")
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if maps, attachments, items, err := other.CleanupFiles(f.ctx, 1); err != nil || maps != 1 || attachments != 1 || items != 1 {
		t.Fatal("unlocked cohort not reclaimed", maps, attachments, items, err)
	}
}
