//go:build integration

package store

import (
	"bytes"
	"context"
	"net/url"
	"testing"
	"time"

	"goodsentry/internal/artifact"
)

// The consumer must finish a pre-expiry map snapshot even when cleanup commits
// between preflight and byte retrieval. A read-committed regression loses those
// acknowledged bytes. A fixture-only updatable view blocks the actual first
// query, without a production clock/flag or a fake Store implementation.
func TestFileRetentionPreservesInFlightMapSnapshot(t *testing.T) {
	f := fileRetentionFixture(t)
	u, err := url.Parse(f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("application_name", "file-retention-snapshot-reader")
	u.RawQuery = q.Encode()
	reader, err := Open(f.ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	// PostgreSQL permits DELETE through a simple view with a computed read-only
	// column; see https://www.postgresql.org/docs/18/sql-createview.html.
	if _, err := f.db.pool.Exec(f.ctx, `ALTER TABLE source_maps RENAME TO file_retention_snapshot_maps;
 CREATE FUNCTION file_retention_snapshot_name(value text) RETURNS text LANGUAGE plpgsql VOLATILE AS $$
 BEGIN IF current_setting('application_name')='file-retention-snapshot-reader' THEN
 PERFORM pg_advisory_xact_lock(hashtextextended(current_schema(),8353)); END IF; RETURN value; END $$;
 CREATE VIEW source_maps AS SELECT id,project_id,file_retention_snapshot_name(name) AS name,release,dist,debug_id,checksum,size,data,created_at,code_files FROM file_retention_snapshot_maps`); err != nil {
		t.Fatal(err)
	}
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),8353))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	type readResult struct {
		maps []artifact.SourceMap
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		maps, err := reader.SourceMaps(ctx, f.project.ID, "atomic", "", nil, 1<<20)
		done <- readResult{maps, err}
	}()
	groupWaitBlocked(t, ctx, barrier, pid, 1)
	bounded, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if maps, attachments, items, err := f.db.CleanupFiles(bounded, 1); err != nil || maps != 1 || attachments != 1 || items != 1 {
		t.Fatal("cleanup did not commit while the reader held its earlier snapshot", maps, attachments, items, err)
	}
	var rows int
	if err := f.db.pool.QueryRow(ctx, "SELECT count(*) FROM file_retention_snapshot_maps").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("file bytes were not physically removed", rows, err)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || len(result.maps) != 1 || result.maps[0].Name != "~/atomic.map" || !bytes.Equal(result.maps[0].Data, emptySourceMap) {
			t.Fatal("in-flight source-map snapshot lost preflight bytes", result.maps, result.err)
		}
	case <-ctx.Done():
		t.Fatal("snapshot reader did not finish", ctx.Err())
	}
	if maps, err := reader.SourceMaps(ctx, f.project.ID, "atomic", "", nil, 1<<20); err != nil || len(maps) != 0 {
		t.Fatal("later capture still observed deleted files", maps, err)
	}
}
