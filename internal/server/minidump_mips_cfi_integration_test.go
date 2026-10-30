//go:build integration

package server

import (
	"crypto/sha1"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/store"
)

func TestNativeMIPSCFIPersistsCaller(t *testing.T) {
	const debugID = "5A9832E5287241C1838ED98914E9B7FF1"
	for _, tc := range []struct {
		name                         string
		missingMemory, wrongIdentity bool
		want                         []string
	}{
		{"matching table", false, false, []string{"0x40001010", "0x40001120"}},
		{"missing captured stack", true, false, []string{"0x40001010"}},
		{"different identity and name", false, true, []string{"0x40001010"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			// CFA=SP+16 and r31=*(CFA-16), the independently observed Go O32 rule.
			section := thumbHTTPBuildArchDebugFrame(4, 31, 29, 16, 0x70, 0x1000, 0x100)
			object := thumbHTTPBuildELF32DebugFrame(elf.EM_MIPS, section)
			name, id := "test_app.pdb", debugID
			if tc.wrongIdentity {
				name, id = "other.pdb", "5A9832E5287241C1838ED98914E9B7FF2"
			}
			hash := sha1.Sum(object)
			if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, name, "elf", id, "mips", name, hex.EncodeToString(hash[:]), nil, nil, object); err != nil {
				t.Fatal(err)
			}
			dump := buildNativeModuleMinidump(t, "test_app.pdb", debugID, 0x1010)
			system, thread := nativeTestMinidumpStreamRVA(dump, 7), nativeTestMinidumpStreamRVA(dump, 3)
			module, exception := nativeTestMinidumpStreamRVA(dump, 4), nativeTestMinidumpStreamRVA(dump, 6)
			stack := binary.LittleEndian.Uint32(dump[thread+40:])
			binary.LittleEndian.PutUint16(dump[system:], 1)
			binary.LittleEndian.PutUint64(dump[module+4:], 0x40000000)
			binary.LittleEndian.PutUint64(dump[exception+24:], 0x40001010)
			binary.LittleEndian.PutUint32(dump[stack:], 0x40001120)
			if tc.missingMemory {
				binary.LittleEndian.PutUint32(dump[thread+36:], 0)
			}
			context := make([]byte, 600)
			binary.LittleEndian.PutUint32(context, 0x4000e)
			binary.LittleEndian.PutUint64(context[8+29*8:], 0x200000)
			binary.LittleEndian.PutUint64(context[312:], 0x40001010)
			contextRVA := uint32(len(dump))
			dump = append(dump, context...)
			binary.LittleEndian.PutUint32(dump[thread+44:], uint32(len(context)))
			binary.LittleEndian.PutUint32(dump[thread+48:], contextRVA)
			body, contentType := minidumpForm(t, dump, nil)
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_key="+f.project.PublicKey, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var ack struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(res.Body).Decode(&ack); err != nil || res.StatusCode != http.StatusOK || ack.ID == "" {
				t.Fatalf("capture status=%d id=%q err=%v", res.StatusCode, ack.ID, err)
			}
			var data []byte
			if err := f.sql.QueryRow(f.ctx, "SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2", f.project.ID, ack.ID).Scan(&data); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []struct {
								Instruction string `json:"instruction_addr"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 {
				t.Fatalf("exceptions=%+v", payload.Exception.Values)
			}
			frames := payload.Exception.Values[0].Stacktrace.Frames
			if len(frames) != len(tc.want) {
				t.Fatalf("frames=%+v want=%v", frames, tc.want)
			}
			for i, want := range tc.want {
				if frames[i].Instruction != want {
					t.Fatalf("frames=%+v want=%v", frames, tc.want)
				}
			}
			var attachments, items int
			if err := f.sql.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM attachments WHERE project_id=$1::bigint AND event_id=$2), (SELECT count(*) FROM envelope_items WHERE project_id=$1::bigint AND event_id=$2)", f.project.ID, ack.ID).Scan(&attachments, &items); err != nil || attachments != 0 || items != 0 {
				t.Fatalf("dump retained: attachments=%d items=%d err=%v", attachments, items, err)
			}
		})
	}
}
