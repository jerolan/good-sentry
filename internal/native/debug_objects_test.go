package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"goodsentry/internal/testfixture/nativesymcache"
)

func TestParseDebugObjectsUsesEmbeddedObjectIdentity(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{name: "linux/libexample.so", file: "testdata/elf-dwarf-amd64", want: "66cac678cd058872052ee3111f0b1f6ed8b3a1a9"},
		{name: "Example.dSYM", file: "testdata/macho-dsym-amd64", want: "220efad905598307f95e9f873725396f"},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			data, err := os.ReadFile(test.file)
			if err != nil {
				t.Fatal(err)
			}
			objects := ParseDebugObjects(test.name, data)
			if len(objects) != 1 || objects[0].DebugID != test.want {
				t.Fatalf("objects=%+v, want one object with DebugID %q", objects, test.want)
			}
		})
	}
}

func TestBuildSymbolTablesUsesFormatOwnedDebugIDAliases(t *testing.T) {
	const uuid = "00112233445566778899aabbccddeeff"
	const otherUUID = "ffeeddccbbaa99887766554433221100"
	for _, version := range []uint32{7, 8, 9} {
		t.Run(fmt.Sprintf("SymCache v%d UUID appendix0", version), func(t *testing.T) {
			cache := nativesymcache.Build(version, 0).Data
			binary.LittleEndian.PutUint32(cache[8+16:], 0)
			objects := ParseDebugObjects("uploaded.symcache", cache)
			if len(objects) != 1 || objects[0].DebugID != uuid {
				t.Fatalf("UUID0 public identity=%+v, want %q", objects, uuid)
			}
			for _, metadata := range []struct {
				name, id string
				admitted bool
			}{
				{name: "omitted", admitted: true},
				{name: "UUID", id: uuid, admitted: true},
				{name: "UUID age0", id: uuid + "0", admitted: true},
				{name: "UUID age1", id: uuid + "1"},
				{name: "different UUID", id: otherUUID},
			} {
				t.Run(metadata.name, func(t *testing.T) {
					symbols, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "uploaded.symcache", DebugID: metadata.id, Content: cache}})
					for _, moduleID := range []string{uuid, uuid + "0", uuid + "1", otherUUID} {
						t.Run(moduleID, func(t *testing.T) {
							crash := Crash{
								Modules: []Module{{Name: "renamed.exe", Base: 0x400000, Size: 0x2000, DebugID: moduleID}},
								Frames:  []Frame{{Module: "renamed.exe", Instruction: 0x401015}},
							}
							Symbolicate(&crash, symbols)
							frame := crash.Frames[0]
							wantSymbol := metadata.admitted && (moduleID == uuid || moduleID == uuid+"0")
							if wantSymbol {
								if frame.Function != "CrashFunction" || frame.File != `C:\project\src\crash.c` || frame.Line != 42 {
									t.Fatalf("UUID0 frame=%+v, want CrashFunction C:\\project\\src\\crash.c:42", frame)
								}
							} else if frame.Function != "" || frame.File != "" || frame.Line != 0 {
								t.Fatalf("incompatible identity returned symbols: %+v", frame)
							}
						})
					}
				})
			}
		})
	}
	t.Run("MachO UUID", func(t *testing.T) {
		data, err := os.ReadFile("testdata/macho-dsym-amd64")
		if err != nil {
			t.Fatal(err)
		}
		const id = "220efad905598307f95e9f873725396f"
		if got := ObjectDebugID(data); got != id {
			t.Fatalf("Mach-O UUID=%q, want %q", got, id)
		}
		for _, metadata := range []struct {
			name, id, uploadName string
			admitted             bool
		}{
			{name: "omitted", admitted: true},
			{name: "UUID", id: id, admitted: true},
			{name: "UUID age0", id: id + "0", admitted: true},
			{name: "alias equals upload name", uploadName: id + "0", admitted: true},
			{name: "UUID age1", id: id + "1"},
			{name: "different UUID", id: uuid},
		} {
			t.Run(metadata.name, func(t *testing.T) {
				uploadName := metadata.uploadName
				if uploadName == "" {
					uploadName = "uploaded.dsym"
				}
				symbols, unwind := BuildSymbolTables([]DebugSymbolFile{{Name: uploadName, DebugID: metadata.id, Content: data}})
				if metadata.admitted && (len(unwind[id]) != 1 || len(unwind[id+"0"]) != 1) {
					t.Fatalf("Mach-O unwind destination counts: primary=%d alias=%d", len(unwind[id]), len(unwind[id+"0"]))
				}
				const base = uint64(0x100000000)
				crash := Crash{
					RIP: base + 0xf6a, RSP: 0x200000, RBP: 0x200000, Arch: "amd64", PointerSize: 8,
					Modules: []Module{{Name: "renamed.bin", Base: base, Size: 0x2000, DebugID: id + "0"}},
					stack:   []memoryRange{{start: 0x200000, data: make([]byte, 32)}},
				}
				frames := UnwindCFI(&crash, unwind)
				if metadata.admitted {
					if len(frames) != 1 || frames[0].Instruction != base+0xf6a {
						t.Fatalf("Mach-O UUID0 CFI lookup returned frames=%+v", frames)
					}
				} else if len(frames) != 0 {
					t.Fatalf("mismatched identity contributed CFI frames=%+v", frames)
				}
				if metadata.admitted {
					if len(symbols[id]) == 0 || len(symbols[id+"0"]) == 0 {
						t.Fatalf("UUID symbols missing: primary=%d alias=%d", len(symbols[id]), len(symbols[id+"0"]))
					}
				} else if len(symbols) != 0 {
					t.Fatalf("mismatched identity contributed %d symbol keys", len(symbols))
				}
			})
		}
	})
	t.Run("ELF sixteen byte GNU build ID has no UUID alias", func(t *testing.T) {
		data, err := os.ReadFile("testdata/elf-dwarf-amd64")
		if err != nil {
			t.Fatal(err)
		}
		file, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		section := file.Section(".note.gnu.build-id")
		if section == nil || section.Size < 36 {
			t.Fatal("ELF fixture lacks the expected GNU build ID note")
		}
		note := data[section.Offset : section.Offset+section.Size]
		if file.ByteOrder.Uint32(note) != 4 || file.ByteOrder.Uint32(note[4:]) != 20 || file.ByteOrder.Uint32(note[8:]) != 3 || string(note[12:16]) != "GNU\x00" {
			t.Fatal("ELF fixture build ID note layout changed")
		}
		file.ByteOrder.PutUint32(note[4:], 16)
		const id = "66cac678cd058872052ee3111f0b1f6e"
		if got := ObjectDebugID(data); got != id {
			t.Fatalf("ELF build ID=%q, want %q", got, id)
		}
		for _, declared := range []string{"", id, id + "0", id + "1"} {
			t.Run("metadata "+declared, func(t *testing.T) {
				symbols, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "uploaded.so", DebugID: declared, Content: data}})
				if declared == "" || declared == id {
					if len(symbols[id]) == 0 || len(symbols[id+"0"]) != 0 {
						t.Fatalf("ELF identity symbols=%d UUID alias symbols=%d", len(symbols[id]), len(symbols[id+"0"]))
					}
				} else if len(symbols) != 0 {
					t.Fatalf("ELF metadata relabeled build ID into %d symbol keys", len(symbols))
				}
			})
		}
	})
}

func TestParseELFBuildIDNoteRejectsMalformedRecords(t *testing.T) {
	valid := make([]byte, 24)
	binary.LittleEndian.PutUint32(valid[0:4], 4)
	binary.LittleEndian.PutUint32(valid[4:8], 4)
	binary.LittleEndian.PutUint32(valid[8:12], elfNoteGNU)
	copy(valid[12:16], "GNU\x00")
	copy(valid[16:20], []byte{0x01, 0x02, 0x03, 0x04})
	if got := parseELFBuildIDNote(valid, binary.LittleEndian); got != "01020304" {
		t.Fatalf("valid debug ID=%q", got)
	}
	for _, malformed := range [][]byte{
		valid[:11],
		valid[:19],
		append(valid[:4:4], 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0),
	} {
		if got := parseELFBuildIDNote(malformed, binary.LittleEndian); got != "" {
			t.Fatalf("malformed note produced debug ID %q", got)
		}
	}
}
