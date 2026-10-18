//go:build integration

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/otlp"
)

// Real PostgreSQL owns physical expiry, receipt age, bounded atomic progress,
// shared batch survival and replica contention. Credible regressions include
// using SDK time, cascading a batch across a retained sibling, deleting beyond
// a pass limit, or committing log removal before a later batch failure. Existing
// ingestion/read tests cannot trigger cleanup. No production test seam is used.
func TestLogRetentionPreservesCurrentSDKAndOTLPSiblings(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	const sdk = `{"items":[{"timestamp":1,"level":"info","body":"old-event-new-receipt","attributes":{"exact":{"type":"integer","value":9223372036854775807}}}]}`
	if n, err := db.IngestLogs(ctx, project.ID, []byte(sdk)); err != nil || n != 1 {
		t.Fatalf("SDK fixture: %d %v", n, err)
	}
	batch := storeOTLPRecords(t, "expired sibling", "retained sibling")
	if n, err := db.IngestOTLP(ctx, project.ID, batch); err != nil || n != 2 {
		t.Fatalf("OTLP fixture: %d %v", n, err)
	}
	var batchID int64
	var selected string
	if err := db.pool.QueryRow(ctx, `SELECT otlp_batch_id,id::text FROM logs WHERE body='retained sibling'`).Scan(&batchID, &selected); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days' WHERE body='expired sibling'`); err != nil {
		t.Fatal(err)
	}
	before, err := db.Log(ctx, owner.ID, org.Slug, selected, otlp.Limits{MaxBytes: 1 << 20, MaxNodes: 2000, MaxRecords: 20})
	if err != nil {
		t.Fatal(err)
	}
	logs, batches, err := db.CleanupLogs(ctx, 1)
	if err != nil || logs != 1 || batches != 0 {
		t.Fatalf("first cleanup=%d/%d err=%v", logs, batches, err)
	}
	after, err := db.Log(ctx, owner.ID, org.Slug, selected, otlp.Limits{MaxBytes: 1 << 20, MaxNodes: 2000, MaxRecords: 20})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("surviving selected detail changed: %v", err)
	}
	var payload []byte
	if err := db.pool.QueryRow(ctx, "SELECT payload FROM otlp_log_batches WHERE id=$1", batchID).Scan(&payload); err != nil || !bytes.Equal(payload, batch.Payload) {
		t.Fatal("shared authority changed", err)
	}
	if n, err := db.IngestLogs(ctx, project.ID, []byte(sdk)); err != nil || n != 0 {
		t.Fatalf("current SDK hash lost: %d %v", n, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days' WHERE body='retained sibling'`); err != nil {
		t.Fatal(err)
	}
	logs, batches, err = db.CleanupLogs(ctx, 1)
	if err != nil || logs != 1 || batches != 1 {
		t.Fatalf("last sibling cleanup=%d/%d err=%v", logs, batches, err)
	}
	var remaining int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM logs").Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("SDK receipt deleted: %d %v", remaining, err)
	}
	logs, batches, err = db.CleanupLogs(ctx, 1)
	if err != nil || logs != 0 || batches != 0 {
		t.Fatalf("repeat cleanup=%d/%d err=%v", logs, batches, err)
	}
}

func TestLogRetentionBoundedProgressAndRollback(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO logs(project_id,timestamp,level,body,data,item_hash,created_at)
 SELECT $1,now()+interval '1 year','info','expired future event','{}','retention-'||n,now()-interval '2 days' FROM generate_series(1,601) n`, project.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{250, 250, 101, 0} {
		logs, batches, err := db.CleanupLogs(ctx, 1)
		if err != nil || logs != want || batches != 0 {
			t.Fatalf("bounded cleanup=%d/%d want=%d err=%v", logs, batches, want, err)
		}
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO otlp_log_batches(project_id,received_at,payload)
 SELECT $1,now()-interval '2 days',$2 FROM generate_series(1,501)`, project.ID, storeOTLPRecords(t, "orphan").Payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{250, 250, 1, 0} {
		logs, batches, err := db.CleanupLogs(ctx, 1)
		if err != nil || logs != 0 || batches != want {
			t.Fatalf("bounded orphan cleanup=%d/%d want=%d err=%v", logs, batches, want, err)
		}
	}
	if n, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "rollback")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days';
 CREATE FUNCTION reject_retention_batch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'declared batch deletion failure'; END $$;
 CREATE TRIGGER reject_retention_batch BEFORE DELETE ON otlp_log_batches FOR EACH ROW EXECUTE FUNCTION reject_retention_batch()`); err != nil {
		t.Fatal(err)
	}
	checkpoint := otlpStoreSnapshot(t, ctx, db)
	logs, batches, err := db.CleanupLogs(ctx, 1)
	if err == nil || logs != 0 || batches != 0 {
		t.Fatalf("failed transaction acknowledged: %d/%d %v", logs, batches, err)
	}
	if actual := otlpStoreSnapshot(t, ctx, db); actual != checkpoint {
		t.Fatal("later batch failure committed log deletion")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := db.CleanupLogs(canceled, 1); err == nil {
		t.Fatal("canceled cleanup succeeded")
	}
	if actual := otlpStoreSnapshot(t, ctx, db); actual != checkpoint {
		t.Fatal("canceled cleanup changed rows")
	}
}

func TestLogRetentionSkipsLockedRowsAndConcurrentMaintainer(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if n, err := db.IngestLogs(ctx, project.ID, []byte(fmt.Sprintf(`{"items":[{"timestamp":%d,"level":"info","body":"locked","attributes":{}}]}`, time.Now().Unix()))); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, `SELECT id FROM logs FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	if logs, batches, err := db.CleanupLogs(ctx, 1); err != nil || logs != 0 || batches != 0 {
		t.Fatalf("locked row not skipped: %d/%d %v", logs, batches, err)
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':log-retention',0))`); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if logs, batches, err := other.CleanupLogs(ctx, 1); err != nil || logs != 0 || batches != 0 {
		t.Fatalf("busy maintainer not idle: %d/%d %v", logs, batches, err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if logs, batches, err := other.CleanupLogs(ctx, 1); err != nil || logs != 1 || batches != 0 {
		t.Fatalf("cleanup after release: %d/%d %v", logs, batches, err)
	}
}

func TestLogRetentionCancellationRollsBackPriorRemoval(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if n, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "cancel after log removal")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days';
 CREATE FUNCTION wait_retention_batch() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,8347)); RETURN OLD; END $$;
 CREATE TRIGGER wait_retention_batch BEFORE DELETE ON otlp_log_batches FOR EACH ROW EXECUTE FUNCTION wait_retention_batch()`); err != nil {
		t.Fatal(err)
	}
	checkpoint := otlpStoreSnapshot(t, ctx, db)
	barrier, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(ctx)
	var pid int
	if err := barrier.QueryRow(ctx, `SELECT pg_backend_pid() FROM (SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),8347))) gate`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		logs, batches, err := db.CleanupLogs(canceled, 1)
		if err == nil || logs != 0 || batches != 0 {
			done <- fmt.Errorf("canceled transaction acknowledged %d/%d err=%v", logs, batches, err)
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
		t.Fatal("canceled cleanup did not return")
	}
	if actual := otlpStoreSnapshot(t, ctx, db); actual != checkpoint {
		t.Fatal("cancel after log removal committed a partial transaction")
	}
}

func TestLogRetentionMigration022PreservesAndReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if n, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "before022")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := db.pool.Exec(ctx, `DROP INDEX logs_receipt_retention; DELETE FROM schema_migrations WHERE version=22`); err != nil {
		t.Fatal(err)
	}
	checkpoint := groupDatabaseSnapshot(t, ctx, db, 21)
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if actual := groupDatabaseSnapshot(t, ctx, reopened, 21); !reflect.DeepEqual(actual, checkpoint) {
		t.Fatal("022 rewrote a prior row or migration receipt")
	}
	var receipt, index string
	if err := reopened.pool.QueryRow(ctx, `SELECT checksum,to_regclass('logs_receipt_retention')::text FROM schema_migrations WHERE version=22`).Scan(&receipt, &index); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/022_log_retention.sql")
	if err != nil {
		t.Fatal(err)
	}
	if index != "logs_receipt_retention" || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatal("022 index/checksum missing")
	}
}
