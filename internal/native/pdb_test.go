package native

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"goodsentry/internal/testfixture/nativepdb"
)

func TestParseNativePDBProcedureSymbols(t *testing.T) {
	data := nativepdb.Minimal().Data
	type pdbCase struct {
		name    string
		data    []byte
		debugID string
	}
	archives := []pdbCase{
		{name: "standalone", data: data},
		{name: "standalone with odd-sized EC substream", data: nativepdb.WithECSubstream().Data},
		{name: "MSF7 obsolete stream zero reuses Info page", data: nativepdb.WithStreamZeroAlias().Data},
		{name: "tar", data: pdbTARDebugArchive(t, data)},
		{name: "tar.gz", data: pdbGzipDebugArchive(t, pdbTARDebugArchive(t, data))},
	}
	var zipBytes bytes.Buffer
	zipWriter := zip.NewWriter(&zipBytes)
	entry, err := zipWriter.Create("crash.pdb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	archives = append(archives, pdbCase{name: "zip", data: zipBytes.Bytes()})
	for _, legacy := range []struct {
		pageSize int
		age      uint32
		alias    bool
	}{
		{pageSize: 1024, age: 1},
		{pageSize: 2048, age: 1},
		{pageSize: 4096, age: 1},
		{pageSize: 1024, age: 0},
		{pageSize: 1024, age: 1, alias: true},
	} {
		archives = append(archives, pdbCase{
			name:    fmt.Sprintf("Small MSF2 page%d age%d stream0alias%t", legacy.pageSize, legacy.age, legacy.alias),
			data:    nativepdb.SmallMSF2(legacy.pageSize, legacy.age, legacy.alias).Data,
			debugID: fmt.Sprintf("65ae7563%x", legacy.age),
		})
	}
	wantID := DebugKey("5A9832E5287241C1838ED98914E9B7FF1")
	want := []SymbolFunction{
		{Start: 0x1010, End: 0x1030, Name: "CrashFunction", Line: -1},
		{Start: 0x1080, End: 0x1090, Name: "OtherFunction", Line: -1},
	}
	for _, archive := range archives {
		t.Run(archive.name, func(t *testing.T) {
			wantID := wantID
			if archive.debugID != "" {
				wantID = archive.debugID
			}
			objects := ParseDebugObjects("crash.pdb", archive.data)
			if len(objects) != 1 {
				t.Fatalf("parsed PDB objects=%d, want one", len(objects))
			}
			object := objects[0]
			if object.Name != "crash.pdb" || object.DebugID != wantID {
				t.Fatalf("PDB identity name=%q debugID=%q", object.Name, object.DebugID)
			}
			if len(object.Functions) != len(want) {
				t.Fatalf("PDB functions=%+v", object.Functions)
			}
			for i, function := range object.Functions {
				if function != want[i] {
					t.Fatalf("PDB function[%d]=%+v, want %+v", i, function, want[i])
				}
			}
			if archive.debugID != "" {
				for _, metadata := range []struct {
					name, debugID, wantFunction string
				}{
					{name: "matching multipart identity", debugID: wantID, wantFunction: "CrashFunction"},
					{name: "mismatching multipart identity", debugID: wantID + "f"},
				} {
					t.Run(metadata.name, func(t *testing.T) {
						symbols, _ := BuildSymbolTables([]DebugSymbolFile{{Name: "uploaded.pdb", DebugID: metadata.debugID, Content: archive.data}})
						crash := Crash{
							Modules: []Module{{Name: "renamed.exe", Base: 0x140000000, Size: 0x10000, DebugID: wantID}},
							Frames:  []Frame{{Instruction: 0x140001015, Module: "renamed.exe"}},
						}
						Symbolicate(&crash, symbols)
						if crash.Frames[0].Function != metadata.wantFunction {
							t.Fatalf("legacy identity returned frame %+v, want function %q", crash.Frames[0], metadata.wantFunction)
						}
					})
				}
			}
		})
	}
	objects := ParseDebugObjects("crash.pdb", data)
	if len(objects) != 1 {
		t.Fatalf("PDB objects=%d, want one for identity check", len(objects))
	}
	object := objects[0]
	crash := Crash{
		Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x10000, DebugID: "5A9832E5287241C1838ED98914E9B7FF1"}},
		Frames:  []Frame{{Instruction: 0x140001015, Module: "crash.exe"}},
	}
	Symbolicate(&crash, SymbolSet{object.DebugID: object.Functions})
	if crash.Frames[0].Function != "CrashFunction" {
		t.Fatalf("matching PDB identity did not symbolize frame: %+v", crash.Frames[0])
	}
	crash.Modules[0].DebugID = "5A9832E5287241C1838ED98914E9B7FF2"
	crash.Frames[0].Function = ""
	Symbolicate(&crash, SymbolSet{object.DebugID: object.Functions})
	if crash.Frames[0].Function != "" {
		t.Fatalf("mismatched PDB identity symbolized frame: %+v", crash.Frames[0])
	}
}

func TestParseNativePDBPublicSymbolsFromStrippedPDB(t *testing.T) {
	fixture := nativepdb.WithPublicOnlySymbols()
	objects := ParseDebugObjects("stripped.pdb", fixture.Data)
	if len(objects) != 1 {
		t.Fatalf("parsed PDB objects=%d, want one", len(objects))
	}
	object := objects[0]
	want := []SymbolFunction{
		{Start: 0x1030, End: 0x1050, Name: "PublicFirst", Line: -1, publicFallback: true},
		{Start: 0x1050, End: 0x1080, Name: "PublicSecond", Line: -1, publicFallback: true},
	}
	if len(object.Functions) != len(want) {
		t.Fatalf("public PDB functions=%+v, want %+v", object.Functions, want)
	}
	for i := range want {
		if object.Functions[i] != want[i] {
			t.Fatalf("public PDB function[%d]=%+v, want %+v", i, object.Functions[i], want[i])
		}
	}

	crash := Crash{
		Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x5000, DebugID: object.DebugID}},
		Frames: []Frame{
			{Instruction: 0x140001035, Module: "crash.exe"},
			{Instruction: 0x14000107f, Module: "crash.exe"},
			{Instruction: 0x140001080, Module: "crash.exe"},
			{Instruction: 0x14000102f, Module: "crash.exe"},
		},
	}
	Symbolicate(&crash, SymbolSet{object.DebugID: object.Functions})
	for i, wantName := range []string{"PublicFirst", "PublicSecond", "", ""} {
		if crash.Frames[i].Function != wantName {
			t.Fatalf("public PDB frame[%d]=%+v, want function %q", i, crash.Frames[i], wantName)
		}
	}
	crash.Modules[0].DebugID = "different-pdb-age"
	crash.Frames[0].Function = ""
	Symbolicate(&crash, SymbolSet{object.DebugID: object.Functions})
	if crash.Frames[0].Function != "" {
		t.Fatalf("mismatched public PDB identity symbolized frame: %+v", crash.Frames[0])
	}

	unstripped := append([]byte(nil), fixture.Data...)
	binary.LittleEndian.PutUint16(unstripped[fixture.DBIBlock*512+56:], 0)
	if objects := ParseDebugObjects("unstripped.pdb", unstripped); len(objects) != 0 {
		t.Fatalf("unstripped PDB without module procedures unexpectedly used public fallback: %+v", objects)
	}
}

func TestParseNativePDBPublicSymbolsRejectsMalformedDataWithoutPartialSymbols(t *testing.T) {
	fixture := nativepdb.WithPublicOnlySymbols()
	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "truncated global symbol record", mutate: func(data []byte) {
			binary.LittleEndian.PutUint16(data[fixture.PublicSymbolsBlock*512:], 0xfffc)
		}},
		{name: "invalid segment index", mutate: func(data []byte) {
			binary.LittleEndian.PutUint16(data[fixture.PublicSymbolsBlock*512+12:], 3)
		}},
		{name: "invalid alignment byte", mutate: func(data []byte) {
			data[fixture.PublicSymbolsBlock*512+26] = 0
		}},
		{name: "unknown public flags", mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[fixture.PublicSymbolsBlock*512+4:], 0x80000000)
		}},
		{name: "mapping outside executable section", mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[fixture.SectionMapStart+4+12:], 0x80)
		}},
		{name: "selector mapping", mutate: func(data []byte) {
			flags := binary.LittleEndian.Uint16(data[fixture.SectionMapStart+4:])
			binary.LittleEndian.PutUint16(data[fixture.SectionMapStart+4:], flags|1<<8)
		}},
		{name: "absolute mapping", mutate: func(data []byte) {
			flags := binary.LittleEndian.Uint16(data[fixture.SectionMapStart+4:])
			binary.LittleEndian.PutUint16(data[fixture.SectionMapStart+4:], flags|1<<9)
		}},
		{name: "group mapping", mutate: func(data []byte) {
			flags := binary.LittleEndian.Uint16(data[fixture.SectionMapStart+4:])
			binary.LittleEndian.PutUint16(data[fixture.SectionMapStart+4:], flags|1<<10)
		}},
		{name: "frame outside PE sections", mutate: func(data []byte) {
			binary.LittleEndian.PutUint16(data[fixture.SectionMapStart+4+6:], 3)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte(nil), fixture.Data...)
			test.mutate(data)
			if objects := ParseDebugObjects("stripped.pdb", data); len(objects) != 0 {
				t.Fatalf("malformed public PDB returned partial symbols: %+v", objects)
			}
		})
	}
}

func TestParseNativePDBPublicSymbolsRejectsExcessiveAlignmentPadding(t *testing.T) {
	// The name is already aligned; appending F1-F4 forms a valid padding prefix
	// followed by a fourth byte that exceeds CodeView's three-byte limit.
	record := []byte{18, 0, 0x0e, 0x11, 3, 0, 0, 0, 0, 0, 0, 0, 1, 0, 'x', 0, 0xf1, 0xf2, 0xf3, 0xf4}
	sectionMap := make([]byte, 4+20)
	binary.LittleEndian.PutUint16(sectionMap, 1)
	binary.LittleEndian.PutUint16(sectionMap[2:], 1)
	binary.LittleEndian.PutUint16(sectionMap[4:], 1<<2)
	binary.LittleEndian.PutUint16(sectionMap[10:], 1)
	binary.LittleEndian.PutUint32(sectionMap[20:], 0x80)
	sections := []pdbSection{{virtualSize: 0x80, virtualAddr: 0x1000, rawSize: 0x80, flags: 0x60000020}}

	functions, ok := parsePDBPublicFunctions(pdbMSFWithGlobalSymbols(record), 0, sectionMap, sections)
	if ok || len(functions) != 0 {
		t.Fatalf("PDB with four alignment bytes returned ok=%t functions=%d", ok, len(functions))
	}
}

func TestSymbolicatePrefersExactPDBProcedureToPublicFallback(t *testing.T) {
	module := Module{Name: "crash.exe", Base: 0x140000000, Size: 0x5000, DebugID: "pdb-id"}
	for _, functions := range [][]SymbolFunction{
		{
			{Start: 0x1020, End: 0x1040, Name: "PublicFallback", Line: -1, publicFallback: true},
			{Start: 0x1000, End: 0x1100, Name: "PrivateProcedure", Line: -1},
		},
		{
			{Start: 0x1000, End: 0x1100, Name: "PrivateProcedure", Line: -1},
			{Start: 0x1020, End: 0x1040, Name: "PublicFallback", Line: -1, publicFallback: true},
		},
	} {
		crash := Crash{Modules: []Module{module}, Frames: []Frame{{Instruction: module.Base + 0x1025, Module: module.Name}}}
		Symbolicate(&crash, SymbolSet{"pdbid": functions})
		if crash.Frames[0].Function != "PrivateProcedure" {
			t.Fatalf("exact PDB procedure lost to public fallback: %+v", crash.Frames[0])
		}
	}
}

func TestParseNativePDBPublicSymbolsEnforcesStreamBudgets(t *testing.T) {
	sectionMap := make([]byte, 4+20)
	binary.LittleEndian.PutUint16(sectionMap, 1)
	binary.LittleEndian.PutUint16(sectionMap[2:], 1)
	binary.LittleEndian.PutUint16(sectionMap[4:], 1<<2)
	binary.LittleEndian.PutUint16(sectionMap[10:], 1)
	binary.LittleEndian.PutUint32(sectionMap[20:], 0x80)
	sections := []pdbSection{{virtualSize: 0x80, virtualAddr: 0x1000, rawSize: 0x80, flags: 0x60000020}}
	tests := []struct {
		name    string
		records []byte
	}{
		{name: "global symbol record limit", records: bytes.Repeat([]byte{2, 0, 0, 0}, maxPDBSymbolRecords+1)},
		{name: "public function limit", records: bytes.Repeat([]byte{14, 0, 0x0e, 0x11, 3, 0, 0, 0, 0, 0, 0, 0, 1, 0, 'x', 0}, maxPDBPublicFunctions+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			msf := pdbMSFWithGlobalSymbols(test.records)
			functions, ok := parsePDBPublicFunctions(msf, 0, sectionMap, sections)
			if ok || len(functions) != 0 {
				t.Fatalf("over-budget PDB returned ok=%t functions=%d", ok, len(functions))
			}
		})
	}
}

func pdbMSFWithGlobalSymbols(records []byte) *pdbMSFFile {
	const blockSize = 512
	blockCount := (len(records) + blockSize - 1) / blockSize
	data := make([]byte, blockCount*blockSize)
	copy(data, records)
	blocks := make([]uint32, blockCount)
	for i := range blocks {
		blocks[i] = uint32(i)
	}
	return &pdbMSFFile{
		data: data, blockSize: blockSize,
		streams: []pdbMSFStream{{size: uint32(len(records)), blocks: blocks}},
	}
}

func TestParseNativePDBSourceLines(t *testing.T) {
	type sourceFrame struct {
		offset   uint64
		function string
		file     string
		line     int
	}
	modern := []sourceFrame{
		{offset: 0x1015, function: "CrashFunction", file: `C:\src\crash.cc`, line: 42},
		{offset: 0x1025, function: "CrashFunction", file: `C:\src\crash.cc`, line: 43},
	}
	c11 := nativepdb.WithC11SourceLines()
	nonASCII := append([]byte(nil), c11.Data...)
	firstFile := c11.C11Start + int(binary.LittleEndian.Uint32(nonASCII[c11.C11Start+4:]))
	nonASCII[firstFile+16+1+7] = 0xe9
	for _, test := range []struct {
		name           string
		data           []byte
		debugID        string
		frames         []sourceFrame
		duplicateLines []int
	}{
		{name: "C13 module string table", data: nativepdb.WithSourceLines(true).Data, debugID: "5a9832e5287241c1838ed98914e9b7ff1", frames: modern},
		{name: "C13 global PDB names stream", data: nativepdb.WithSourceLines(false).Data, debugID: "5a9832e5287241c1838ed98914e9b7ff1", frames: modern},
		{name: "C11 inclusive spans duplicate policy and gaps", data: c11.Data, debugID: "65ae75631", duplicateLines: []int{42, 43}, frames: []sourceFrame{
			{offset: 0x1010, function: "CrashFunction", file: `C:\src\first.cc`, line: 42},
			{offset: 0x1013, function: "CrashFunction", file: `C:\src\first.cc`, line: 42},
			{offset: 0x1014, function: "CrashFunction", file: `C:\src\first.cc`, line: 65535},
			{offset: 0x1017, function: "CrashFunction", file: `C:\src\first.cc`, line: 65535},
			{offset: 0x1018, function: "CrashFunction", line: -1},
			{offset: 0x101b, function: "CrashFunction", line: -1},
			{offset: 0x101c, function: "CrashFunction", file: `C:\src\first.cc`, line: 44},
			{offset: 0x101f, function: "CrashFunction", file: `C:\src\first.cc`, line: 44},
			{offset: 0x1020, function: "CrashFunction", line: -1},
			{offset: 0x1027, function: "CrashFunction", line: -1},
			{offset: 0x1028, function: "CrashFunction", file: `C:\src\second.cc`, line: 52},
			{offset: 0x102f, function: "CrashFunction", file: `C:\src\second.cc`, line: 52},
			{offset: 0x1030},
			{offset: 0x1085, function: "OtherFunction", line: -1},
		}},
		{name: "C11 non ASCII ST path keeps procedures and other files", data: nonASCII, debugID: "65ae75631", frames: []sourceFrame{
			{offset: 0x1010, function: "CrashFunction", line: -1},
			{offset: 0x101f, function: "CrashFunction", line: -1},
			{offset: 0x1028, function: "CrashFunction", file: `C:\src\second.cc`, line: 52},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := ParseDebugObjects("crash.pdb", test.data)
			if len(objects) != 1 {
				t.Fatalf("parsed PDB objects=%d, want one", len(objects))
			}
			if objects[0].DebugID != test.debugID {
				t.Fatalf("source PDB identity=%q, want %q", objects[0].DebugID, test.debugID)
			}
			if len(test.duplicateLines) > 0 {
				var lines []int
				for _, function := range objects[0].Functions {
					if function.Start == 0x1010 && function.End == 0x1014 && function.Name == "CrashFunction" && function.File == `C:\src\first.cc` {
						lines = append(lines, function.Line)
					}
				}
				if len(lines) != len(test.duplicateLines) {
					t.Fatalf("duplicate source lines=%v, want %v", lines, test.duplicateLines)
				}
				for i, line := range lines {
					if line != test.duplicateLines[i] {
						t.Fatalf("duplicate source lines=%v, want %v", lines, test.duplicateLines)
					}
				}
			}
			crash := Crash{
				Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x10000, DebugID: test.debugID}},
			}
			for _, frame := range test.frames {
				crash.Frames = append(crash.Frames, Frame{Instruction: 0x140000000 + frame.offset, Module: "crash.exe"})
			}
			Symbolicate(&crash, SymbolSet{objects[0].DebugID: objects[0].Functions})
			for i, want := range test.frames {
				frame := crash.Frames[i]
				if frame.Function != want.function || frame.File != want.file || frame.Line != want.line {
					t.Errorf("source frame at %#x=%+v, want %s %s:%d", want.offset, frame, want.function, want.file, want.line)
				}
			}
		})
	}
}

func TestParseNativePDBExpandsNestedInlineFrames(t *testing.T) {
	fixture := nativepdb.WithInlineFrames()
	objects := ParseDebugObjects("crash.pdb", fixture.Data)
	if len(objects) != 1 {
		t.Fatalf("parsed PDB objects=%d, want one", len(objects))
	}
	crash := Crash{
		Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x10000, DebugID: "5A9832E5287241C1838ED98914E9B7FF1"}},
		Frames:  []Frame{{Instruction: 0x140001015, Module: "crash.exe"}},
	}
	Symbolicate(&crash, SymbolSet{objects[0].DebugID: objects[0].Functions})
	frame := crash.Frames[0]
	if frame.Function != "CrashFunction" || frame.File != `C:\src\crash.cc` || frame.Line != 42 {
		t.Fatalf("physical frame=%+v", frame)
	}
	wantInline := []InlineFrame{
		{Function: "OuterInline", File: `C:\include\inline.hpp`, Line: 100},
		{Function: "InnerInline", File: `C:\include\inner.hpp`, Line: 200},
		{Function: "DeepInline", File: `C:\include\deep.hpp`, Line: 300},
	}
	if len(frame.InlineFrames) != len(wantInline) {
		t.Fatalf("inline frames=%+v, want %+v", frame.InlineFrames, wantInline)
	}
	for i, want := range wantInline {
		if frame.InlineFrames[i] != want {
			t.Fatalf("inline frame[%d]=%+v, want %+v", i, frame.InlineFrames[i], want)
		}
	}

	eventJSON, _, err := BuildEvent(crash, nil, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []struct {
						Instruction string `json:"instruction_addr"`
						Function    string `json:"function"`
						Filename    string `json:"filename"`
						Line        int    `json:"lineno"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		t.Fatal(err)
	}
	if len(event.Exception.Values) != 1 {
		t.Fatalf("exception values=%d", len(event.Exception.Values))
	}
	frames := event.Exception.Values[0].Stacktrace.Frames
	wantFunctions := []string{"DeepInline", "InnerInline", "OuterInline", "CrashFunction"}
	if len(frames) != len(wantFunctions) {
		t.Fatalf("event frames=%+v, want functions %v", frames, wantFunctions)
	}
	for i, name := range wantFunctions {
		if frames[i].Function != name || frames[i].Instruction != "0x140001015" {
			t.Fatalf("event frame[%d]=%+v, want %s at the captured instruction", i, frames[i], name)
		}
	}
	if frames[0].Filename != `C:\include\deep.hpp` || frames[0].Line != 300 || frames[3].Filename != `C:\src\crash.cc` || frames[3].Line != 42 {
		t.Fatalf("event inline/physical source locations=%+v", frames)
	}
}

func TestParseNativePDBInlineRejectsMalformedInputWithoutPartialSymbols(t *testing.T) {
	fixture := nativepdb.WithInlineFrames()
	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "missing IPI stream", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[fixture.IPIStreamSizeOffset:], ^uint32(0)) }},
		{name: "invalid procedure end pointer", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[fixture.ProcedureEndPointerOffset:], ^uint32(0)) }},
		{name: "procedure end kind mismatch", mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[fixture.ProcedureEndOffset+2:], 0x114f) }},
		{name: "inline parent pointer mismatch", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[fixture.InlineSiteOffset+4:], ^uint32(0)) }},
		{name: "inline end pointer escapes parent", mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[fixture.InlineSiteOffset+8:], binary.LittleEndian.Uint32(data[fixture.ProcedureEndPointerOffset:]))
		}},
		{name: "unknown inlinee ID", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[fixture.InlineSiteOffset+12:], 0x1099) }},
		{name: "missing inlinee checksum", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[fixture.InlineeLinesOffset+8:], ^uint32(0)) }},
		{name: "missing inlinee-lines subsection", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[fixture.InlineeLinesOffset-8:], 0x7777) }},
		{name: "unknown annotation opcode", mutate: func(data []byte) { data[fixture.InlineAnnotationOffset] = 0x0e }},
		{name: "truncated annotation operand", mutate: func(data []byte) { data[fixture.InlineAnnotationOffset+5] = 0xc0 }},
		{name: "unbalanced inline scope", mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[fixture.InlineEndOffset:], 0x0006) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corrupt := append([]byte(nil), fixture.Data...)
			test.mutate(corrupt)
			if objects := ParseDebugObjects("corrupt.pdb", corrupt); len(objects) != 0 {
				t.Fatalf("malformed inline PDB produced partial symbols: %+v", objects)
			}
		})
	}
}

func TestPDBInlineAnnotationsResolveLineAndFileChanges(t *testing.T) {
	annotations := []byte{
		1, 0, // code offset zero
		6, 2, // first line is one after the inlinee declaration
		5, 24, // next source file
		6, 2, // next source line is one more
		4, 4, // first range is four bytes
		1, 4, // second range starts four bytes later
		0,
	}
	checksums := map[uint32]string{0: "outer.hpp", 24: "inner.hpp"}
	ranges, ok := parsePDBInlineAnnotations(annotations, pdbInlinee{fileID: 0, line: 100}, checksums, 8, new(int))
	if !ok {
		t.Fatal("parser rejected valid line and file changes")
	}
	want := []pdbInlineRange{{start: 0, end: 4, file: "outer.hpp", line: 101}, {start: 4, end: 8, file: "inner.hpp", line: 102}}
	if len(ranges) != len(want) {
		t.Fatalf("inline ranges=%+v, want %+v", ranges, want)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("inline range[%d]=%+v, want %+v", i, ranges[i], want[i])
		}
	}

	combined, ok := parsePDBInlineAnnotations([]byte{6, 0, 12, 4, 0}, pdbInlinee{fileID: 0, line: 7}, checksums, 4, new(int))
	if !ok || len(combined) != 1 || combined[0] != (pdbInlineRange{start: 0, end: 4, file: "outer.hpp", line: 7}) {
		t.Fatalf("combined code length/offset annotation: ranges=%+v ok=%t", combined, ok)
	}

	implicitLine, ok := parsePDBInlineAnnotations([]byte{1, 0, 4, 4, 0}, pdbInlinee{fileID: 0, line: 7}, checksums, 4, new(int))
	if !ok || len(implicitLine) != 1 || implicitLine[0].line != 7 {
		t.Fatalf("implicit inlinee source line: ranges=%+v ok=%t", implicitLine, ok)
	}
}

func TestPDBInlineRangesCoverUsesAdjacentRangesAndRejectsGaps(t *testing.T) {
	parent := []pdbInlineRange{{start: 0, end: 4}, {start: 4, end: 8}, {start: 12, end: 16}}
	if !pdbInlineRangesCover(parent, []pdbInlineRange{{start: 1, end: 7}, {start: 12, end: 16}}) {
		t.Fatal("adjacent parent ranges should cover a child range")
	}
	if pdbInlineRangesCover(parent, []pdbInlineRange{{start: 1, end: 13}}) {
		t.Fatal("parent range gap should not cover a child range")
	}
	if pdbInlineRangesCover(parent, []pdbInlineRange{{start: 0, end: 0}}) {
		t.Fatal("empty child ranges must be rejected")
	}
}

func TestPDBC13InlineeLinesParsesExtraFilesAndRecordBudgets(t *testing.T) {
	data := make([]byte, 4+12+4+4)
	binary.LittleEndian.PutUint32(data, 1)
	binary.LittleEndian.PutUint32(data[4:], 0x1000)
	binary.LittleEndian.PutUint32(data[8:], 0)
	binary.LittleEndian.PutUint32(data[12:], 7)
	binary.LittleEndian.PutUint32(data[16:], 1)
	binary.LittleEndian.PutUint32(data[20:], 24)
	entries, ok := parsePDBC13InlineeLines(data, map[uint32]string{0: "a.hpp", 24: "b.hpp"}, new(int))
	if !ok || entries[0x1000] != (pdbInlinee{fileID: 0, line: 7}) {
		t.Fatalf("inlinee extra-file entry=%+v ok=%t", entries, ok)
	}
	if _, ok := parsePDBC13InlineeLines(data, map[uint32]string{0: "a.hpp", 24: "b.hpp"}, intPointer(maxPDBInlineRecords)); ok {
		t.Fatal("parser accepted inlinee records beyond the per-PDB budget")
	}
}

func TestPDBInlineParserEnforcesSiteDepthIPIAndAnnotationBudgets(t *testing.T) {
	fixture := nativepdb.Minimal()
	moduleOffset := fixture.ModuleBlock*512 + 4
	procedure := fixture.Data[moduleOffset : moduleOffset+fixture.FirstProcedureLength]
	sections := []pdbSection{{virtualSize: 0x100, virtualAddr: 0x1000, rawSize: 0x100, flags: 0x20000000}}
	siteRecord := func() []byte {
		body := make([]byte, 12)
		binary.LittleEndian.PutUint32(body[8:], 0x1000)
		return pdbTestCodeViewRecord(0x114d, body)
	}
	t.Run("site count", func(t *testing.T) {
		data := append(append([]byte(nil), procedure...), siteRecord()...)
		var functions []SymbolFunction
		procedures, records, siteCount := 0, 0, maxPDBInlineSites
		if parsePDBModuleSymbolsWithInlineSites(data, sections, &functions, &procedures, &records, new([]pdbInlineSite), &siteCount, 4) {
			t.Fatal("parser accepted more than the per-PDB inline-site budget")
		}
	})
	t.Run("nesting depth", func(t *testing.T) {
		depth := maxPDBInlineDepth + 1
		data := append([]byte(nil), procedure...)
		siteLength, endLength := len(siteRecord()), len(pdbTestCodeViewRecord(0x114e, nil))
		procedureEnd := len(procedure) + depth*(siteLength+endLength) + 4
		binary.LittleEndian.PutUint32(data[4:], 0)
		binary.LittleEndian.PutUint32(data[8:], uint32(procedureEnd))
		siteOffsets := make([]uint32, depth)
		for i := 0; i < depth; i++ {
			siteOffsets[i] = uint32(len(data) + 4)
			body := make([]byte, 12)
			if i == 0 {
				binary.LittleEndian.PutUint32(body, 4)
			} else {
				binary.LittleEndian.PutUint32(body, siteOffsets[i-1])
			}
			endOffset := len(procedure) + depth*siteLength + (depth-1-i)*endLength + 4
			binary.LittleEndian.PutUint32(body[4:], uint32(endOffset))
			binary.LittleEndian.PutUint32(body[8:], 0x1000)
			data = append(data, pdbTestCodeViewRecord(0x114d, body)...)
		}
		for i := depth - 1; i >= 0; i-- {
			data = append(data, pdbTestCodeViewRecord(0x114e, nil)...)
		}
		data = append(data, pdbTestCodeViewRecord(0x0006, nil)...)
		var functions []SymbolFunction
		var sites []pdbInlineSite
		procedures, records, siteCount := 0, 0, 0
		if parsePDBModuleSymbolsWithInlineSites(data, sections, &functions, &procedures, &records, &sites, &siteCount, 4) {
			t.Fatal("parser accepted inline nesting beyond the per-PDB limit")
		}
	})
	t.Run("IPI type records", func(t *testing.T) {
		data := make([]byte, 56)
		binary.LittleEndian.PutUint32(data[4:], 56)
		binary.LittleEndian.PutUint32(data[8:], 0x1000)
		binary.LittleEndian.PutUint32(data[12:], 0x1000+maxPDBInlineRecords+1)
		if _, ok := parsePDBInlineIDNames(data, new(int)); ok {
			t.Fatal("parser accepted IPI records beyond the per-PDB budget")
		}
	})
	t.Run("annotation operations", func(t *testing.T) {
		count := maxPDBInlineAnnots
		if _, ok := parsePDBInlineAnnotations([]byte{1, 0, 4, 1, 0}, pdbInlinee{fileID: 0, line: 1}, map[uint32]string{0: "a.hpp"}, 1, &count); ok {
			t.Fatal("parser accepted annotations beyond the per-PDB budget")
		}
	})
}

func intPointer(value int) *int {
	return &value
}

func pdbTestCodeViewRecord(kind uint16, body []byte) []byte {
	var record bytes.Buffer
	binary.Write(&record, binary.LittleEndian, uint16(2+len(body)))
	binary.Write(&record, binary.LittleEndian, kind)
	record.Write(body)
	return record.Bytes()
}

func TestParseNativePDBKeepsSourceLinesWithinTheirModule(t *testing.T) {
	for _, test := range []struct {
		name, debugID string
		data          []byte
	}{
		{name: "C13", debugID: "5a9832e5287241c1838ed98914e9b7ff1", data: nativepdb.WithMultipleSourceLineModules().Data},
		{name: "C11", debugID: "65ae75631", data: nativepdb.WithMultipleC11SourceLineModules().Data},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := ParseDebugObjects("crash.pdb", test.data)
			if len(objects) != 1 {
				t.Fatalf("parsed PDB objects=%d, want one", len(objects))
			}
			if objects[0].DebugID != test.debugID {
				t.Fatalf("multi-module identity=%q, want %q", objects[0].DebugID, test.debugID)
			}
			crash := Crash{
				Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x10000, DebugID: test.debugID}},
				Frames:  []Frame{{Instruction: 0x140001015, Module: "crash.exe"}, {Instruction: 0x140001045, Module: "crash.exe"}},
			}
			Symbolicate(&crash, SymbolSet{objects[0].DebugID: objects[0].Functions})
			want := []struct {
				function string
				line     int
			}{{"CrashFunction", 42}, {"OtherModuleFunction", 52}}
			for i, frame := range crash.Frames {
				if frame.Function != want[i].function || frame.File != `C:\src\crash.cc` || frame.Line != want[i].line {
					t.Errorf("multi-module source frame[%d]=%+v, want %s C:\\src\\crash.cc:%d", i, frame, want[i].function, want[i].line)
				}
			}
		})
	}
}

func TestParsePDBC13LineRangesStayWithinTheirFileBlocks(t *testing.T) {
	data := make([]byte, 52)
	binary.LittleEndian.PutUint32(data, 0x10)
	binary.LittleEndian.PutUint32(data[8:], 0x20)
	binary.LittleEndian.PutUint16(data[4:], 1)
	putBlock := func(offset int, nameIndex, codeOffset, line uint32) {
		binary.LittleEndian.PutUint32(data[offset:], nameIndex)
		binary.LittleEndian.PutUint32(data[offset+4:], 1)
		binary.LittleEndian.PutUint32(data[offset+8:], 20)
		binary.LittleEndian.PutUint32(data[offset+12:], codeOffset)
		binary.LittleEndian.PutUint32(data[offset+16:], line)
	}
	putBlock(12, 0, 0, 42)
	putBlock(32, 4, 0x10, 52)

	sections := []pdbSection{{virtualSize: 0x100, virtualAddr: 0x1000, rawSize: 0x100, flags: 0x20000000}}
	functions := newPDBFunctionIndex([]SymbolFunction{{Start: 0x1010, End: 0x1030, Name: "CrashFunction", Line: -1}})
	checksums := map[uint32]string{0: "first.cc", 4: "second.cc"}
	lineCount, mappingCount := 0, 0
	got, ok := parsePDBC13Lines(data, checksums, sections, functions, &lineCount, &mappingCount)
	if !ok {
		t.Fatal("parser rejected valid line blocks")
	}
	if len(got) != 2 || got[0].Start != 0x1010 || got[0].End != 0x1030 || got[0].File != "first.cc" || got[1].Start != 0x1020 || got[1].End != 0x1030 || got[1].File != "second.cc" {
		t.Fatalf("line blocks produced unexpected ranges: %+v", got)
	}
}

func TestParseNativePDBSourceLinesRejectsMalformedInputWithoutPartialSymbols(t *testing.T) {
	global := nativepdb.WithSourceLines(false)
	local := nativepdb.WithSourceLines(true)
	c11 := nativepdb.WithC11SourceLines()
	root := c11.C11Start
	firstFile := root + int(binary.LittleEndian.Uint32(c11.Data[root+4:]))
	secondFile := root + int(binary.LittleEndian.Uint32(c11.Data[root+8:]))
	firstTable := root + int(binary.LittleEndian.Uint32(c11.Data[firstFile+4:]))
	secondTable := root + int(binary.LittleEndian.Uint32(c11.Data[secondFile+4:]))
	multi := nativepdb.WithMultipleC11SourceLineModules()
	fallbackText := []byte("\nFUNC 10 10 0 UnexpectedFallback\n")
	tests := []struct {
		name    string
		fixture nativepdb.Fixture
		mutate  func([]byte)
	}{
		{name: "truncated subsection", fixture: local, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[local.LineSubsectionLength:], ^uint32(0)) }},
		{name: "missing checksum entry", fixture: local, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[local.LineNameIndexOffset:], 4) }},
		{name: "invalid line segment", fixture: local, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[local.LineSegmentOffset:], 2) }},
		{name: "line contribution exceeds section", fixture: local, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[local.LineCodeSizeOffset:], 0x1000) }},
		{name: "decreasing line offsets", fixture: local, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[local.FirstLineOffset:], 0x10)
			binary.LittleEndian.PutUint32(data[local.SecondLineOffset:], 0)
		}},
		{name: "missing global names stream", fixture: global, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[global.NamesStreamIndexOffset:], ^uint32(0)) }},
		{name: "missing source filename", fixture: global, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[global.C13Start+8+56:], ^uint32(0)) }},
		{name: "malformed MSF7 C13 cannot fall back to embedded Breakpad text", fixture: local, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[local.LineSubsectionLength:], ^uint32(0))
			copy(data[len(data)-64:], fallbackText)
		}},
		{name: "malformed MSF2 C11 cannot fall back to embedded Breakpad text", fixture: c11.Fixture, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[firstFile+4:], 0xfffffffc)
			copy(data[len(data)-64:], fallbackText)
		}},
		{name: "C11 extent unaligned", fixture: c11.Fixture, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[c11.DBIBlock*1024+64+40:], uint32(c11.C11Size-1))
		}},
		{name: "C11 root file array truncated", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[root:], 0xffff) }},
		{name: "C11 root segment array truncated", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[root+2:], 0xffff) }},
		{name: "C11 file reference outside extent", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[root+4:], 0xfffffffc) }},
		{name: "C11 file reference unaligned", fixture: c11.Fixture, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[root+4:], uint32(firstFile-root+1))
		}},
		{name: "C11 root range reversed", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[root+12:], 0x40) }},
		{name: "C11 root last byte outside section", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[root+16:], 0x100) }},
		{name: "C11 root section unknown", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[root+20:], 2) }},
		{name: "C11 file contribution arrays truncated", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[firstFile:], 0xffff) }},
		{name: "C11 line table reference outside extent", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[firstFile+4:], 0xfffffffc) }},
		{name: "C11 line table reference unaligned", fixture: c11.Fixture, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[firstFile+4:], uint32(firstTable-root+1))
		}},
		{name: "C11 file contribution reversed", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[firstFile+8:], 0x20) }},
		{name: "C11 file contribution outside root coverage", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[secondFile+12:], 0x40) }},
		{name: "C11 ST filename empty", fixture: c11.Fixture, mutate: func(data []byte) { data[firstFile+16] = 0 }},
		{name: "C11 ST filename truncated", fixture: c11.Fixture, mutate: func(data []byte) { data[firstFile+16] = 255 }},
		{name: "C11 ST filename embedded NUL", fixture: c11.Fixture, mutate: func(data []byte) { data[firstFile+17] = 0 }},
		{name: "C11 line table section unknown", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[firstTable:], 2) }},
		{name: "C11 line table pairs truncated", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[firstTable+2:], 0xffff) }},
		{name: "C11 line offset outside contribution", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[firstTable+4:], 0xf) }},
		{name: "C11 decreasing line offsets", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[firstTable+4+5*4:], 0x17) }},
		{name: "C11 late file reference invalidates earlier symbols", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[secondFile+4:], 0xfffffffc) }},
		{name: "C11 late line range invalidates earlier symbols", fixture: c11.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[secondTable+8:], 0x40) }},
		{name: "C11 late module reference invalidates earlier symbols", fixture: multi.Fixture, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[multi.SecondC11Start+4:], 0xfffffffc) }},
		{name: "C11 non ASCII filename still validates line arrays", fixture: c11.Fixture, mutate: func(data []byte) {
			data[firstFile+17+7] = 0xe9
			binary.LittleEndian.PutUint16(data[firstTable+2:], 0xffff)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corrupt := append([]byte(nil), test.fixture.Data...)
			test.mutate(corrupt)
			if objects := ParseDebugObjects("corrupt.pdb", corrupt); len(objects) != 0 {
				t.Fatalf("malformed PDB produced partial symbols: %+v", objects)
			}
		})
	}
}

func TestParsePDBSourceLinesEnforceRecordBudgets(t *testing.T) {
	fixture := nativepdb.WithSourceLines(true)
	lineData := fixture.Data[fixture.C13Start+8 : fixture.C13Start+8+48]
	sections := []pdbSection{{virtualSize: 0x100, virtualAddr: 0x1000, rawSize: 0x100, flags: 0x20000000}}
	functions := newPDBFunctionIndex([]SymbolFunction{{Start: 0x1010, End: 0x1030, Name: "CrashFunction", Line: -1}})
	checksums := map[uint32]string{0: `C:\src\crash.cc`}
	t.Run("source line records", func(t *testing.T) {
		lineCount, mappings := maxPDBSourceLines, 0
		if _, ok := parsePDBC13Lines(lineData, checksums, sections, functions, &lineCount, &mappings); ok {
			t.Fatal("parser accepted source lines beyond the per-PDB budget")
		}
	})
	t.Run("line to procedure mappings", func(t *testing.T) {
		lineCount, mappings := 0, maxPDBLineMappings
		if _, ok := parsePDBC13Lines(lineData, checksums, sections, functions, &lineCount, &mappings); ok {
			t.Fatal("parser accepted source-to-procedure mappings beyond the per-PDB budget")
		}
	})
	t.Run("subsection count", func(t *testing.T) {
		data := make([]byte, (maxPDBC13Subsections+1)*8)
		for offset := 0; offset < len(data); offset += 8 {
			binary.LittleEndian.PutUint32(data[offset:], 0x7777)
		}
		lines, files, mappings := 0, 0, 0
		if _, ok := parsePDBC13(data, nil, nil, nil, nil, &lines, &files, &mappings); ok {
			t.Fatal("parser accepted more C13 subsections than the per-module budget")
		}
	})
	t.Run("checksum file records", func(t *testing.T) {
		data := make([]byte, (maxPDBSourceFiles+1)*8)
		for offset := 0; offset < len(data); offset += 8 {
			binary.LittleEndian.PutUint32(data[offset:], 1)
		}
		files := 0
		if _, ok := parsePDBC13Checksums(data, []byte{0, 'x', 0}, &files); ok {
			t.Fatal("parser accepted more checksum files than the per-PDB budget")
		}
	})
	c11 := nativepdb.WithC11SourceLines()
	c11Data := c11.Data[c11.C11Start : c11.C11Start+c11.C11Size]
	procedures := []SymbolFunction{{Start: 0x1010, End: 0x1030, Name: "CrashFunction", Line: -1}}
	for _, test := range []struct {
		name   string
		budget pdbSourceBudget
	}{
		{name: "C11 cumulative bytes", budget: pdbSourceBudget{c11Bytes: maxPDBBytes - len(c11Data)}},
		{name: "C11 cumulative source files", budget: pdbSourceBudget{files: maxPDBSourceFiles - 2}},
		{name: "C11 cumulative line pairs", budget: pdbSourceBudget{lines: maxPDBSourceLines - 8}},
		{name: "C11 cumulative mappings", budget: pdbSourceBudget{mappings: maxPDBLineMappings - 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := test.budget
			if mapped, ok := parsePDBC11(c11Data, sections, procedures, &budget); !ok || len(mapped) != 5 {
				t.Fatalf("last available capacity rejected valid C11 source: mappings=%d accepted=%t", len(mapped), ok)
			}
			if mapped, ok := parsePDBC11(c11Data, sections, procedures, &budget); ok || len(mapped) != 0 {
				t.Fatalf("repeated C11 exceeded cumulative budget: mappings=%d accepted=%t", len(mapped), ok)
			}
		})
	}
	t.Run("C11 empty tables consume references", func(t *testing.T) {
		data := append([]byte(nil), c11Data...)
		for _, fileField := range []int{4, 8} {
			file := int(binary.LittleEndian.Uint32(data[fileField:]))
			table := int(binary.LittleEndian.Uint32(data[file+4:]))
			binary.LittleEndian.PutUint16(data[table+2:], 0)
		}
		for _, test := range []struct {
			remaining int
			accepted  bool
		}{{remaining: 3, accepted: true}, {remaining: 2}} {
			budget := pdbSourceBudget{c11References: maxPDBC11References - test.remaining}
			mapped, ok := parsePDBC11(data, sections, procedures, &budget)
			if ok != test.accepted || len(mapped) != 0 {
				t.Fatalf("empty tables with %d references remaining: accepted=%t mappings=%d", test.remaining, ok, len(mapped))
			}
		}
	})
	t.Run("C11 root ranges consume references without files", func(t *testing.T) {
		data := make([]byte, 16)
		binary.LittleEndian.PutUint16(data[2:], 1)
		binary.LittleEndian.PutUint32(data[4:], 0x10)
		binary.LittleEndian.PutUint32(data[8:], 0x3f)
		binary.LittleEndian.PutUint16(data[12:], 1)
		budget := pdbSourceBudget{c11References: maxPDBC11References - 1}
		if mapped, ok := parsePDBC11(data, sections, procedures, &budget); !ok || len(mapped) != 0 {
			t.Fatal("last root reference rejected")
		}
		if mapped, ok := parsePDBC11(data, sections, procedures, &budget); ok || len(mapped) != 0 {
			t.Fatal("root reference exceeded the cumulative budget")
		}
	})
}

func TestParseNativePDBRejectsMalformedInputWithoutPartialSymbols(t *testing.T) {
	fixture := nativepdb.Minimal()
	data, moduleBlock, firstProcedureLen, dbiBlock := fixture.Data, fixture.ModuleBlock, fixture.FirstProcedureLength, fixture.DBIBlock
	small := nativepdb.SmallMSF2(1024, 1, false)
	directoryStart := int(binary.LittleEndian.Uint16(small.Data[60:])) * 1024
	const smallPageList = 4 + 6*8
	secondProcedure := small.ModuleBlock*1024 + 4 + small.FirstProcedureLength
	c13 := nativepdb.WithSourceLines(true)
	tests := []struct {
		name   string
		input  []byte
		mutate func([]byte)
	}{
		{name: "invalid block size", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[32:], 256) }},
		{name: "block count exceeds file", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[40:], ^uint32(0)) }},
		{name: "block map outside file", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[52:], ^uint32(0)) }},
		{name: "directory block outside file", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[4*512:], ^uint32(0)) }},
		{name: "too many streams", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[3*512:], 4097) }},
		{name: "PDB age disagrees with DBI", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[5*512+8:], 2) }},
		{name: "invalid CodeView module signature", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[moduleBlock*512:], 5) }},
		{name: "modern procedures with CodeView signature2", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[moduleBlock*512:], 2) }},
		{name: "legacy ST with C13 and CodeView signature2", input: c13.Data, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[c13.ModuleBlock*512:], 2)
			// Keep both procedures valid under signature2 so only the C13
			// signature restriction can reject their valid source mappings.
			offset := c13.ModuleBlock*512 + 4
			for _, kind := range []uint16{0x100b, 0x100a} {
				end := offset + 2 + int(binary.LittleEndian.Uint16(data[offset:]))
				binary.LittleEndian.PutUint16(data[offset+2:], kind)
				nameStart := offset + 39
				nameLength := bytes.IndexByte(data[nameStart:end], 0)
				copy(data[nameStart+1:nameStart+1+nameLength], data[nameStart:nameStart+nameLength])
				data[nameStart] = byte(nameLength)
				offset = end
			}
		}},
		{name: "section header stream missing", mutate: func(data []byte) {
			moduleInfoSize := binary.LittleEndian.Uint32(data[dbiBlock*512+24:])
			offset := int(dbiBlock*512) + 64 + int(moduleInfoSize) + 10
			binary.LittleEndian.PutUint16(data[offset:], ^uint16(0))
		}},
		{name: "later procedure is truncated", mutate: func(data []byte) {
			offset := int(moduleBlock*512) + 4 + firstProcedureLen
			binary.LittleEndian.PutUint16(data[offset:], ^uint16(0))
		}},
		{name: "Small MSF2 header truncated", input: small.Data[:59]},
		{name: "Small MSF2 signature mismatch", input: small.Data, mutate: func(data []byte) { data[43] = 1 }},
		{name: "Small MSF2 page size unsupported", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[44:], 512) }},
		{name: "Small MSF2 FPM invalid", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[48:], 3) }},
		{name: "Small MSF2 page count exceeds file", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[50:], 100) }},
		{name: "Small MSF2 directory empty", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[52:], 0) }},
		{name: "Small MSF2 directory page outside file", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[60:], 0xffff) }},
		{name: "Small MSF2 directory overlaps FPM", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[60:], 16) }},
		{name: "Small MSF2 too many streams", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[directoryStart:], 4097) }},
		{name: "Small MSF2 trailing directory bytes", input: small.Data, mutate: func(data []byte) {
			binary.LittleEndian.PutUint32(data[52:], binary.LittleEndian.Uint32(data[52:])+2)
		}},
		{name: "Small MSF2 stream zero page list truncated", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[directoryStart+4:], 12) }},
		{name: "Small MSF2 active byte budget exceeded", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[directoryStart+4+8:], 64<<20+1) }},
		{name: "Small MSF2 active stream overlaps header", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[directoryStart+smallPageList:], 0) }},
		{name: "Small MSF2 active stream overlaps first FPM last page", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[directoryStart+smallPageList:], 8) }},
		{name: "Small MSF2 active stream overlaps second FPM last page", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[directoryStart+smallPageList:], 16) }},
		{name: "Small MSF2 active stream overlaps directory", input: small.Data, mutate: func(data []byte) {
			binary.LittleEndian.PutUint16(data[directoryStart+smallPageList:], uint16(directoryStart/1024))
		}},
		{name: "Small MSF2 active streams share page", input: small.Data, mutate: func(data []byte) {
			// Give unused active stream 2 twelve bytes on Info's page, keeping
			// every required stream valid independently of the ownership check.
			binary.LittleEndian.PutUint32(data[directoryStart+4+2*8:], 12)
			directorySize := int(binary.LittleEndian.Uint32(data[52:]))
			insert := directoryStart + smallPageList + 2
			copy(data[insert+2:directoryStart+directorySize+2], data[insert:directoryStart+directorySize])
			binary.LittleEndian.PutUint16(data[insert:], uint16(small.InfoBlock))
			binary.LittleEndian.PutUint32(data[52:], uint32(directorySize+2))
		}},
		{name: "Small MSF2 active page outside file", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint16(data[directoryStart+smallPageList:], 0xffff) }},
		{name: "Small MSF2 Info header truncated", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[directoryStart+4+8:], 11) }},
		{name: "Small MSF2 Info and DBI ages disagree", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[small.DBIBlock*1024+8:], 2) }},
		{name: "Small MSF2 later Pascal name truncated", input: small.Data, mutate: func(data []byte) { data[secondProcedure+4+35] = 255 }},
		{name: "Small MSF2 later procedure range invalid", input: small.Data, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[secondProcedure+4+12:], 0xffffffff) }},
		{name: "truncated MSF7 header magic only", input: data[:32]},
		{name: "truncated MSF7 header 55 bytes", input: data[:55]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := test.input
			if input == nil {
				input = data
			}
			corrupt := append([]byte(nil), input...)
			if test.mutate != nil {
				test.mutate(corrupt)
			}
			if objects := ParseDebugObjects("corrupt.pdb", corrupt); len(objects) != 0 {
				t.Fatalf("malformed PDB produced partial symbols: %+v", objects)
			}
		})
	}
}

func TestParsePDBModuleSymbolsEnforcesRecordAndProcedureBudgets(t *testing.T) {
	const recordBytes = 4
	data := make([]byte, (maxPDBSymbolRecords+1)*recordBytes)
	for offset := 0; offset < len(data); offset += recordBytes {
		binary.LittleEndian.PutUint16(data[offset:], 2)
		binary.LittleEndian.PutUint16(data[offset+2:], 0x0006)
	}
	var functions []SymbolFunction
	procedures, records := 0, 0
	if parsePDBModuleSymbols(data, nil, &functions, &procedures, &records) {
		t.Fatal("parser accepted more than the CodeView record budget")
	}
	if records != maxPDBSymbolRecords || len(functions) != 0 {
		t.Fatalf("record budget result: records=%d functions=%d", records, len(functions))
	}

	fixture := nativepdb.Minimal()
	moduleStart := fixture.ModuleBlock*512 + 4
	procedureData := fixture.Data[moduleStart : moduleStart+fixture.FirstProcedureLength]
	sections := []pdbSection{{virtualSize: 0x100, virtualAddr: 0x1000, rawSize: 0x100, flags: 0x20000000}}
	procedures, records = maxPDBProcedures, 0
	functions = nil
	if parsePDBModuleSymbols(procedureData, sections, &functions, &procedures, &records) {
		t.Fatal("parser accepted more than the procedure budget")
	}
	if len(functions) != 0 {
		t.Fatalf("over-limit procedure produced partial symbols: %+v", functions)
	}
}

func pdbTARDebugArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: "crash.pdb", Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func pdbGzipDebugArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := gzip.NewWriter(&archive)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
