package native

import (
	"bytes"
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativepdb"
)

func buildMinimalPDBMSF(blockSize uint32) []byte {
	blockCount := int(blockSize) + 3
	data := make([]byte, blockCount*int(blockSize))
	copy(data, msf7Magic)
	binary.LittleEndian.PutUint32(data[32:], blockSize)
	binary.LittleEndian.PutUint32(data[36:], 1)
	binary.LittleEndian.PutUint32(data[40:], uint32(blockCount))
	binary.LittleEndian.PutUint32(data[44:], 20)
	binary.LittleEndian.PutUint32(data[52:], 3)
	binary.LittleEndian.PutUint32(data[3*int(blockSize):], 4)
	binary.LittleEndian.PutUint32(data[4*int(blockSize):], 4)
	return data
}

func TestPDBMSFBlockSizes(t *testing.T) {
	for _, blockSize := range []uint32{512, 1024, 2048, 4096} {
		data := buildMinimalPDBMSF(blockSize)
		msf, ok := parsePDBMSF(data)
		if !ok || msf.blockSize != blockSize || len(msf.streams) != 4 {
			t.Fatalf("block size %d was rejected", blockSize)
		}
	}
	truncated := buildMinimalPDBMSF(4096)
	if _, ok := parsePDBMSF(truncated[:len(truncated)-1]); ok {
		t.Fatal("truncated MSF was accepted")
	}
}

func pdbProcedureRecord(kind uint16, name string) []byte {
	body := make([]byte, 35)
	binary.LittleEndian.PutUint32(body[12:], 0x20)
	binary.LittleEndian.PutUint32(body[28:], 0x10)
	binary.LittleEndian.PutUint16(body[32:], 1)
	body = append(body, name...)
	body = append(body, 0)
	record := make([]byte, 4, 4+len(body))
	binary.LittleEndian.PutUint16(record, uint16(len(body)+2))
	binary.LittleEndian.PutUint16(record[2:], kind)
	return append(record, body...)
}

func TestPDBProcedureKindsAndOriginalSpelling(t *testing.T) {
	sections := []pdbSection{{virtualSize: 0x100, virtualAddr: 0x1000, rawSize: 0x100, flags: 0x20000000}}
	for _, kind := range []uint16{0x110f, 0x1110, 0x1146, 0x1147, 0x1155, 0x1156} {
		record := pdbProcedureRecord(kind, "Crash Function (odd)")
		var functions []SymbolFunction
		procedures, records := 0, 0
		if !parsePDBModuleSymbols(record, sections, &functions, &procedures, &records) {
			t.Fatalf("procedure kind %#x was rejected", kind)
		}
		if len(functions) != 1 || functions[0].Start != 0x1010 || functions[0].End != 0x1030 || functions[0].Name != "Crash Function (odd)" || functions[0].Line != -1 {
			t.Fatalf("procedure kind %#x parsed %+v", kind, functions)
		}
	}
}

func TestPDBModuleBudget(t *testing.T) {
	record := make([]byte, 68)
	binary.LittleEndian.PutUint16(record[34:], pdbModuleStreamAbsent)
	if functions, ok := parsePDBModules(&pdbMSFFile{}, bytes.Repeat(record, maxPDBModules), nil, nil); !ok || len(functions) != 0 {
		t.Fatal("4096 absent modules were rejected")
	}
	if _, ok := parsePDBModules(&pdbMSFFile{}, bytes.Repeat(record, maxPDBModules+1), nil, nil); ok {
		t.Fatal("4097 modules were accepted")
	}
}

func TestPDBSectionsBudget(t *testing.T) {
	header := make([]byte, 40)
	binary.LittleEndian.PutUint32(header[12:], 0x1000)
	binary.LittleEndian.PutUint32(header[36:], pdbPESectionExecute)
	data := bytes.Repeat(header, maxPDBSections)
	sections, ok := parsePDBSections(data)
	if !ok || len(sections) != maxPDBSections {
		t.Fatal("512 PE sections were rejected")
	}
	if _, ok := parsePDBSections(append(data, header...)); ok {
		t.Fatal("513 PE sections were accepted")
	}
}

func TestPDBOversizedFile(t *testing.T) {
	if _, _, ok := parsePDBSymbols(make([]byte, maxPDBBytes+1)); ok {
		t.Fatal("a file above 64MiB was accepted")
	}
}

func TestPDBC13SkipsUnknownFramedSubsection(t *testing.T) {
	data := make([]byte, 28)
	binary.LittleEndian.PutUint32(data, 0x7777)
	binary.LittleEndian.PutUint32(data[4:], 0)
	binary.LittleEndian.PutUint32(data[8:], 0xf3) // DEBUG_S_STRINGTABLE
	binary.LittleEndian.PutUint32(data[12:], 4)
	binary.LittleEndian.PutUint32(data[20:], 0xf4) // DEBUG_S_FILECHKSMS
	binary.LittleEndian.PutUint32(data[24:], 0)
	lines, files, mappings := 0, 0, 0
	got, ok := parsePDBC13(data, nil, nil, nil, nil, &lines, &files, &mappings)
	if !ok || len(got) != 0 {
		t.Fatalf("unknown framed subsection was not skipped: ok=%v lines=%+v", ok, got)
	}
}

func TestPDBStringLengthBudget(t *testing.T) {
	valid := append([]byte{0}, bytes.Repeat([]byte{'a'}, maxPDBProcedureName)...)
	valid = append(valid, 0)
	if name, ok := pdbStringAt(valid, 1); !ok || len(name) != maxPDBProcedureName {
		t.Fatal("a 4096-byte filename was rejected")
	}
	tooLong := append([]byte{0}, bytes.Repeat([]byte{'a'}, maxPDBProcedureName+1)...)
	tooLong = append(tooLong, 0)
	if _, ok := pdbStringAt(tooLong, 1); ok {
		t.Fatal("a filename above 4096 bytes was accepted")
	}
}

func TestPDBC11OnlyProducesNoSourceLines(t *testing.T) {
	fixture := nativepdb.WithSourceLines(true)
	data := append([]byte(nil), fixture.Data...)
	moduleInfo := fixture.DBIBlock*512 + 64
	binary.LittleEndian.PutUint32(data[moduleInfo+40:], 4)
	binary.LittleEndian.PutUint32(data[moduleInfo+44:], 0)
	objects := ParseDebugObjects("c11.pdb", data)
	if len(objects) != 1 || len(objects[0].Functions) == 0 {
		t.Fatalf("C11-only PDB lost its procedures: %+v", objects)
	}
	for _, function := range objects[0].Functions {
		if function.Line >= 0 {
			t.Fatalf("C11-only PDB produced a source line: %+v", function)
		}
	}
}

func TestPDBC11AndC13TogetherRejected(t *testing.T) {
	fixture := nativepdb.WithSourceLines(true)
	data := append([]byte(nil), fixture.Data...)
	binary.LittleEndian.PutUint32(data[fixture.DBIBlock*512+64+40:], 4)
	if objects := ParseDebugObjects("mixed.pdb", data); len(objects) != 0 {
		t.Fatalf("C11 and C13 together produced partial symbols: %+v", objects)
	}
}

func TestPDBC13ColumnsAreValidatedButNotReturned(t *testing.T) {
	data := make([]byte, 36)
	binary.LittleEndian.PutUint16(data[4:], 1)
	binary.LittleEndian.PutUint16(data[6:], 1) // columns present
	binary.LittleEndian.PutUint32(data[8:], 0x20)
	binary.LittleEndian.PutUint32(data[12:], 0) // checksum boundary
	binary.LittleEndian.PutUint32(data[16:], 1)
	binary.LittleEndian.PutUint32(data[20:], 24)
	binary.LittleEndian.PutUint32(data[24:], 0)
	binary.LittleEndian.PutUint32(data[28:], 42)
	binary.LittleEndian.PutUint16(data[32:], 7)
	binary.LittleEndian.PutUint16(data[34:], 9)
	sections := []pdbSection{{virtualSize: 0x100, virtualAddr: 0x1000, rawSize: 0x100, flags: pdbPESectionExecute}}
	functions := newPDBFunctionIndex([]SymbolFunction{{Start: 0x1000, End: 0x1020, Name: "CrashFunction", Line: -1}})
	lineCount, mappingCount := 0, 0
	got, ok := parsePDBC13Lines(data, map[uint32]string{0: `C:srccrash.cc`}, sections, functions, &lineCount, &mappingCount)
	if !ok || len(got) != 1 || got[0].File != `C:srccrash.cc` || got[0].Line != 42 {
		t.Fatalf("column-bearing line block: ok=%v functions=%+v", ok, got)
	}
}

func TestPDBSectionMapBudget(t *testing.T) {
	build := func(count int) []byte {
		data := make([]byte, 4+count*20)
		binary.LittleEndian.PutUint16(data, uint16(count))
		binary.LittleEndian.PutUint16(data[2:], uint16(count))
		return data
	}
	if entries, ok := parsePDBSectionMap(build(maxPDBSections)); !ok || len(entries) != maxPDBSections {
		t.Fatal("512 Section Map descriptors were rejected")
	}
	if _, ok := parsePDBSectionMap(build(maxPDBSections + 1)); ok {
		t.Fatal("513 Section Map descriptors were accepted")
	}
}

func TestPDBPublicSymbolsDoNotNeedGSIOrPSI(t *testing.T) {
	fixture := nativepdb.WithPublicOnlySymbols()
	data := append([]byte(nil), fixture.Data...)
	binary.LittleEndian.PutUint16(data[fixture.DBIBlock*512+12:], 0xffff)
	binary.LittleEndian.PutUint16(data[fixture.DBIBlock*512+16:], 0xffff)
	objects := ParseDebugObjects("stripped.pdb", data)
	if len(objects) != 1 || len(objects[0].Functions) != 2 {
		t.Fatalf("public scan depended on GSI/PSI: %+v", objects)
	}
}
