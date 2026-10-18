//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Physical expiry, ownership and all-or-nothing progress belong to PostgreSQL.
// These tests detect occurrence-clock cleanup, independent-data cascades,
// unbounded passes and a late failure committing earlier removals. Existing
// ingestion/query tests never invoke this owner. No production seam is needed.
func TestTransactionRetentionReceiptAgeAndIndependentData(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	old := operationEvent(1, time.Now().Add(365*24*time.Hour), `[{"op":"db","description":"expired"}]`)
	old.Release = "checkout@2"
	retained := operationEvent(2, time.Now().Add(-365*24*time.Hour), `[{"op":"db","description":"retained"}]`)
	retained.Release = old.Release
	for i, label := range []string{"expired", "retained"} {
		incoming := old
		if i == 1 {
			incoming = retained
		}
		if _, fresh, err := db.Ingest(ctx, project.ID, incoming, deleteOrderParts(label)); err != nil || !fresh {
			t.Fatal(fresh, err)
		}
	}
	exception := groupEvent(t, 3, "retention-preserved", "Preserved error", time.Now())
	exception.Release, exception.TraceID = old.Release, old.TraceID
	if _, _, err := db.Ingest(ctx, project.ID, exception, deleteOrderParts("error")); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveEnvelopeItem(ctx, project.ID, "independent-orphan", "future", []byte(`{"type":"future"}`), []byte{0, 255}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestLogs(ctx, project.ID, []byte(fmt.Sprintf(`{"items":[{"timestamp":1,"trace_id":%q,"level":"info","body":"independent","attributes":{}}]}`, old.TraceID))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "independent canonical bytes")); err != nil {
		t.Fatal(err)
	}
	if n, err := db.IngestSpans(ctx, project.ID, spanBatch(t,
		spanFixture("1111111111111111", true, float64(time.Now().Add(365*24*time.Hour).Unix()), float64(time.Now().Add(365*24*time.Hour).Unix()+1)),
		spanFixture("2222222222222222", true, 1, 2)), 2); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE spans SET created_at=now()-interval '2 days' WHERE span_id='1111111111111111'`); err != nil {
		t.Fatal(err)
	}
	var oldID, retainedID string
	if err := db.pool.QueryRow(ctx, "SELECT id::text FROM events WHERE event_id=$1", old.ID).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT id::text FROM events WHERE event_id=$1", retained.ID).Scan(&retainedID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 23)
	detail, err := db.TransactionDetail(ctx, user.ID, org.Slug, "event", retainedID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n, spans, err := db.CleanupTransactions(ctx, 1); err != nil || n != 1 || spans != 1 {
		t.Fatalf("receipt cleanup=%d/%d err=%v", n, spans, err)
	}
	after := groupDatabaseSnapshot(t, ctx, db, 23)
	for table, checkpoint := range before {
		switch table {
		case "events", "spans", "attachments", "envelope_items", "transaction_span_operations":
			continue
		}
		if after[table] != checkpoint {
			t.Fatalf("independent %s changed", table)
		}
	}
	var events, spans, attachments, opaque, operations int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM events),(SELECT count(*) FROM spans),
 (SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items),(SELECT count(*) FROM transaction_span_operations)`).Scan(&events, &spans, &attachments, &opaque, &operations); err != nil || !reflect.DeepEqual([]int{events, spans, attachments, opaque, operations}, []int{2, 1, 2, 3, 1}) {
		t.Fatalf("dependent ownership=%v err=%v", []int{events, spans, attachments, opaque, operations}, err)
	}
	if _, err := db.TransactionDetail(ctx, user.ID, org.Slug, "event", oldID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired selected receipt remains readable", err)
	}
	if actual, err := db.TransactionDetail(ctx, user.ID, org.Slug, "event", retainedID, 1); err != nil || !reflect.DeepEqual(actual, detail) {
		t.Fatal("retained detail changed", err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, retained); err != nil || fresh {
		t.Fatal("retained duplicate lost identity", fresh, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, old); err != nil || !fresh {
		t.Fatal("expired identity cannot start new receipt", fresh, err)
	}
	if n, spans, err := db.CleanupTransactions(ctx, 1); err != nil || n != 0 || spans != 0 {
		t.Fatalf("new receipt immediately re-expired=%d/%d %v", n, spans, err)
	}
}

func TestTransactionRetentionBoundsAndLateFailureRollback(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data,created_at)
 SELECT $1,'retention-'||n,'transaction','expired','info',now()+interval '1 year','{}',now()-interval '2 days' FROM generate_series(1,601) n`, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO spans(project_id,trace_id,span_id,name,status,is_segment,start_timestamp,end_timestamp,duration,data,created_at)
 SELECT $1,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',n::text,'expired','ok',true,now(),now(),0,'{}',now()-interval '2 days' FROM generate_series(1,601) n`, project.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{250, 250, 101, 0} {
		if n, spans, err := db.CleanupTransactions(ctx, 1); err != nil || n != want || spans != want {
			t.Fatalf("bounded cleanup=%d/%d want=%d err=%v", n, spans, want, err)
		}
	}
	if _, _, err := db.Ingest(ctx, project.ID, operationEvent(1, time.Now(), `[{"op":"db"}]`), deleteOrderParts("rollback")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("1111111111111111", true, 1, 2)), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days'; UPDATE spans SET created_at=now()-interval '2 days';
 CREATE FUNCTION reject_transaction_retention() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'declared late span failure'; END $$;
 CREATE TRIGGER reject_transaction_retention BEFORE DELETE ON spans FOR EACH ROW EXECUTE FUNCTION reject_transaction_retention()`); err != nil {
		t.Fatal(err)
	}
	checkpoint := groupDatabaseSnapshot(t, ctx, db, 23)
	if n, spans, err := db.CleanupTransactions(ctx, 1); err == nil || n != 0 || spans != 0 {
		t.Fatalf("failed pass acknowledged=%d/%d err=%v", n, spans, err)
	}
	if actual := groupDatabaseSnapshot(t, ctx, db, 23); !reflect.DeepEqual(actual, checkpoint) {
		t.Fatal("late span failure committed legacy/dependent deletion")
	}
}

func TestTransactionRetentionSkipsLockedReceiptsAndBusyMaintainer(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, _, err := db.Ingest(ctx, project.ID, operationEvent(1, time.Now(), `[]`)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("1111111111111111", true, 1, 2)), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days'; UPDATE spans SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	for _, statement := range []string{"SELECT id FROM events FOR UPDATE", "SELECT id FROM spans FOR UPDATE"} {
		if _, err := blocker.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if n, spans, err := db.CleanupTransactions(ctx, 1); err != nil || n != 0 || spans != 0 {
		t.Fatalf("locked rows not skipped=%d/%d %v", n, spans, err)
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':transaction-retention',0))`); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if n, spans, err := other.CleanupTransactions(ctx, 1); err != nil || n != 0 || spans != 0 {
		t.Fatalf("busy maintainer not idle=%d/%d %v", n, spans, err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, spans, err := other.CleanupTransactions(ctx, 1); err != nil || n != 1 || spans != 1 {
		t.Fatalf("unlocked receipts not reclaimed=%d/%d %v", n, spans, err)
	}
}

func TestTransactionRetentionMigration023PreservesRowsAndReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, _, err := db.Ingest(ctx, project.ID, operationEvent(1, time.Now(), `[{"op":"db"}]`), deleteOrderParts("before023")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("1111111111111111", true, 1, 2)), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP INDEX events_transaction_receipt_retention; DROP INDEX spans_receipt_retention; DELETE FROM schema_migrations WHERE version=23`); err != nil {
		t.Fatal(err)
	}
	checkpoint := groupDatabaseSnapshot(t, ctx, db, 22)
	for range 2 {
		reopened, err := Open(ctx, db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if actual := groupDatabaseSnapshot(t, ctx, reopened, 22); !reflect.DeepEqual(actual, checkpoint) {
			t.Fatal("023 rewrote prior rows or migration receipts")
		}
		var receipt, eventIndex, spanIndex string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum,to_regclass('events_transaction_receipt_retention')::text,to_regclass('spans_receipt_retention')::text FROM schema_migrations WHERE version=23`).Scan(&receipt, &eventIndex, &spanIndex); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/023_transaction_retention.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) || eventIndex != "events_transaction_receipt_retention" || spanIndex != "spans_receipt_retention" {
			t.Fatal("023 receipt/indexes missing or changed", err)
		}
		reopened.Close()
	}
}
