package native

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativepdb"
)

func TestParseNativePDBProcedureSymbols(t *testing.T) {
	data := nativepdb.Minimal().Data
	archives := []struct {
		name string
		data []byte
	}{
		{name: "standalone", data: data},
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
	archives = append(archives, struct {
		name string
		data []byte
	}{name: "zip", data: zipBytes.Bytes()})
	wantID := DebugKey("5A9832E5287241C1838ED98914E9B7FF1")
	want := []SymbolFunction{
		{Start: 0x1010, End: 0x1030, Name: "CrashFunction", Line: -1},
		{Start: 0x1080, End: 0x1090, Name: "OtherFunction", Line: -1},
	}
	for _, archive := range archives {
		t.Run(archive.name, func(t *testing.T) {
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

func TestParseNativePDBC13SourceLines(t *testing.T) {
	for _, test := range []struct {
		name         string
		localStrings bool
	}{
		{name: "module string table", localStrings: true},
		{name: "global PDB names stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := nativepdb.WithSourceLines(test.localStrings)
			objects := ParseDebugObjects("crash.pdb", fixture.Data)
			if len(objects) != 1 {
				t.Fatalf("parsed PDB objects=%d, want one", len(objects))
			}
			crash := Crash{
				Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x10000, DebugID: "5A9832E5287241C1838ED98914E9B7FF1"}},
				Frames:  []Frame{{Instruction: 0x140001015, Module: "crash.exe"}, {Instruction: 0x140001025, Module: "crash.exe"}},
			}
			Symbolicate(&crash, SymbolSet{objects[0].DebugID: objects[0].Functions})
			for i, wantLine := range []int{42, 43} {
				frame := crash.Frames[i]
				if frame.Function != "CrashFunction" || frame.File != `C:\src\crash.cc` || frame.Line != wantLine {
					t.Fatalf("source frame[%d]=%+v, want CrashFunction C:\\src\\crash.cc:%d", i, frame, wantLine)
				}
			}
		})
	}
}

func TestParseNativePDBC13KeepsSourceLinesWithinTheirModule(t *testing.T) {
	fixture := nativepdb.WithMultipleSourceLineModules()
	objects := ParseDebugObjects("crash.pdb", fixture.Data)
	if len(objects) != 1 {
		t.Fatalf("parsed PDB objects=%d, want one", len(objects))
	}
	crash := Crash{
		Modules: []Module{{Name: "crash.exe", Base: 0x140000000, Size: 0x10000, DebugID: "5A9832E5287241C1838ED98914E9B7FF1"}},
		Frames:  []Frame{{Instruction: 0x140001015, Module: "crash.exe"}, {Instruction: 0x140001045, Module: "crash.exe"}},
	}
	Symbolicate(&crash, SymbolSet{objects[0].DebugID: objects[0].Functions})
	want := []struct {
		function string
		line     int
	}{{"CrashFunction", 42}, {"OtherModuleFunction", 52}}
	for i, frame := range crash.Frames {
		if frame.Function != want[i].function || frame.File != `C:\src\crash.cc` || frame.Line != want[i].line {
			t.Fatalf("multi-module source frame[%d]=%+v, want %s C:\\src\\crash.cc:%d", i, frame, want[i].function, want[i].line)
		}
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

func TestParseNativePDBC13RejectsMalformedInputWithoutPartialSymbols(t *testing.T) {
	global := nativepdb.WithSourceLines(false)
	local := nativepdb.WithSourceLines(true)
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

func TestParsePDBC13EnforcesRecordBudgets(t *testing.T) {
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
}

func TestParseNativePDBRejectsMalformedInputWithoutPartialSymbols(t *testing.T) {
	fixture := nativepdb.Minimal()
	data, moduleBlock, firstProcedureLen, dbiBlock := fixture.Data, fixture.ModuleBlock, fixture.FirstProcedureLength, fixture.DBIBlock
	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "invalid block size", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[32:], 256) }},
		{name: "block count exceeds file", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[40:], ^uint32(0)) }},
		{name: "block map outside file", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[52:], ^uint32(0)) }},
		{name: "directory block outside file", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[4*512:], ^uint32(0)) }},
		{name: "too many streams", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[3*512:], 4097) }},
		{name: "PDB age disagrees with DBI", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[5*512+8:], 2) }},
		{name: "invalid CodeView module signature", mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[moduleBlock*512:], 5) }},
		{name: "section header stream missing", mutate: func(data []byte) {
			moduleInfoSize := binary.LittleEndian.Uint32(data[dbiBlock*512+24:])
			offset := int(dbiBlock*512) + 64 + int(moduleInfoSize) + 10
			binary.LittleEndian.PutUint16(data[offset:], ^uint16(0))
		}},
		{name: "later procedure is truncated", mutate: func(data []byte) {
			offset := int(moduleBlock*512) + 4 + firstProcedureLen
			binary.LittleEndian.PutUint16(data[offset:], ^uint16(0))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corrupt := append([]byte(nil), data...)
			test.mutate(corrupt)
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
