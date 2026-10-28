package native

import (
	"bytes"
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativepdb"
)

func TestLegacyThunkSymbolication(t *testing.T) {
	original := nativepdb.WithLegacyThunk().Data
	name := bytes.Index(original, []byte("LegacyThunk"))
	if name < 26 {
		t.Fatal("fixture thunk missing")
	}
	offset := name - 26
	mutate := func(edit func([]byte)) []byte {
		data := append([]byte(nil), original...)
		edit(data[offset:])
		return data
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		want   string
		reject bool
	}{
		{"ordinal zero", original, "LegacyThunk", false},
		{"unsupported ordinal", mutate(func(r []byte) { r[24] = 1 }), "", false},
		{"truncated name", mutate(func(r []byte) { r[25] = 255 }), "", true},
		{"nonzero alignment", mutate(func(r []byte) { r[25]-- }), "", true},
		{"empty name", mutate(func(r []byte) { r[25] = 0 }), "", true},
		{"invalid section", mutate(func(r []byte) { binary.LittleEndian.PutUint16(r[20:], 2) }), "", true},
		{"outside section", mutate(func(r []byte) { binary.LittleEndian.PutUint32(r[16:], 0xff) }), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tables, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "fixture.pdb", Content: tc.data}})
			if tc.reject {
				if len(tables) != 0 {
					t.Fatal("malformed thunk returned partial symbols")
				}
				return
			}
			crash := Crash{Modules: []Module{{Name: "program.exe", Base: 0x10000000, Size: 0x10000, DebugID: "65ae75631"}}, Frames: []Frame{{Instruction: 0x10001065, Module: "program.exe"}}}
			Symbolicate(&crash, tables)
			if crash.Frames[0].Function != tc.want {
				t.Fatalf("frame=%+v want=%q", crash.Frames[0], tc.want)
			}
		})
	}
}

func TestC7LegacyThunkUsesCompanionPE(t *testing.T) {
	pdb := nativepdb.OldDBIC7WithThunk().Data
	pe := nativepdb.V50CompanionPE(0x65ae7563, 1).Data
	tables, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "old.pdb", Content: pdb}, {Name: "old.exe", Content: pe}})
	crash := Crash{Modules: []Module{{Name: "program.exe", Base: 0x10000000, Size: 0x10000, DebugID: "65ae75631"}}, Frames: []Frame{{Instruction: 0x10001065, Module: "program.exe"}}}
	Symbolicate(&crash, tables)
	if crash.Frames[0].Function != "LegacyThunk" {
		t.Fatalf("C7 thunk frame=%+v", crash.Frames[0])
	}
}

func TestLegacyThunkSharesProcedureBudget(t *testing.T) {
	data := nativepdb.WithLegacyThunk().Data
	name := bytes.Index(data, []byte("LegacyThunk"))
	record := data[name-26 : name+len("LegacyThunk")]
	sections := []pdbSection{{virtualAddr: 0x1000, virtualSize: 0x100, rawSize: 0x100, flags: pdbPESectionExecute}}
	used := maxPDBProcedures - 1
	if result, ok := parsePDBLegacyThunks(record, sections, &used); !ok || len(result) != 1 || used != maxPDBProcedures {
		t.Fatal("last available procedure slot was not accepted")
	}
	if result, ok := parsePDBLegacyThunks(record, sections, &used); ok || len(result) != 0 {
		t.Fatal("thunk exceeded the shared procedure budget")
	}
}
