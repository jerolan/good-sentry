//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestSpanArchiveManifestMigrationPreservesRowsAndReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	_, err := db.pool.Exec(ctx, `INSERT INTO spans(project_id,trace_id,span_id,name,status,is_segment,
		start_timestamp,end_timestamp,duration,data,created_at)
		VALUES($1,'archive-migration-trace','archive-migration-span','Migration span','ok',false,$2,$3,25,'{"kept":true}',$2)`, project.ID, base, base.Add(25*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "DROP TABLE span_archive_objects; DELETE FROM schema_migrations WHERE version=59"); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 58)
	databaseURL := db.pool.Config().ConnString()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after := groupDatabaseSnapshot(t, ctx, reopened, 58)
	delete(after, "span_archive_objects")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("059 changed preexisting rows or migration receipts")
	}
	var checksum string
	if err := reopened.pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=59").Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/059_span_archive_objects.sql")
	if err != nil || checksum != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatal("059 checksum receipt absent or changed", err)
	}
	objectKey := fmt.Sprintf("projects/%s/spans/archive-migration.parquet", project.ID)
	if _, err := reopened.pool.Exec(ctx, `INSERT INTO span_archive_objects(project_id,backend,object_key,row_count,object_size_bytes,
		min_created_at,max_created_at,min_start_timestamp,max_start_timestamp,min_end_timestamp,max_end_timestamp)
		VALUES($1,'local',$2,1,128,$3,$3,$3,$3,$4,$4)`, project.ID, objectKey, base, base.Add(25*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.pool.Exec(ctx, "DELETE FROM projects WHERE id=$1", project.ID); err != nil {
		t.Fatal(err)
	}
	var orphaned bool
	if err := reopened.pool.QueryRow(ctx, `SELECT project_id IS NULL FROM span_archive_objects
		WHERE object_key=$1`, objectKey).Scan(&orphaned); err != nil || !orphaned {
		t.Fatalf("project deletion must preserve the object key for cleanup: orphaned=%v err=%v", orphaned, err)
	}
	final, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	var count int
	if err := final.pool.QueryRow(ctx, `SELECT count(*) FROM span_archive_objects
		WHERE project_id IS NULL AND object_key=$1`, objectKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("orphan manifest did not survive reopen: count=%d err=%v", count, err)
	}
}
