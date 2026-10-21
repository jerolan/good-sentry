//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestWizardTransferMigration051PreservesCredentialsAndReopens(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	secretHash := "9999999999999999999999999999999999999999999999999999999999999999"
	if _, err := db.pool.Exec(ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
		VALUES($1::bigint,$2::bigint,'Before051',$3,'gs_99999999',ARRAY['project:read'],true,now()+interval '1 day')`, org.ID, owner.ID, secretHash); err != nil {
		t.Fatal(err)
	}
	before := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]')::text FROM api_tokens t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	credentials := before()
	if _, err := db.pool.Exec(ctx, `DROP TABLE wizard_transfers; DELETE FROM schema_migrations WHERE version=51`); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		var receipt, after string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=51`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/051_wizard_transfers.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("051 checksum receipt absent or changed", err)
		}
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]')::text FROM api_tokens t`).Scan(&after); err != nil || after != credentials {
			reopened.Close()
			t.Fatalf("051 changed credentials: %v", err)
		}
		reopened.Close()
	}
}
