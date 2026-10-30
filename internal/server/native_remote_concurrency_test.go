//go:build integration

package server

import (
	"bytes"
	"context"
	"debug/elf"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
)

// This exercises queue and event locks through a real remote lookup and real
// PostgreSQL transactions, including the rollback and subsequent retry.
func TestNativeRemoteReprocessingConcurrencyAndCancellation(t *testing.T) {
	const buildID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	object, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	elfFile, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var base, function uint64
	for _, p := range elfFile.Progs {
		if p.Type == elf.PT_LOAD && p.Filesz > 0 && (base == 0 || p.Vaddr < base) {
			base = p.Vaddr
		}
	}
	syms, err := elfFile.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, sym := range syms {
		if sym.Name == "fixture_function" {
			function = sym.Value
		}
	}
	if base == 0 || function < base {
		t.Fatal("missing fixture function")
	}

	entered := make(chan struct{})
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/buildid/"+buildID+"/debuginfo" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(object)
	}))
	defer remote.Close()
	f := newResolutionHTTPFixture(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other remote reprocessing", "native")
	if err != nil {
		t.Fatal(err)
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	data := fmt.Sprintf(`{"platform":"native","debug_meta":{"images":[{"type":"elf","code_file":"/app/fixture","code_id":"%s","debug_id":"%s","image_addr":"0x%x","image_size":1048576}]},"stacktrace":{"frames":[{"instruction_addr":"0x%x","package":"/app/fixture"}]}}`, buildID, buildID, base, function+11)
	var eventID int64
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,created_at,data) VALUES($1,'remote-concurrency','error','remote','error',now(),now(),$2::jsonb) RETURNING id`, f.project.ID, data).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,created_at,data) VALUES($1,'remote-other','error','remote','error',now(),now(),'{"platform":"native"}')`, other.ID); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE id=$1`, eventID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := f.db.EnqueueNativeReprocessing(f.ctx, access, f.project.ID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DebugInfodURLs: []string{remote.URL}, DebugInfodAllowPrivate: true}
	workerCtx, cancel := context.WithCancel(f.ctx)
	stop := jobs.StartNativeReprocessing(workerCtx, f.db, time.Hour, cfg)
	defer stop()
	defer cancel()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not start remote lookup")
	}

	if err := f.db.EnqueueNativeReprocessing(f.ctx, access, other.ID); err != nil {
		t.Fatal(err)
	}
	secondStop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour, cfg)
	defer secondStop()
	wait := func(label string, query string, args ...any) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var ready bool
			if err := f.sql.QueryRow(f.ctx, query, args...).Scan(&ready); err != nil {
				t.Fatal(err)
			}
			if ready {
				return
			}
			runtime.Gosched()
		}
		t.Fatalf("timed out waiting for %s", label)
	}
	wait("other project to progress", `SELECT NOT EXISTS(SELECT 1 FROM project_native_reprocessing WHERE project_id=$1::bigint)`, other.ID)
	// Do not let the second worker claim the first project's queue after cancellation.
	secondStop()
	reenqueueDone := make(chan error, 1)
	reenqueueCtx, cancelReenqueue := context.WithCancel(f.ctx)
	defer cancelReenqueue()
	go func() { reenqueueDone <- f.db.EnqueueNativeReprocessing(reenqueueCtx, access, f.project.ID) }()
	wait("same-project reenqueue to block on a database lock", `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%INSERT INTO project_native_reprocessing%')`)
	var cursor int64
	if err := f.sql.QueryRow(f.ctx, `SELECT cursor_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&cursor); err != nil || cursor != 0 {
		t.Fatalf("cursor before cancellation=%d err=%v", cursor, err)
	}
	cancelReenqueue()
	select {
	case err := <-reenqueueDone:
		if err == nil {
			t.Fatal("blocked reenqueue unexpectedly succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reenqueue remained blocked after cancellation")
	}
	cancel()
	stop()
	var after []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE id=$1`, eventID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("cancelled lookup changed the event")
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT cursor_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&cursor); err != nil || cursor != 0 {
		t.Fatalf("cursor after cancellation=%d err=%v", cursor, err)
	}
	resume := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour, cfg)
	defer resume()
	wait("resumed project to finish", `SELECT NOT EXISTS(SELECT 1 FROM project_native_reprocessing WHERE project_id=$1::bigint)`, f.project.ID)
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE id=$1`, eventID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"fixture_function"`) {
		t.Fatalf("resumed event has no remote symbol: %s", after)
	}
}
