//go:build integration

package server

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/native"
	"goodsentry/internal/store"
)

// Unlike capture success, these cases exercise the configured destination
// policy and remote response admission at the actual ingestion boundary.
func TestDebuginfodCapturePolicy(t *testing.T) {
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
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Filesz > 0 && (base == 0 || program.Vaddr < base) {
			base = program.Vaddr
		}
	}
	symbols, err := file.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range symbols {
		if symbol.Name == "fixture_function" {
			function = symbol.Value
		}
	}
	if base == 0 || function < base {
		t.Fatal("ELF fixture lacks executable base or function")
	}
	id, err := hex.DecodeString(buildID)
	if err != nil {
		t.Fatal(err)
	}
	missingID := bytes.Clone(object)
	index := bytes.Index(missingID, id)
	if index < 4 || !bytes.Equal(missingID[index-4:index], []byte{'G', 'N', 'U', 0}) {
		t.Fatal("fixture lacks the expected GNU build-id note")
	}
	copy(missingID[index-4:index], "BAD\x00")
	if native.ObjectDebugID(missingID) != "" {
		t.Fatal("fixture still has an ELF build ID")
	}

	for _, test := range []struct {
		name, mode string
		wantSource int32
	}{
		{"source omitted", "disabled", 0},
		{"private destinations disabled", "private", 0},
		{"redirect is not followed", "redirect", 1},
		{"ELF without build ID is rejected", "missing-id", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sourceCalls, targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				_, _ = w.Write(object)
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/buildid/"+buildID+"/debuginfo" {
					t.Errorf("unexpected debuginfod request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if test.mode == "redirect" {
					http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
					return
				}
				_, _ = w.Write(missingID)
			}))
			defer source.Close()
			serverURL := source.URL
			if test.mode == "disabled" {
				serverURL = ""
			}
			allowPrivate := "true"
			if test.mode == "private" {
				allowPrivate = "false"
			}
			t.Setenv("GOODSENTRY_DEBUGINFOD_URLS", serverURL)
			t.Setenv("GOODSENTRY_DEBUGINFOD_ALLOW_PRIVATE_IPS", allowPrivate)
			t.Setenv("SECRET_KEY", strings.Repeat("r", 64))
			loaded, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			f := newResolutionHTTPFixtureWithConfig(t, func(cfg *config.Config) {
				cfg.DebugInfodURLs = loaded.DebugInfodURLs
				cfg.DebugInfodAllowPrivate = loaded.DebugInfodAllowPrivate
			})
			dump := buildNativeModuleMinidump(t, "/app/fixture", "5A9832E5287241C1838ED98914E9B7FF1", function-base+0xb)
			cv := append([]byte{0x4c, 0x45, 0x70, 0x42}, id...)
			modules := nativeTestMinidumpStreamRVA(dump, 4)
			binary.LittleEndian.PutUint32(dump[modules+4+76:], uint32(len(cv)))
			binary.LittleEndian.PutUint32(dump[modules+4+80:], uint32(len(dump)))
			dump = append(dump, cv...)
			body, contentType := minidumpForm(t, dump, nil)
			url := f.server.URL + "/good/api/" + f.project.ID + "/minidump/?sentry_version=7&sentry_key=" + f.project.PublicKey
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, url, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var accepted struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &accepted); err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
				t.Fatalf("capture status=%d body=%s err=%v", res.StatusCode, raw, err)
			}
			if sourceCalls.Load() != test.wantSource || targetCalls.Load() != 0 {
				t.Fatalf("source requests=%d want=%d redirect target requests=%d", sourceCalls.Load(), test.wantSource, targetCalls.Load())
			}
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range events {
				var event struct {
					ID        string `json:"event_id"`
					Exception struct {
						Values []struct {
							Stacktrace struct {
								Frames []nativeFrame `json:"frames"`
							} `json:"stacktrace"`
						} `json:"values"`
					} `json:"exception"`
				}
				if err := json.Unmarshal(item.Data, &event); err != nil {
					t.Fatal(err)
				}
				if event.ID != accepted.ID {
					continue
				}
				if len(event.Exception.Values) == 0 || len(event.Exception.Values[0].Stacktrace.Frames) == 0 {
					t.Fatal("native event has no frame")
				}
				frame := event.Exception.Values[0].Stacktrace.Frames[0]
				if frame.Function != "" || frame.Filename != "" || frame.Line != 0 {
					t.Fatalf("untrusted symbols resolved frame: %+v", frame)
				}
				found = true
			}
			if !found {
				t.Fatal("accepted native event not persisted")
			}
		})
	}
}
