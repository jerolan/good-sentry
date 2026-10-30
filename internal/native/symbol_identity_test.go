package native

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func symbolIdentityMinidump(t *testing.T, record []byte) []byte {
	t.Helper()
	dump := buildAMD64Minidump()
	_, body, _ := minidumpStream(t, dump, streamModuleList)
	rva := len(dump)
	dump = append(dump, record...)
	binary.LittleEndian.PutUint32(dump[body+80:], uint32(len(record)))
	binary.LittleEndian.PutUint32(dump[body+84:], uint32(rva))
	return dump
}

func TestMinidumpSymbolLookupIdentitySurvivesEventRoundTrip(t *testing.T) {
	rsds := make([]byte, 24)
	copy(rsds, "RSDS")
	for i := 0; i < 16; i++ {
		rsds[4+i] = byte(i)
	}
	binary.LittleEndian.PutUint32(rsds[20:], 1)
	nb10 := make([]byte, 16)
	copy(nb10, "NB10")
	binary.LittleEndian.PutUint32(nb10[8:], 0x12345678)
	binary.LittleEndian.PutUint32(nb10[12:], 2)
	withPath := func(prefix []byte, path string, terminated bool) []byte {
		out := append(append([]byte(nil), prefix...), []byte(path)...)
		if terminated {
			out = append(out, 0)
		}
		return out
	}
	for _, test := range []struct {
		name, imageType, debugID, debugFile, codeID string
		record                                      []byte
	}{
		{"RSDS Windows path", "pe", "030201000504070608090A0B0C0D0E0F1", "app.pdb", "", withPath(rsds, `C:\private\app.pdb`, true)},
		{"NB10 mixed path", "pe", "123456782", "legacy.pdb", "", withPath(nb10, `C:\private/more\legacy.pdb`, true)},
		{"full ELF build ID", "elf", "66cac678cd0102030405", "", "66cac678cd0102030405", append([]byte{0x4c, 0x45, 0x70, 0x42}, 0x66, 0xca, 0xc6, 0x78, 0xcd, 1, 2, 3, 4, 5)},
		{"unterminated PDB name", "pe", "030201000504070608090A0B0C0D0E0F1", "", "", withPath(rsds, "file.pdb", false)},
		{"parent component", "pe", "030201000504070608090A0B0C0D0E0F1", "", "", withPath(rsds, `C:\private\..`, true)},
		{"control character", "pe", "030201000504070608090A0B0C0D0E0F1", "", "", withPath(rsds, "bad\nname.pdb", true)},
		{"invalid UTF8", "pe", "030201000504070608090A0B0C0D0E0F1", "", "", append(append(append([]byte(nil), rsds...), 0xff), 0)},
		{"oversized component", "pe", "030201000504070608090A0B0C0D0E0F1", "", "", withPath(rsds, strings.Repeat("a", 256), true)},
		{"oversized path", "pe", "030201000504070608090A0B0C0D0E0F1", "", "", withPath(rsds, strings.Repeat("a", 4096)+"/x.pdb", true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			crash, err := ParseMinidump(symbolIdentityMinidump(t, test.record))
			if err != nil {
				t.Fatal(err)
			}
			raw, _, err := BuildEvent(crash, nil, time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			check := func(raw json.RawMessage) {
				t.Helper()
				var event struct {
					DebugMeta struct {
						Images []map[string]any `json:"images"`
					} `json:"debug_meta"`
				}
				if err := json.Unmarshal(raw, &event); err != nil || len(event.DebugMeta.Images) != 1 {
					t.Fatalf("debug metadata: %s, error=%v", raw, err)
				}
				image := event.DebugMeta.Images[0]
				if image["type"] != test.imageType || image["debug_id"] != test.debugID {
					t.Fatalf("image type/identity=%v/%v, want %s/%s", image["type"], image["debug_id"], test.imageType, test.debugID)
				}
				for key, want := range map[string]string{"debug_file": test.debugFile, "code_id": test.codeID} {
					got, present := image[key]
					if want == "" && present || want != "" && got != want {
						t.Fatalf("image %s=%v, want %q", key, got, want)
					}
				}
			}
			check(raw)
			merged, err := MergeMinidumpEvent(json.RawMessage(`{"platform":"native","event_id":"original"}`), crash, time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			check(merged)
			var container map[string]json.RawMessage
			if err := json.Unmarshal(merged, &container); err != nil {
				t.Fatal(err)
			}
			modules := nativeEventModules(container["debug_meta"])
			if len(modules) != 1 {
				t.Fatalf("reconstructed modules=%+v", modules)
			}
			rebuilt, _, err := BuildEvent(Crash{Modules: modules}, nil, time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			check(rebuilt)
		})
	}
}

func TestHistoricalNativeImageIgnoresMalformedOptionalSymbolFields(t *testing.T) {
	for _, test := range []struct {
		name, extra string
	}{
		{"type is not a string", `"type":3`},
		{"debug file is not a string", `"type":"pe","debug_file":{}`},
		{"code ID is not a string", `"type":"elf","code_id":[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			meta := json.RawMessage(`{"images":[{"code_file":"app","image_addr":"0x1000","image_size":4096,"debug_id":"1234",` + test.extra + `}]}`)
			modules := nativeEventModules(meta)
			if len(modules) != 1 {
				t.Fatalf("historical image lost: %+v", modules)
			}
			raw, _, err := BuildEvent(Crash{Modules: modules}, nil, time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				DebugMeta struct {
					Images []map[string]any `json:"images"`
				} `json:"debug_meta"`
			}
			if err := json.Unmarshal(raw, &event); err != nil || len(event.DebugMeta.Images) != 1 || event.DebugMeta.Images[0]["debug_id"] != "1234" {
				t.Fatalf("historical identity lost: %s, error=%v", raw, err)
			}
		})
	}
}
