package native

import (
	"bytes"
	"encoding/binary"
	"testing"
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
