//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

// Separate events in one durable batch must share the same remote request budget.
func TestNativeRemoteReprocessingBatchBudgetAndLocalPriority(t *testing.T) {
	const elfID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	const pdbID = "5A9832E5287241C1838ED98914E9B7FF1"
	elfBytes, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	object, err := elf.NewFile(bytes.NewReader(elfBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	var base, function uint64
	for _, p := range object.Progs {
		if p.Type == elf.PT_LOAD && p.Filesz > 0 && (base == 0 || p.Vaddr < base) {
			base = p.Vaddr
		}
	}
	symbols, err := object.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range symbols {
		if s.Name == "fixture_function" {
			function = s.Value
		}
	}
	if base == 0 || function < base {
		t.Fatal("missing ELF fixture function")
	}
	var mu sync.Mutex
	counts := [2]int{}
	localRequested := false
	linux := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[0]++
		if strings.Contains(r.URL.Path, elfID) {
			localRequested = true
		}
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer linux.Close()
	pdb := nativepdb.WithSourceLines(false).Data
	windows := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[1]++
		mu.Unlock()
		if r.URL.Path == "/symbols/app.pdb/"+pdbID+"/app.pdb" {
			_, _ = w.Write(pdb)
			return
		}
		http.NotFound(w, r)
	}))
	defer windows.Close()
	f := newResolutionHTTPFixture(t)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	digest := sha1.Sum(elfBytes)
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "local.elf", "elf", elfID, "x86_64", "fixture", hex.EncodeToString(digest[:]), nil, nil, elfBytes); err != nil {
		t.Fatal(err)
	}
	makeImage := func(i int) map[string]any {
		image := map[string]any{"code_file": fmt.Sprintf("/app/module%d", i), "image_addr": fmt.Sprintf("0x%x", 0x1000000+i*0x10000), "image_size": 65536}
		if i%2 == 0 {
			image["type"] = "elf"
			image["code_id"] = fmt.Sprintf("%040x", i+1)
		} else {
			image["type"] = "pe"
			image["debug_id"] = fmt.Sprintf("%032X1", i+1)
			image["debug_file"] = "app.pdb"
		}
		return image
	}
	first := []map[string]any{
		{"type": "elf", "code_file": "/app/fixture", "code_id": elfID, "debug_id": elfID, "image_addr": fmt.Sprintf("0x%x", base), "image_size": 1048576},
		{"type": "pe", "code_file": "app.exe", "debug_id": pdbID, "debug_file": "app.pdb", "image_addr": "0x20000000", "image_size": 65536},
	}
	for i := 0; i < 15; i++ {
		first = append(first, makeImage(i))
	}
	second := []map[string]any{}
	for i := 15; i < 30; i++ {
		second = append(second, makeImage(i))
	}
	insert := func(id string, images []map[string]any, frames []map[string]any) {
		t.Helper()
		data, err := json.Marshal(map[string]any{"platform": "native", "debug_meta": map[string]any{"images": images}, "stacktrace": map[string]any{"frames": frames}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.sql.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,created_at,data) VALUES($1,$2,'error','remote budget','error',now(),now(),$3::jsonb)`, f.project.ID, id, data); err != nil {
			t.Fatal(err)
		}
	}
	insert("batch-budget-first", first, []map[string]any{{"instruction_addr": fmt.Sprintf("0x%x", function+11), "package": "/app/fixture"}, {"instruction_addr": "0x20001015", "package": "app.exe"}})
	insert("batch-budget-second", second, []map[string]any{})
	if err := f.db.EnqueueNativeReprocessing(f.ctx, access, f.project.ID); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("durable queue rows=%d err=%v", queued, err)
	}
	cfg := config.Config{DebugInfodURLs: []string{linux.URL}, DebugInfodAllowPrivate: true, SymStoreURLs: []string{windows.URL + "/symbols"}, SymStoreAllowPrivate: true}
	stop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour, cfg)
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if queued == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if queued != 0 {
		t.Fatal("reprocessing queue did not drain")
	}
	mu.Lock()
	got, requestedLocal := counts, localRequested
	mu.Unlock()
	if got[0] == 0 || got[1] == 0 || got[0]+got[1] != 25 || requestedLocal {
		t.Fatalf("requests linux/windows=%v local requested=%v; want both protocols, 25 total, no local lookup", got, requestedLocal)
	}
	var result []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id='batch-budget-first'`, f.project.ID).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), `"fixture_function"`) || !strings.Contains(string(result), `"CrashFunction"`) {
		t.Fatalf("expected local ELF and remote PDB symbols: %s", result)
	}
}
