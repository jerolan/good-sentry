//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Actual blocked transactions expose stale receipt clocks and leaked locks;
// ordinary sequential capture cannot reach either failure. Existing generic
// retention lifecycle coverage does not own these report-specific SQL effects.
func TestUserReportReceiptClockAndCanceledWriter(t *testing.T) {
	f := resolutionStore(t)
	input := UserReportInput{EventID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Comments: "After waiting"}
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, f.project.ID+":"+input.EventID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		report UserReport
		err    error
	}
	done := make(chan result, 1)
	go func() { report, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input); done <- result{report, err} }()
	groupWaitBlocked(t, f.ctx, barrier, pid, 1)
	var released time.Time
	if err := barrier.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.report.DateCreated.Before(released) {
		t.Fatal("report age started before the receipt gate released", got.report, released, got.err)
	}
	barrier, pid = captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, f.project.ID+":"+input.EventID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	go func() { report, err := f.db.SubmitUserReport(ctx, f.project.ID, input); done <- result{report, err} }()
	groupWaitBlocked(t, f.ctx, barrier, pid, 1)
	cancel()
	if got := <-done; got.err == nil || got.report.ID != "" {
		t.Fatal("canceled report acknowledged success", got)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	input.Comments = "A later writer still works"
	if next, err := f.db.SubmitUserReport(f.ctx, f.project.ID, input); err != nil || next.ID != got.report.ID || !next.DateCreated.Equal(got.report.DateCreated) {
		t.Fatal("canceled transaction leaked locks or replaced identity", next, err)
	}
}

func TestPendingUserReportCleanupStrictServerCutoff(t *testing.T) {
	f := resolutionStore(t)
	for n := 1; n <= 3; n++ {
		if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: fmt.Sprintf("%032x", n), Comments: "Cutoff"}); err != nil {
			t.Fatal(err)
		}
	}
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `LOCK TABLE user_reports IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		n, err := f.db.CleanupPendingUserReports(f.ctx)
		if err == nil && n != 1 {
			err = fmt.Errorf("expired%d reports, want only the strictly old receipt", n)
		}
		done <- err
	}()
	cleaner := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
	var start time.Time
	if err := barrier.QueryRow(f.ctx, `SELECT xact_start FROM pg_stat_activity WHERE pid=$1`, cleaner).Scan(&start); err != nil {
		t.Fatal(err)
	}
	// The actual cleanup transaction has begun and is blocked before reading
	// these rows. Publish exact microsecond neighbors of its fixed cutoff.
	if _, err := barrier.Exec(f.ctx, `UPDATE user_reports SET created_at=$1::timestamptz-interval '30 minutes'+CASE event_id
 WHEN '00000000000000000000000000000001' THEN -interval '1 microsecond'
 WHEN '00000000000000000000000000000002' THEN interval '0 seconds' ELSE interval '1 microsecond' END`, start); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, f.ctx, done)
	var remaining []string
	rows, err := f.db.pool.Query(f.ctx, `SELECT event_id FROM user_reports ORDER BY event_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || !reflect.DeepEqual(remaining, []string{fmt.Sprintf("%032x", 2), fmt.Sprintf("%032x", 3)}) {
		t.Fatal("cleanup used a moving clock or inclusive expiry", remaining, err)
	}
}

func TestPendingUserReportCleanupBoundedSkipsLockedAndBound(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "bound", "Bound report", time.Now())
	groupCapture(t, f.ctx, f.db, f.project, ev)
	bound, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: ev.ID, Comments: "Bound survives orphan cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE user_reports SET created_at=clock_timestamp()-interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO user_reports(project_id,event_id,name,email,comments,created_at)
 SELECT $1::bigint,lpad(to_hex(n),32,'0'),'','','Orphan',clock_timestamp()-interval '1 day' FROM generate_series(2,602) n`, f.project.ID); err != nil {
		t.Fatal(err)
	}
	barrier, _ := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, `SELECT id FROM user_reports WHERE event_id=$1 FOR UPDATE`, fmt.Sprintf("%032x", 2)); err != nil {
		t.Fatal(err)
	}
	other := captureItemsOther(t, f)
	for _, want := range []int64{250, 250, 100, 0} {
		if n, err := other.CleanupPendingUserReports(f.ctx); err != nil || n != want {
			t.Fatal("bounded independent cleanup", n, want, err)
		}
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := other.CleanupPendingUserReports(f.ctx); err != nil || n != 1 {
		t.Fatal("unlocked orphan not reclaimed", n, err)
	}
	var retained string
	if err := f.db.pool.QueryRow(f.ctx, `SELECT id::text FROM user_reports`).Scan(&retained); err != nil || retained != bound.ID {
		t.Fatal("orphan cleanup removed a linked report", retained, err)
	}
}

func TestPendingUserReportCleanupFailureRollsBack(t *testing.T) {
	for _, mode := range []string{"statement", "commit", "cancel", "maintainer"} {
		t.Run(mode, func(t *testing.T) {
			f := resolutionStore(t)
			if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO user_reports(project_id,event_id,name,email,comments,created_at)
 SELECT $1::bigint,lpad(to_hex(n),32,'0'),'','','Failure fixture',clock_timestamp()-interval '1 day' FROM generate_series(1,3) n`, f.project.ID); err != nil {
				t.Fatal(err)
			}
			barrier, pid := captureItemsBarrier(t, f)
			failure := `RAISE EXCEPTION 'declared report cleanup failure';`
			trigger := `CREATE TRIGGER fail_user_reports AFTER DELETE ON user_reports FOR EACH ROW EXECUTE FUNCTION fail_user_reports()`
			if mode == "commit" {
				trigger = `CREATE CONSTRAINT TRIGGER fail_user_reports AFTER DELETE ON user_reports DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_user_reports()`
			} else if mode == "cancel" {
				failure = `PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,9351));`
				if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema(),9351))`); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "maintainer" {
				if _, err := f.db.pool.Exec(f.ctx, `CREATE SEQUENCE user_report_visits;
 CREATE FUNCTION fail_user_reports() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF nextval('user_report_visits')=2 THEN `+failure+` END IF; RETURN OLD; END $$; `+trigger); err != nil {
					t.Fatal(err)
				}
			} else if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':pending-user-reports',0))`); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
			if mode == "cancel" {
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					n, err := f.db.CleanupPendingUserReports(ctx)
					if err == nil || n != 0 {
						done <- fmt.Errorf("canceled cleanup acknowledged%d: %v", n, err)
						return
					}
					done <- nil
				}()
				groupWaitBlocked(t, f.ctx, barrier, pid, 1)
				cancel()
				deleteOrderAwait(t, f.ctx, done)
			} else {
				n, err := f.db.CleanupPendingUserReports(f.ctx)
				if n != 0 || (mode == "maintainer" && err != nil) || (mode != "maintainer" && err == nil) {
					t.Fatal("uncommitted report cleanup acknowledged", n, err)
				}
			}
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			if got := groupDatabaseSnapshot(t, f.ctx, f.db, 29); !reflect.DeepEqual(got, before) {
				t.Fatal("failed/busy cleanup changed durable rows")
			}
			guard, err := f.db.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Rollback(f.ctx)
			if _, err := guard.Exec(f.ctx, `LOCK TABLE user_reports IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal("report cleanup leaked locks", err)
			}
			if err := guard.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUserReportsMigration029PreservesAndReopens(t *testing.T) {
	f := resolutionStore(t)
	f.capture(t, 1, "before029@1", time.Now().Add(-365*24*time.Hour))
	if _, err := f.db.pool.Exec(f.ctx, `DROP TABLE user_reports; ALTER TABLE events DROP CONSTRAINT events_report_receipt; DELETE FROM schema_migrations WHERE version=29`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 28)
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		after := groupDatabaseSnapshot(t, f.ctx, reopened, 28)
		delete(after, "user_reports")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("029 rewrote old rows or historical migration receipts")
		}
		var receipt string
		if err := reopened.pool.QueryRow(f.ctx, `SELECT checksum FROM schema_migrations WHERE version=29`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/029_user_reports.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("029 receipt differs from the actual migration", err)
		}
		reopened.Close()
	}
}
