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
)

func TestNativeMIPS32MinidumpIngestion(t *testing.T) {
	const debugID = "5A9832E5287241C1838ED98914E9B7FF1"
	const pc uint64 = 0x40001010
	f := newResolutionHTTPFixture(t)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	sym := []byte("MODULE linux mips TEST test_app.pdb\nFILE 1 /src/mips.c\nFUNC 1010 20 0 MIPSFunction\n1010 20 42 1\n")
	digest := sha1.Sum(sym)
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "test_app.sym", "sym", debugID, "mips", "test_app.pdb", hex.EncodeToString(digest[:]), nil, nil, sym); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := f.sql.QueryRow(f.ctx, "SELECT count(*) FROM events WHERE project_id=$1::bigint", f.project.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, tc := range []struct {
		name  string
		size  int
		flags uint64
		valid bool
	}{
		{"valid", 600, 0x4000e, true},
		{"high PC", 600, 0x4000e, false},
		{"MIPS64 flags", 600, 0x8000e, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := count()
			dump := buildNativeModuleMinidump(t, "test_app.pdb", debugID, 0x1010)
			system := nativeTestMinidumpStreamRVA(dump, 7)
			thread := nativeTestMinidumpStreamRVA(dump, 3)
			binary.LittleEndian.PutUint16(dump[system:], 1)
			module := nativeTestMinidumpStreamRVA(dump, 4)
			exception := nativeTestMinidumpStreamRVA(dump, 6)
			binary.LittleEndian.PutUint64(dump[module+4:], 0x40000000)
			binary.LittleEndian.PutUint64(dump[exception+24:], pc)
			context := make([]byte, tc.size)
			binary.LittleEndian.PutUint32(context, uint32(tc.flags))
			binary.LittleEndian.PutUint32(context[4:], 0xfeedbeef)
			binary.LittleEndian.PutUint64(context[8+29*8:], 0x200000)
			binary.LittleEndian.PutUint64(context[8+30*8:], 0x200000)
			binary.LittleEndian.PutUint64(context[8+31*8:], 0x40001050)
			binary.LittleEndian.PutUint64(context[312:], pc)
			if tc.name == "high PC" {
				binary.LittleEndian.PutUint64(context[312:], 0x140001010)
			}
			binary.LittleEndian.PutUint64(context[336:], 0x40001080)
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
			if !tc.valid {
				if res.StatusCode != http.StatusBadRequest {
					t.Fatalf("invalid dump status=%d", res.StatusCode)
				}
				if after := count(); after != before {
					t.Fatalf("invalid dump persisted: before=%d after=%d", before, after)
				}
				return
			}
			var ack struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(res.Body).Decode(&ack); err != nil || res.StatusCode != http.StatusOK || ack.ID == "" {
				t.Fatalf("valid dump status=%d id=%q err=%v", res.StatusCode, ack.ID, err)
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
								Function    string `json:"function"`
								Filename    string `json:"filename"`
								Line        int    `json:"lineno"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 || len(payload.Exception.Values[0].Stacktrace.Frames) != 1 {
				t.Fatalf("frames=%+v", payload.Exception.Values)
			}
			frame := payload.Exception.Values[0].Stacktrace.Frames[0]
			if frame.Instruction != "0x40001010" || frame.Function != "MIPSFunction" || frame.Filename != "/src/mips.c" || frame.Line != 42 {
				t.Fatalf("wrong captured/symbolicated frame: %+v", frame)
			}
			var attachments, items int
			if err := f.sql.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM attachments WHERE project_id=$1::bigint AND event_id=$2), (SELECT count(*) FROM envelope_items WHERE project_id=$1::bigint AND event_id=$2)", f.project.ID, ack.ID).Scan(&attachments, &items); err != nil || attachments != 0 || items != 0 {
				t.Fatalf("raw dump retained: attachments=%d items=%d err=%v", attachments, items, err)
			}
		})
	}
}
