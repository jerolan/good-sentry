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

// Two captured signal trampolines produce two recovered frames. The historical
// frame-pointer chain also has two frames, but its second PC is different.
func arm64SignalTieMinidump(t *testing.T, mode string) []byte {
	t.Helper()
	const pc uint64 = 0x200001000
	const resumed uint64 = 0x200002000
	const stackBase uint64 = 0x200000
	const resumedSP uint64 = 0x300000
	data := buildNativeModuleMinidump(t, "C:\\test\\app.pdb", "5A9832E5287241C1838ED98914E9B7FF1", 0x1010)
	find := func(typ uint32) uint32 {
		for i := uint32(0); i < 4; i++ {
			entry := 32 + i*12
			if binary.LittleEndian.Uint32(data[entry:]) == typ {
				return binary.LittleEndian.Uint32(data[entry+8:])
			}
		}
		t.Fatalf("missing stream %d", typ)
		return 0
	}
	thread := find(3)
	exception := find(6)
	binary.LittleEndian.PutUint64(data[exception+24:], pc)
	context := make([]byte, 272)
	binary.LittleEndian.PutUint64(context[240:], stackBase+0x300)
	binary.LittleEndian.PutUint64(context[256:], stackBase)
	binary.LittleEndian.PutUint64(context[264:], pc)
	contextRVA := uint32(len(data))
	data = append(data, context...)
	binary.LittleEndian.PutUint32(data[thread+44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(data[thread+48:], contextRVA)
	signal := make([]byte, 0x400)
	binary.LittleEndian.PutUint64(signal[560:], resumedSP)
	binary.LittleEndian.PutUint64(signal[568:], resumed)
	binary.LittleEndian.PutUint64(signal[0x308:], 0x140001050)
	if mode == "long historical" {
		binary.LittleEndian.PutUint64(signal[0x300:], stackBase+0x320)
		binary.LittleEndian.PutUint64(signal[0x328:], 0x140001090)
	}
	if mode == "invalid signal PC" {
		binary.LittleEndian.PutUint64(signal[568:], 0)
	}
	stackRVA := uint32(len(data))
	data = append(data, signal...)
	binary.LittleEndian.PutUint64(data[thread+28:], stackBase)
	binary.LittleEndian.PutUint32(data[thread+36:], uint32(len(signal)))
	binary.LittleEndian.PutUint32(data[thread+40:], stackRVA)
	system := make([]byte, 56)
	binary.LittleEndian.PutUint16(system, 12)
	binary.LittleEndian.PutUint32(system[20:], 0x8201)
	systemRVA := uint32(len(data))
	data = append(data, system...)
	instructions := make([]byte, 8)
	binary.LittleEndian.PutUint32(instructions, 0xd2801168)
	binary.LittleEndian.PutUint32(instructions[4:], 0xd4000001)
	memory := make([]byte, 16+3*16)
	binary.LittleEndian.PutUint64(memory, 3)
	memoryRVA := uint32(len(data))
	data = append(data, memory...)
	binary.LittleEndian.PutUint64(data[memoryRVA+8:], uint64(len(data)))
	ranges := []struct {
		address uint64
		bytes   []byte
	}{{pc, instructions}, {resumed, instructions}, {resumedSP, make([]byte, 576)}}
	for i, r := range ranges {
		offset := memoryRVA + 16 + uint32(i)*16
		binary.LittleEndian.PutUint64(data[offset:], r.address)
		binary.LittleEndian.PutUint64(data[offset+8:], uint64(len(r.bytes)))
		data = append(data, r.bytes...)
	}
	directory := uint32(len(data))
	data = append(data, data[32:80]...)
	data = append(data, make([]byte, 12)...)
	binary.LittleEndian.PutUint32(data[8:], 5)
	binary.LittleEndian.PutUint32(data[12:], directory)
	for i := uint32(0); i < 4; i++ {
		entry := directory + i*12
		if binary.LittleEndian.Uint32(data[entry:]) == 7 {
			binary.LittleEndian.PutUint32(data[entry+4:], uint32(len(system)))
			binary.LittleEndian.PutUint32(data[entry+8:], systemRVA)
		}
	}
	binary.LittleEndian.PutUint32(data[directory+48:], 9)
	binary.LittleEndian.PutUint32(data[directory+52:], uint32(len(memory)))
	binary.LittleEndian.PutUint32(data[directory+56:], memoryRVA)
	return data
}

func TestNativeARM64SignalTiePersistsRecoveredPC(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		historical, recovered int
		want                  []string
	}{
		{"tie", 2, 2, []string{"0x200001000", "0x200002000"}},
		{"long historical", 3, 2, []string{"0x200001000", "0x200002000"}},
		{"invalid signal PC", 2, 1, []string{"0x200001000", "0x140001050"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			dump := arm64SignalTieMinidump(t, tc.mode)
			crash, err := native.ParseMinidump(dump)
			if err != nil {
				t.Fatal(err)
			}
			recovered := native.UnwindCFI(&crash, nil)
			if len(crash.Frames) != tc.historical || len(recovered) != tc.recovered || (tc.recovered == 2 && (recovered[1].Instruction != 0x200002000 || crash.Frames[1].Instruction == recovered[1].Instruction)) {
				t.Fatalf("fixture is not a differing tie: historical=%+v recovered=%+v", crash.Frames, recovered)
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
