//go:build integration

package server

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

func TestLegacyThunkMinidumpProjectIsolation(t *testing.T) {
	for _, other := range []bool{false, true} {
		t.Run(map[bool]string{false: "same project", true: "other project"}[other], func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			project := f.project.ID
			if other {
				p, e := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other thunk project", "native")
				if e != nil {
					t.Fatal(e)
				}
				project = p.ID
			}
			pdb := nativepdb.WithLegacyThunk().Data
			digest := sha1.Sum(pdb)
			if _, e := f.db.SaveDebugFile(f.ctx, access, project, "legacy.pdb", "pdb", "65ae75631", "x86_64", "legacy.pdb", hex.EncodeToString(digest[:]), nil, nil, pdb); e != nil {
				t.Fatal(e)
			}
			dump := buildNativeModuleMinidump(t, "C:\\test\\crash.exe", "5A9832E5287241C1838ED98914E9B7FF1", 0x1065)
			cv := make([]byte, 16+len("legacy.pdb\x00"))
			copy(cv, "NB10")
			binary.LittleEndian.PutUint32(cv[8:], 0x65ae7563)
			binary.LittleEndian.PutUint32(cv[12:], 1)
			copy(cv[16:], "legacy.pdb\x00")
			rva := nativeTestMinidumpStreamRVA(dump, 4)
			binary.LittleEndian.PutUint32(dump[rva+4+76:], uint32(len(cv)))
			binary.LittleEndian.PutUint32(dump[rva+4+80:], uint32(len(dump)))
			dump = append(dump, cv...)
			body, contentType := minidumpForm(t, dump, nil)
			req, e := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Content-Type", contentType)
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", res.StatusCode)
			}
			events, e := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
			if e != nil || len(events) == 0 {
				t.Fatalf("events=%d err=%v", len(events), e)
			}
			var payload struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []nativeFrame `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if e = json.Unmarshal(events[0].Data, &payload); e != nil {
				t.Fatal(e)
			}
			frame := payload.Exception.Values[0].Stacktrace.Frames[0]
			want := "LegacyThunk"
			if other {
				want = ""
			}
			if frame.Function != want {
				t.Fatalf("frame=%+v want=%q", frame, want)
			}
		})
	}
}
