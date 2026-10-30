//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

// This boundary verifies destination policy and local precedence with the real
// capture endpoint. Parser tests cannot establish whether a request is sent.
func TestSymStoreCapturePolicy(t *testing.T) {
	const debugID = "5A9832E5287241C1838ED98914E9B7FF1"
	pdb := nativepdb.WithSourceLines(false).Data
	for _, test := range []struct {
		name, mode string
		wantSource int32
		wantSymbol bool
	}{
		{"source omitted", "disabled", 0, false},
		{"private destination disabled", "private", 0, false},
		{"redirect not followed", "redirect", 1, false},
		{"project symbols take precedence", "local", 0, true},
		{"RSDS without debug filename", "no-name", 0, false},
		{"NB10 is not queried", "nb10", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sourceCalls, targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				_, _ = w.Write(pdb)
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls.Add(1)
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/symbols/app.pdb/"+debugID+"/app.pdb" || r.URL.RawQuery != "" {
					t.Errorf("unexpected SymStore request %s %s", r.Method, r.URL.String())
					http.NotFound(w, r)
					return
				}
				if test.mode == "redirect" {
					http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
					return
				}
				_, _ = w.Write(pdb)
			}))
			defer source.Close()
			serverURL := source.URL + "/symbols"
			if test.mode == "disabled" {
				serverURL = ""
			}
			allowPrivate := "true"
			if test.mode == "private" {
				allowPrivate = "false"
			}
			t.Setenv("GOODSENTRY_SYMSTORE_URLS", serverURL)
			t.Setenv("GOODSENTRY_SYMSTORE_ALLOW_PRIVATE_IPS", allowPrivate)
			t.Setenv("GOODSENTRY_DEBUGINFOD_URLS", "")
			t.Setenv("SECRET_KEY", strings.Repeat("r", 64))
			loaded, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			f := newResolutionHTTPFixtureWithConfig(t, func(cfg *config.Config) {
				cfg.SymStoreURLs = loaded.SymStoreURLs
				cfg.SymStoreAllowPrivate = loaded.SymStoreAllowPrivate
			})
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			if test.mode == "local" {
				digest := sha1.Sum(pdb)
				if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "app.pdb", "pdb", debugID, "x86_64", "app.pdb", hex.EncodeToString(digest[:]), nil, nil, pdb); err != nil {
					t.Fatal(err)
				}
			}
			dump := buildNativeModuleMinidump(t, `C:\app\app.exe`, debugID, 0x1015)
			modules := nativeTestMinidumpStreamRVA(dump, 4)
			oldCV := binary.LittleEndian.Uint32(dump[modules+4+80:])
			cv := bytes.Clone(dump[oldCV : oldCV+24])
			if test.mode == "nb10" {
				cv = make([]byte, 16)
				copy(cv, "NB10")
				binary.LittleEndian.PutUint32(cv[8:], 0x12345678)
				binary.LittleEndian.PutUint32(cv[12:], 1)
				cv = append(cv, []byte(`C:\private\app.pdb`+"\x00")...)
			} else if test.mode != "no-name" {
				cv = append(cv, []byte(`C:\private\app.pdb`+"\x00")...)
			}
			binary.LittleEndian.PutUint32(dump[modules+4+76:], uint32(len(cv)))
			binary.LittleEndian.PutUint32(dump[modules+4+80:], uint32(len(dump)))
			body, contentType := minidumpForm(t, append(dump, cv...), nil)
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
				t.Fatalf("source requests=%d want=%d redirect target=%d", sourceCalls.Load(), test.wantSource, targetCalls.Load())
			}
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
				if test.wantSymbol {
					if frame.Function != "CrashFunction" || frame.Filename != `C:\src\crash.cc` || frame.Line != 42 {
						t.Fatalf("project PDB did not symbolicate: %+v", frame)
					}
				} else if frame.Function != "" || frame.Filename != "" || frame.Line != 0 {
					t.Fatalf("unexpected symbolication: %+v", frame)
				}
				found = true
			}
			if !found {
				t.Fatal("accepted native event not persisted")
			}
		})
	}
}
