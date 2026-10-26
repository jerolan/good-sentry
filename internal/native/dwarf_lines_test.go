package native

import (
	"bytes"
	"debug/dwarf"
	"encoding/binary"
	"testing"
)

const (
	dwarfTagCompileUnit = 0x11
	dwarfTagSubprogram  = 0x2e
	dwarfAtName         = 0x03
	dwarfAtStmtList     = 0x10
	dwarfAtLowPC        = 0x11
	dwarfAtHighPC       = 0x12
	dwarfAtRanges       = 0x55
	dwarfFormAddr       = 0x01
	dwarfFormData8      = 0x07
	dwarfFormString     = 0x08
	dwarfFormSecOffset  = 0x17
)

type dwarfSubprogram struct {
	name         string
	low          uint64
	size         uint64
	rangesOffset int64
}

type dwarfLineRow struct {
	address uint64
	line    int
	file    uint64
	end     bool
}

func dwarfULEB(value uint64) []byte {
	var encoded []byte
	for {
		byteValue := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			byteValue |= 0x80
		}
		encoded = append(encoded, byteValue)
		if value == 0 {
			return encoded
		}
	}
}

func dwarfSLEB(value int64) []byte {
	var encoded []byte
	for {
		byteValue := byte(value & 0x7f)
		value >>= 7
		done := value == 0 && byteValue&0x40 == 0 || value == -1 && byteValue&0x40 != 0
		if !done {
			byteValue |= 0x80
		}
		encoded = append(encoded, byteValue)
		if done {
			return encoded
		}
	}
}

func dwarfLE32(value uint32) []byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	return encoded[:]
}

func dwarfLE64(value uint64) []byte {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], value)
	return encoded[:]
}

func buildDwarfData(t *testing.T, program, ranges []byte, subprograms []dwarfSubprogram) *dwarf.Data {
	t.Helper()
	var abbrev bytes.Buffer
	writeAttributes := func(attributes ...byte) {
		abbrev.Write(attributes)
		abbrev.Write([]byte{0, 0})
	}
	abbrev.Write(dwarfULEB(1))
	abbrev.Write(dwarfULEB(dwarfTagCompileUnit))
	abbrev.WriteByte(1)
	writeAttributes(dwarfAtName, dwarfFormString, dwarfAtStmtList, dwarfFormSecOffset)
	abbrev.Write(dwarfULEB(2))
	abbrev.Write(dwarfULEB(dwarfTagSubprogram))
	abbrev.WriteByte(0)
	writeAttributes(dwarfAtName, dwarfFormString, dwarfAtLowPC, dwarfFormAddr, dwarfAtHighPC, dwarfFormData8)
	abbrev.Write(dwarfULEB(3))
	abbrev.Write(dwarfULEB(dwarfTagSubprogram))
	abbrev.WriteByte(0)
	writeAttributes(dwarfAtName, dwarfFormString, dwarfAtRanges, dwarfFormSecOffset)
	abbrev.WriteByte(0)

	var body bytes.Buffer
	body.Write(dwarfULEB(1))
	body.WriteString("fixture.c\x00")
	body.Write(dwarfLE32(0))
	for _, subprogram := range subprograms {
		if subprogram.rangesOffset >= 0 {
			body.Write(dwarfULEB(3))
			body.WriteString(subprogram.name + "\x00")
			body.Write(dwarfLE32(uint32(subprogram.rangesOffset)))
			continue
		}
		body.Write(dwarfULEB(2))
		body.WriteString(subprogram.name + "\x00")
		body.Write(dwarfLE64(subprogram.low))
		body.Write(dwarfLE64(subprogram.size))
	}
	body.WriteByte(0)
	var info bytes.Buffer
	info.Write(dwarfLE32(uint32(7 + body.Len())))
	info.Write([]byte{4, 0})
	info.Write(dwarfLE32(0))
	info.WriteByte(8)
	info.Write(body.Bytes())

	var header bytes.Buffer
	header.Write([]byte{1, 1, 1, 0xfb, 14, 13})
	header.Write([]byte{0, 1, 1, 1, 1, 0, 0, 0, 1, 0, 0, 1})
	header.WriteByte(0)
	header.WriteString("fixture.c\x00")
	header.Write(dwarfULEB(0))
	header.Write(dwarfULEB(0))
	header.Write(dwarfULEB(0))
	header.WriteByte(0)
	var line bytes.Buffer
	line.Write(dwarfLE32(uint32(2 + 4 + header.Len() + len(program))))
	line.Write([]byte{4, 0})
	line.Write(dwarfLE32(uint32(header.Len())))
	line.Write(header.Bytes())
	line.Write(program)

	data, err := dwarf.New(abbrev.Bytes(), nil, nil, info.Bytes(), line.Bytes(), nil, ranges, nil)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func buildDwarfLineProgram(rows []dwarfLineRow) []byte {
	var program bytes.Buffer
	line := 1
	for _, row := range rows {
		program.Write([]byte{0, 9, 2})
		program.Write(dwarfLE64(row.address))
		if row.end {
			program.Write([]byte{0, 1, 1})
			line = 1
			continue
		}
		program.WriteByte(4)
		program.Write(dwarfULEB(row.file))
		if row.line != line {
			program.WriteByte(3)
			program.Write(dwarfSLEB(int64(row.line - line)))
			line = row.line
		}
		program.WriteByte(1)
	}
	return program.Bytes()
}

func TestDwarfLineSpansRespectSequencesGapsAndFallback(t *testing.T) {
	program := buildDwarfLineProgram([]dwarfLineRow{
		{address: 0x1000, line: 2, file: 1},
		{address: 0x1010, line: 3, file: 1},
		{address: 0x1020, end: true},
		{address: 0x1020, line: 4, file: 0},
		{address: 0x1040, line: 5, file: 1},
		{address: 0x1050, line: 6, file: 1},
		{address: 0x1058, end: true},
		{address: 0x1080, line: 7, file: 1},
		{address: 0x1090, end: true},
		{address: 0x10a0, line: 8, file: 1},
		{address: 0x10a8, end: true},
	})
	ranges := make([]byte, 0, 48)
	for _, value := range []uint64{0x1080, 0x1090, 0x10a0, 0x10b0, 0, 0} {
		ranges = append(ranges, dwarfLE64(value)...)
	}
	data := buildDwarfData(t, program, ranges, []dwarfSubprogram{
		{name: "first", low: 0x1000, size: 0x30, rangesOffset: -1},
		{name: "second", low: 0x1040, size: 0x20, rangesOffset: -1},
		{name: "split", rangesOffset: 0},
	})
	functions := dwarfFunctions(data)
	type identity struct {
		start, end uint64
		name       string
		file       string
		line       int
	}
	got := map[identity]bool{}
	for _, function := range functions {
		got[identity{function.Start, function.End, function.Name, function.File, function.Line}] = true
	}
	want := []identity{
		{0x1000, 0x1030, "first", "", -1},
		{0x1000, 0x1010, "first", "fixture.c", 2},
		{0x1010, 0x1020, "first", "fixture.c", 3},
		{0x1040, 0x1060, "second", "", -1},
		{0x1040, 0x1050, "second", "fixture.c", 5},
		{0x1050, 0x1058, "second", "fixture.c", 6},
		{0x1080, 0x1090, "split", "", -1},
		{0x1080, 0x1090, "split", "fixture.c", 7},
		{0x10a0, 0x10b0, "split", "", -1},
		{0x10a0, 0x10a8, "split", "fixture.c", 8},
	}
	if len(functions) != len(want) {
		t.Fatalf("functions=%d want=%d: %+v", len(functions), len(want), functions)
	}
	for _, expected := range want {
		if !got[expected] {
			t.Errorf("missing %+v in %+v", expected, functions)
		}
	}
	crash := Crash{Modules: []Module{{Name: "fixture.c", Base: 0, Size: 0x2000}}, Frames: []Frame{
		{Instruction: 0x1005, Module: "fixture.c"},
		{Instruction: 0x1025, Module: "fixture.c"},
		{Instruction: 0x105a, Module: "fixture.c"},
		{Instruction: 0x10a5, Module: "fixture.c"},
	}}
	Symbolicate(&crash, SymbolSet{moduleKey("fixture.c"): functions})
	expectations := []struct {
		function string
		file     string
		line     int
	}{
		{"first", "fixture.c", 2},
		{"first", "", -1},
		{"second", "", -1},
		{"split", "fixture.c", 8},
	}
	for index, expected := range expectations {
		frame := crash.Frames[index]
		if frame.Function != expected.function || frame.File != expected.file || frame.Line != expected.line {
			t.Errorf("frame %d: got function=%q file=%q line=%d want %+v", index, frame.Function, frame.File, frame.Line, expected)
		}
	}
}

func TestDwarfLineRowBoundOmitsSpansButKeepsFunctions(t *testing.T) {
	rows := make([]dwarfLineRow, 0, maxDWARFLineRows+1)
	for index := 0; index <= maxDWARFLineRows; index++ {
		rows = append(rows, dwarfLineRow{address: 0x1000 + uint64(index), line: 2, file: 1})
	}
	rows = append(rows, dwarfLineRow{address: 0x1000 + uint64(len(rows)), end: true})
	data := buildDwarfData(t, buildDwarfLineProgram(rows), nil, []dwarfSubprogram{
		{name: "bounded", low: 0x1000, size: uint64(len(rows)), rangesOffset: -1},
	})
	functions := dwarfFunctions(data)
	if len(functions) != 1 || functions[0].Name != "bounded" || functions[0].File != "" || functions[0].Line != -1 {
		t.Fatalf("excess line rows did not preserve the function range: %+v", functions)
	}
}

func TestDwarfObjectSpanBoundRetainsFunctions(t *testing.T) {
	count := maxDWARFLineSpans/2 + 1
	rows := make([]dwarfLineRow, 0, count+1)
	for index := 0; index < count; index++ {
		rows = append(rows, dwarfLineRow{address: 0x1000 + uint64(index), line: 2, file: 1})
	}
	rows = append(rows, dwarfLineRow{address: 0x1000 + uint64(count), end: true})
	data := buildDwarfData(t, buildDwarfLineProgram(rows), nil, []dwarfSubprogram{
		{name: "left", low: 0x1000, size: uint64(count) + 1, rangesOffset: -1},
		{name: "right", low: 0x1000, size: uint64(count) + 1, rangesOffset: -1},
	})
	functions := dwarfFunctions(data)
	sources, fallbacks := 0, map[string]bool{}
	for _, function := range functions {
		if function.Line >= 0 {
			sources++
			continue
		}
		fallbacks[function.Name] = true
	}
	if sources != maxDWARFLineSpans || !fallbacks["left"] || !fallbacks["right"] {
		t.Fatalf("span bound sources=%d functions=%d fallbacks=%v", sources, len(functions), fallbacks)
	}
}

func TestDwarfDecreasingLineAddressKeepsFunctions(t *testing.T) {
	program := buildDwarfLineProgram([]dwarfLineRow{
		{address: 0x1000, line: 2, file: 1},
		{address: 0x0ff0, line: 3, file: 1},
		{address: 0x1010, end: true},
	})
	data := buildDwarfData(t, program, nil, []dwarfSubprogram{
		{name: "malformed", low: 0x1000, size: 0x20, rangesOffset: -1},
	})
	functions := dwarfFunctions(data)
	if len(functions) != 1 || functions[0].Name != "malformed" || functions[0].File != "" || functions[0].Line != -1 {
		t.Fatalf("malformed line table did not preserve the function range: %+v", functions)
	}
}
