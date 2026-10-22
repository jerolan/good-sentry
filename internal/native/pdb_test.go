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
