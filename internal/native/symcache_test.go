package native

import (
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativesymcache"
)

func TestSymCacheVersionsSymbolicateByEmbeddedDebugID(t *testing.T) {
	for _, test := range []struct {
		name           string
		version        uint32
		variableHeader uint16
	}{
		{name: "v7", version: 7},
		{name: "v8", version: 8},
		{name: "v9 without variables", version: 9},
		{name: "v9 with current variable header", version: 9, variableHeader: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := nativesymcache.Build(test.version, test.variableHeader)
			objects := ParseDebugObjects("cached.symcache", fixture.Data)
			if len(objects) != 1 {
				t.Fatalf("parsed SymCache objects=%d, want one", len(objects))
			}
			object := objects[0]
			if object.DebugID != "00112233445566778899aabbccddeeff1" {
				t.Fatalf("embedded DebugID=%q", object.DebugID)
			}
			if len(object.Functions) != 1 || object.Functions[0] != (SymbolFunction{
				Start: 0x1000, End: 0x1020, Name: "CrashFunction", File: `C:\project\src\crash.c`, Line: 42,
			}) {
				t.Fatalf("SymCache ranges=%+v", object.Functions)
			}

			crash := Crash{
				Modules: []Module{{Name: "renamed.exe", Base: 0x400000, Size: 0x2000, DebugID: "{00112233-4455-6677-8899-AABBCCDDEEFF}1"}},
				Frames:  []Frame{{Module: "renamed.exe", Instruction: 0x401015}},
			}
			Symbolicate(&crash, SymbolSet{object.DebugID: object.Functions})
			frame := crash.Frames[0]
			if frame.Function != "CrashFunction" || frame.File != `C:\project\src\crash.c` || frame.Line != 42 {
				t.Fatalf("SymCache frame=%+v", frame)
			}
		})
	}
}

func TestSymCachePDB20DebugIDUsesTimestampAndAge(t *testing.T) {
	fixture := nativesymcache.Build(9, 0)
	for i := 0; i < 16; i++ {
		fixture.Data[8+i] = 0
	}
	binary.BigEndian.PutUint32(fixture.Data[8:], 0x12345678)
	fixture.Data[8+31] = 1

	objects := ParseDebugObjects("cached.symcache", fixture.Data)
	if len(objects) != 1 || objects[0].DebugID != "123456781" {
		t.Fatalf("PDB 2.0 SymCache DebugID=%+v", objects)
	}
}

func TestSymCacheStringCountIsBounded(t *testing.T) {
	fixture := nativesymcache.Build(8, 0)
	stringCount := maxSymCacheEntries + 1
	data := append([]byte(nil), fixture.Data[:fixture.StringsOffset]...)
	data = append(data, make([]byte, stringCount)...)
	binary.LittleEndian.PutUint32(data[8+52:], uint32(stringCount))
	if objects := ParseDebugObjects("too-many-strings.symcache", data); len(objects) != 0 {
		t.Fatalf("over-budget SymCache string table returned objects: %+v", objects)
	}
}

func TestMalformedSymCacheReturnsNoPartialSymbols(t *testing.T) {
	tests := []struct {
		name           string
		version        uint32
		variableHeader uint16
		mutate         func([]byte, nativesymcache.Fixture)
	}{
		{name: "magic", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { copy(data, "NOPE") }},
		{name: "wrong endianness", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { copy(data, "CMYS") }},
		{name: "unsupported version", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { binary.LittleEndian.PutUint32(data[4:], 10) }},
		{name: "truncated string table", version: 7, mutate: func(data []byte, _ nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[8+52:], uint32(len(data)))
		}},
		{name: "over-budget file count", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { binary.LittleEndian.PutUint32(data[8+36:], 1_000_001) }},
		{name: "invalid function string offset", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.FunctionsOffset:], ^uint32(0))
		}},
		{name: "invalid file reference", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.SourceLocationsOffset+16:], 9)
		}},
		{name: "invalid function reference", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.SourceLocationsOffset+16+8:], 9)
		}},
		{name: "inline cycle", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.SourceLocationsOffset+12:], 1)
			binary.LittleEndian.PutUint32(data[fixture.SourceLocationsOffset+16+12:], 0)
		}},
		{name: "duplicate ranges", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.RangesOffset+4:], 0x1000)
		}},
		{name: "unsorted ranges", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.RangesOffset:], 0x1030)
			binary.LittleEndian.PutUint32(data[fixture.RangesOffset+4:], 0x1020)
		}},
		{name: "invalid v7 string length", version: 7, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.StringsOffset:], ^uint32(0))
		}},
		{name: "invalid v8 leb128 string length", version: 8, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			copy(data[fixture.StringsOffset:fixture.StringsOffset+5], []byte{0x80, 0x80, 0x80, 0x80, 0x80})
		}},
		{name: "invalid utf-8 string", version: 9, mutate: func(data []byte, fixture nativesymcache.Fixture) { data[fixture.FirstStringContentOffset] = 0xff }},
		{name: "unsupported DebugId encoding", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { data[8+31] = 2 }},
		{name: "nonzero DebugId padding", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { data[8+20] = 1 }},
		{name: "unsupported v9 variable header", version: 9, mutate: func(data []byte, _ nativesymcache.Fixture) { binary.LittleEndian.PutUint16(data[8+56:], 2) }},
		{name: "truncated v9 variable table", version: 9, variableHeader: 1, mutate: func(data []byte, fixture nativesymcache.Fixture) {
			binary.LittleEndian.PutUint32(data[fixture.VariableHeaderOffset:], 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := nativesymcache.Build(test.version, test.variableHeader)
			if objects := ParseDebugObjects("valid.symcache", fixture.Data); len(objects) != 1 {
				t.Fatalf("baseline SymCache fixture was not valid: %d objects", len(objects))
			}
			malformed := append([]byte(nil), fixture.Data...)
			test.mutate(malformed, fixture)
			if objects := ParseDebugObjects("invalid.symcache", malformed); len(objects) != 0 {
				t.Fatalf("malformed SymCache returned partial symbols: %+v", objects)
			}
		})
	}
}
