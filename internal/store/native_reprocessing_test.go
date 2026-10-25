//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestNativeReprocessingQueueCoalescesAndCascades(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
		VALUES($1,'native-reprocess-1','error','native','error',now(),'{"platform":"native"}')`, project.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueNativeReprocessing(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	var firstID, cursor, through int64
	if err := db.pool.QueryRow(ctx, `SELECT max(id) FROM events WHERE project_id=$1::bigint`, project.ID).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT cursor_event_id,through_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&cursor, &through); err != nil || cursor != 0 || through != firstID {
		t.Fatalf("initial queue cursor=%d through=%d first=%d err=%v", cursor, through, firstID, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE project_native_reprocessing SET cursor_event_id=through_event_id WHERE project_id=$1::bigint;
		INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
		VALUES($1,'native-reprocess-2','error','native','error',now(),'{"platform":"native"}')`, project.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueNativeReprocessing(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	processed, err := db.ProcessNextNativeReprocessingBatch(ctx, "", 0, func(_ string, _ int64, _ bool, _ []DebugSymbolFile, _ []NativeReprocessingEvent) (map[int64][]byte, error) {
		return nil, errors.New("retry this batch")
	})
	if err == nil || processed {
		t.Fatalf("failed transform processed=%v err=%v", processed, err)
	}
	var count int
	var generation int64
	if err := db.pool.QueryRow(ctx, `SELECT count(*),min(cursor_event_id),max(through_event_id),max(generation) FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&count, &cursor, &through, &generation); err != nil || count != 1 || cursor != 0 || through <= firstID || generation != 2 {
		t.Fatalf("coalesced queue rows=%d cursor=%d through=%d generation=%d err=%v", count, cursor, through, generation, err)
	}
	processed, err = db.ProcessNextNativeReprocessingBatch(ctx, "", 0, func(_ string, _ int64, _ bool, _ []DebugSymbolFile, _ []NativeReprocessingEvent) (map[int64][]byte, error) {
		return nil, nil
	})
	if err != nil || !processed {
		t.Fatalf("successful batch processed=%v err=%v", processed, err)
	}
	if err := db.EnqueueNativeReprocessing(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT generation,cursor_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&generation, &cursor); err != nil || generation != 3 || cursor != 0 {
		t.Fatalf("new queue generation=%d cursor=%d err=%v", generation, cursor, err)
	}
	if err := db.EnqueueNativeReprocessing(ctx, ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: false}, project.ID); err == nil {
		t.Fatal("project request without a project grant was accepted")
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1::bigint`, project.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("project deletion did not cascade queue row: %d %v", count, err)
	}
}

func TestNativeReprocessingMigrationReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	var oldProject, oldReceipts string
	if err := db.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM projects p WHERE id=$1::bigint`, project.ID).Scan(&oldProject); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=67`).Scan(&oldReceipts); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP INDEX events_native_reprocessing; DROP TABLE project_native_reprocessing;
		DROP SEQUENCE project_native_reprocessing_generation; DELETE FROM schema_migrations WHERE version=68`); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var projectAfter, receiptsAfter, checksum string
	if err := reopened.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM projects p WHERE id=$1::bigint`, project.ID).Scan(&projectAfter); err != nil || projectAfter != oldProject {
		t.Fatalf("migration changed existing project: %v", err)
	}
	if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=67`).Scan(&receiptsAfter); err != nil || receiptsAfter != oldReceipts {
		t.Fatalf("migration changed old receipts: %v", err)
	}
	if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=68`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/068_native_reprocessing.sql")
	if err != nil || checksum != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("068 checksum incorrect: %v", err)
	}
	var table, sequence, index string
	if err := reopened.pool.QueryRow(ctx, `SELECT to_regclass('project_native_reprocessing')::text`).Scan(&table); err != nil || table == "" {
		t.Fatalf("068 queue table missing: %q %v", table, err)
	}
	if err := reopened.pool.QueryRow(ctx, `SELECT to_regclass('project_native_reprocessing_generation')::text,to_regclass('events_native_reprocessing')::text`).Scan(&sequence, &index); err != nil || sequence == "" || index == "" {
		t.Fatalf("068 sequence/index missing: %q %q %v", sequence, index, err)
	}
}
