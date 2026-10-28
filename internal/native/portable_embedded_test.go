package native

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSymbolicateEmbeddedPortableSDKEvent(t *testing.T) {
	pe, err := os.ReadFile("testdata/portable-embedded/Probe.dll")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/portable-embedded/event.json")
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err = json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	values := event["exception"].(map[string]any)["values"].([]any)
	frames := values[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	for _, value := range frames {
		frame := value.(map[string]any)
		delete(frame, "filename")
		delete(frame, "abs_path")
		delete(frame, "lineno")
	}
	missing, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	zipPE := func(data []byte) []byte {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		f, e := w.Create("managed/Probe.dll")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(data); e != nil {
			t.Fatal(e)
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		return b.Bytes()
	}
	damaged := func(offset int) []byte { out := append([]byte(nil), pe...); out[offset] ^= 0x40; return out }
	oversized := append([]byte(nil), pe...)
	binary.LittleEndian.PutUint32(oversized[6449:], 64*1024*1024+1)
	withoutCodeView := append([]byte(nil), pe...)
	binary.LittleEndian.PutUint32(withoutCodeView[6272:], 0)
	conflictingCodeView := append([]byte(nil), pe...)
	copy(conflictingCodeView[6316:6344], pe[6260:6288])
	conflictingCodeView[6320] ^= 1
	duplicateEmbedded := append([]byte(nil), pe...)
	copy(duplicateEmbedded[6316:6344], pe[6344:6372])
	trailingDeflate := append([]byte(nil), pe...)
	binary.LittleEndian.PutUint32(trailingDeflate[6360:], 7544)
	if len(pe) < 6500 || string(pe[6372:6376]) != "RSDS" || string(pe[6445:6449]) != "MPDB" {
		t.Fatal("unexpected fixture debug directory")
	}
	const embeddedID = "6755b4cd-d3c5-4986-89d6-490d7bd5545c-8e698d33"
	const peID = "6755b4cd-d3c5-4986-89d6-490d7bd5545c-1"
	for _, tc := range []struct {
		name string
		data []byte
		id   string
		want bool
	}{
		{"PE", pe, "", true}, {"embedded ID", pe, embeddedID, true}, {"PE CodeView ID", pe, peID, true},
		{"without CodeView", withoutCodeView, "", true}, {"zip", zipPE(pe), "", true},
		{"wrong upload identity", pe, "000000000000000000000000000000001", false},
		{"mismatched CodeView", damaged(6376), "", false}, {"bad embedded signature", damaged(6445), "", false},
		{"conflicting CodeView entries", conflictingCodeView, "", false}, {"duplicate embedded entries", duplicateEmbedded, "", false},
		{"bad deflate", damaged(6454), "", false}, {"bad checksum", damaged(6413), "", false},
		{"trailing deflate data", trailingDeflate, "", false},
		{"unsupported checksum", damaged(6406), "", false}, {"unsupported embedded version", damaged(6354), "", false},
		{"invalid embedded stamp", damaged(6348), "", false}, {"oversized expansion", oversized, "", false},
		{"truncated PE", pe[:len(pe)/2], "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, changed, err := SymbolicatePortableEvent(missing, []DebugSymbolFile{{Name: "Probe.dll", DebugID: tc.id, Content: tc.data}})
			if err != nil || changed != tc.want {
				t.Fatalf("changed=%v want=%v err=%v", changed, tc.want, err)
			}
			if !tc.want {
				if !bytes.Equal(got, missing) {
					t.Fatal("invalid PE changed event")
				}
				return
			}
			var out map[string]any
			if err = json.Unmarshal(got, &out); err != nil {
				t.Fatal(err)
			}
			fs := out["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
			var found bool
			for _, value := range fs {
				f := value.(map[string]any)
				if strings.Contains(f["function"].(string), "ThrowProbe") {
					found = true
					name, _ := f["filename"].(string)
					if !strings.HasSuffix(name, "/Program.cs") || f["lineno"] != float64(18) {
						t.Fatalf("frame=%v", f)
					}
				}
			}
			if !found {
				t.Fatal("SDK probe frame missing")
			}
		})
	}
	got, _, err := SymbolicatePortableEvent(raw, []DebugSymbolFile{{Name: "Probe.dll", Content: pe}})
	if err != nil {
		t.Fatal(err)
	}
	var original, updated map[string]any
	if json.Unmarshal(raw, &original) != nil || json.Unmarshal(got, &updated) != nil {
		t.Fatal("invalid event")
	}
	originalFrames := original["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	updatedFrames := updated["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	for i := range originalFrames {
		a, _ := json.Marshal(originalFrames[i])
		b, _ := json.Marshal(updatedFrames[i])
		if !bytes.Equal(a, b) {
			t.Fatalf("SDK source fields changed: %s -> %s", a, b)
		}
	}
}

func TestEmbeddedPortableExpansionBudget(t *testing.T) {
	pdb, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	pe, err := os.ReadFile("testdata/portable-embedded/Probe.dll")
	if err != nil {
		t.Fatal(err)
	}
	// Valid PE framing with a deliberately false 64 MiB expanded size: each
	// attempted decompression consumes the shared budget even though it fails.
	binary.LittleEndian.PutUint32(pe[6449:], 64*1024*1024)
	raw := []byte(`{"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"c6f075cc-4407-4e01-aeee-5d0399a7d7d1-b08a78f8"}]},"stacktrace":{"frames":[{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x5"}]}}`)
	standalone := DebugSymbolFile{Name: "standalone.pdb", Content: pdb}
	invalid := DebugSymbolFile{Name: "bad.dll", Content: pe}
	if _, changed, e := SymbolicatePortableEvent(raw, []DebugSymbolFile{standalone, invalid, invalid}); e != nil || !changed {
		t.Fatalf("at budget: changed=%v err=%v", changed, e)
	}
	got, changed, e := SymbolicatePortableEvent(raw, []DebugSymbolFile{standalone, invalid, invalid, invalid})
	if e != nil || changed || !bytes.Equal(got, raw) {
		t.Fatalf("over budget: changed=%v err=%v", changed, e)
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for name, data := range map[string][]byte{"bad.dll": pe, "broken.zip": {'P', 'K', 3, 4, 0}} {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	invalidArchive := DebugSymbolFile{Name: "bad.zip", Content: archive.Bytes()}
	if _, changed, e = SymbolicatePortableEvent(raw, []DebugSymbolFile{standalone, invalidArchive, invalid, invalid}); e != nil || !changed {
		t.Fatalf("invalid archive charged: changed=%v err=%v", changed, e)
	}
}
