//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/uptime"
)

// Cleanup must roll back all selected history after a later delete fails or is
// cancelled, skip busy rows/maintainers and preserve existing upgrade receipts.
// Seed actual completions and induce failures at the real PostgreSQL boundary.
func TestUptimeRetentionLateDeleteFailureAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", canceled), func(t *testing.T) {
			ctx, db, owner, org, _ := telemetryStore(t)
			c := uptime.Defaults()
			c.Name, c.Type = "Atomic history", uptime.Heartbeat
			m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := db.Heartbeat(ctx, m.HeartbeatKey); err != nil {
					t.Fatal(err)
				}
			}
			failure := `RAISE EXCEPTION 'declared late check deletion failure';`
			if canceled {
				failure = `PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,8351));`
			}
			if _, err := db.pool.Exec(ctx, `UPDATE monitor_checks SET started_at=started_at-interval '2 days',completed_at=completed_at-interval '2 days';
 CREATE SEQUENCE retention_check_deletes;
 CREATE FUNCTION wait_retention_check() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF nextval('retention_check_deletes')>1 THEN `+failure+` END IF; RETURN OLD; END $$;
 CREATE TRIGGER wait_retention_check BEFORE DELETE ON monitor_checks FOR EACH ROW EXECUTE FUNCTION wait_retention_check()`); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, ctx, db, 25)
			if !canceled {
				if n, err := db.CleanupMonitorChecks(ctx, 1); err == nil || n != 0 {
					t.Fatal("failed late delete acknowledged removal", n, err)
				}
			} else {
				barrier, err := db.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer barrier.Rollback(ctx)
				var pid int
				if err := barrier.QueryRow(ctx, `SELECT pg_backend_pid() FROM (SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),8351))) gate`).Scan(&pid); err != nil {
					t.Fatal(err)
				}
				bounded, cancel := context.WithCancel(ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					n, err := db.CleanupMonitorChecks(bounded, 1)
					if err == nil || n != 0 {
						done <- fmt.Errorf("canceled late delete acknowledged %d: %v", n, err)
						return
					}
					done <- nil
				}()
				groupWaitBlocked(t, ctx, barrier, pid, 1)
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("cleanup ignored cancellation during a later row deletion")
				}
				if err := barrier.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				// Wait for the real deleting transaction to release its table lock,
				// so a still-running cancelled statement cannot hide behind MVCC.
				guardCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				defer stop()
				guard, err := db.pool.Begin(guardCtx)
				if err != nil {
					t.Fatal(err)
				}
				defer guard.Rollback(ctx)
				if _, err := guard.Exec(guardCtx, "LOCK TABLE monitor_checks IN ACCESS EXCLUSIVE MODE"); err != nil {
					t.Fatal("cancelled SQL retained a live history transaction", err)
				}
				if err := guard.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var visited int64
			if err := db.pool.QueryRow(ctx, "SELECT last_value FROM retention_check_deletes").Scan(&visited); err != nil || visited != 2 {
				t.Fatal("failure did not follow a prior deletion", visited, err)
			}
			if after := groupDatabaseSnapshot(t, ctx, db, 25); !reflect.DeepEqual(after, before) {
				t.Fatal("late failure/cancellation committed partial history or parent changes")
			}
		})
	}
}

func TestUptimeRetentionSkipsLockedCheckAndBusyMaintainer(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	c := uptime.Defaults()
	c.Name, c.Type = "Locked history", uptime.Heartbeat
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Heartbeat(ctx, m.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE monitor_checks SET started_at=started_at-interval '2 days',completed_at=completed_at-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 25)
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "SELECT id FROM monitor_checks FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CleanupMonitorChecks(ctx, 1); err != nil || n != 0 {
		t.Fatal("locked check not skipped", n, err)
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':uptime-retention',0))`); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if n, err := other.CleanupMonitorChecks(ctx, 1); err != nil || n != 0 {
		t.Fatal("busy maintainer did not idle", n, err)
	}
	if after := groupDatabaseSnapshot(t, ctx, db, 25); !reflect.DeepEqual(after, before) {
		t.Fatal("idle cleanup changed data")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := other.CleanupMonitorChecks(ctx, 1); err != nil || n != 1 {
		t.Fatal("released check not reclaimed", n, err)
	}
}

func TestUptimeRetentionMigration025PreservesAndReopens(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	c := uptime.Defaults()
	c.Name, c.Type = "Before025", uptime.Heartbeat
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Heartbeat(ctx, m.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP INDEX monitor_checks_completion_retention; DELETE FROM schema_migrations WHERE version=25`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 24)
	for range 2 {
		reopened, err := Open(ctx, db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if after := groupDatabaseSnapshot(t, ctx, reopened, 24); !reflect.DeepEqual(after, before) {
			t.Fatal("025 rewrote prior rows or migration receipts")
		}
		var receipt, index string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum,to_regclass('monitor_checks_completion_retention')::text FROM schema_migrations WHERE version=25`).Scan(&receipt, &index); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/025_uptime_retention.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) || index != "monitor_checks_completion_retention" {
			t.Fatal("025 index/receipt absent or changed", err)
		}
		reopened.Close()
	}
}
