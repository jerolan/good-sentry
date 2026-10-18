//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"
)

// Exercise the actual multi-query release reader across physical metadata
// deletion. A fixture-only updatable view pauses its first identity lookup;
// cleanup is free to commit, without mocking a snapshot or adding a Store seam.
func TestReleaseRetentionReaderCompletesItsAuthorizedSnapshot(t *testing.T) {
	f := resolutionStore(t)
	r := f.release(t, "snapshot@1")
	if _, err := f.db.ReplaceReleaseCommits(f.ctx, f.access, r.Version, []ReleaseCommitInput{{ID: "preserve-head", Repository: "snapshot/catalog", Data: json.RawMessage(`{"id":"preserve-head"}`)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateReleaseDeploy(f.ctx, f.access, r.Version, ReleaseDeployInput{Environment: "production"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	before, err := f.db.Release(f.ctx, f.access, r.Version, "")
	if err != nil || before.CommitCount != 1 || before.DeployCount != 1 || before.LastCommit == nil {
		t.Fatal("snapshot fixture did not expose owned metadata", before, err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `ALTER TABLE releases RENAME TO release_snapshot_rows;
 CREATE FUNCTION snapshot_release_version(value text) RETURNS text LANGUAGE plpgsql VOLATILE AS $$
 BEGIN IF current_setting('application_name')='release-snapshot-reader' THEN
 PERFORM pg_advisory_xact_lock(hashtextextended(current_schema(),9346)); END IF; RETURN value; END $$;
 CREATE VIEW releases AS SELECT id,organization_id,snapshot_release_version(version) AS version,
 created_at,date_started,date_released,ref,url,status FROM release_snapshot_rows`); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("application_name", "release-snapshot-reader")
	u.RawQuery = query.Encode()
	reader, err := Open(f.ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),9346))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	type result struct {
		release Release
		err     error
	}
	done := make(chan result, 1)
	go func() {
		got, err := reader.Release(ctx, f.access, r.Version, "")
		done <- result{got, err}
	}()
	groupWaitBlocked(t, ctx, barrier, pid, 1)
	if n, err := f.db.CleanupReleases(ctx, 1); err != nil || n != 1 {
		t.Fatal("cleanup blocked on an independent reader", n, err)
	}
	if _, err := f.db.Release(ctx, f.access, r.Version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("later reader still sees deleted metadata", err)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || !reflect.DeepEqual(before, got.release) {
			t.Fatal("in-flight release snapshot lost metadata after physical expiry", got.err, got.release)
		}
	case <-ctx.Done():
		t.Fatal("snapshot reader did not finish", ctx.Err())
	}
}
