//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
)

func TestProjectKeyRateUsageMigration041PreservesKeysAndReopens(t *testing.T) {
	ctx, db, _, _, _ := telemetryStore(t)
	if _, err := db.pool.Exec(ctx, `DROP TABLE project_key_rate_usage; DELETE FROM schema_migrations WHERE version=41`); err != nil {
		t.Fatal(err)
	}
	before := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(k) ORDER BY public_key),'[]')::text FROM project_keys k`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	keys := before()
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if after := before(); !reflect.DeepEqual(keys, after) {
			reopened.Close()
			t.Fatal("041 changed existing project keys")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=41`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/041_project_key_rate_usage.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("041 checksum receipt absent or changed", err)
		}
		var table string
		if err := reopened.pool.QueryRow(ctx, `SELECT to_regclass('project_key_rate_usage')::text`).Scan(&table); err != nil || table == "" {
			reopened.Close()
			t.Fatalf("041 usage table missing: %q %v", table, err)
		}
		reopened.Close()
	}
}
