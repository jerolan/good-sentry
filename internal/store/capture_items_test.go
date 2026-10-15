//go:build integration

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are CaptureItems receipt/byte/transaction owners. The deletion provider
// has separate owners; SQL below only establishes blockers or receipt fixtures.
type captureItemsResult struct {
	issue string
	fresh bool
	err   error
}

func captureItemsOther(t *testing.T, f resolutionFixture) *Store {
	t.Helper()
	other, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	return other
}

func captureItemsBarrier(t *testing.T, f resolutionFixture) (pgx.Tx, int) {
	t.Helper()
	tx, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(f.ctx) })
	var pid int
	if err := tx.QueryRow(f.ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return tx, pid
}

func captureItemsAwait(t *testing.T, done <-chan captureItemsResult) captureItemsResult {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("capture did not finish after the observed blocker was released")
		return captureItemsResult{}
	}
}

func captureItemsDigest(prefix string, fragments ...[]byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(prefix))
	for _, fragment := range fragments {
		_, _ = hash.Write(fragment)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func captureItemsPopulatedOutbox(t *testing.T, f resolutionFixture) {
	t.Helper()
	var deliveries int
	if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM alert_deliveries").Scan(&deliveries); err != nil || deliveries == 0 {
		t.Fatalf("fixture must have actual queued deliveries: count=%d err=%v", deliveries, err)
	}
}

func TestCaptureItemsAllBatchesBinaryIdentityAndRetryChildren(t *testing.T) {
	f := resolutionStore(t)
	config := alertConfig("Capture byte receipts")
	config.Quantity = 1
	if _, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config); err != nil {
		t.Fatal(err)
	}
	ev := groupEvent(t, 1, "capture-items", "Original receipt", time.Now().UTC())
	a := CaptureAttachment{Filename: "binary.dat", ContentType: "application/octet-stream", Data: []byte{0, 255, 10, 13, 128}}
	b := CaptureAttachment{Filename: "later.txt", ContentType: "text/plain", Data: []byte("later batch\x00")}
	header := json.RawMessage(` { "type": "future_binary", "length": 3 } `)
	x := CaptureEnvelopeItem{Type: "future_binary", Header: header, Data: []byte{255, 0, 10}}
	y := x
	y.Header = json.RawMessage(`{"type":"future_binary","length":3}`)
	duplicate := a
	duplicate.ContentType = "application/second-copy"
	parts := []CaptureItems{{Attachments: []CaptureAttachment{a}, EnvelopeItems: []CaptureEnvelopeItem{x}}, {}, {Attachments: []CaptureAttachment{duplicate, b}, EnvelopeItems: []CaptureEnvelopeItem{x, y}}}
	issue, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts...)
	if err != nil || !fresh || issue == "" {
		t.Fatalf("parent capture issue=%s fresh=%v err=%v", issue, fresh, err)
	}
	captureItemsPopulatedOutbox(t, f)
	for _, want := range []CaptureAttachment{a, b} {
		var id, contentType string
		var data []byte
		err := f.db.pool.QueryRow(f.ctx, "SELECT id,content_type,data FROM attachments WHERE project_id=$1::bigint AND event_id=$2 AND filename=$3", f.project.ID, ev.ID, want.Filename).Scan(&id, &contentType, &data)
		if err != nil || contentType != want.ContentType || !bytes.Equal(data, want.Data) || id != captureItemsDigest(f.project.ID+":"+ev.ID+":"+want.Filename+":", want.Data) {
			t.Fatalf("attachment %s lost exact bytes, first metadata or content identity: id=%s type=%s data=%x err=%v", want.Filename, id, contentType, data, err)
		}
	}
	for _, want := range []CaptureEnvelopeItem{x, y} {
		id := captureItemsDigest(f.project.ID+":"+ev.ID+":"+want.Type+":", want.Header, want.Data)
		var storedHeader, data []byte
		err := f.db.pool.QueryRow(f.ctx, "SELECT header,data FROM envelope_items WHERE id=$1 AND project_id=$2::bigint AND event_id=$3", id, f.project.ID, ev.ID).Scan(&storedHeader, &data)
		var gotJSON, wantJSON any
		if err != nil || !bytes.Equal(data, want.Data) || json.Unmarshal(storedHeader, &gotJSON) != nil || json.Unmarshal(want.Header, &wantJSON) != nil || !reflect.DeepEqual(gotJSON, wantJSON) {
			t.Fatalf("opaque raw-header identity/binary bytes were not retained: id=%s data=%x err=%v", id, data, err)
		}
	}
	var attachments, opaque int
	if err := f.db.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)").Scan(&attachments, &opaque); err != nil || attachments != 2 || opaque != 2 {
		t.Fatalf("all batches and content dedup counts=%d/%d err=%v", attachments, opaque, err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	retry := groupEvent(t, 1, "must-not-allocate", "Changed duplicate payload", ev.Timestamp.Add(time.Hour))
	c := CaptureAttachment{Filename: "retry.bin", ContentType: "application/octet-stream", Data: []byte{128, 0, 254}}
	z := CaptureEnvelopeItem{Type: "future_retry", Header: json.RawMessage(`{"type":"future_retry"}`), Data: []byte{13, 0, 255}}
	added := CaptureItems{Attachments: []CaptureAttachment{c}, EnvelopeItems: []CaptureEnvelopeItem{z}}
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, retry, CaptureItems{}, added); err != nil || fresh || id != issue {
		t.Fatalf("duplicate adding new children id=%s fresh=%v err=%v", id, fresh, err)
	}
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, table := range []string{"attachments", "envelope_items"} {
		if before[table] == after[table] {
			t.Fatalf("duplicate failed to add new %s", table)
		}
		delete(before, table)
		delete(after, table)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("duplicate children replayed raw receipt, grouping, counters, release, policies or outbox")
	}
	stable := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, retry, added, parts[0], added); err != nil || fresh || id != issue {
		t.Fatalf("repeated child content id=%s fresh=%v err=%v", id, fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, stable)
	gate, _ := captureItemsBarrier(t, f)
	if err := lockProjectIssues(f.ctx, gate, f.project.ID, true); err != nil {
		t.Fatal(err)
	}
	fastCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	if id, fresh, err := f.db.Ingest(fastCtx, f.project.ID, retry); err != nil || fresh || id != issue {
		t.Fatalf("bare duplicate did not finish while the exclusive project gate was held: id=%s fresh=%v err=%v", id, fresh, err)
	}
}

func TestCaptureItemsChildFailureAndPostWriteCancellationRollback(t *testing.T) {
	for _, cancelAfterWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel-after-write=%v", cancelAfterWrite), func(t *testing.T) {
			f := resolutionStore(t)
			config := alertConfig("Capture rollback history")
			config.Quantity = 1
			if _, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config); err != nil {
				t.Fatal(err)
			}
			groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "retained-control", "Retained control", time.Now()))
			captureItemsPopulatedOutbox(t, f)
			ev := groupEvent(t, 2, "atomic-new-parent", "Atomic parent", time.Now())
			parts := CaptureItems{Attachments: []CaptureAttachment{{Filename: "uncommitted.bin", ContentType: "application/octet-stream", Data: []byte{0, 255}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "future", Header: json.RawMessage(`{"type":`), Data: []byte{255, 0}}}}
			if !cancelAfterWrite {
				before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
				_, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts)
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "22P02" {
					t.Fatalf("invalid opaque JSON did not fail at actual child storage: %v", err)
				}
				groupRequireUnchanged(t, f.ctx, f.db, before)
			} else {
				f.release(t, ev.Release)
				before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
				parts.EnvelopeItems[0].Header = json.RawMessage(`{"type":"future"}`)
				barrier, pid := captureItemsBarrier(t, f)
				if _, err := barrier.Exec(f.ctx, "SELECT id FROM releases WHERE version=$1 FOR UPDATE", ev.Release); err != nil {
					t.Fatal(err)
				}
				other := captureItemsOther(t, f)
				captureCtx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan captureItemsResult, 1)
				go func() {
					id, fresh, err := other.Ingest(captureCtx, f.project.ID, ev, parts)
					done <- captureItemsResult{id, fresh, err}
				}()
				capturePID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
				for _, table := range []string{"attachments", "envelope_items"} {
					var holds bool
					if err := barrier.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation=$2::regclass AND mode='RowExclusiveLock' AND granted)", capturePID, table).Scan(&holds); err != nil || !holds {
						t.Fatalf("capture reached release wait without retaining its prior child writes on %s: lock=%v err=%v", table, holds, err)
					}
				}
				cancel()
				if result := captureItemsAwait(t, done); result.err == nil {
					t.Fatal("canceled capture acknowledged uncommitted bytes")
				}
				if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				groupRequireUnchanged(t, f.ctx, f.db, before)
			}
			parts.EnvelopeItems[0].Header = json.RawMessage(`{"type":"future"}`)
			if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts); err != nil || !fresh || id == "" {
				t.Fatalf("failed capture identity was not retryable: id=%s fresh=%v err=%v", id, fresh, err)
			}
		})
	}
}

func TestCaptureItemsParentAndChildrenCommitBeforeExclusiveProjectCut(t *testing.T) {
	for _, transaction := range []bool{false, true} {
		t.Run(fmt.Sprintf("transaction=%v", transaction), func(t *testing.T) {
			f := resolutionStore(t)
			other := captureItemsOther(t, f)
			ev := groupEvent(t, 1, "commit-cut", "Commit cut", time.Now())
			if transaction {
				var err error
				ev, err = event.Normalize(json.RawMessage(`{"type":"transaction","event_id":"00000000000000000000000000000001","transaction":"GET /capture","start_timestamp":1700000000,"timestamp":1700000001}`), "", time.Now())
				if err != nil {
					t.Fatal(err)
				}
			}
			barrier, pid := captureItemsBarrier(t, f)
			if _, err := barrier.Exec(f.ctx, "LOCK TABLE attachments IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			captureCtx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			done := make(chan captureItemsResult, 1)
			parts := CaptureItems{Attachments: []CaptureAttachment{{Filename: "cut.bin", ContentType: "application/octet-stream", Data: []byte{0, 255}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "future_cut", Header: json.RawMessage(`{"type":"future_cut"}`), Data: []byte{254, 0}}}}
			go func() {
				id, fresh, err := other.Ingest(captureCtx, f.project.ID, ev, parts)
				done <- captureItemsResult{id, fresh, err}
			}()
			capturePID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
			var parentRows int
			if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM events").Scan(&parentRows); err != nil || parentRows != 0 {
				t.Fatalf("parent became visible before its blocked attachment: rows=%d err=%v", parentRows, err)
			}
			cut, err := f.db.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer cut.Rollback(f.ctx)
			gateDone := make(chan error, 1)
			go func() { gateDone <- lockProjectIssues(captureCtx, cut, f.project.ID, true) }()
			groupWaitBlocked(t, f.ctx, barrier, capturePID, 1)
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			if result := captureItemsAwait(t, done); result.err != nil || !result.fresh || (!transaction && result.issue == "") || (transaction && result.issue != "") {
				t.Fatalf("capture after attachment barrier=%+v", result)
			}
			select {
			case err := <-gateDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("exclusive project cut did not follow capture commit")
			}
			var events, attachments, opaque int
			if err := cut.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM events),(SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)").Scan(&events, &attachments, &opaque); err != nil || events != 1 || attachments != 1 || opaque != 1 {
				t.Fatalf("exclusive cut did not observe the complete committed capture: %d/%d/%d err=%v", events, attachments, opaque, err)
			}
		})
	}
}

func TestCaptureItemsDuplicatePinsNumericReceiptAcrossProjectWait(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%v", replacement), func(t *testing.T) {
			f := resolutionStore(t)
			other := captureItemsOther(t, f)
			ev := groupEvent(t, 1, "receipt-before-cut", "Original receipt", time.Now())
			owner := groupCapture(t, f.ctx, f.db, f.project, ev)
			replacementOwner := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "different-receipt-owner", "Other owner", time.Now()))
			var oldRow int64
			if err := f.db.pool.QueryRow(f.ctx, "SELECT id FROM events WHERE event_id=$1", ev.ID).Scan(&oldRow); err != nil {
				t.Fatal(err)
			}
			barrier, pid := captureItemsBarrier(t, f)
			if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
				t.Fatal(err)
			}
			captureCtx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			done := make(chan captureItemsResult, 1)
			parts := CaptureItems{Attachments: []CaptureAttachment{{Filename: "stale.bin", ContentType: "application/octet-stream", Data: []byte{255, 0}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "stale", Header: json.RawMessage(`{"type":"stale"}`), Data: []byte{0, 254}}}}
			go func() {
				id, fresh, err := other.Ingest(captureCtx, f.project.ID, ev, parts)
				done <- captureItemsResult{id, fresh, err}
			}()
			groupWaitBlocked(t, f.ctx, barrier, pid, 1)
			// This is a receipt ABA fixture, not a simulated issue DELETE provider.
			// It changes only this known row while the real duplicate awaits the gate.
			if replacement {
				var newRow int64
				err := barrier.QueryRow(f.ctx, `WITH removed AS (DELETE FROM events WHERE id=$1 RETURNING *)
INSERT INTO events(project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at)
SELECT project_id,event_id,$2::bigint,$2::bigint,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data,created_at FROM removed RETURNING id`, oldRow, replacementOwner).Scan(&newRow)
				if err != nil || newRow == oldRow {
					t.Fatalf("receipt ABA fixture new=%d old=%d err=%v", newRow, oldRow, err)
				}
			} else if _, err := barrier.Exec(f.ctx, "DELETE FROM events WHERE id=$1", oldRow); err != nil {
				t.Fatal(err)
			}
			// Snapshot through the owning fixture transaction before the waiter runs.
			var expectedEvents string
			if err := barrier.QueryRow(f.ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY to_jsonb(e)::text),'[]'::jsonb)::text FROM events e").Scan(&expectedEvents); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
			before["events"] = expectedEvents
			if err := barrier.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			if result := captureItemsAwait(t, done); result.err != nil || result.fresh || result.issue != owner {
				t.Fatalf("stale duplicate did not retain its original acknowledgment: %+v", result)
			}
			groupRequireUnchanged(t, f.ctx, f.db, before)
			freshEvent := ev
			if replacement {
				freshEvent = groupEvent(t, 3, "receipt-before-cut", "Later actual capture", time.Now())
			}
			if id, fresh, err := other.Ingest(f.ctx, f.project.ID, freshEvent, parts); err != nil || !fresh || id != owner {
				t.Fatalf("later actual capture was not admitted with its own children: id=%s fresh=%v err=%v", id, fresh, err)
			}
			var attachments, opaque int
			if err := f.db.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)").Scan(&attachments, &opaque); err != nil || attachments != 1 || opaque != 1 {
				t.Fatalf("later capture child counts=%d/%d err=%v", attachments, opaque, err)
			}
		})
	}
}

func TestCaptureItemsStandaloneOpaqueOrdersBeforeAndAfterExclusiveCut(t *testing.T) {
	f := resolutionStore(t)
	other := captureItemsOther(t, f)
	ev := groupEvent(t, 1, "standalone-parent", "Standalone parent", time.Now())
	groupCapture(t, f.ctx, f.db, f.project, ev)
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "LOCK TABLE envelope_items IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	writeCtx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	beforeDone := make(chan error, 1)
	go func() {
		beforeDone <- other.SaveEnvelopeItem(writeCtx, f.project.ID, ev.ID, "before_cut", json.RawMessage(`{"type":"before_cut"}`), []byte{255, 0})
	}()
	writerPID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
	cut, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cut.Rollback(f.ctx)
	var cutPID int
	if err := cut.QueryRow(f.ctx, "SELECT pg_backend_pid()").Scan(&cutPID); err != nil {
		t.Fatal(err)
	}
	gateDone := make(chan error, 1)
	go func() { gateDone <- lockProjectIssues(writeCtx, cut, f.project.ID, true) }()
	groupWaitBlocked(t, f.ctx, barrier, writerPID, 1)
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{beforeDone, gateDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("standalone write/exclusive cut did not finish after table unlock")
		}
	}
	var count int
	if err := cut.QueryRow(f.ctx, "SELECT count(*) FROM envelope_items WHERE item_type='before_cut' AND event_id=$1", ev.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("standalone parented write was not committed before the cut: count=%d err=%v", count, err)
	}
	afterDone := make(chan error, 1)
	go func() {
		afterDone <- other.SaveEnvelopeItem(writeCtx, f.project.ID, "", "after_cut", json.RawMessage(`{"type":"after_cut"}`), []byte{0, 254})
	}()
	groupWaitBlocked(t, f.ctx, cut, cutPID, 1)
	if err := cut.QueryRow(f.ctx, "SELECT count(*) FROM envelope_items WHERE item_type='after_cut'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("later independent opaque item crossed the exclusive cut: count=%d err=%v", count, err)
	}
	if err := cut.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-afterDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("standalone later write did not follow the exclusive commit")
	}
	if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM envelope_items WHERE item_type='after_cut' AND event_id=''").Scan(&count); err != nil || count != 1 {
		t.Fatalf("later independent opaque write was discarded: count=%d err=%v", count, err)
	}
}
