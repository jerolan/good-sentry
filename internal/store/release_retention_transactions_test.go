//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// These real lock/failure paths own risks that sequential expiry cannot reach:
// an actual writer can hold its identity gate before taking any row lock;
// statement, deferred commit and cancellation must roll back every cascade.
func TestReleaseRetentionSkipsBusyWriterRowsAndMaintainer(t *testing.T) {
	for _, busy := range []string{"writer", "row", "maintainer"} {
		t.Run(busy, func(t *testing.T) {
			f := resolutionStore(t)
			old := f.release(t, "busy@1")
			f.release(t, "free@1")
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '2 days'`); err != nil {
				t.Fatal(err)
			}
			barrier, pid := captureItemsBarrier(t, f)
			ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
			defer cancel()
			var writer chan error
			switch busy {
			case "writer":
				if _, err := f.db.pool.Exec(ctx, `CREATE FUNCTION pause_release_writer() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,9344)); RETURN NEW; END $$;
 CREATE TRIGGER pause_release_writer BEFORE INSERT ON releases FOR EACH ROW EXECUTE FUNCTION pause_release_writer()`); err != nil {
					t.Fatal(err)
				}
				if _, err := barrier.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),9344))`); err != nil {
					t.Fatal(err)
				}
				writer = make(chan error, 1)
				go func() {
					r, fresh, err := f.db.CreateRelease(ctx, f.access, ReleaseCreate{Version: old.Version, Projects: []string{f.project.ID}})
					if err == nil && (fresh || r.ID != old.ID) {
						err = fmt.Errorf("busy duplicate changed its retained identity: created=%v id=%s", fresh, r.ID)
					}
					writer <- err
				}()
				groupWaitBlocked(t, ctx, barrier, pid, 1)
			case "row":
				if _, err := barrier.Exec(ctx, `SELECT id FROM releases WHERE id::text=$1 FOR UPDATE`, old.ID); err != nil {
					t.Fatal(err)
				}
			case "maintainer":
				if _, err := barrier.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':release-retention',0))`); err != nil {
					t.Fatal(err)
				}
			}
			want := int64(1)
			if busy == "maintainer" {
				want = 0
			}
			other := captureItemsOther(t, f)
			if n, err := other.CleanupReleases(ctx, 1); err != nil || n != want {
				t.Fatal("busy identity did not let independent work proceed", n, want, err)
			}
			if _, err := f.db.Release(ctx, f.access, old.Version, ""); err != nil {
				t.Fatal("busy identity removed", err)
			}
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			if writer != nil {
				deleteOrderAwait(t, ctx, writer)
			}
			if n, err := other.CleanupReleases(ctx, 1); err != nil || n != 2-want {
				t.Fatal("unlocked expired identities not reclaimed", n, err)
			}
		})
	}
}

func TestReleaseRetentionLateFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"statement", "commit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := resolutionStore(t)
			for _, version := range []string{"first@1", "later@1"} {
				r := f.release(t, version)
				if _, err := f.db.ReplaceReleaseCommits(f.ctx, f.access, r.Version, []ReleaseCommitInput{{ID: version, Repository: "kept/catalog", Data: json.RawMessage(`{"id":"opaque"}`)}}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.CreateReleaseDeploy(f.ctx, f.access, r.Version, ReleaseDeployInput{Environment: "production"}); err != nil {
					t.Fatal(err)
				}
			}
			failure := `RAISE EXCEPTION 'declared later release failure';`
			trigger := `CREATE TRIGGER fail_release_retention AFTER DELETE ON releases FOR EACH ROW EXECUTE FUNCTION fail_release_retention()`
			if mode == "commit" {
				trigger = `CREATE CONSTRAINT TRIGGER fail_release_retention AFTER DELETE ON releases DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_release_retention()`
			} else if mode == "cancel" {
				failure = `PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,9345));`
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '2 days';
 CREATE SEQUENCE release_retention_visits;
 CREATE FUNCTION fail_release_retention() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF nextval('release_retention_visits')=2 THEN `+failure+` END IF; RETURN OLD; END $$;
 `+trigger); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
			if mode == "cancel" {
				barrier, pid := captureItemsBarrier(t, f)
				if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),9345))`); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					n, err := f.db.CleanupReleases(ctx, 1)
					if err == nil || n != 0 {
						done <- fmt.Errorf("canceled release deletion acknowledged%d: %v", n, err)
						return
					}
					done <- nil
				}()
				groupWaitBlocked(t, f.ctx, barrier, pid, 1)
				cancel()
				deleteOrderAwait(t, f.ctx, done)
				if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				guardCtx, stop := context.WithTimeout(f.ctx, 5*time.Second)
				defer stop()
				guard, err := f.db.pool.Begin(guardCtx)
				if err != nil {
					t.Fatal(err)
				}
				defer guard.Rollback(f.ctx)
				if _, err := guard.Exec(guardCtx, "LOCK TABLE releases,release_projects,release_commits,release_deploys,release_deploy_projects IN ACCESS EXCLUSIVE MODE"); err != nil {
					t.Fatal("canceled transaction leaked release locks", err)
				}
				if err := guard.Commit(f.ctx); err != nil {
					t.Fatal(err)
				}
			} else if n, err := f.db.CleanupReleases(f.ctx, 1); err == nil || n != 0 {
				t.Fatal("later failure acknowledged partial release removal", n, err)
			}
			var visits int
			if err := f.db.pool.QueryRow(f.ctx, "SELECT last_value FROM release_retention_visits").Scan(&visits); err != nil || visits != 2 {
				t.Fatal("failure did not follow prior parent removal", visits, err)
			}
			groupRequireUnchanged(t, f.ctx, f.db, before)
		})
	}
}
