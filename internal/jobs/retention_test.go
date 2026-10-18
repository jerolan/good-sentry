//go:build integration

package jobs_test

import (
	"context"
	"testing"
	"time"

	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/uptime"
)

// The lifecycle owns startup backlog draining, idle catch-up, retry and prompt
// idempotent shutdown. SQL tests cannot detect a worker that never starts or
// sleeps for an hour during shutdown. Exercise real rows through public
// start/stop functions; no fake store, timer, logger or call-order seam.
func TestRetentionWorkersDrainRetryAndStop(t *testing.T) {
	for _, class := range []string{"logs", "transactions", "events", "uptime", "files", "releases", "pending_reports"} {
		t.Run(class, func(t *testing.T) { retentionWorkerLifecycle(t, class) })
	}
}

func retentionWorkerLifecycle(t *testing.T, class string) {
	f := newResolutionWorkerFixture(t)
	project, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Retention worker", "go")
	if err != nil {
		t.Fatal(err)
	}
	start := jobs.StartLogRetention
	countQuery := "SELECT count(*) FROM logs"
	failureTable, valueColumn := "logs", "body"
	var monitorID string
	if class == "transactions" {
		start = jobs.StartTransactionRetention
		countQuery = "SELECT (SELECT count(*) FROM events)+(SELECT count(*) FROM spans)"
		failureTable, valueColumn = "spans", "name"
	} else if class == "events" {
		start = jobs.StartEventRetention
		countQuery = "SELECT count(*) FROM events"
		failureTable, valueColumn = "events", "title"
	} else if class == "uptime" {
		start = jobs.StartUptimeRetention
		countQuery = "SELECT count(*) FROM monitor_checks"
		failureTable, valueColumn = "monitor_checks", "reason"
		c := uptime.Defaults()
		c.Name, c.Type, c.Enabled = "Paused retention backlog", uptime.Heartbeat, false
		m, err := f.db.CreateMonitor(f.ctx, f.owner.ID, f.org.Slug, c)
		if err != nil {
			t.Fatal(err)
		}
		monitorID = m.ID
	} else if class == "files" {
		start = jobs.StartFileRetention
		countQuery = "SELECT (SELECT count(*) FROM source_maps)+(SELECT count(*) FROM attachments)+(SELECT count(*) FROM envelope_items)"
		failureTable, valueColumn = "envelope_items", "item_type"
	} else if class == "releases" {
		start = jobs.StartReleaseRetention
		countQuery = "SELECT count(*) FROM releases"
		failureTable, valueColumn = "releases", "version"
	} else if class == "pending_reports" {
		start = func(ctx context.Context, db *store.Store, _ int, interval time.Duration) func() {
			return jobs.StartPendingUserReports(ctx, db, interval)
		}
		countQuery = "SELECT count(*) FROM user_reports"
		failureTable, valueColumn = "user_reports", "comments"
	}
	seed := func(prefix string, count int) {
		t.Helper()
		if class == "pending_reports" {
			if _, err := f.admin.Exec(f.ctx, `INSERT INTO user_reports(project_id,event_id,name,email,comments,created_at)
 SELECT $1::bigint,md5($2||n::text),'','',$2,clock_timestamp()-interval '2 hours'
 FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			return
		}
		if class == "releases" {
			if _, err := f.admin.Exec(f.ctx, `WITH inserted AS (
 INSERT INTO releases(organization_id,version,created_at)
 SELECT $1::bigint,CASE WHEN $3::integer=1 THEN $2 ELSE $2||n END,clock_timestamp()-interval '2 days'
 FROM generate_series(1,$3::integer) n RETURNING id
) INSERT INTO release_projects(release_id,project_id) SELECT id,$4::bigint FROM inserted`, f.org.ID, prefix, count, project.ID); err != nil {
				t.Fatal(err)
			}
			return
		}
		if class == "files" {
			// A running worker must observe the complete three-table backlog,
			// including the later row that establishes atomic retry behavior.
			tx, err := f.admin.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err := tx.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data) VALUES($1,$2,'error','Keep file worker parent','error',now(),'{}')`, project.ID, prefix+"-parent"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(f.ctx, `INSERT INTO source_maps(project_id,name,debug_id,checksum,size,data,created_at)
 SELECT $1,$2||n,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bulk',1,'x',now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(f.ctx, `INSERT INTO attachments(id,project_id,event_id,filename,content_type,data,created_at)
 SELECT $2||n,$1,$2||'-parent',$2,'application/octet-stream','x',now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(f.ctx, `INSERT INTO envelope_items(id,project_id,event_id,item_type,header,data,created_at)
 SELECT $2||n,$1,'',$2,'{}','x',now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			return
		}
		if class == "uptime" {
			if _, err := f.admin.Exec(f.ctx, `INSERT INTO monitor_checks(monitor_id,started_at,completed_at,up,status,previous_status,is_change,reason)
 SELECT $1,now()-interval '2 days',now()-interval '2 days',true,'up','up',false,$2 FROM generate_series(1,$3::integer)`, monitorID, prefix, count); err != nil {
				t.Fatal(err)
			}
			return
		}
		if class == "transactions" {
			// Publish the existing backlog together. A running worker may clean
			// between two independent commits before the late-failure row exists.
			tx, err := f.admin.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err := tx.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data,created_at)
 SELECT $1,$2||n,'transaction',$2,'info',now(),'{}',now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(f.ctx, `INSERT INTO spans(project_id,trace_id,span_id,name,status,is_segment,start_timestamp,end_timestamp,duration,data,created_at)
 SELECT $1,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$2||n,$2,'ok',true,now(),now(),0,'{}',now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			return
		}
		if class == "events" {
			if _, err := f.admin.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data,created_at)
 SELECT $1,$2||n,'error',$2,'error',now(),'{}',now()-interval '2 days' FROM generate_series(1,$3::integer) n`, project.ID, prefix, count); err != nil {
				t.Fatal(err)
			}
			return
		}
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
			if err := f.admin.QueryRow(f.ctx, countQuery).Scan(&count); err != nil {
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
	stop := start(f.ctx, f.db, 1, time.Hour)
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
	first := start(parent, f.db, 1, 20*time.Millisecond)
	defer first()
	second := start(parent, other, 1, 20*time.Millisecond)
	defer second()
	waitEmpty()
	seed("after-idle", 2)
	waitEmpty()
	if _, err := f.admin.Exec(f.ctx, `CREATE SEQUENCE retention_fail_attempt;
 CREATE FUNCTION fail_worker_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('retention_fail_attempt'); RAISE EXCEPTION 'declared retention failure'; END $$;
 CREATE TRIGGER fail_worker_cleanup BEFORE DELETE ON `+failureTable+` FOR EACH ROW EXECUTE FUNCTION fail_worker_cleanup()`); err != nil {
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
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM "+failureTable+" WHERE "+valueColumn+"='retry'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failure lost row count=%d err=%v", count, err)
	}
	if class == "transactions" {
		if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM events WHERE title='retry'").Scan(&count); err != nil || count != 1 {
			t.Fatalf("late failure lost transaction receipt: %d %v", count, err)
		}
	} else if class == "files" {
		if err := f.admin.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM source_maps WHERE name='retry1')+(SELECT count(*) FROM attachments WHERE filename='retry')").Scan(&count); err != nil || count != 2 {
			t.Fatalf("late failure lost earlier file receipts: %d %v", count, err)
		}
	}
	if _, err := f.admin.Exec(f.ctx, "DROP TRIGGER fail_worker_cleanup ON "+failureTable); err != nil {
		t.Fatal(err)
	}
	waitEmpty()
	cancel()
	first()
	second()
	seed("stopped", 1)
	time.Sleep(60 * time.Millisecond)
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM "+failureTable+" WHERE "+valueColumn+"='stopped'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("stopped worker changed data: %d %v", count, err)
	}
}
