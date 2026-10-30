//go:build integration

package server

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

// A queued event crosses the HTTP, database and worker boundaries. Capture-time
// tests cannot prove that previously stored image metadata reaches the worker.
func TestNativeReprocessingUsesRemoteSymbols(t *testing.T) {
	const elfID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	const pdbID = "5A9832E5287241C1838ED98914E9B7FF1"
	elfData, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	object, err := elf.NewFile(bytes.NewReader(elfData))
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
	syms, err := object.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range syms {
		if s.Name == "fixture_function" {
			function = s.Value
		}
	}
	if base == 0 || function < base {
		t.Fatal("ELF fixture lacks its base or function")
	}
	id, err := hex.DecodeString(elfID)
	if err != nil {
		t.Fatal(err)
	}
	wrongELF := bytes.Clone(elfData)
	index := bytes.Index(wrongELF, id)
	if index < 0 || bytes.Index(wrongELF[index+len(id):], id) >= 0 {
		t.Fatal("ambiguous ELF build ID")
	}
	wrongELF[index] ^= 1
	pdb := nativepdb.WithSourceLines(false)
	wrongPDB := bytes.Clone(pdb.Data)
	wrongPDB[pdb.InfoBlock*512+12] ^= 1

	for _, tc := range []struct {
		name, path, wantFunction, wantFile string
		data, wrong                        []byte
		linux                              bool
	}{
		{"ELF", "/buildid/" + elfID + "/debuginfo", "fixture_function", "elf-dwarf-source.c", elfData, wrongELF, true},
		{"PDB", "/symbols/app.pdb/" + pdbID + "/app.pdb", "CrashFunction", `C:\src\crash.cc`, pdb.Data, wrongPDB, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var phase, requests atomic.Int32
			var wrongPath atomic.Bool
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != tc.path || r.URL.RawQuery != "" {
					wrongPath.Store(true)
					http.NotFound(w, r)
					return
				}
				switch phase.Load() {
				case 0:
					http.NotFound(w, r)
				case 1:
					_, _ = w.Write(tc.wrong)
				default:
					_, _ = w.Write(tc.data)
				}
			}))
			defer remote.Close()
			f := newResolutionHTTPFixture(t) // capture intentionally has no remote sources
			dump := buildNativeModuleMinidump(t, `C:\app\app.exe`, pdbID, 0x1015)
			modules := nativeTestMinidumpStreamRVA(dump, 4)
			if tc.linux {
				dump = buildNativeModuleMinidump(t, "/app/fixture", pdbID, function-base+0xb)
				modules = nativeTestMinidumpStreamRVA(dump, 4)
				cv := append([]byte{0x4c, 0x45, 0x70, 0x42}, id...)
				binary.LittleEndian.PutUint32(dump[modules+4+76:], uint32(len(cv)))
				binary.LittleEndian.PutUint32(dump[modules+4+80:], uint32(len(dump)))
				dump = append(dump, cv...)
			} else {
				oldCV := binary.LittleEndian.Uint32(dump[modules+4+80:])
				cv := append(bytes.Clone(dump[oldCV:oldCV+24]), []byte("C:\\private\\app.pdb\x00")...)
				binary.LittleEndian.PutUint32(dump[modules+4+76:], uint32(len(cv)))
				binary.LittleEndian.PutUint32(dump[modules+4+80:], uint32(len(dump)))
				dump = append(dump, cv...)
			}
			body, contentType := minidumpForm(t, dump, map[string]string{"crash_context": "keep-me"})
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var accepted struct {
				ID string `json:"id"`
			}
			err = json.NewDecoder(res.Body).Decode(&accepted)
			res.Body.Close()
			if err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
				t.Fatalf("capture status=%d id=%q err=%v", res.StatusCode, accepted.ID, err)
			}
			if tc.linux {
				// Sentry's ELF debug ID is a lossy GUID transformation of the
				// full build ID retained separately in code_id.
				const debugID = "78C6CA6605CD7288052EE3111F0B1F6E0"
				if _, err := f.sql.Exec(f.ctx, `UPDATE events SET data=jsonb_set(data,'{debug_meta,images,0,debug_id}',to_jsonb($3::text)) WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, accepted.ID, debugID); err != nil {
					t.Fatal(err)
				}
			}
			read := func() []byte {
				t.Helper()
				var data []byte
				if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, accepted.ID).Scan(&data); err != nil {
					t.Fatal(err)
				}
				return data
			}
			cfg := config.Config{DebugInfodURLs: []string{remote.URL}, DebugInfodAllowPrivate: true, SymStoreURLs: []string{remote.URL + "/symbols"}, SymStoreAllowPrivate: true}
			run := func() {
				t.Helper()
				path := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/reprocessing/"
				f.request(t, f.client, http.MethodPost, path, "", http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusOK)
				stop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour, cfg)
				defer stop()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					var queued int
					if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued); err != nil {
						t.Fatal(err)
					}
					if queued == 0 {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("reprocessing queue did not drain")
			}
			before := read()
			for i := int32(0); i < 2; i++ {
				phase.Store(i)
				run()
				if got := read(); !bytes.Equal(got, before) {
					t.Fatalf("phase %d changed event without matching symbols", i)
				}
			}
			phase.Store(2)
			run()
			if requests.Load() != 3 || wrongPath.Load() {
				t.Fatalf("requests=%d wrong path=%t", requests.Load(), wrongPath.Load())
			}
			after := read()
			var old, updated map[string]any
			if json.Unmarshal(before, &old) != nil || json.Unmarshal(after, &updated) != nil {
				t.Fatal("invalid event JSON")
			}
			frames := func(event map[string]any) []any {
				t.Helper()
				return event["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
			}
			oldFrames, newFrames := frames(old), frames(updated)
			if len(oldFrames) != 1 || len(newFrames) != len(oldFrames) {
				t.Fatalf("frame count before=%d after=%d", len(oldFrames), len(newFrames))
			}
			oldFrame := oldFrames[0].(map[string]any)
			if value := oldFrame["function"]; value != nil && value != "" {
				t.Fatalf("capture unexpectedly symbolicated: %v", value)
			}
			frame := newFrames[0].(map[string]any)
			wantLine := float64(42)
			if tc.linux {
				wantLine = 3
			}
			filename, _ := frame["filename"].(string)
			if frame["function"] != tc.wantFunction || !strings.HasSuffix(filename, tc.wantFile) || frame["lineno"] != wantLine {
				t.Fatalf("reprocessed frame=%+v", frame)
			}
			newFrames[0] = oldFrames[0]
			if !reflect.DeepEqual(old, updated) {
				t.Fatal("reprocessing modified unrelated event fields")
			}
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			files, err := f.db.DebugFiles(f.ctx, access, f.project.ID, 0, 10)
			if err != nil || len(files) != 0 {
				t.Fatalf("remote symbols persisted: files=%d err=%v", len(files), err)
			}
		})
	}
}
