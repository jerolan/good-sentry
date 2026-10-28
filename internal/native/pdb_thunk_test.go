package native

import (
	"bytes"
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativepdb"
)

func TestLegacyThunkSymbolication(t *testing.T) {
	original := nativepdb.WithLegacyThunk().Data
	variant := nativepdb.WithLegacyThunkVariant().Data
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
	mutateVariant := func(edit func([]byte)) []byte {
		data := append([]byte(nil), variant...)
		position := bytes.Index(data, []byte("LegacyThunk")) - 26
		edit(data[position:])
		return data
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		want   string
		reject bool
	}{
		{"ordinal zero", original, "LegacyThunk", false},
		{"nonzero ordinal with variant", variant, "LegacyThunk", false},
		{"first nonzero ordinal", mutateVariant(func(r []byte) { r[24] = 1 }), "LegacyThunk", false},
		{"vcall ordinal", mutateVariant(func(r []byte) { r[24] = 2 }), "LegacyThunk", false},
		{"unknown-load ordinal", mutateVariant(func(r []byte) { r[24] = 4 }), "LegacyThunk", false},
		{"incremental ordinal", mutateVariant(func(r []byte) { r[24] = 5 }), "LegacyThunk", false},
		{"last documented ordinal", mutateVariant(func(r []byte) { r[24] = 6 }), "LegacyThunk", false},
		{"variant with invalid name", mutateVariant(func(r []byte) { r[25] = 255 }), "", true},
		{"variant with invalid range", mutateVariant(func(r []byte) { binary.LittleEndian.PutUint32(r[16:], 0xff) }), "", true},
		{"unsupported ordinal", mutate(func(r []byte) { r[24] = 7 }), "", false},
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

func TestC7LegacyThunkVariantUsesCompanionPE(t *testing.T) {
	pdb := nativepdb.OldDBIC7WithThunkVariant().Data
	pe := nativepdb.V50CompanionPE(0x65ae7563, 1).Data
	tables, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "old.pdb", Content: pdb}, {Name: "old.exe", Content: pe}})
	crash := Crash{Modules: []Module{{Name: "program.exe", Base: 0x10000000, Size: 0x10000, DebugID: "65ae75631"}}, Frames: []Frame{{Instruction: 0x10001065, Module: "program.exe"}}}
	Symbolicate(&crash, tables)
	if crash.Frames[0].Function != "LegacyThunk" {
		t.Fatalf("C7 thunk variant frame=%+v", crash.Frames[0])
	}
}

func TestLegacyThunkSharesProcedureBudget(t *testing.T) {
	data := nativepdb.WithLegacyThunk().Data
	name := bytes.Index(data, []byte("LegacyThunk"))
	record := data[name-26 : name+len("LegacyThunk")]
	sections := []pdbSection{{virtualAddr: 0x1000, virtualSize: 0x100, rawSize: 0x100, flags: pdbPESectionExecute}}
	used := maxPDBProcedures - 1
	if result, ok := parsePDBThunks(record, sections, &used, 2); !ok || len(result) != 1 || used != maxPDBProcedures {
		t.Fatal("last available procedure slot was not accepted")
	}
	if result, ok := parsePDBThunks(record, sections, &used, 2); ok || len(result) != 0 {
		t.Fatal("thunk exceeded the shared procedure budget")
	}
}

func TestModernThunkSymbolicationAndSourcePreservation(t *testing.T) {
	original := nativepdb.WithModernThunk().Data
	name := bytes.Index(original, []byte("ModernThunk"))
	if name < 25 {
		t.Fatal("fixture thunk missing")
	}
	offset := name - 25
	mutate := func(edit func([]byte)) []byte { b := append([]byte(nil), original...); edit(b[offset:]); return b }
	for _, tc := range []struct {
		name   string
		data   []byte
		want   string
		reject bool
	}{
		{"pcode ordinal and opaque variant", original, "ModernThunk", false},
		{"standard ordinal", mutate(func(r []byte) { r[24] = 0 }), "ModernThunk", false},
		{"branch island ordinal", mutate(func(r []byte) { r[24] = 6 }), "ModernThunk", false},
		{"unknown ordinal", mutate(func(r []byte) { r[24] = 7 }), "", false},
		{"unterminated name", mutate(func(r []byte) { r[25+len("ModernThunk")] = 0xff }), "", true},
		{"invalid section", mutate(func(r []byte) { binary.LittleEndian.PutUint16(r[20:], 2) }), "", true},
		{"range outside section", mutate(func(r []byte) { binary.LittleEndian.PutUint32(r[16:], 0xff) }), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := ParseDebugObjects("modern.pdb", tc.data)
			if tc.reject {
				if len(objects) != 0 {
					t.Fatal("invalid thunk returned partial symbols")
				}
				return
			}
			if len(objects) != 1 {
				t.Fatal("valid PDB rejected")
			}
			var lineFound bool
			for _, f := range objects[0].Functions {
				if f.Name == "CrashFunction" && f.File == `C:\src\crash.cc` && f.Line == 42 {
					lineFound = true
				}
			}
			if !lineFound {
				t.Fatal("existing procedure source line lost")
			}
			tables, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "modern.pdb", Content: tc.data}})
			crash := Crash{Modules: []Module{{Name: "program.exe", Base: 0x10000000, Size: 0x10000, DebugID: "5a9832e5287241c1838ed98914e9b7ff1"}}, Frames: []Frame{{Instruction: 0x10001062, Module: "program.exe"}}}
			Symbolicate(&crash, tables)
			if crash.Frames[0].Function != tc.want {
				t.Fatalf("frame=%+v want=%q", crash.Frames[0], tc.want)
			}
		})
	}
}

func TestModernThunkSharesProcedureBudget(t *testing.T) {
	data := nativepdb.WithModernThunk().Data
	name := bytes.Index(data, []byte("ModernThunk"))
	record := data[name-25 : name+len("ModernThunk")+3]
	sections := []pdbSection{{virtualAddr: 0x1000, virtualSize: 0x100, rawSize: 0x100, flags: pdbPESectionExecute}}
	used := maxPDBProcedures - 1
	if result, ok := parsePDBThunks(record, sections, &used, 4); !ok || len(result) != 1 || used != maxPDBProcedures {
		t.Fatal("last procedure slot rejected a modern thunk")
	}
	if result, ok := parsePDBThunks(record, sections, &used, 4); ok || len(result) != 0 {
		t.Fatal("modern thunk exceeded the shared procedure budget")
	}
}
