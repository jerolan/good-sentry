//go:build integration

package store

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
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
	if _, err := db.pool.Exec(ctx, `UPDATE project_native_reprocessing SET cursor_event_id=through_event_id WHERE project_id=$1::bigint`, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
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

func TestNativeReprocessingBatchScopeLockAndAtomicity(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	foreign, err := db.CreateProject(ctx, user.ID, org.Slug, "Foreign native", "native")
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 26; index++ {
		content := []byte(fmt.Sprintf("MODULE Linux x86_64 %040d native-%02d.sym\n", index, index))
		hash := sha1.Sum(content)
		name := fmt.Sprintf("native-%02d.sym", index)
		if _, err := db.SaveDebugFile(ctx, access, project.ID, name, "sym", "", "x86_64", name, hex.EncodeToString(hash[:]), nil, nil, content); err != nil {
			t.Fatal(err)
		}
	}
	foreignContent := []byte("MODULE Linux x86_64 foreign foreign.sym\n")
	foreignHash := sha1.Sum(foreignContent)
	if _, err := db.SaveDebugFile(ctx, access, foreign.ID, "foreign.sym", "sym", "", "x86_64", "foreign.sym", hex.EncodeToString(foreignHash[:]), nil, nil, foreignContent); err != nil {
		t.Fatal(err)
	}
	insertEvent := func(eventID, platform string) int64 {
		t.Helper()
		var id int64
		if err := db.pool.QueryRow(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
			VALUES($1,$2::text,'error','native reprocessing','error',now(),jsonb_build_object('event_id',$2::text,'platform',$3::text)) RETURNING id`,
			project.ID, eventID, platform).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	nativeIDs := []int64{insertEvent("scope-native-1", "native"), insertEvent("scope-native-2", "native"), insertEvent("scope-native-3", "native")}
	insertEvent("scope-javascript", "javascript")
	if err := db.EnqueueNativeReprocessing(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	insertEvent("scope-native-late", "native")

	hold, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var locked int
	if err := hold.QueryRow(ctx, `SELECT 1 FROM project_native_reprocessing WHERE project_id=$1::bigint FOR UPDATE`, project.ID).Scan(&locked); err != nil {
		hold.Rollback(ctx)
		t.Fatal(err)
	}
	lockedCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	processed, err := db.ProcessNextNativeReprocessingBatch(lockedCtx, "", 0, func(string, int64, bool, []DebugSymbolFile, []NativeReprocessingEvent) (map[int64][]byte, error) {
		t.Fatal("transform ran while the queue row was locked")
		return nil, nil
	})
	if err != nil || processed {
		hold.Rollback(ctx)
		t.Fatalf("locked queue processed=%v err=%v", processed, err)
	}
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var seenFiles []string
	var seenEvents []int64
	processed, err = db.ProcessNextNativeReprocessingBatch(ctx, "", 0, func(_ string, _ int64, _ bool, files []DebugSymbolFile, events []NativeReprocessingEvent) (map[int64][]byte, error) {
		for _, file := range files {
			seenFiles = append(seenFiles, file.Name)
		}
		for _, event := range events {
			seenEvents = append(seenEvents, event.ID)
		}
		return nil, nil
	})
	if err != nil || !processed {
		t.Fatalf("scope batch processed=%v err=%v", processed, err)
	}
	if len(seenFiles) != 25 {
		t.Fatalf("symbol files=%d want=25", len(seenFiles))
	}
	for _, name := range seenFiles {
		if name == "foreign.sym" {
			t.Fatal("reprocessing loaded another project's symbols")
		}
	}
	if len(seenEvents) != len(nativeIDs) {
		t.Fatalf("batch events=%v want=%v", seenEvents, nativeIDs)
	}
	for index, id := range nativeIDs {
		if seenEvents[index] != id {
			t.Fatalf("batch events=%v want=%v", seenEvents, nativeIDs)
		}
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("completed batch remains queued: %d %v", count, err)
	}

	if err := db.EnqueueNativeReprocessing(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := db.pool.QueryRow(ctx, `SELECT data FROM events WHERE id=$1`, nativeIDs[0]).Scan(&before); err != nil {
		t.Fatal(err)
	}
	processed, err = db.ProcessNextNativeReprocessingBatch(ctx, "", 0, func(_ string, _ int64, _ bool, _ []DebugSymbolFile, events []NativeReprocessingEvent) (map[int64][]byte, error) {
		if len(events) != 4 {
			t.Fatalf("atomicity batch events=%d want=4", len(events))
		}
		return map[int64][]byte{events[0].ID: []byte(`{"event_id":"changed"}`), -1: []byte(`{}`)}, nil
	})
	if err == nil || processed {
		t.Fatalf("invalid update processed=%v err=%v", processed, err)
	}
	var after []byte
	var cursor int64
	if err := db.pool.QueryRow(ctx, `SELECT data FROM events WHERE id=$1`, nativeIDs[0]).Scan(&after); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("partial event update was committed: %v", err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT cursor_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&cursor); err != nil || cursor != 0 {
		t.Fatalf("failed transaction advanced cursor=%d err=%v", cursor, err)
	}
	processed, err = db.ProcessNextNativeReprocessingBatch(ctx, "", 0, func(string, int64, bool, []DebugSymbolFile, []NativeReprocessingEvent) (map[int64][]byte, error) {
		return nil, nil
	})
	if err != nil || !processed {
		t.Fatalf("retry processed=%v err=%v", processed, err)
	}
}

func TestNativeReprocessingFileByteBudget(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	older := []byte("MODULE Linux x86_64 older older.sym\n")
	olderHash := sha1.Sum(older)
	if _, err := db.SaveDebugFile(ctx, access, project.ID, "older.sym", "sym", "", "x86_64", "older.sym", hex.EncodeToString(olderHash[:]), nil, nil, older); err != nil {
		t.Fatal(err)
	}
	large := bytes.Repeat([]byte("s"), 64<<20)
	largeHash := sha1.Sum(large)
	if _, err := db.SaveDebugFile(ctx, access, project.ID, "large.sym", "sym", "", "x86_64", "large.sym", hex.EncodeToString(largeHash[:]), nil, nil, large); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
		VALUES($1,'budget-native','error','native reprocessing','error',now(),'{"platform":"native"}')`, project.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueNativeReprocessing(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	var names []string
	processed, err := db.ProcessNextNativeReprocessingBatch(ctx, "", 0, func(_ string, _ int64, _ bool, files []DebugSymbolFile, _ []NativeReprocessingEvent) (map[int64][]byte, error) {
		for _, file := range files {
			names = append(names, file.Name)
		}
		return nil, nil
	})
	if err != nil || !processed || len(names) != 1 || names[0] != "large.sym" {
		t.Fatalf("64MiB budget files=%v processed=%v err=%v", names, processed, err)
	}
}
