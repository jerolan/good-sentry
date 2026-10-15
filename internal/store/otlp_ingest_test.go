//go:build integration

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/otlp"

	"github.com/jackc/pgx/v5"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

func prepareStoreOTLP(t *testing.T, raw string) *otlp.Batch {
	t.Helper()
	received := time.Date(2026, 10, 15, 12, 30, 40, 123456789, time.UTC)
	batch, err := otlp.Prepare(strings.NewReader(raw), otlp.JSON, "identity", otlp.Limits{MaxBytes: 1 << 20, MaxRecords: 20, MaxNodes: 2000}, received)
	if err != nil {
		t.Fatalf("real OTLP Prepare fixture: %v", err)
	}
	return batch
}

func storeOTLPRecords(t *testing.T, bodies ...string) *otlp.Batch {
	t.Helper()
	records := make([]any, len(bodies))
	for index, body := range bodies {
		records[index] = map[string]any{"body": map[string]string{"stringValue": body}}
	}
	raw, err := json.Marshal(map[string]any{"resourceLogs": []any{map[string]any{"scopeLogs": []any{map[string]any{"logRecords": records}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return prepareStoreOTLP(t, string(raw))
}

func requireOTLPCounts(t *testing.T, ctx context.Context, db *Store, batches, logs int) {
	t.Helper()
	var actualBatches, actualLogs int
	if err := db.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM otlp_log_batches),(SELECT count(*) FROM logs)").Scan(&actualBatches, &actualLogs); err != nil {
		t.Fatal(err)
	}
	if actualBatches != batches || actualLogs != logs {
		t.Fatalf("durable rows batches=%d logs=%d want=%d/%d", actualBatches, actualLogs, batches, logs)
	}
}

func otlpStoreSnapshot(t *testing.T, ctx context.Context, db *Store) string {
	t.Helper()
	var result string
	err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object(
'batches',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM otlp_log_batches b),
'logs',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM logs l))::text`).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitOTLPInsertGate(t *testing.T, ctx context.Context, observer pgx.Tx, blockerPID int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := observer.Exec(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
			t.Fatal(err)
		}
		var blocked bool
		if err := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`, blockerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("OTLP insert did not reach the held external database gate")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIngestOTLPPersistsCanonicalBatchAndOccurrences(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	batch := prepareStoreOTLP(t, `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc\u0000one"}}]},"scopeLogs":[{}, {"scope":{"name":"store-fixture"},"logRecords":[{"timeUnixNano":"1000000001","severityNumber":17,"body":{"stringValue":"first\u0000record"}},{"observedTimeUnixNano":"2000000002","body":{"stringValue":"second"}}]}]},{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"third"}}]}]}]}`)
	canonical := bytes.Clone(batch.Payload)
	// The capability assertion precedes queries of021's new table/columns.
	// The compiling stub must fail here, not on a missing fixture table.
	count, err := db.IngestOTLP(ctx, project.ID, batch)
	if err != nil || count != 3 {
		t.Fatalf("admitted batch was not durably accepted: count=%d want=3 err=%v", count, err)
	}
	var batchID, storedProject string
	var received time.Time
	var payload []byte
	if err := db.pool.QueryRow(ctx, "SELECT id::text,project_id::text,received_at,payload FROM otlp_log_batches").Scan(&batchID, &storedProject, &received, &payload); err != nil {
		t.Fatal(err)
	}
	if storedProject != project.ID || !received.Equal(time.Date(2026, 10, 15, 12, 30, 40, 123456000, time.UTC)) || !bytes.Equal(payload, canonical) {
		t.Fatalf("canonical batch/project/receiver timestamp changed: project=%s received=%s", storedProject, received)
	}
	var decoded logspb.LogsData
	if err := proto.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ResourceLogs[0].Resource.Attributes[0].Value.GetStringValue() != "svc\x00one" || decoded.ResourceLogs[0].ScopeLogs[1].LogRecords[0].Body.GetStringValue() != "first\x00record" {
		t.Fatal("canonical bytea lost the resource/body NUL character")
	}
	rows, err := db.pool.Query(ctx, "SELECT otlp_batch_id::text,otlp_record_index,body,service,attributes::text,data::text,item_hash FROM logs ORDER BY otlp_record_index")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantBodies := []string{`first\u0000record`, "second", "third"}
	hashes := map[string]bool{}
	n := 0
	for rows.Next() {
		var id, body, service, attributes, data, hash string
		var ordinal int
		if err := rows.Scan(&id, &ordinal, &body, &service, &attributes, &data, &hash); err != nil {
			t.Fatal(err)
		}
		if n >= len(wantBodies) || id != batchID || ordinal != n || body != wantBodies[n] || attributes != "{}" || data != "{}" || hash == "" || hashes[hash] {
			t.Fatalf("occurrence%d id=%s ordinal=%d body=%q attributes=%s data=%s hash=%q", n, id, ordinal, body, attributes, data, hash)
		}
		if (n < 2 && service != `svc\u0000one`) || (n == 2 && service != "") {
			t.Fatalf("occurrence%d service=%q", n, service)
		}
		hashes[hash] = true
		n++
	}
	if err := rows.Err(); err != nil || n != 3 {
		t.Fatalf("durable occurrences=%d want=3 err=%v", n, err)
	}
	var batchCount int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM otlp_log_batches").Scan(&batchCount); err != nil || batchCount != 1 {
		t.Fatalf("canonical payload copies=%d want=1 err=%v", batchCount, err)
	}
}

func TestIngestOTLPIdenticalRecordsAndConcurrentReplaysRemainOccurrences(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	batch := storeOTLPRecords(t, "identical", "identical")
	canonical := bytes.Clone(batch.Payload)
	records := append([]otlp.Record(nil), batch.Records...)
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	for range 2 {
		if count, err := db.IngestOTLP(ctx, project.ID, batch); err != nil || count != 2 {
			t.Fatalf("sequential identical export count=%d err=%v", count, err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 4)
	for index := range 4 {
		target := db
		if index%2 != 0 {
			target = other
		}
		go func() {
			<-start
			count, err := target.IngestOTLP(ctx, project.ID, batch)
			if err == nil && count != 2 {
				err = fmt.Errorf("concurrent replay accepted%d records, want2", count)
			}
			results <- err
		}()
	}
	close(start)
	var failure error
	for range 4 {
		if err := <-results; err != nil {
			failure = err
		}
	}
	if failure != nil {
		t.Fatal(failure)
	}
	requireOTLPCounts(t, ctx, db, 6, 12)
	rows, err := db.pool.Query(ctx, `SELECT b.id::text,b.payload,ARRAY(SELECT l.otlp_record_index FROM logs l WHERE l.otlp_batch_id=b.id ORDER BY l.otlp_record_index) FROM otlp_log_batches b ORDER BY b.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id string
		var payload []byte
		var ordinals []int
		if err := rows.Scan(&id, &payload, &ordinals); err != nil {
			t.Fatal(err)
		}
		if id == "" || !bytes.Equal(payload, canonical) || !reflect.DeepEqual(ordinals, []int{0, 1}) {
			t.Fatalf("replay batch=%s ordinals=%v canonicalEqual=%v", id, ordinals, bytes.Equal(payload, canonical))
		}
		n++
	}
	if err := rows.Err(); err != nil || n != 6 {
		t.Fatalf("separate replay batches=%d err=%v", n, err)
	}
	var hashes int
	if err := db.pool.QueryRow(ctx, "SELECT count(DISTINCT item_hash) FROM logs").Scan(&hashes); err != nil || hashes != 12 {
		t.Fatalf("identical records collapsed: unique durable hashes=%d err=%v", hashes, err)
	}
	if !bytes.Equal(batch.Payload, canonical) || !reflect.DeepEqual(batch.Records, records) {
		t.Fatal("concurrent ingestion mutated the admitted input batch")
	}
}

func TestIngestOTLPEmptyExportsAndInvalidProjectDoNotWrite(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	for _, raw := range []string{`{}`, `{"resourceLogs":[{"scopeLogs":[{}]}]}`} {
		if count, err := db.IngestOTLP(ctx, project.ID, prepareStoreOTLP(t, raw)); err != nil || count != 0 {
			t.Fatalf("empty export count=%d err=%v", count, err)
		}
		requireOTLPCounts(t, ctx, db, 0, 0)
	}
	batch := storeOTLPRecords(t, "valid project positive control")
	if count, err := db.IngestOTLP(ctx, "9223372036854775807", batch); err == nil || count != 0 {
		t.Fatalf("invalid project acknowledged count=%d err=%v", count, err)
	}
	requireOTLPCounts(t, ctx, db, 0, 0)
	if count, err := db.IngestOTLP(ctx, project.ID, batch); err != nil || count != 1 {
		t.Fatalf("valid project positive control count=%d err=%v", count, err)
	}
	before := otlpStoreSnapshot(t, ctx, db)
	if count, err := db.IngestOTLP(ctx, project.ID, prepareStoreOTLP(t, `{}`)); err != nil || count != 0 {
		t.Fatalf("empty export after existing rows count=%d err=%v", count, err)
	}
	if after := otlpStoreSnapshot(t, ctx, db); after != before {
		t.Fatal("empty export changed existing batches/logs")
	}
}

func TestIngestOTLPRollsBackLaterInsertAndDeferredCommitFailures(t *testing.T) {
	for _, mode := range []string{"later insert", "deferred commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, db, _, _, project := telemetryStore(t)
			if count, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "preexisting")); err != nil || count != 1 {
				t.Fatalf("baseline positive control count=%d err=%v", count, err)
			}
			before := otlpStoreSnapshot(t, ctx, db)
			var ddl, diagnostic string
			if mode == "later insert" {
				diagnostic = "fixture later insert observed prior OTLP row"
				ddl = `CREATE FUNCTION reject_otlp_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
IF EXISTS(SELECT 1 FROM logs WHERE otlp_batch_id=NEW.otlp_batch_id) THEN
 IF NOT EXISTS(SELECT 1 FROM otlp_log_batches WHERE id=NEW.otlp_batch_id) THEN RAISE EXCEPTION 'fixture did not observe actual batch'; END IF;
 RAISE EXCEPTION 'fixture later insert observed prior OTLP row';
END IF;
RETURN NEW; END $$;
CREATE TRIGGER reject_otlp_fixture BEFORE INSERT ON logs FOR EACH ROW WHEN(NEW.otlp_batch_id IS NOT NULL) EXECUTE FUNCTION reject_otlp_fixture()`
			} else {
				diagnostic = "fixture deferred commit observed complete OTLP batch"
				ddl = `CREATE FUNCTION reject_otlp_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
IF (SELECT count(*) FROM logs WHERE otlp_batch_id=NEW.otlp_batch_id)<>3 OR NOT EXISTS(SELECT 1 FROM otlp_log_batches WHERE id=NEW.otlp_batch_id) THEN RAISE EXCEPTION 'fixture did not observe complete batch'; END IF;
RAISE EXCEPTION 'fixture deferred commit observed complete OTLP batch';
END $$;
CREATE CONSTRAINT TRIGGER reject_otlp_fixture AFTER INSERT ON logs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.otlp_batch_id IS NOT NULL) EXECUTE FUNCTION reject_otlp_fixture()`
			}
			if _, err := db.pool.Exec(ctx, ddl); err != nil {
				t.Fatal(err)
			}
			batch := storeOTLPRecords(t, "first", "second", "third")
			count, err := db.IngestOTLP(ctx, project.ID, batch)
			if err == nil || count != 0 || !strings.Contains(err.Error(), diagnostic) {
				t.Fatalf("wrong database failure stage: count=%d err=%v", count, err)
			}
			if after := otlpStoreSnapshot(t, ctx, db); after != before {
				t.Fatal("failed export committed part of either table family or changed baseline rows")
			}
			if _, err := db.pool.Exec(ctx, "DROP TRIGGER reject_otlp_fixture ON logs; DROP FUNCTION reject_otlp_fixture()"); err != nil {
				t.Fatal(err)
			}
			if count, err := db.IngestOTLP(ctx, project.ID, batch); err != nil || count != 3 {
				t.Fatalf("same export after database repair count=%d err=%v", count, err)
			}
			requireOTLPCounts(t, ctx, db, 2, 4)
		})
	}
}

func TestIngestOTLPCancellationAfterFirstInsertRollsBack(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if count, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "preexisting")); err != nil || count != 1 {
		t.Fatalf("baseline positive control count=%d err=%v", count, err)
	}
	before := otlpStoreSnapshot(t, ctx, db)
	_, err := db.pool.Exec(ctx, `CREATE TABLE otlp_insert_gate(id integer PRIMARY KEY); INSERT INTO otlp_insert_gate VALUES(1);
CREATE FUNCTION wait_otlp_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
IF EXISTS(SELECT 1 FROM logs WHERE otlp_batch_id=NEW.otlp_batch_id) THEN
 IF NOT EXISTS(SELECT 1 FROM otlp_log_batches WHERE id=NEW.otlp_batch_id) THEN RAISE EXCEPTION 'fixture did not observe actual batch'; END IF;
 PERFORM id FROM otlp_insert_gate WHERE id=1 FOR UPDATE;
END IF;
RETURN NEW; END $$;
CREATE TRIGGER wait_otlp_fixture BEFORE INSERT ON logs FOR EACH ROW WHEN(NEW.otlp_batch_id IS NOT NULL) EXECUTE FUNCTION wait_otlp_fixture()`)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var blockerPID int
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "SELECT id FROM otlp_insert_gate WHERE id=1 FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	batch := storeOTLPRecords(t, "actual first", "blocked second", "last")
	insertCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		count, err := other.IngestOTLP(insertCtx, project.ID, batch)
		done <- result{count, err}
	}()
	waitOTLPInsertGate(t, ctx, blocker, blockerPID)
	// Reaching this held row proves the real SQL trigger observed the first
	// inserted log and its batch inside the same still-uncommitted transaction.
	select {
	case got := <-done:
		t.Fatalf("provider returned while later insert remained blocked: count=%d err=%v", got.count, got.err)
	default:
	}
	if current := otlpStoreSnapshot(t, ctx, db); current != before {
		t.Fatal("uncommitted first insert became visible before request completion")
	}
	cancel()
	select {
	case got := <-done:
		if got.err == nil || got.count != 0 || insertCtx.Err() != context.Canceled {
			t.Fatalf("canceled export acknowledged success: count=%d err=%v", got.count, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not stop the blocked insert")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if after := otlpStoreSnapshot(t, ctx, db); after != before {
		t.Fatal("cancellation retained an incomplete batch/log or changed baseline rows")
	}
	if count, err := other.IngestOTLP(ctx, project.ID, batch); err != nil || count != 3 {
		t.Fatalf("retry after real gate release count=%d err=%v", count, err)
	}
	requireOTLPCounts(t, ctx, db, 2, 4)
}
