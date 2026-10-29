package native

import (
	"bytes"
	"encoding/binary"
	"goodsentry/internal/testfixture/nativepdb"
	"testing"
)

func TestLegacyPublicFallbackAndFraming(t *testing.T) {
	f := nativepdb.WithLegacyPublicSymbols()
	original := f.Data
	start := f.PublicSymbolsBlock * 1024
	ref := bytes.Index(original[start:], []byte{3, 'r', 'e', 'f'})
	name := bytes.Index(original[start:], []byte("PublicSecond"))
	if ref < 0 || name < 13 {
		t.Fatal("fixture records missing")
	}
	for _, tc := range []struct {
		name          string
		edit          func([]byte)
		valid, public bool
	}{
		{"direct map and external reference", func([]byte) {}, true, true},
		{"private-stripped legacy references", func(b []byte) { binary.LittleEndian.PutUint16(b[f.DBIBlock*1024+56:], 2) }, true, true},
		{"private-stripped data reference", func(b []byte) {
			binary.LittleEndian.PutUint16(b[f.DBIBlock*1024+56:], 2)
			binary.LittleEndian.PutUint16(b[start+ref-16+2:], 0x0401)
		}, true, true},
		{"private-stripped local procedure reference", func(b []byte) {
			binary.LittleEndian.PutUint16(b[f.DBIBlock*1024+56:], 2)
			binary.LittleEndian.PutUint16(b[start+ref-16+2:], 0x0403)
		}, true, true},
		{"truncated external name", func(b []byte) { b[start+ref] = 255 }, false, false},
		{"truncated symbol record", func(b []byte) { binary.LittleEndian.PutUint16(b[start:], 0xfffc) }, false, false},
		{"invalid public section", func(b []byte) { binary.LittleEndian.PutUint16(b[start+name-13+8:], 9) }, false, false},
		{"unknown public flags", func(b []byte) { binary.LittleEndian.PutUint32(b[start+4:], 0x80000000) }, false, false},
		{"unsupported map skipped", func(b []byte) { binary.LittleEndian.PutUint16(b[f.SectionMapStart+4+6:], 2) }, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), original...)
			tc.edit(data)
			objects := ParseDebugObjects("legacy.pdb", data)
			if !tc.valid {
				if len(objects) != 0 {
					t.Fatal("malformed public stream returned symbols")
				}
				return
			}
			if len(objects) != 1 {
				t.Fatal("valid PDB rejected")
			}
			var alias, first, second bool
			for _, symbol := range objects[0].Functions {
				if symbol.publicFallback && symbol.Start == 0x1060 && symbol.End == 0x1070 {
					alias = alias || symbol.Name == "PublicAlias"
					first = first || symbol.Name == "PublicFirst"
				}
				if symbol.publicFallback && symbol.Start == 0x1070 && symbol.End == 0x1100 && symbol.Name == "PublicSecond" {
					second = true
				}
			}
			if (alias && first && second) != tc.public {
				t.Fatalf("public ranges present=%v want=%v", alias && first && second, tc.public)
			}
			crash := Crash{Modules: []Module{{Name: "program.exe", Base: 0x10000000, Size: 0x10000, DebugID: "65ae75631"}}, Frames: []Frame{{Instruction: 0x10001015, Module: "program.exe"}}}
			tables, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "legacy.pdb", Content: data}})
			Symbolicate(&crash, tables)
			frame := crash.Frames[0]
			if frame.Function != "CrashFunction" || frame.File != `C:\src\first.cc` || frame.Line != 43 {
				t.Fatalf("detailed source lost: %+v", frame)
			}
		})
	}
}

func TestC7LegacyPublicUsesCompanion(t *testing.T) {
	pdb := nativepdb.OldDBIC7WithPublicSymbols().Data
	pe := nativepdb.V50CompanionPE(0x65ae7563, 1).Data
	tables, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "old.pdb", Content: pdb}, {Name: "old.exe", Content: pe}})
	crash := Crash{Modules: []Module{{Name: "program.exe", Base: 0x10000000, Size: 0x10000, DebugID: "65ae75631"}}, Frames: []Frame{
		{Instruction: 0x10001015, Module: "program.exe"}, {Instruction: 0x10001075, Module: "program.exe"},
	}}
	Symbolicate(&crash, tables)
	if crash.Frames[0].Function != "CrashFunction" || crash.Frames[1].Function != "PublicSecond" {
		t.Fatalf("C7 frames=%+v", crash.Frames)
	}
}
