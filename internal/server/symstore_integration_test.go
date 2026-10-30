//go:build integration

package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
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

// Capture owns authorization, remote URL construction, identity validation and
// the stored event. Local debug-file tests cannot cover this network boundary.
func TestSymStoreRSDSMinidumpCapture(t *testing.T) {
	const debugID = "5A9832E5287241C1838ED98914E9B7FF1"
	const name = "App?#.pdb"
	fixture := nativepdb.WithSourceLines(false)
	wrongPDB := bytes.Clone(fixture.Data)
	wrongPDB[fixture.InfoBlock*512+12] ^= 1

	for _, test := range []struct {
		name, route               string
		wrongIdentity, invalidKey bool
	}{
		{"multipart matching PDB", "minidump", false, false},
		{"envelope matching PDB", "envelope", false, false},
		{"different PDB identity", "minidump", true, false},
		{"invalid key does not contact server", "minidump", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			var wrongPath atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/symbols/App%3F%23.pdb/"+debugID+"/App%3F%23.pdb" || r.URL.RawQuery != "" {
					wrongPath.Store(true)
					http.NotFound(w, r)
					return
				}
				if test.wrongIdentity {
					_, _ = w.Write(wrongPDB)
				} else {
					_, _ = w.Write(fixture.Data)
				}
			}))
			defer server.Close()
			t.Setenv("GOODSENTRY_SYMSTORE_URLS", server.URL+"/symbols")
			t.Setenv("GOODSENTRY_SYMSTORE_ALLOW_PRIVATE_IPS", "true")
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
			dump := buildNativeModuleMinidump(t, `C:\app\app.exe`, debugID, 0x1015)
			modules := nativeTestMinidumpStreamRVA(dump, 4)
			oldCV := binary.LittleEndian.Uint32(dump[modules+4+80:])
			cv := append(bytes.Clone(dump[oldCV:oldCV+24]), []byte(`C:\private\`+name+"\x00")...)
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
				const eventID = "a1b1c1d1e1f1a1b1c1d1e1f1a1b1c1d1"
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
			raw, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if test.invalidKey {
				if res.StatusCode < 400 || requests.Load() != 0 {
					t.Fatalf("invalid key status=%d outbound=%d", res.StatusCode, requests.Load())
				}
				return
			}
			var accepted struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &accepted); err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
				t.Fatalf("capture status=%d body=%s err=%v", res.StatusCode, raw, err)
			}
			if requests.Load() != 1 || wrongPath.Load() {
				t.Fatalf("SymStore requests=%d wrong-path=%t", requests.Load(), wrongPath.Load())
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
				if test.wrongIdentity {
					if frame.Function != "" || frame.Filename != "" || frame.Line != 0 {
						t.Fatalf("mismatched PDB unexpectedly symbolicated: %+v", frame)
					}
				} else if frame.Function != "CrashFunction" || frame.Filename != `C:\src\crash.cc` || frame.Line != 42 {
					t.Fatalf("matching PDB did not symbolicate: %+v", frame)
				}
				found = true
			}
			if !found {
				t.Fatal("accepted native event not persisted")
			}
			files, err := f.db.DebugFiles(f.ctx, access, f.project.ID, 0, 10)
			if err != nil || len(files) != 0 {
				t.Fatalf("remote PDB must not be persisted: files=%d err=%v", len(files), err)
			}
		})
	}
}
