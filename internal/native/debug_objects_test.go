package native

import (
	"encoding/binary"
	"os"
	"testing"
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

func TestBuildSymbolTablesAcceptsMachOUUIDWithZeroAge(t *testing.T) {
	data, err := os.ReadFile("testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	id := ObjectDebugID(data)
	if len(id) != 32 {
		t.Fatalf("Mach-O UUID=%q", id)
	}
	symbols, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "uploaded.dsym", DebugID: id + "0", Content: data}})
	if len(symbols[id]) == 0 || len(symbols[id+"0"]) == 0 {
		t.Fatalf("UUID symbols missing from either compatible key: %v", symbols)
	}
	symbols, _ = BuildSymbolTables([]DebugSymbolFile{{Name: "uploaded.dsym", DebugID: "00112233445566778899aabbccddeeff", Content: data}})
	if len(symbols) != 0 {
		t.Fatalf("mismatched identity contributed symbols: %v", symbols)
	}
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
