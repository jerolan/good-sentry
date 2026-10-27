//go:build integration

package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLogArchiveS3UploadRestoreAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	payload := json.RawMessage(`{"items":[{"timestamp":1,"level":"warn","body":"archived S3 log","service":"s3-svc","environment":"s3-env","attributes":{}}]}`)
	if n, err := db.IngestLogs(ctx, project.ID, payload); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var logID int64
	if err := db.pool.QueryRow(ctx, `SELECT id FROM logs WHERE body='archived S3 log'`).Scan(&logID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days' WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	objects := &memoryEventObjects{}
	archived, err := db.ArchiveLogsS3(ctx, time.Now().UTC().Add(-24*time.Hour), 250, objects, "logs")
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	assertLogArchiveReaders(t, ctx, db, owner.ID, org.Slug, access, "archived S3 log", 1)

	if _, err := db.pool.Exec(ctx, `DELETE FROM logs WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM log_archive WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	restored, err := db.RestoreLogArchiveS3(ctx, objects, 250)
	if err != nil || restored != 1 {
		t.Fatalf("restored=%d err=%v", restored, err)
	}
	assertLogArchiveReaders(t, ctx, db, owner.ID, org.Slug, access, "archived S3 log", 1)

	if _, err := db.pool.Exec(ctx, `UPDATE log_archive_objects SET created_at=now()-interval '40 days' WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	objects.failDelete = true
	if _, err := db.CleanupLogArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250); err == nil {
		t.Fatal("delete failure was hidden")
	}
	objects.failDelete = false
	removed, err := db.CleanupLogArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	assertLogArchiveReaders(t, ctx, db, owner.ID, org.Slug, access, "archived S3 log", 0)
}
