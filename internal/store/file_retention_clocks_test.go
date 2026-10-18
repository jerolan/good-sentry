//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Real waits distinguish insertion time from transaction start; the boundary
// and same-hash replacement cuts cannot be reached by sequential expiry tests.
// Migration/reopen owns historical preservation. Use real PG clocks and gates.
func TestFileReceiptUsesInsertionClockAfterGateWait(t *testing.T) {
	for _, writer := range []string{"map", "capture", "orphan"} {
		t.Run(writer, func(t *testing.T) {
			f := resolutionStore(t)
			barrier, pid := captureItemsBarrier(t, f)
			if writer == "map" {
				if err := lockReleaseArtifacts(f.ctx, barrier, f.project.ID, ""); err != nil {
					t.Fatal(err)
				}
			} else if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			ev := groupEvent(t, 1, "clock", "Clock receipt", time.Now())
			go func() {
				var err error
				switch writer {
				case "map":
					_, _, err = f.db.SaveSourceMap(ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/clock.map", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", emptySourceMap)
				case "capture":
					_, _, err = f.db.Ingest(ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "clock.bin", Data: []byte("clock")}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile"}`), Data: []byte("clock")}}})
				case "orphan":
					err = f.db.SaveEnvelopeItem(ctx, f.project.ID, "", "session", json.RawMessage(`{"type":"session"}`), []byte("clock"))
				}
				done <- err
			}()
			groupWaitBlocked(t, ctx, barrier, pid, 1)
			var released time.Time
			if err := barrier.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&released); err != nil {
				t.Fatal(err)
			}
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, done)
			tables := []string{"envelope_items"}
			if writer == "map" {
				tables = []string{"source_maps"}
			} else if writer == "capture" {
				tables = append(tables, "attachments")
			}
			for _, table := range tables {
				var receipt time.Time
				if err := f.db.pool.QueryRow(ctx, "SELECT min(created_at) FROM "+table).Scan(&receipt); err != nil || receipt.Before(released) {
					t.Fatal("file clock precedes insertion after gate wait", table, receipt, released, err)
				}
			}
		})
	}
}

func TestFileRetentionRechecksFrozenCutoffAndReplacedHashes(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%v", replacement), func(t *testing.T) {
			f := resolutionStore(t)
			if _, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/boundary.map", "boundary", "", "", emptySourceMap); err != nil {
				t.Fatal(err)
			}
			groupCaptureWith := CaptureItems{Attachments: []CaptureAttachment{{Filename: "boundary.bin", Data: []byte("boundary")}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile"}`), Data: []byte("boundary")}}}
			if _, _, err := f.db.Ingest(f.ctx, f.project.ID, groupEvent(t, 1, "boundary", "Keep boundary parent", time.Now()), groupCaptureWith); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE source_maps SET created_at=now()-interval '2 days'; UPDATE attachments SET created_at=now()-interval '2 days'; UPDATE envelope_items SET created_at=now()-interval '2 days'`); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
			barrier, blockerPID := captureItemsBarrier(t, f)
			if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				maps, attachments, items, err := f.db.CleanupFiles(ctx, 1)
				if err == nil && (maps != 0 || attachments != 0 || items != 0) {
					err = fmt.Errorf("frozen boundary/fresh generation removed: %d/%d/%d", maps, attachments, items)
				}
				done <- err
			}()
			pid := groupWaitBlocked(t, ctx, barrier, blockerPID, 1)[0]
			var cutoff time.Time
			if err := barrier.QueryRow(ctx, "SELECT xact_start-interval '24 hours' FROM pg_stat_activity WHERE pid=$1", pid).Scan(&cutoff); err != nil {
				t.Fatal(err)
			}
			if replacement {
				// Own fixture only: a prior gate owner replaces immutable children.
				// Hash IDs are reused, but defaults must stamp fresh receipts.
				if _, err := barrier.Exec(ctx, `WITH old AS (DELETE FROM attachments RETURNING *)
 INSERT INTO attachments(id,project_id,event_id,filename,content_type,data) SELECT id,project_id,event_id,filename,content_type,data FROM old;
 WITH old AS (DELETE FROM envelope_items RETURNING *)
 INSERT INTO envelope_items(id,project_id,event_id,item_type,header,data) SELECT id,project_id,event_id,item_type,header,data FROM old;
 WITH old AS (DELETE FROM source_maps RETURNING *)
 INSERT INTO source_maps(project_id,name,release,dist,debug_id,checksum,size,data,code_files) SELECT project_id,name,release,dist,debug_id,checksum,size,data,code_files FROM old`); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, table := range []string{"source_maps", "attachments", "envelope_items"} {
					if _, err := barrier.Exec(ctx, "UPDATE "+table+" SET created_at=$1", cutoff); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := barrier.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, done)
			for _, table := range []string{"source_maps", "attachments", "envelope_items"} {
				var count int
				var receipt time.Time
				if err := f.db.pool.QueryRow(ctx, "SELECT count(*),min(created_at) FROM "+table).Scan(&count, &receipt); err != nil || count != 1 || replacement && !receipt.After(cutoff) || !replacement && !receipt.Equal(cutoff) {
					t.Fatal("boundary/replacement survivor lost", table, count, receipt, cutoff, err)
				}
			}
			after := groupDatabaseSnapshot(t, ctx, f.db, 26)
			for table, checkpoint := range before {
				if table != "source_maps" && table != "attachments" && table != "envelope_items" && after[table] != checkpoint {
					t.Fatal("waiting/replacement changed independent state", table)
				}
			}
		})
	}
}

func TestFileRetentionMigration026PreservesAndReopens(t *testing.T) {
	f := resolutionStore(t)
	if _, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/before026.map", "before026", "", "", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, groupEvent(t, 1, "before026", "Keep migration parent", time.Now()), CaptureItems{Attachments: []CaptureAttachment{{Filename: "before026.bin", Data: []byte("old")}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile"}`), Data: []byte("old")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `DROP INDEX source_maps_receipt_retention; DROP INDEX attachments_receipt_retention; DROP INDEX envelope_items_receipt_retention;
 ALTER TABLE source_maps ALTER COLUMN created_at SET DEFAULT now(); ALTER TABLE attachments ALTER COLUMN created_at SET DEFAULT now(); ALTER TABLE envelope_items ALTER COLUMN created_at SET DEFAULT now(); DELETE FROM schema_migrations WHERE version=26`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 25)
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if after := groupDatabaseSnapshot(t, f.ctx, reopened, 25); !reflect.DeepEqual(after, before) {
			t.Fatal("026 rewrote prior rows or migration receipts")
		}
		var receipt string
		if err := reopened.pool.QueryRow(f.ctx, "SELECT checksum FROM schema_migrations WHERE version=26").Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/026_file_retention.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("026 receipt missing/changed", err)
		}
		for _, table := range []string{"source_maps", "attachments", "envelope_items"} {
			var index, expression string
			if err := reopened.pool.QueryRow(f.ctx, `SELECT to_regclass($1)::text,column_default FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$2 AND column_name='created_at'`, table+"_receipt_retention", table).Scan(&index, &expression); err != nil || index != table+"_receipt_retention" || expression != "clock_timestamp()" {
				t.Fatal("026 index or insertion default missing", table, index, expression, err)
			}
		}
		reopened.Close()
	}
}
