//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// The capture HTTP boundary owns authentication, retrieval and the persisted
// symbolicated event. A real loopback debuginfod endpoint covers network wiring;
// local debug-file tests do not exercise outbound admission or ELF identity.
func TestDebuginfodMinidumpCapture(t *testing.T) {
	const buildID = "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"
	elfBytes, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(elfBytes))
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
		t.Fatal("ELF fixture lacks its executable base or fixture_function")
	}
	idBytes, err := hex.DecodeString(buildID)
	if err != nil {
		t.Fatal(err)
	}
	wrongELF := bytes.Clone(elfBytes)
	index := bytes.Index(wrongELF, idBytes)
	if index < 0 || bytes.Index(wrongELF[index+len(idBytes):], idBytes) >= 0 {
		t.Fatal("fixture build ID is absent or ambiguous")
	}
	wrongELF[index] ^= 1

	for _, test := range []struct {
		name, route                            string
		wrongIdentity, invalidKey, localSymbol bool
	}{
		{name: "multipart retrieves matching ELF", route: "minidump"},
		{name: "envelope retrieves matching ELF", route: "envelope"},
		{name: "wrong identity leaves frame unresolved", route: "minidump", wrongIdentity: true},
		{name: "invalid SDK key causes no outbound request", route: "minidump", invalidKey: true},
		{name: "project symbols take precedence", route: "minidump", localSymbol: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			var unexpectedPath atomic.Bool
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/buildid/"+buildID+"/debuginfo" {
					unexpectedPath.Store(true)
					http.NotFound(w, r)
					return
				}
				if test.wrongIdentity {
					_, _ = w.Write(wrongELF)
				} else {
					_, _ = w.Write(elfBytes)
				}
			}))
			defer endpoint.Close()
			t.Setenv("GOODSENTRY_DEBUGINFOD_URLS", endpoint.URL)
			t.Setenv("GOODSENTRY_DEBUGINFOD_ALLOW_PRIVATE_IPS", "true")
			t.Setenv("SECRET_KEY", strings.Repeat("r", 64))
			loaded, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			f := newResolutionHTTPFixtureWithConfig(t, func(cfg *config.Config) {
				cfg.DebugInfodURLs = loaded.DebugInfodURLs
				cfg.DebugInfodAllowPrivate = loaded.DebugInfodAllowPrivate
			})
			if test.localSymbol {
				digest := sha1.Sum(elfBytes)
				access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, Role: "owner", AllProjects: true}
				if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "fixture.elf", "elf", buildID, "x86_64", "fixture.elf", hex.EncodeToString(digest[:]), nil, nil, elfBytes); err != nil {
					t.Fatal(err)
				}
			}
			dump := buildNativeModuleMinidump(t, "/app/fixture", "5A9832E5287241C1838ED98914E9B7FF1", function-base+0xb)
			cv := append([]byte{0x4c, 0x45, 0x70, 0x42}, idBytes...)
			modules := nativeTestMinidumpStreamRVA(dump, 4)
			binary.LittleEndian.PutUint32(dump[modules+4+76:], uint32(len(cv)))
			binary.LittleEndian.PutUint32(dump[modules+4+80:], uint32(len(dump)))
			dump = append(dump, cv...)
			key := f.project.PublicKey
			if test.invalidKey {
				key = "wrong"
			}
			var body io.Reader
			contentType := "application/x-sentry-envelope"
			if test.route == "minidump" {
				form, formType := minidumpForm(t, dump, nil)
				body, contentType = form, formType
			} else {
				const eventID = "cafecafecafecafecafecafecafecafe"
				event := []byte(`{"event_id":"` + eventID + `","platform":"native","message":"native crash"}`)
				var envelope bytes.Buffer
				fmt.Fprintf(&envelope, `{"event_id":"%s"}`+"\n", eventID)
				fmt.Fprintf(&envelope, `{"type":"event","length":%d}`+"\n", len(event))
				envelope.Write(event)
				envelope.WriteByte('\n')
				fmt.Fprintf(&envelope, `{"type":"attachment","attachment_type":"event.minidump","filename":"crash.dmp","length":%d}`+"\n", len(dump))
				envelope.Write(dump)
				envelope.WriteByte('\n')
				body = &envelope
			}
			url := f.server.URL + "/good/api/" + f.project.ID + "/" + test.route + "/?sentry_version=7&sentry_key=" + key
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, url, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if test.invalidKey {
				if res.StatusCode < 400 || requests.Load() != 0 {
					t.Fatalf("invalid key status=%d outbound=%d", res.StatusCode, requests.Load())
				}
				return
			}
			if res.StatusCode != http.StatusOK {
				t.Fatalf("capture status=%d body=%s", res.StatusCode, response)
			}
			wantRequests := int32(1)
			if test.localSymbol {
				wantRequests = 0
			}
			if requests.Load() != wantRequests || unexpectedPath.Load() {
				t.Fatalf("debuginfod requests=%d wrong-path=%t", requests.Load(), unexpectedPath.Load())
			}
			var accepted struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(response, &accepted); err != nil || accepted.ID == "" {
				t.Fatalf("capture response=%s err=%v", response, err)
			}
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			var frame nativeFrame
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
				if event.ID == accepted.ID && len(event.Exception.Values) > 0 && len(event.Exception.Values[0].Stacktrace.Frames) > 0 {
					frame, found = event.Exception.Values[0].Stacktrace.Frames[0], true
				}
			}
			if !found {
				t.Fatal("accepted native event and frame were not persisted")
			}
			if test.wrongIdentity {
				if frame.Function != "" || frame.Filename != "" || frame.Line != 0 {
					t.Fatalf("mismatched ELF unexpectedly symbolicated: %+v", frame)
				}
			} else if frame.Function != "fixture_function" || !strings.HasSuffix(frame.Filename, "elf-dwarf-source.c") || frame.Line != 3 {
				t.Fatalf("matching ELF did not symbolicate: %+v", frame)
			}
			files, err := f.db.DebugFiles(f.ctx, access, f.project.ID, 0, 10)
			wantFiles := 0
			if test.localSymbol {
				wantFiles = 1
			}
			if err != nil || len(files) != wantFiles {
				t.Fatalf("remote symbols must not be persisted: files=%d err=%v", len(files), err)
			}
		})
	}
}
