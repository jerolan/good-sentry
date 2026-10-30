//go:build integration

package server

import (
	"crypto/sha1"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"goodsentry/internal/store"
)

func TestNativeARM32InterruptedThumbPersistsBoundedCaller(t *testing.T) {
	for _, tc := range []struct {
		name          string
		savedPC, cpsr uint32
		want          []string
	}{
		{"Thumb PC exact and one caller", 0x401102, 0x30, []string{"0x6001000", "0x401102", "0x401220"}},
		{"odd Thumb PC rejected", 0x401103, 0x30, []string{"0x6001000", "0x40001050"}},
		{"Jazelle CPSR rejected", 0x401102, 0x01000030, []string{"0x6001000", "0x40001050"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			dump := arm32SignalTieMinidump(t, "tie")
			module := nativeTestMinidumpStreamRVA(dump, 4)
			thread := nativeTestMinidumpStreamRVA(dump, 3)
			memory := nativeTestMinidumpStreamRVA(dump, 9)
			stack := binary.LittleEndian.Uint32(dump[thread+40:])
			code := binary.LittleEndian.Uint64(dump[memory+8:])
			binary.LittleEndian.PutUint64(dump[module+4:], 0x400000)
			binary.LittleEndian.PutUint32(dump[stack+32+4*4:], 0x300030)
			binary.LittleEndian.PutUint32(dump[stack+32+15*4:], tc.savedPC)
			binary.LittleEndian.PutUint32(dump[stack+32+16*4:], tc.cpsr)
			// The third Memory64 range starts after two eight-byte code captures.
			binary.LittleEndian.PutUint32(dump[code+16+0x30:], 0x401220)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			for i, section := range [][]byte{thumbHTTPBuildArchDebugFrame(4, 14, 4, 4, 0x7c, 0x1102, 0xfe), thumbHTTPBuildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1200, 0x100)} {
				object := thumbHTTPBuildELF32DebugFrame(elf.EM_ARM, section)
				digest := sha1.Sum(object)
				if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, fmt.Sprintf("app%d.pdb", i), "elf", "", "arm", "app.pdb", hex.EncodeToString(digest[:]), nil, nil, object); err != nil {
					t.Fatal(err)
				}
			}
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
				t.Fatalf("response %d id=%q err=%v", res.StatusCode, ack.ID, err)
			}
			var raw []byte
			if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, ack.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var event struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []struct {
								PC string `json:"instruction_addr"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if len(event.Exception.Values) != 1 {
				t.Fatalf("exceptions=%+v", event.Exception)
			}
			frames := event.Exception.Values[0].Stacktrace.Frames
			if len(frames) != len(tc.want) {
				t.Fatalf("frames=%+v want=%v", frames, tc.want)
			}
			for i, want := range tc.want {
				if frames[i].PC != want {
					t.Fatalf("frames=%+v want=%v", frames, tc.want)
				}
			}
			var attachments, items int
			if err := f.sql.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM attachments WHERE project_id=$1::bigint AND event_id=$2), (SELECT count(*) FROM envelope_items WHERE project_id=$1::bigint AND event_id=$2)", f.project.ID, ack.ID).Scan(&attachments, &items); err != nil || attachments != 0 || items != 0 {
				t.Fatalf("dump retained attachments=%d items=%d err=%v", attachments, items, err)
			}
		})
	}
}
