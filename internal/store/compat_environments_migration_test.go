//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestProjectEnvironmentsMigration049BackfillsAndReopens(t *testing.T) {
	ctx, db, _, org, project := telemetryStore(t)
	compatibleCapture(t, ctx, db, project, 1, time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC), map[string]any{"environment": "production", "message": "migration environment"})
	stamp := time.Date(2026, 2, 3, 4, 5, 7, 0, time.UTC)
	if _, err := db.pool.Exec(ctx, `INSERT INTO monitors(organization_id,project_id,name,kind,url,interval_seconds,timeout_seconds,expected_status,confirmation_threshold,enabled,next_check,created_at,environment)
VALUES($1::bigint,$2::bigint,'Environment monitor','GET','https://environment.invalid',60,5,200,1,true,$3,$3,'monitor-env')`, org.ID, project.ID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO logs(project_id,timestamp,level,body,data,item_hash,environment)
VALUES($1::bigint,$2,'info','environment log','{}'::jsonb,'environment-migration-log','log-env')`, project.ID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO spans(project_id,trace_id,span_id,name,status,is_segment,start_timestamp,end_timestamp,duration,data,environment)
VALUES($1::bigint,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','1111111111111111','environment span','ok',true,$2,$2,0,'{}'::jsonb,'span-env')`, project.ID, stamp); err != nil {
		t.Fatal(err)
	}
	projectBefore := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM projects p WHERE id=$1::bigint`, project.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	eventsBefore := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY id),'[]')::text FROM events e WHERE project_id=$1::bigint`, project.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}()
	receiptsBefore := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=48`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}()
	projectValue := projectBefore()
	if _, err := db.pool.Exec(ctx, `DROP TABLE project_environments; DELETE FROM schema_migrations WHERE version=49`); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=49`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/049_project_environments.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("049 checksum receipt absent or changed", err)
		}
		rows, err := reopened.pool.Query(ctx, `SELECT name,is_hidden FROM project_environments WHERE project_id=$1::bigint ORDER BY name`, project.ID)
		if err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		var names []string
		for rows.Next() {
			var name string
			var hidden bool
			if err = rows.Scan(&name, &hidden); err != nil || hidden {
				rows.Close()
				reopened.Close()
				t.Fatalf("environment backfill name=%q hidden=%t err=%v", name, hidden, err)
			}
			names = append(names, name)
		}
		rows.Close()
		if fmt.Sprint(names) != "[log-env monitor-env production span-env]" {
			reopened.Close()
			t.Fatalf("environment backfill=%v", names)
		}
		var events, receipts string
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY id),'[]')::text FROM events e WHERE project_id=$1::bigint`, project.ID).Scan(&events); err != nil || events != eventsBefore {
			reopened.Close()
			t.Fatalf("049 changed events: %v", err)
		}
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=48`).Scan(&receipts); err != nil || receipts != receiptsBefore {
			reopened.Close()
			t.Fatalf("049 changed prior receipts: %v", err)
		}
		reopened.Close()
	}
	if projectBefore() != projectValue {
		t.Fatal("049 changed existing project rows")
	}
}
