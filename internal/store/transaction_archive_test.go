//go:build integration

package store

import (
	"context"
	"testing"
	"time"
)

func TestTransactionArchiveCopyUnionRestartAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	ev := operationEvent(801, at, `[{"op":"db","description":"archive child","start_timestamp":1,"timestamp":2}]`)
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
	archived, err := db.ArchiveTransactions(ctx, time.Now().UTC().Add(-24*time.Hour), 250)
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	assertTransactionArchiveReaders(t, ctx, db, owner.ID, org.Slug, project.ID, 1)
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE id=$1`, rowID); err != nil {
		t.Fatal(err)
	}
	assertTransactionArchiveReaders(t, ctx, db, owner.ID, org.Slug, project.ID, 1)
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertTransactionArchiveReaders(t, ctx, reopened, owner.ID, org.Slug, project.ID, 1)
	if _, err := reopened.pool.Exec(ctx, `UPDATE transaction_archive SET created_at=now()-interval '40 days' WHERE id=$1`, rowID); err != nil {
		t.Fatal(err)
	}
	removed, err := reopened.CleanupTransactionArchive(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	assertTransactionArchiveReaders(t, ctx, reopened, owner.ID, org.Slug, project.ID, 0)
}

func assertTransactionArchiveReaders(t *testing.T, ctx context.Context, db *Store, userID, orgSlug, projectID string, want int) {
	t.Helper()
	groups, err := db.Transactions(ctx, userID, orgSlug, projectID, nil)
	if err != nil || len(groups) != want {
		t.Fatalf("transactions=%d want=%d err=%v", len(groups), want, err)
	}
}
