//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryEventObjects struct {
	data       map[string][]byte
	failDelete bool
}

func (m *memoryEventObjects) Put(_ context.Context, key string, data []byte) error {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	if current, ok := m.data[key]; ok && string(current) != string(data) {
		return errors.New("object already exists with different contents")
	}
	m.data[key] = append([]byte(nil), data...)
	return nil
}

func (m *memoryEventObjects) Get(_ context.Context, key string) ([]byte, error) {
	data, ok := m.data[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return append([]byte(nil), data...), nil
}

func (m *memoryEventObjects) Delete(_ context.Context, key string) error {
	if m.failDelete {
		return errors.New("delete failed")
	}
	delete(m.data, key)
	return nil
}

func TestEventArchiveS3UploadRestoreAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	ev := groupEvent(t, 702, "event-archive-s3", "S3 archived error", at)
	issueID, fresh, err := db.Ingest(ctx, project.ID, ev)
	if err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=$1`, ev.ID); err != nil {
		t.Fatal(err)
	}
	objects := &memoryEventObjects{}
	archived, err := db.ArchiveEventsS3(ctx, time.Now().UTC().Add(-24*time.Hour), 250, objects, "events")
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	var key string
	if err := db.pool.QueryRow(ctx, `SELECT object_key FROM event_archive_objects WHERE event_id=$1`, ev.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if len(objects.data[key]) == 0 {
		t.Fatal("S3 object was not uploaded")
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	assertEventArchiveReaders(t, ctx, db, owner.ID, access, project.ID, issueID, ev.ID, 1)

	// Simulate a restart with an empty local cache: remove both the hot row and
	// the materialized archive row, then restore only from the S3 catalog/object.
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2`, project.ID, ev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM event_archive WHERE event_id=$1`, ev.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := db.RestoreEventArchiveS3(ctx, objects, 250)
	if err != nil || restored != 1 {
		t.Fatalf("restored=%d err=%v", restored, err)
	}
	assertEventArchiveReaders(t, ctx, db, owner.ID, access, project.ID, issueID, ev.ID, 1)

	// A second cached row must not hide a later missing row from the restore
	// selector; the query filters cache hits instead of always taking the first 250.
	second := groupEvent(t, 704, "event-archive-s3-second", "S3 second archived error", at)
	if _, fresh, err := db.Ingest(ctx, project.ID, second); err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ArchiveEventsS3(ctx, time.Now().UTC().Add(-24*time.Hour), 250, objects, "events"); err != nil || n != 1 {
		t.Fatalf("second archive=%d err=%v", n, err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM event_archive WHERE event_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := db.RestoreEventArchiveS3(ctx, objects, 250); err != nil || n != 1 {
		t.Fatalf("selective restore=%d err=%v", n, err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE event_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM event_archive WHERE event_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM event_archive_objects WHERE event_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.pool.Exec(ctx, `UPDATE event_archive_objects SET created_at=now()-interval '40 days' WHERE event_id=$1`, ev.ID); err != nil {
		t.Fatal(err)
	}
	removed, err := db.CleanupEventArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, ok := objects.data[key]; ok {
		t.Fatal("expired S3 object was retained")
	}
	if _, err := db.CleanupEventArchive(ctx, time.Now().UTC().Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertEventArchiveReaders(t, ctx, db, owner.ID, access, project.ID, issueID, ev.ID, 0)
}

func TestEventArchiveS3DeleteFailureLeavesCatalog(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	ev := groupEvent(t, 703, "event-archive-s3-delete", "S3 delete failure", time.Now().UTC())
	if _, fresh, err := db.Ingest(ctx, project.ID, ev); err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=$1`, ev.ID); err != nil {
		t.Fatal(err)
	}
	objects := &memoryEventObjects{}
	if _, err := db.ArchiveEventsS3(ctx, time.Now().UTC().Add(-24*time.Hour), 250, objects, "events"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE event_archive_objects SET created_at=now()-interval '40 days' WHERE event_id=$1`, ev.ID); err != nil {
		t.Fatal(err)
	}
	objects.failDelete = true
	if _, err := db.CleanupEventArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250); err == nil {
		t.Fatal("delete failure was hidden")
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM event_archive_objects WHERE event_id=$1`, ev.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("catalog rows after failure=%d err=%v", count, err)
	}
	objects.failDelete = false
	if removed, err := db.CleanupEventArchiveS3(ctx, time.Now().UTC().Add(-30*24*time.Hour), objects, 250); err != nil || removed != 1 {
		t.Fatalf("retry removed=%d err=%v", removed, err)
	}
}
