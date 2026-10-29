package native

import (
	"encoding/binary"
	"goodsentry/internal/testfixture/nativepdb"
	"testing"
)

func TestModernPublicDirectSelectorAndOpaquePadding(t *testing.T) {
	f := nativepdb.WithPublicOnlySymbols()
	for _, tc := range []struct {
		name              string
		selector, padding bool
		first, second     uint64
	}{
		{"direct selector", true, false, 0x1010, 0x1030},
		{"opaque padding", false, true, 0x1030, 0x1050},
		{"both", true, true, 0x1010, 0x1030},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), f.Data...)
			if tc.selector {
				entry := data[f.SectionMapStart+4:]
				binary.LittleEndian.PutUint16(entry, binary.LittleEndian.Uint16(entry)|0x100)
				binary.LittleEndian.PutUint32(entry[12:], 0)
			}
			if tc.padding {
				data[f.PublicSymbolsBlock*512+26] = 0x55
			}
			objects := ParseDebugObjects("symbols.pdb", data)
			if len(objects) != 1 {
				t.Fatal("valid public symbols rejected")
			}
			var first, second bool
			for _, symbol := range objects[0].Functions {
				first = first || symbol.Name == "PublicFirst" && symbol.Start == tc.first
				second = second || symbol.Name == "PublicSecond" && symbol.Start == tc.second
			}
			if !first || !second {
				t.Fatalf("public names and addresses missing: first=%v second=%v", first, second)
			}
		})
	}
}

func TestModernPDBImageAgeUsesDBI(t *testing.T) {
	f := nativepdb.WithPublicOnlySymbols()
	baseline := ParseDebugObjects("symbols.pdb", f.Data)
	if len(baseline) != 1 {
		t.Fatal("baseline PDB rejected")
	}
	for _, tc := range []struct {
		name            string
		infoAge, dbiAge uint32
		valid           bool
	}{
		{"newer Info age", 3, 1, true},
		{"DBI newer than Info", 1, 3, false},
		{"zero DBI age", 3, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), f.Data...)
			binary.LittleEndian.PutUint32(data[f.InfoBlock*512+8:], tc.infoAge)
			binary.LittleEndian.PutUint32(data[f.DBIBlock*512+8:], tc.dbiAge)
			objects := ParseDebugObjects("symbols.pdb", data)
			if !tc.valid {
				if len(objects) != 0 {
					t.Fatal("invalid ages accepted")
				}
				return
			}
			if len(objects) != 1 || objects[0].DebugID != baseline[0].DebugID || len(objects[0].Functions) != len(baseline[0].Functions) {
				t.Fatalf("image identity or symbols lost: %+v", objects)
			}
			files := []DebugSymbolFile{{Name: "symbols.pdb", DebugID: baseline[0].DebugID, Content: data}}
			symbols, _ := BuildSymbolTables(files)
			crash := Crash{Modules: []Module{{Name: "crash.exe", Base: 0x100000, Size: 0x5000, DebugID: baseline[0].DebugID}}, Frames: []Frame{{Instruction: 0x101035, Module: "crash.exe"}}}
			Symbolicate(&crash, symbols)
			if crash.Frames[0].Function != "PublicFirst" {
				t.Fatalf("image age did not resolve: %+v", crash.Frames[0])
			}
			files[0].DebugID = baseline[0].DebugID[:len(baseline[0].DebugID)-1] + "3"
			symbols, _ = BuildSymbolTables(files)
			if len(symbols) != 0 {
				t.Fatal("Info age admitted as image identity")
			}
		})
	}
}

func TestPDBRSDSCanonicalGUIDIdentity(t *testing.T) {
	const id = "1b72224d37b8179228200ed8994498b21"
	guid := []byte{0x4d, 0x22, 0x72, 0x1b, 0xb8, 0x37, 0x92, 0x17, 0x28, 0x20, 0x0e, 0xd8, 0x99, 0x44, 0x98, 0xb2}
	record := make([]byte, 24)
	copy(record, "RSDS")
	copy(record[4:], guid)
	binary.LittleEndian.PutUint32(record[20:], 1)
	if got := DebugKey(codeViewDebugID(record)); got != id {
		t.Fatalf("RSDS identity=%q, want %q", got, id)
	}
	f := nativepdb.WithPublicOnlySymbols()
	copy(f.Data[f.InfoBlock*512+12:], guid)
	objects := ParseDebugObjects("symbols.pdb", f.Data)
	if len(objects) != 1 || objects[0].DebugID != id {
		t.Fatalf("PDB identity does not match RSDS: %+v", objects)
	}
}
