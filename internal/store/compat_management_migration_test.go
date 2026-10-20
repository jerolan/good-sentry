//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
)

func TestCompatibilityManagementMigration039PreservesRowsAndReopens(t *testing.T) {
	ctx, db, _, org, _ := telemetryStore(t)
	if _, err := db.pool.Exec(ctx, `DROP TABLE IF EXISTS project_key_rate_usage;
 DELETE FROM schema_migrations WHERE version=41;
 ALTER TABLE project_keys DROP CONSTRAINT IF EXISTS project_keys_rate_limit_pair;
 DROP INDEX IF EXISTS project_keys_api_id;
 ALTER TABLE project_keys DROP COLUMN IF EXISTS api_id, DROP COLUMN IF EXISTS label,
  DROP COLUMN IF EXISTS updated_at, DROP COLUMN IF EXISTS rate_limit_window, DROP COLUMN IF EXISTS rate_limit_count;
 ALTER TABLE teams DROP COLUMN IF EXISTS created_at;
 DELETE FROM schema_migrations WHERE version=39`); err != nil {
		t.Fatal(err)
	}
	oldKeys := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
  'public_key',public_key,'project_id',project_id,'enabled',enabled,'created_at',created_at) ORDER BY public_key),'[]')::text FROM project_keys`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	oldTeams := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
  'id',id,'organization_id',organization_id,'slug',slug,'name',name) ORDER BY id),'[]')::text FROM teams`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	oldReceipts := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=38`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	keysBefore, teamsBefore, receiptsBefore := oldKeys(), oldTeams(), oldReceipts()
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if oldKeys() != keysBefore || oldTeams() != teamsBefore || oldReceipts() != receiptsBefore {
			reopened.Close()
			t.Fatal("039 changed existing key/team values or earlier receipts")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=39`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/039_compat_management.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("039 checksum receipt absent or changed", err)
		}
		var columns int
		if err := reopened.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
  WHERE table_schema=current_schema() AND table_name='project_keys'
  AND column_name IN ('api_id','label','updated_at','rate_limit_window','rate_limit_count')`).Scan(&columns); err != nil || columns != 5 {
			reopened.Close()
			t.Fatalf("039 project-key columns=%d err=%v", columns, err)
		}
		var teamCreated, orgCreated string
		if err := reopened.pool.QueryRow(ctx, `SELECT t.created_at::text,o.created_at::text FROM teams t
  JOIN organizations o ON o.id=t.organization_id WHERE o.slug=$1 AND t.slug='default'`, org.Slug).Scan(&teamCreated, &orgCreated); err != nil || teamCreated != orgCreated {
			reopened.Close()
			t.Fatalf("team creation instant=%q organization=%q err=%v", teamCreated, orgCreated, err)
		}
		var apiID string
		if err := reopened.pool.QueryRow(ctx, `SELECT api_id::text FROM project_keys LIMIT 1`).Scan(&apiID); err != nil || !validKeyUUID(apiID) {
			reopened.Close()
			t.Fatalf("existing key api_id=%q err=%v", apiID, err)
		}
		reopened.Close()
	}
	if !reflect.DeepEqual(keysBefore, oldKeys()) || !reflect.DeepEqual(teamsBefore, oldTeams()) {
		t.Fatal("final reopened pool changed preserved rows")
	}
}
