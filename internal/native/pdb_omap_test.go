package native

import (
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativepdb"
)

func TestPDBOMAPReorderedSourceAndInlineFrames(t *testing.T) {
	f := nativepdb.WithOMAPInlineFrames()
	objects := ParseDebugObjects("crash.pdb", f.Data)
	if len(objects) != 1 {
		t.Fatalf("objects=%d, want one", len(objects))
	}
	crash := Crash{
		Modules: []Module{{Name: "crash.exe", Base: 0x100000, Size: 0x5000, DebugID: objects[0].DebugID}},
		Frames: []Frame{
			{Instruction: 0x102045, Module: "crash.exe"},
			{Instruction: 0x102024, Module: "crash.exe"},
			{Instruction: 0x102038, Module: "crash.exe"},
			{Instruction: 0x101015, Module: "crash.exe"},
		},
	}
	Symbolicate(&crash, SymbolSet{objects[0].DebugID: objects[0].Functions})
	first := crash.Frames[0]
	if first.Function != "CrashFunction" || first.File != `C:\src\crash.cc` || first.Line != 42 || len(first.InlineFrames) != 3 || first.InlineFrames[2].Function != "DeepInline" {
		t.Fatalf("mapped source and nested inline frames=%+v", first)
	}
	if crash.Frames[1].Function != "CrashFunction" || len(crash.Frames[1].InlineFrames) != 1 || crash.Frames[1].InlineFrames[0].Function != "OuterInline" {
		t.Fatalf("reordered fragment=%+v", crash.Frames[1])
	}
	for _, frame := range crash.Frames[2:] {
		if frame.Function != "" || len(frame.InlineFrames) != 0 {
			t.Fatalf("unmapped address resolved: %+v", frame)
		}
	}
}

func TestPDBOMAPRejectsInvalidMaps(t *testing.T) {
	f := nativepdb.WithOMAPInlineFrames()
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"duplicate source", func(b []byte) { binary.LittleEndian.PutUint32(b[f.OMAPFromBlock*512+8:], 0x1010) }},
		{"decreasing reverse source", func(b []byte) { binary.LittleEndian.PutUint32(b[f.OMAPToBlock*512+8:], 0x2010) }},
		{"mapped outside executable section", func(b []byte) { binary.LittleEndian.PutUint32(b[f.OMAPFromBlock*512+4:], 0x4000) }},
		{"target address overflow", func(b []byte) { binary.LittleEndian.PutUint32(b[f.OMAPFromBlock*512+4:], 0xfffffffc) }},
		{"missing map", func(b []byte) { binary.LittleEndian.PutUint16(b[f.OMAPHeaderOffset+8:], 0xffff) }},
		{"misaligned map length", func(b []byte) { binary.LittleEndian.PutUint32(b[3*512+4+7*4:], 31) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), f.Data...)
			tc.mutate(data)
			if objects := ParseDebugObjects("corrupt.pdb", data); len(objects) != 0 {
				t.Fatalf("invalid map produced %d objects", len(objects))
			}
		})
	}
}

func TestPDBOMAPLimitsRecordAndIntervalWork(t *testing.T) {
	if _, ok := parsePDBOMAP(make([]byte, (maxPDBOMAPRecords+1)*8)); ok {
		t.Fatal("oversized OMAP accepted")
	}
	entries := make([]pdbOMAPEntry, 1024)
	for i := range entries {
		entries[i].source = uint32(i)
	}
	functions := make([]SymbolFunction, maxPDBOMAPWork/len(entries)+1)
	for i := range functions {
		functions[i].End = uint64(len(entries))
	}
	if _, ok := mapPDBFunctions(functions, entries, nil); ok {
		t.Fatal("excessive interval work accepted")
	}
}

func TestPDBOMAPLimitsMappedFragments(t *testing.T) {
	entries := make([]pdbOMAPEntry, 1000)
	for i := range entries {
		entries[i] = pdbOMAPEntry{source: uint32(i), target: 0x1000}
	}
	functions := make([]SymbolFunction, maxPDBOMAPFragments/len(entries)+1)
	for i := range functions {
		functions[i].End = uint64(len(entries))
	}
	sections := []pdbSection{{virtualAddr: 0x1000, virtualSize: 0x100, flags: pdbPESectionExecute}}
	if _, ok := mapPDBFunctions(functions, entries, sections); ok {
		t.Fatal("excessive mapped fragments accepted")
	}
}

func TestPDBOMAPInlineProcedureAtOriginalZero(t *testing.T) {
	sites := &pdbInlineSiteSet{sites: []pdbInlineSite{{parent: -1, procedureStart: 0, name: "Inline", depth: 1, ranges: []pdbInlineRange{{start: 0, end: 8, line: 9}}}}}
	functions := []SymbolFunction{{Start: 0x2000, End: 0x2008, Name: "Outer", inlineSites: sites, omapOrigin: 0, omapMapped: true}}
	crash := Crash{Modules: []Module{{Name: "zero.exe", Base: 0x100000, Size: 0x5000}}, Frames: []Frame{{Module: "zero.exe", Instruction: 0x102001}}}
	Symbolicate(&crash, SymbolSet{"zero.exe": functions})
	if got := crash.Frames[0].InlineFrames; len(got) != 1 || got[0].Function != "Inline" {
		t.Fatalf("inline frame at original address zero: %+v", got)
	}
}
