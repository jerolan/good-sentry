// Package nativepdb builds small Microsoft PDB fixtures for parser and server
// acceptance tests. The fixture uses only the documented MSF 7 and CodeView
// structures needed to carry two procedure symbols.
package nativepdb

import (
	"bytes"
	"encoding/binary"
)

type Fixture struct {
	Data                   []byte
	DBIBlock               int
	InfoBlock              int
	ModuleBlock            int
	NamesBlock             int
	FirstProcedureLength   int
	C13Start               int
	LineSubsectionLength   int
	LineSegmentOffset      int
	LineCodeSizeOffset     int
	LineNameIndexOffset    int
	FirstLineOffset        int
	SecondLineOffset       int
	SecondLineNumber       int
	NamesStreamIndexOffset int
}

func Minimal() Fixture {
	return build(false, false, false)
}

func WithSourceLines(localStrings bool) Fixture {
	return build(true, !localStrings, false)
}

func WithMultipleSourceLineModules() Fixture {
	return build(true, false, true)
}

func build(includeLines, globalStrings, multipleModules bool) Fixture {
	const blockSize = 512
	info := make([]byte, 28)
	binary.LittleEndian.PutUint32(info, 20000404)
	binary.LittleEndian.PutUint32(info[4:], 1)
	binary.LittleEndian.PutUint32(info[8:], 1)
	copy(info[12:], []byte{0xe5, 0x32, 0x98, 0x5a, 0x72, 0x28, 0xc1, 0x41, 0xff, 0xb7, 0xe9, 0x14, 0x89, 0xd9, 0x8e, 0x83})

	firstProcedure := procedure("CrashFunction", 0x10, 0x20, 0x1110)
	secondProcedure := procedure("OtherFunction", 0x80, 0x10, 0x1147)
	symbols := append([]byte(nil), firstProcedure...)
	symbols = append(symbols, secondProcedure...)
	module := append([]byte{4, 0, 0, 0}, symbols...)
	var c13, globalStringsData []byte
	if includeLines {
		c13, globalStringsData = sourceLines(globalStrings, 0x10, 42, 43)
	}
	module = append(module, c13...)

	symbolBytes := 4 + len(symbols)
	moduleInfo := moduleInfoRecord(4, symbolBytes, len(c13), "fixture.obj", "fixture.obj")
	var secondModule []byte
	if multipleModules {
		secondProcedure := procedure("OtherModuleFunction", 0x40, 0x20, 0x1110)
		secondSymbols := append([]byte(nil), secondProcedure...)
		var secondC13 []byte
		if includeLines {
			secondC13, _ = sourceLines(false, 0x40, 52, 53)
		}
		secondModule = append([]byte{4, 0, 0, 0}, secondSymbols...)
		secondModule = append(secondModule, secondC13...)
		moduleInfo = append(moduleInfo, moduleInfoRecord(6, 4+len(secondSymbols), len(secondC13), "fixture-extra.obj", "fixture-extra.obj")...)
	}
	const optionalHeaderBytes = 11 * 2
	dbi := make([]byte, 64)
	binary.LittleEndian.PutUint32(dbi, ^uint32(0))
	binary.LittleEndian.PutUint32(dbi[4:], 19990903)
	binary.LittleEndian.PutUint32(dbi[8:], 1)
	binary.LittleEndian.PutUint16(dbi[12:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[16:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[20:], ^uint16(0))
	binary.LittleEndian.PutUint32(dbi[24:], uint32(len(moduleInfo)))
	binary.LittleEndian.PutUint32(dbi[48:], optionalHeaderBytes)
	dbi = append(dbi, moduleInfo...)
	optional := bytes.Repeat([]byte{0xff}, optionalHeaderBytes)
	binary.LittleEndian.PutUint16(optional[10:], 5)
	dbi = append(dbi, optional...)

	section := make([]byte, 40)
	copy(section, ".text\x00\x00\x00")
	binary.LittleEndian.PutUint32(section[8:], 0x100)
	binary.LittleEndian.PutUint32(section[12:], 0x1000)
	binary.LittleEndian.PutUint32(section[16:], 0x100)
	binary.LittleEndian.PutUint32(section[36:], 0x60000020)

	streams := [][]byte{nil, info, nil, dbi, module, section}
	namesStreamIndex := uint32(len(streams))
	if multipleModules {
		streams = append(streams, secondModule)
	}
	namesStreamIndexOffset := 0
	if globalStrings {
		namesStreamIndex = uint32(len(streams))
		info, namesStreamIndexOffset = withNamedStream(info, namesStreamIndex)
		streams[1] = info
		streams = append(streams, pdbNamesStream(globalStringsData))
	}
	data, blocks := writeMSF(blockSize, streams)
	fixture := Fixture{
		Data: data, DBIBlock: blocks[3][0], InfoBlock: blocks[1][0], ModuleBlock: blocks[4][0],
		FirstProcedureLength: len(firstProcedure), C13Start: blocks[4][0]*blockSize + symbolBytes,
	}
	if globalStrings {
		fixture.NamesBlock = blocks[namesStreamIndex][0]
		fixture.NamesStreamIndexOffset = namesStreamIndexOffset + blocks[1][0]*blockSize
	}
	fixture.LineSubsectionLength = fixture.C13Start + 4
	fixture.LineSegmentOffset = fixture.C13Start + 8 + 4
	fixture.LineCodeSizeOffset = fixture.C13Start + 8 + 8
	fixture.LineNameIndexOffset = fixture.C13Start + 8 + 12
	fixture.FirstLineOffset = fixture.C13Start + 8 + 12 + 12
	fixture.SecondLineOffset = fixture.FirstLineOffset + 8
	fixture.SecondLineNumber = fixture.SecondLineOffset + 4
	return fixture
}

func moduleInfoRecord(streamIndex uint16, symbolBytes, c13Bytes int, moduleName, objectName string) []byte {
	record := make([]byte, 64)
	binary.LittleEndian.PutUint16(record[34:], streamIndex)
	binary.LittleEndian.PutUint32(record[36:], uint32(symbolBytes))
	binary.LittleEndian.PutUint32(record[44:], uint32(c13Bytes))
	record = append(record, moduleName...)
	record = append(record, 0)
	record = append(record, objectName...)
	record = append(record, 0)
	for len(record)%4 != 0 {
		record = append(record, 0)
	}
	return record
}

func sourceLines(globalStrings bool, relocationOffset, firstLine, secondLine uint32) ([]byte, []byte) {
	path := []byte("C:\\src\\crash.cc\x00")
	strings := append([]byte{0}, path...)
	checksum := make([]byte, 24)
	binary.LittleEndian.PutUint32(checksum, 1)
	checksum[4] = 16
	checksum[5] = 1 // MD5
	line := make([]byte, 48)
	binary.LittleEndian.PutUint32(line, relocationOffset)
	binary.LittleEndian.PutUint16(line[4:], 1)
	binary.LittleEndian.PutUint16(line[6:], 1) // includes columns
	binary.LittleEndian.PutUint32(line[8:], 0x20)
	binary.LittleEndian.PutUint32(line[12:], 0) // checksum subsection offset
	binary.LittleEndian.PutUint32(line[16:], 2)
	binary.LittleEndian.PutUint32(line[20:], 36)
	binary.LittleEndian.PutUint32(line[24:], 0)
	binary.LittleEndian.PutUint32(line[28:], firstLine)
	binary.LittleEndian.PutUint32(line[32:], 0x10)
	binary.LittleEndian.PutUint32(line[36:], secondLine)
	binary.LittleEndian.PutUint16(line[40:], 1)
	binary.LittleEndian.PutUint16(line[42:], 3)
	binary.LittleEndian.PutUint16(line[44:], 1)
	binary.LittleEndian.PutUint16(line[46:], 4)

	var c13 bytes.Buffer
	appendSubsection(&c13, 0xf2, line)
	appendSubsection(&c13, 0xf4, checksum)
	if !globalStrings {
		appendSubsection(&c13, 0xf3, strings)
	}
	return c13.Bytes(), strings
}

func appendSubsection(output *bytes.Buffer, kind uint32, data []byte) {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header, kind)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(data)))
	output.Write(header)
	output.Write(data)
	for output.Len()%4 != 0 {
		output.WriteByte(0xf0)
	}
}

func withNamedStream(info []byte, streamIndex uint32) ([]byte, int) {
	nameBytes := []byte("/names\x00")
	var offset int
	var tail bytes.Buffer
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, uint32(len(nameBytes)))
	tail.Write(word)
	tail.Write(nameBytes)
	writeU32(&tail, 1)  // one named stream
	writeU32(&tail, 64) // hash table capacity
	writeU32(&tail, 2)  // present bitvector words through bucket 35
	writeU32(&tail, 0)
	writeU32(&tail, 1<<3) // bucket 35 is populated
	writeU32(&tail, 0)    // no deleted bitvector words
	writeU32(&tail, 0)    // name offset
	offset = 28 + tail.Len()
	writeU32(&tail, streamIndex)
	info = append(info, tail.Bytes()...)
	return info, offset
}

func pdbNamesStream(strings []byte) []byte {
	var stream bytes.Buffer
	writeU32(&stream, 0xeffeeffe)
	writeU32(&stream, 1)
	writeU32(&stream, uint32(len(strings)))
	stream.Write(strings)
	writeU32(&stream, 1) // one string ID
	writeU32(&stream, 1)
	writeU32(&stream, 1) // one indexed name
	return stream.Bytes()
}

func writeU32(output *bytes.Buffer, value uint32) {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	output.Write(word[:])
}

func procedure(name string, offset, codeSize uint32, kind uint16) []byte {
	body := make([]byte, 35)
	binary.LittleEndian.PutUint32(body[12:], codeSize)
	binary.LittleEndian.PutUint32(body[28:], offset)
	binary.LittleEndian.PutUint16(body[32:], 1)
	body = append(body, name...)
	body = append(body, 0)
	var record bytes.Buffer
	binary.Write(&record, binary.LittleEndian, uint16(len(body)+2))
	binary.Write(&record, binary.LittleEndian, kind)
	record.Write(body)
	return record.Bytes()
}

func writeMSF(blockSize int, streams [][]byte) ([]byte, [][]int) {
	streamBlocks := make([][]int, len(streams))
	block := 5
	for i, stream := range streams {
		if stream == nil {
			continue
		}
		count := (len(stream) + blockSize - 1) / blockSize
		for j := 0; j < count; j++ {
			streamBlocks[i] = append(streamBlocks[i], block)
			block++
		}
	}
	directory := make([]byte, 4+4*len(streams))
	binary.LittleEndian.PutUint32(directory, uint32(len(streams)))
	for i, stream := range streams {
		size := ^uint32(0)
		if stream != nil {
			size = uint32(len(stream))
		}
		binary.LittleEndian.PutUint32(directory[4+i*4:], size)
	}
	for _, pages := range streamBlocks {
		for _, page := range pages {
			word := make([]byte, 4)
			binary.LittleEndian.PutUint32(word, uint32(page))
			directory = append(directory, word...)
		}
	}
	if (len(directory)+blockSize-1)/blockSize != 1 {
		panic("native PDB fixture directory no longer fits one block")
	}
	data := make([]byte, block*blockSize)
	copy(data, []byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS\x00\x00\x00"))
	binary.LittleEndian.PutUint32(data[32:], uint32(blockSize))
	binary.LittleEndian.PutUint32(data[36:], 1)
	binary.LittleEndian.PutUint32(data[40:], uint32(block))
	binary.LittleEndian.PutUint32(data[44:], uint32(len(directory)))
	binary.LittleEndian.PutUint32(data[52:], 4)
	binary.LittleEndian.PutUint32(data[4*blockSize:], 3)
	copy(data[3*blockSize:], directory)
	for i, stream := range streams {
		for j, page := range streamBlocks[i] {
			start := j * blockSize
			end := min(start+blockSize, len(stream))
			copy(data[page*blockSize:], stream[start:end])
		}
	}
	return data, streamBlocks
}
