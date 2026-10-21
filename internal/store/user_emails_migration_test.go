//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestUserEmailsMigration050PreservesUsersAndReopens(t *testing.T) {
	ctx, db, _, _, _ := telemetryStore(t)
	usersBefore := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY id),'[]')::text FROM users u`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}()
	receiptsBefore := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=49`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}()
	databaseURL := db.pool.Config().ConnString()
	if _, err := db.pool.Exec(ctx, `DROP TABLE user_emails; DELETE FROM schema_migrations WHERE version=50`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		var receipt, users, receipts string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=50`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/050_user_emails.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("050 checksum receipt absent or changed", err)
		}
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY id),'[]')::text FROM users u`).Scan(&users); err != nil || users != usersBefore {
			reopened.Close()
			t.Fatalf("050 changed users: %v", err)
		}
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=49`).Scan(&receipts); err != nil || receipts != receiptsBefore {
			reopened.Close()
			t.Fatalf("050 changed prior receipts: %v", err)
		}
		reopened.Close()
	}
}
