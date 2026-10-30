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

// A deferred database failure occurs after the update and queue deletion have
// been issued. Neither can commit; reopening the store must retry the work.
func TestNativeRemoteReprocessingDatabaseFailureAndReopen(t *testing.T) {
	const buildID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	object, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var base, function uint64
	for _, p := range file.Progs {
		if p.Type == elf.PT_LOAD && p.Filesz > 0 && (base == 0 || p.Vaddr < base) {
			base = p.Vaddr
		}
	}
	syms, err := file.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range syms {
		if symbol.Name == "fixture_function" {
			function = symbol.Value
		}
	}
	if base == 0 || function < base {
		t.Fatal("missing fixture function")
	}
	var requests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/buildid/"+buildID+"/debuginfo" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		_, _ = w.Write(object)
	}))
	defer remote.Close()
	f := newResolutionHTTPFixture(t)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	data := fmt.Sprintf(`{"platform":"native","debug_meta":{"images":[{"type":"elf","code_file":"/app/fixture","code_id":"%s","debug_id":"%s","image_addr":"0x%x","image_size":1048576}]},"stacktrace":{"frames":[{"instruction_addr":"0x%x","package":"/app/fixture"}]}}`, buildID, buildID, base, function+11)
	var eventID int64
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,created_at,data) VALUES($1,'remote-recovery','error','remote','error',now(),now(),$2::jsonb) RETURNING id`, f.project.ID, data).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE id=$1`, eventID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `CREATE SEQUENCE remote_failure_observed;
CREATE FUNCTION fail_remote_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('remote_failure_observed'); RAISE EXCEPTION 'injected commit failure'; END $$;
CREATE CONSTRAINT TRIGGER fail_remote_commit AFTER UPDATE ON events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_remote_commit()`); err != nil {
		t.Fatal(err)
	}
	if err := f.db.EnqueueNativeReprocessing(f.ctx, access, f.project.ID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DebugInfodURLs: []string{remote.URL}, DebugInfodAllowPrivate: true}
	stop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour, cfg)
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		if err := f.sql.QueryRow(f.ctx, `SELECT is_called FROM remote_failure_observed`).Scan(&observed); err != nil {
			t.Fatal(err)
		}
		if observed {
			break
		}
		runtime.Gosched()
	}
	if !observed {
		t.Fatalf("deferred database failure did not run; remote requests=%d", requests.Load())
	}
	// The blocking lock confirms the failed transaction has released the queue.
	lockCtx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	tx, err := f.sql.Begin(lockCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var cursor int64
	if err := tx.QueryRow(lockCtx, `SELECT cursor_event_id FROM project_native_reprocessing WHERE project_id=$1::bigint FOR UPDATE`, f.project.ID).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	stop()
	var after []byte
	if err := tx.QueryRow(lockCtx, `SELECT data FROM events WHERE id=$1`, eventID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if cursor != 0 || !bytes.Equal(before, after) {
		t.Fatalf("failed commit persisted cursor=%d or event changes=%t", cursor, !bytes.Equal(before, after))
	}
	if err := tx.Rollback(lockCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `DROP TRIGGER fail_remote_commit ON events; DROP FUNCTION fail_remote_commit(); DROP SEQUENCE remote_failure_observed`); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one remote request before failure, got %d", requests.Load())
	}
	url := f.sql.Config().ConnString()
	f.db.Close()
	reopened, err := store.Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resume := jobs.StartNativeReprocessing(f.ctx, reopened, time.Hour, cfg)
	defer resume()
	deadline = time.Now().Add(10 * time.Second)
	finished := false
	for time.Now().Before(deadline) {
		if err := f.sql.QueryRow(f.ctx, `SELECT NOT EXISTS(SELECT 1 FROM project_native_reprocessing WHERE project_id=$1::bigint)`, f.project.ID).Scan(&finished); err != nil {
			t.Fatal(err)
		}
		if finished {
			break
		}
		runtime.Gosched()
	}
	if !finished {
		t.Fatal("queue did not finish after reopening the store")
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE id=$1`, eventID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || !strings.Contains(string(after), `"fixture_function"`) {
		t.Fatalf("retry requests=%d event=%s", requests.Load(), after)
	}
}
