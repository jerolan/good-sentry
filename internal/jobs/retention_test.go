//go:build integration

package jobs_test

import (
	"context"
	"testing"
	"time"

	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
)

// The lifecycle owns startup backlog draining, idle catch-up, retry and prompt
// idempotent shutdown. SQL tests cannot detect a worker that never starts or
// sleeps for an hour during shutdown. Exercise real rows through public
// start/stop functions; no fake store, timer, logger or call-order seam.
func TestLogRetentionWorkerDrainsRetriesAndStops(t *testing.T) {
	f := newResolutionWorkerFixture(t)
	project, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Retention worker", "go")
	if err != nil {
		t.Fatal(err)
	}
	seed := func(prefix string, count int) {
		t.Helper()
		if _, err := f.admin.Exec(f.ctx, `INSERT INTO logs(project_id,timestamp,level,body,data,item_hash,created_at)
 SELECT $1,now(),'info',$2,'{}',$2||n,now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
			t.Fatal(err)
		}
	}
	waitEmpty := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var count int
			if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM logs").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("running retention worker did not drain real expired rows")
	}
	seed("startup", 601)
	stop := jobs.StartLogRetention(f.ctx, f.db, 1, time.Hour)
	waitEmpty()
	done := make(chan struct{})
	go func() { stop(); stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop waited for idle hour")
	}
	seed("idle", 3)
	other, err := store.Open(f.ctx, f.url)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	parent, cancel := context.WithCancel(f.ctx)
	first := jobs.StartLogRetention(parent, f.db, 1, 20*time.Millisecond)
	defer first()
	second := jobs.StartLogRetention(parent, other, 1, 20*time.Millisecond)
	defer second()
	waitEmpty()
	seed("after-idle", 2)
	waitEmpty()
	if _, err := f.admin.Exec(f.ctx, `CREATE SEQUENCE retention_fail_attempt;
 CREATE FUNCTION fail_worker_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('retention_fail_attempt'); RAISE EXCEPTION 'declared retention failure'; END $$;
 CREATE TRIGGER fail_worker_cleanup BEFORE DELETE ON logs FOR EACH ROW EXECUTE FUNCTION fail_worker_cleanup()`); err != nil {
		t.Fatal(err)
	}
	seed("retry", 1)
	failedAttempt := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.admin.QueryRow(f.ctx, "SELECT is_called FROM retention_fail_attempt").Scan(&failedAttempt); err != nil {
			t.Fatal(err)
		}
		if failedAttempt {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !failedAttempt {
		t.Fatal("worker never reached the declared database failure")
	}
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM logs WHERE body='retry'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failure lost row count=%d err=%v", count, err)
	}
	if _, err := f.admin.Exec(f.ctx, "DROP TRIGGER fail_worker_cleanup ON logs"); err != nil {
		t.Fatal(err)
	}
	waitEmpty()
	cancel()
	first()
	second()
	seed("stopped", 1)
	time.Sleep(60 * time.Millisecond)
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM logs WHERE body='stopped'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("stopped worker changed data: %d %v", count, err)
	}
}
