//go:build integration

package jobs_test

import (
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
)

func TestNativeReprocessingWorkerResumesAfterDatabaseReopen(t *testing.T) {
	f := newResolutionWorkerFixture(t)
	project, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Native reprocessing restart", "native")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
		SELECT $1,'native-restart-'||n,'error','native error','error',now(),
			jsonb_build_object('event_id','native-restart-'||n,'platform','native')
		FROM generate_series(1,12) AS n`, project.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.EnqueueNativeReprocessing(f.ctx, f.access(), project.ID); err != nil {
		t.Fatal(err)
	}
	processed, err := f.db.ProcessNextNativeReprocessingBatch(f.ctx, "", 0, func(_ string, _ int64, _ bool, _ []store.DebugSymbolFile, events []store.NativeReprocessingEvent) (map[int64][]byte, error) {
		if len(events) != 10 {
			t.Fatalf("first batch size=%d want=10", len(events))
		}
		return nil, nil
	})
	if err != nil || !processed {
		t.Fatalf("first batch processed=%v err=%v", processed, err)
	}
	var cursor, through int64
	if err := f.admin.QueryRow(f.ctx, `SELECT cursor_event_id,through_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint`, project.ID).Scan(&cursor, &through); err != nil || cursor == 0 || cursor >= through {
		t.Fatalf("partial cursor=%d through=%d err=%v", cursor, through, err)
	}
	databaseURL := f.url
	f.db.Close()
	reopened, err := store.Open(f.ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	f.db = reopened
	t.Cleanup(reopened.Close)
	stop := jobs.StartNativeReprocessing(f.ctx, reopened, time.Hour, config.Config{})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var queued, events int
		if err := f.admin.QueryRow(f.ctx, `SELECT
			(SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint),
			(SELECT count(*) FROM events WHERE project_id=$1::bigint)`, project.ID).Scan(&queued, &events); err != nil {
			stop()
			t.Fatal(err)
		}
		if queued == 0 && events == 12 {
			stop()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	t.Fatal("worker did not resume and drain the persisted cursor after reopen")
}
