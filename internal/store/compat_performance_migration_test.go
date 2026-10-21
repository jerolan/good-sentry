//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestCompatibleTransactionGroupMigration052PreservesTelemetryAndReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	compatibleCapture(t, ctx, db, project, 1, time.Now().UTC().Add(-time.Hour), map[string]any{"type": "transaction", "transaction": "GET /migration052", "start_timestamp": float64(time.Now().Add(-time.Hour).Unix()), "timestamp": float64(time.Now().Add(-time.Hour).Unix()) + 0.1, "contexts": map[string]any{"trace": map[string]any{"trace_id": "55555555555555555555555555555555", "span_id": "5555555555555555", "op": "http.server", "status": "ok"}}})
	before := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY id),'[]')::text FROM events e`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	events := before()
	if _, err := db.pool.Exec(ctx, `DROP TABLE compatible_transaction_group_ids; DELETE FROM schema_migrations WHERE version=52`); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		var receipt, after string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=52`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/052_compatible_transaction_group_ids.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("052 checksum receipt absent or changed", err)
		}
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY id),'[]')::text FROM events e`).Scan(&after); err != nil || after != events {
			reopened.Close()
			t.Fatalf("052 changed telemetry: %v", err)
		}
		reopened.Close()
	}
}
