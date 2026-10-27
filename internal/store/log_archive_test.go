//go:build integration

package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestLogArchiveCopyUnionRestartAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	payload := json.RawMessage(`{"items":[{"timestamp":1,"level":"info","body":"archived local log","service":"archive-svc","environment":"archive-env","attributes":{}}]}`)
	if n, err := db.IngestLogs(ctx, project.ID, payload); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var logID int64
	if err := db.pool.QueryRow(ctx, `SELECT id FROM logs WHERE body='archived local log'`).Scan(&logID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE logs SET created_at=now()-interval '2 days' WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	archived, err := db.ArchiveLogs(ctx, time.Now().UTC().Add(-24*time.Hour), 250)
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	assertLogArchiveReaders(t, ctx, db, owner.ID, org.Slug, access, "archived local log", 1)

	if _, err := db.pool.Exec(ctx, `DELETE FROM logs WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	assertLogArchiveReaders(t, ctx, db, owner.ID, org.Slug, access, "archived local log", 1)
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertLogArchiveReaders(t, ctx, reopened, owner.ID, org.Slug, access, "archived local log", 1)

	if _, err := reopened.pool.Exec(ctx, `UPDATE log_archive SET created_at=now()-interval '40 days' WHERE id=$1`, logID); err != nil {
		t.Fatal(err)
	}
	removed, err := reopened.CleanupLogArchive(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	assertLogArchiveReaders(t, ctx, reopened, owner.ID, org.Slug, access, "archived local log", 0)
}

func assertLogArchiveReaders(t *testing.T, ctx context.Context, db *Store, userID, orgSlug string, access ManagementAccess, body string, want int) {
	t.Helper()
	compatible, err := db.CompatibleLogs(ctx, access, CompatibleLogFilter{Limit: 100})
	if err != nil || len(compatible) != want {
		t.Fatalf("compatible logs=%d want=%d err=%v", len(compatible), want, err)
	}
	if want == 1 {
		one, err := db.CompatibleLog(ctx, access, compatible[0].ID)
		if err != nil || one.Body != body {
			t.Fatalf("compatible detail=%+v err=%v", one, err)
		}
	}
	scoped, err := db.ScopedLogs(ctx, access, LogFilter{}, 0, 100)
	if err != nil || len(scoped) != want {
		t.Fatalf("scoped logs=%d want=%d err=%v", len(scoped), want, err)
	}
	native, err := db.Logs(ctx, userID, orgSlug, LogFilter{})
	if err != nil || len(native) != want {
		t.Fatalf("native logs=%d want=%d err=%v", len(native), want, err)
	}
}
