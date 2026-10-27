//go:build integration

package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"goodsentry/internal/otlp"
)

// This owns the020->021 migration, public relational reference constraints and
// exact prior-row/checksum preservation. The provider tests cannot prove an
// existing database upgrades without rewriting historical values. No production
// hooks or test-only exports are needed; every failure is real PostgreSQL.
func TestOTLPMigration021PreservesSDKHistoryAndReopens(t *testing.T) {
	f := resolutionStore(t)
	const sdk = `{"items":[{"timestamp":1791981000.25,"level":"error","body":"before021","attributes":{"attempt":{"type":"integer","value":9223372036854775807}},"future":{"preserved":[1,2]}}]}`
	if n, err := f.db.IngestLogs(f.ctx, f.project.ID, []byte(sdk)); err != nil || n != 1 {
		t.Fatalf("old SDK log: n=%d err=%v", n, err)
	}
	var initialNull bool
	if err := f.db.pool.QueryRow(f.ctx, `SELECT bool_and(otlp_batch_id IS NULL AND otlp_record_index IS NULL) FROM logs`).Scan(&initialNull); err != nil || !initialNull {
		t.Fatalf("021 must provide nullable references for existing SDK rows: null=%v err=%v", initialNull, err)
	}
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	event := groupEvent(t, 1, "migration021-history", "Historical error", stamp)
	capture := CaptureItems{Attachments: []CaptureAttachment{{Filename: "history021.bin", ContentType: "application/octet-stream", Data: []byte{0, 255, 10, 128}}}}
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, event, capture); err != nil || !fresh || id == "" {
		t.Fatalf("historical capture: id=%s fresh=%v err=%v", id, fresh, err)
	}
	// Reconstruct020 only inside this owned disposable schema. The old SDK row
	// and every other current table remain intact; no old migration is edited.
	if _, err := f.db.pool.Exec(f.ctx, `DROP VIEW logs_with_archive;
 ALTER TABLE logs DROP COLUMN otlp_batch_id,DROP COLUMN otlp_record_index;
 DROP TABLE otlp_log_batches;
 DELETE FROM schema_migrations WHERE version=21`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 20)
	databaseURL := f.db.pool.Config().ConnString()
	f.db.Close()
	upgraded, err := Open(f.ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	after := groupDatabaseSnapshot(t, f.ctx, upgraded, 20)
	if after["otlp_log_batches"] != "[]" {
		t.Fatal("upgrade fabricated an OTLP batch")
	}
	delete(after, "otlp_log_batches")
	var oldShape string
	if err := upgraded.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(payload ORDER BY payload::text),'[]'::jsonb)::text
 FROM (SELECT to_jsonb(l)-'otlp_batch_id'-'otlp_record_index' AS payload FROM logs l) history`).Scan(&oldShape); err != nil {
		t.Fatal(err)
	}
	after["logs"] = oldShape
	if !reflect.DeepEqual(before, after) {
		t.Fatal("021 rewrote a prior row, SDK hash/data, capture bytes or001..020 receipt")
	}
	var oldReferences int
	if err := upgraded.pool.QueryRow(f.ctx, `SELECT count(*) FROM logs WHERE otlp_batch_id IS NOT NULL OR otlp_record_index IS NOT NULL`).Scan(&oldReferences); err != nil || oldReferences != 0 {
		t.Fatalf("historical log references were populated: count=%d err=%v", oldReferences, err)
	}
	if n, err := upgraded.IngestLogs(f.ctx, f.project.ID, []byte(sdk)); err != nil || n != 0 {
		t.Fatalf("upgrade changed SDK retry identity: n=%d err=%v", n, err)
	}
	body, err := migrations.ReadFile("migrations/021_otlp_logs.sql")
	if err != nil {
		t.Fatal(err)
	}
	var receipt string
	if err := upgraded.pool.QueryRow(f.ctx, `SELECT checksum FROM schema_migrations WHERE version=21`).Scan(&receipt); err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("021 checksum receipt: err=%v", err)
	}
	// Use a real admitted payload as authority for all relational controls.
	limits := otlp.Limits{MaxBytes: 4096, MaxNodes: 1000, MaxRecords: 10}
	batch, err := otlp.Prepare(bytes.NewBufferString(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{},{}]}]}]}`), otlp.JSON, "", limits, stamp)
	if err != nil {
		t.Fatal(err)
	}
	var batchID int64
	if err := upgraded.pool.QueryRow(f.ctx, `INSERT INTO otlp_log_batches(project_id,received_at,payload) VALUES($1,$2,$3) RETURNING id`, f.project.ID, batch.ReceivedAt, batch.Payload).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	other, err := upgraded.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Foreign batch", "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignBatchID int64
	if err := upgraded.pool.QueryRow(f.ctx, `INSERT INTO otlp_log_batches(project_id,received_at,payload) VALUES($1,$2,$3) RETURNING id`, other.ID, batch.ReceivedAt, batch.Payload).Scan(&foreignBatchID); err != nil {
		t.Fatal(err)
	}
	const insert = `INSERT INTO logs(project_id,timestamp,level,body,data,item_hash,otlp_batch_id,otlp_record_index)
 VALUES($1,$2,'unknown','relational-proof','{}',$3,$4,$5)`
	if _, err := upgraded.pool.Exec(f.ctx, insert, f.project.ID, stamp, "valid021", batchID, 0); err != nil {
		t.Fatalf("valid same-project ordinal: %v", err)
	}
	for _, c := range []struct {
		name           string
		batchID, index any
		code           string
	}{
		{"missing-index", batchID, nil, "23514"},
		{"missing-batch", nil, 1, "23514"},
		{"negative-index", batchID, -1, "23514"},
		{"foreign-project", foreignBatchID, 1, "23503"},
		{"duplicate-ordinal", batchID, 0, "23505"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx, err := upgraded.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, rejected := tx.Exec(f.ctx, insert, f.project.ID, stamp, "invalid021-"+c.name, c.batchID, c.index)
			if err := tx.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			var pgError *pgconn.PgError
			if !errors.As(rejected, &pgError) || pgError.Code != c.code {
				t.Fatalf("required relational contract: err=%v expected SQLSTATE%s", rejected, c.code)
			}
		})
	}
	checkpoint := groupDatabaseSnapshot(t, f.ctx, upgraded, 0)
	upgraded.Close()
	reopened, err := Open(f.ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	groupRequireUnchanged(t, f.ctx, reopened, checkpoint)
}
