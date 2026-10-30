//go:build integration

package server

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/native"
	"goodsentry/internal/store"
)

// Build a Linux ARM-state non-RT signal frame using serialized Minidump streams.
func arm32SignalTieMinidump(t *testing.T, mode string) []byte {
	t.Helper()
	data := arm64SignalTieMinidump(t, mode)
	system := nativeTestMinidumpStreamRVA(data, 7)
	thread := nativeTestMinidumpStreamRVA(data, 3)
	exception := nativeTestMinidumpStreamRVA(data, 6)
	memory := nativeTestMinidumpStreamRVA(data, 9)
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	code := binary.LittleEndian.Uint64(data[memory+8:])
	binary.LittleEndian.PutUint16(data[system:], 5)
	binary.LittleEndian.PutUint64(data[exception+24:], 0x6001000)
	context := make([]byte, 368)
	binary.LittleEndian.PutUint32(context[4+11*4:], 0x200300)  // r11: historical frame pointer
	binary.LittleEndian.PutUint32(context[4+13*4:], 0x200000)  // r13: SP
	binary.LittleEndian.PutUint32(context[4+15*4:], 0x6001000) // r15: PC
	contextRVA := uint32(len(data))
	data = append(data, context...)
	binary.LittleEndian.PutUint32(data[thread+44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(data[thread+48:], contextRVA)
	binary.LittleEndian.PutUint32(data[stack:], 0x5ac3c35a)
	binary.LittleEndian.PutUint32(data[stack+32+13*4:], 0x300000)
	binary.LittleEndian.PutUint32(data[stack+32+15*4:], 0x6002000)
	binary.LittleEndian.PutUint32(data[stack+32+16*4:], 0x10) // CPSR user, ARM state
	binary.LittleEndian.PutUint32(data[stack+0x300:], 0)
	binary.LittleEndian.PutUint32(data[stack+0x304:], 0x40001050)
	if mode == "long historical" {
		binary.LittleEndian.PutUint32(data[stack+0x300:], 0x200320)
		binary.LittleEndian.PutUint32(data[stack+0x320:], 0)
		binary.LittleEndian.PutUint32(data[stack+0x324:], 0x40001090)
	}
	if mode == "invalid signal PC" {
		binary.LittleEndian.PutUint32(data[stack+32+15*4:], 0)
	}
	if mode == "invalid CPSR" {
		binary.LittleEndian.PutUint32(data[stack+32+16*4:], 0x01000010)
	} // Jazelle bit
	binary.LittleEndian.PutUint64(data[memory+16:], 0x6001000)
	binary.LittleEndian.PutUint64(data[memory+32:], 0x6002000)
	for _, off := range []uint64{code, code + 8} {
		binary.LittleEndian.PutUint32(data[off:], 0xe3a07077)
		binary.LittleEndian.PutUint32(data[off+4:], 0xef000000)
	}
	return data
}

func TestNativeARM32SignalTiePersistsRecoveredPC(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		historical, recovered int
		want                  []string
	}{
		{"tie", 2, 2, []string{"0x6001000", "0x6002000"}},
		{"long historical", 3, 2, []string{"0x6001000", "0x6002000"}},
		{"invalid signal PC", 2, 1, []string{"0x6001000", "0x40001050"}},
		{"invalid CPSR", 2, 1, []string{"0x6001000", "0x40001050"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			dump := arm32SignalTieMinidump(t, tc.mode)
			crash, err := native.ParseMinidump(dump)
			if err != nil {
				t.Fatal(err)
			}
			if len(crash.Frames) != tc.historical || crash.Frames[1].Instruction != 0x40001050 {
				t.Fatalf("unexpected historical chain: %+v", crash.Frames)
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
			if res.StatusCode != http.StatusOK {
				t.Fatalf("minidump HTTP status=%d", res.StatusCode)
			}
			var accepted struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil || accepted.ID == "" {
				t.Fatalf("response id=%q err=%v", accepted.ID, err)
			}
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
			if err != nil {
				t.Fatalf("stored events=%d err=%v", len(events), err)
			}
			var stored json.RawMessage
			for _, event := range events {
				var identity struct {
					EventID string `json:"event_id"`
				}
				if err := json.Unmarshal(event.Data, &identity); err != nil {
					t.Fatal(err)
				}
				if identity.EventID == accepted.ID {
					stored = event.Data
					break
				}
			}
			if len(stored) == 0 {
				t.Fatalf("event %s missing", accepted.ID)
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
			if err := json.Unmarshal(stored, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 {
				t.Fatalf("exceptions=%+v", payload.Exception)
			}
			frames := payload.Exception.Values[0].Stacktrace.Frames
			if len(frames) != len(tc.want) {
				t.Fatalf("persisted frames=%+v; want %v", frames, tc.want)
			}
			for i, want := range tc.want {
				if frames[i].Instruction != want {
					t.Fatalf("persisted frames=%+v; want %v", frames, tc.want)
				}
			}
		})
	}
}
