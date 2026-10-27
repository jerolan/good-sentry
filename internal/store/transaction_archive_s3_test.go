//go:build integration

package store

import (
	"testing"
	"time"
)

func TestTransactionArchiveS3UploadRestoreAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	ev := operationEvent(802, at, `[{"op":"db","description":"s3 child","start_timestamp":1,"timestamp":2}]`)
	if _, _, err := db.Ingest(ctx, project.ID, ev); err != nil {
		t.Fatal(err)
	}
	var rowID int64
	if err := db.pool.QueryRow(ctx, `SELECT id FROM events WHERE project_id=$1::bigint AND event_id=$2`, project.ID, ev.ID).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE id=$1`, rowID); err != nil {
		t.Fatal(err)
	}
	objects := &memoryEventObjects{}
	archived, err := db.ArchiveTransactionsS3(ctx, time.Now().UTC().Add(-24*time.Hour), 250, objects, "transactions")
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	assertTransactionArchiveReaders(t, ctx, db, owner.ID, org.Slug, project.ID, 1)
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE id=$1`, rowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM transaction_archive WHERE id=$1`, rowID); err != nil {
		t.Fatal(err)
	}
	restored, err := db.RestoreTransactionArchiveS3(ctx, objects, 250)
	if err != nil || restored != 1 {
		t.Fatalf("restored=%d err=%v", restored, err)
	}
	assertTransactionArchiveReaders(t, ctx, db, owner.ID, org.Slug, project.ID, 1)
	if _, err := db.pool.Exec(ctx, `UPDATE transaction_archive_objects SET created_at=now()-interval '40 days' WHERE id=$1`, rowID); err != nil {
		t.Fatal(err)
	}
	objects.failDelete = true
	if _, err := db.CleanupTransactionArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250); err == nil {
		t.Fatal("delete failure was hidden")
	}
	objects.failDelete = false
	removed, err := db.CleanupTransactionArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	assertTransactionArchiveReaders(t, ctx, db, owner.ID, org.Slug, project.ID, 0)
}
