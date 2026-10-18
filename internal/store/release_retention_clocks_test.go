//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"
)

// A real DELETE holds its uncommitted generation while a real create waits.
// Without the writer gate, INSERT evaluates its default before the unique wait
// and stamps the new identity too early. Sequential recreation cannot see this.
func TestReleaseRetentionFreshIdentityClockAfterCleanupWait(t *testing.T) {
	f := resolutionStore(t)
	old := f.release(t, "waiting@1")
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '2 days';
 CREATE FUNCTION pause_release_expiry() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,9342)); RETURN OLD; END $$;
 CREATE TRIGGER pause_release_expiry AFTER DELETE ON releases FOR EACH ROW EXECUTE FUNCTION pause_release_expiry()`); err != nil {
		t.Fatal(err)
	}
	barrier, blockerPID := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),9342))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	cleaned := make(chan error, 1)
	go func() {
		n, err := f.db.CleanupReleases(ctx, 1)
		if err == nil && n != 1 {
			err = fmt.Errorf("expired%d release identities, want1", n)
		}
		cleaned <- err
	}()
	cleanerPID := groupWaitBlocked(t, ctx, barrier, blockerPID, 1)[0]
	type result struct {
		release Release
		created bool
		err     error
	}
	created := make(chan result, 1)
	go func() {
		r, fresh, err := f.db.CreateRelease(ctx, f.access, ReleaseCreate{Version: old.Version, Projects: []string{f.project.ID}})
		created <- result{r, fresh, err}
	}()
	groupWaitBlocked(t, ctx, barrier, cleanerPID, 1)
	var released time.Time
	if err := barrier.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&released); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, cleaned)
	select {
	case got := <-created:
		if got.err != nil || !got.created || got.release.ID == old.ID || got.release.DateCreated.Before(released) {
			t.Fatalf("fresh identity acknowledged before insertion after cleanup wait: created=%v id=%s receipt=%s gateReleased=%s err=%v", got.created, got.release.ID, got.release.DateCreated, released, got.err)
		}
	case <-ctx.Done():
		t.Fatal("writer did not finish after real cleanup", ctx.Err())
	}
}

func TestReleaseRetentionMigration028PreservesAndReopens(t *testing.T) {
	f := resolutionStore(t)
	old := f.release(t, "before028@1")
	if _, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/before028.map", string(old.Version), "", "", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '400 days';
 DROP INDEX releases_retention; DELETE FROM schema_migrations WHERE version=28`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 27)
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if after := groupDatabaseSnapshot(t, f.ctx, reopened, 27); !reflect.DeepEqual(after, before) {
			t.Fatal("028 rewrote prior rows, ages or migration receipts")
		}
		var receipt, index string
		if err := reopened.pool.QueryRow(f.ctx, `SELECT checksum,to_regclass('releases_retention')::text FROM schema_migrations WHERE version=28`).Scan(&receipt, &index); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/028_release_retention.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) || index != "releases_retention" {
			t.Fatal("028 index/receipt missing or changed", receipt, index, err)
		}
		reopened.Close()
	}
}

func TestReleaseRetentionFrozenIDsAndStrictElapsedCutoff(t *testing.T) {
	for _, change := range []string{"boundary", "newly-old", "replaced"} {
		t.Run(change, func(t *testing.T) {
			f := resolutionStore(t)
			old, kept := f.release(t, "frozen@1"), f.release(t, "kept@1")
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '400 days' WHERE id::text=$1`, old.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.pool.Exec(f.ctx, `ALTER TABLE releases RENAME TO release_retention_rows;
 CREATE FUNCTION frozen_release_version(value text) RETURNS text LANGUAGE plpgsql VOLATILE AS $$
 BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(current_schema(),9343)); RETURN value; END $$;
 CREATE VIEW releases AS SELECT id,organization_id,frozen_release_version(version) AS version,
 created_at,date_started,date_released,ref,url,status FROM release_retention_rows`); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(f.db.pool.Config().ConnString())
			if err != nil {
				t.Fatal(err)
			}
			query := u.Query()
			query.Set("timezone", "America/New_York")
			u.RawQuery = query.Encode()
			cleaner, err := Open(f.ctx, u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer cleaner.Close()
			barrier, blockerPID := captureItemsBarrier(t, f)
			if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),9343))`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
			defer cancel()
			want := int64(0)
			if change == "newly-old" {
				want = 1
			}
			done := make(chan error, 1)
			go func() {
				n, err := cleaner.CleanupReleases(ctx, 250)
				if err == nil && n != want {
					err = fmt.Errorf("frozen identity/cutoff removed%d want%d", n, want)
				}
				done <- err
			}()
			pid := groupWaitBlocked(t, ctx, barrier, blockerPID, 1)[0]
			var start time.Time
			if err := barrier.QueryRow(ctx, "SELECT xact_start FROM pg_stat_activity WHERE pid=$1", pid).Scan(&start); err != nil {
				t.Fatal(err)
			}
			cutoff := start.Add(-250 * 24 * time.Hour)
			switch change {
			case "boundary":
				_, err = barrier.Exec(ctx, `UPDATE release_retention_rows SET created_at=$1 WHERE id::text=$2`, cutoff, old.ID)
			case "newly-old":
				_, err = barrier.Exec(ctx, `UPDATE release_retention_rows SET created_at=clock_timestamp()-interval '400 days' WHERE id::text=$1`, kept.ID)
			case "replaced":
				_, err = barrier.Exec(ctx, `DELETE FROM release_retention_rows WHERE id::text=$1`, old.ID)
				if err == nil {
					_, err = barrier.Exec(ctx, `INSERT INTO release_retention_rows(organization_id,version) VALUES($1::bigint,$2)`, f.org.ID, string(old.Version))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, done)
			if _, err := f.db.pool.Exec(ctx, `DROP VIEW releases; ALTER TABLE release_retention_rows RENAME TO releases`); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.db.pool.QueryRow(ctx, "SELECT count(*) FROM releases").Scan(&count); err != nil || count != 2-int(want) {
				t.Fatal("unfrozen/new generation lost", count, err)
			}
			if change == "newly-old" {
				if n, err := cleaner.CleanupReleases(ctx, 250); err != nil || n != 1 {
					t.Fatal("next pass did not reclaim newly eligible identity", n, err)
				}
			}
		})
	}
}
