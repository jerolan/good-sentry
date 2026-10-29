// Package nativepdb builds small Microsoft PDB fixtures for parser and server
// acceptance tests. The fixture uses only the documented MSF 7 and CodeView
// structures needed to carry two procedure symbols.
package nativepdb

import (
	"bytes"
	"encoding/binary"
)

type Fixture struct {
	Data                      []byte
	DBIBlock                  int
	InfoBlock                 int
	ModuleBlock               int
	NamesBlock                int
	FirstProcedureLength      int
	C13Start                  int
	LineSubsectionLength      int
	LineSegmentOffset         int
	LineCodeSizeOffset        int
	LineNameIndexOffset       int
	FirstLineOffset           int
	SecondLineOffset          int
	SecondLineNumber          int
	NamesStreamIndexOffset    int
	InlineSiteOffset          int
	InlineAnnotationOffset    int
	InlineEndOffset           int
	InlineeLinesOffset        int
	IPIStreamSizeOffset       int
	ProcedureEndPointerOffset int
	ProcedureEndOffset        int
	SecondProcedureOffset     int
	PublicSymbolsBlock        int
	SectionMapStart           int
}

func Minimal() Fixture {
	return build(false, false, false, nil, nil)
}

// WithECSubstream places 25 opaque EC bytes immediately before the optional
// debug header, without aligning its start.
func WithECSubstream() Fixture {
	return build(false, false, false, bytes.Repeat([]byte{0xe1}, 25), nil)
}

func WithSourceLines(localStrings bool) Fixture {
	return build(true, !localStrings, false, nil, nil)
}

// WithGlobalEmptyHashEntry retains a nonempty source filename while the
// global string hash refers to a separate empty string at a nonzero offset.
func WithGlobalEmptyHashEntry() Fixture {
	f := WithSourceLines(false)
	base := f.NamesBlock * 512
	stringSize := binary.LittleEndian.Uint32(f.Data[base+8:])
	hash := base + 12 + int(stringSize)
	copy(f.Data[hash+1:hash+13], f.Data[hash:hash+12])
	f.Data[hash] = 0
	binary.LittleEndian.PutUint32(f.Data[base+8:], stringSize+1)
	binary.LittleEndian.PutUint32(f.Data[hash+5:], stringSize)
	streamIndex := binary.LittleEndian.Uint32(f.Data[f.NamesStreamIndexOffset:])
	directorySize := 3*512 + 4 + int(streamIndex)*4
	oldSize := binary.LittleEndian.Uint32(f.Data[directorySize:])
	binary.LittleEndian.PutUint32(f.Data[directorySize:], oldSize+1)
	return f
}

func WithMultipleSourceLineModules() Fixture {
	return build(true, false, true, nil, nil)
}

func WithModernThunk() Fixture {
	name := []byte("ModernThunk")
	record := make([]byte, 25+len(name)+1+2)
	binary.LittleEndian.PutUint16(record, uint16(len(record)-2))
	binary.LittleEndian.PutUint16(record[2:], 0x1102)
	binary.LittleEndian.PutUint32(record[16:], 0x60)
	binary.LittleEndian.PutUint16(record[20:], 1)
	binary.LittleEndian.PutUint16(record[22:], 4)
	record[24] = 3
	copy(record[25:], name)
	copy(record[26+len(name):], []byte{0xaa, 0xbb})
	return build(true, true, false, nil, record)
}

func WithInlineFrames() Fixture {
	const blockSize = 512
	info := make([]byte, 28)
	binary.LittleEndian.PutUint32(info, 20000404)
	binary.LittleEndian.PutUint32(info[4:], 1)
	binary.LittleEndian.PutUint32(info[8:], 1)
	copy(info[12:], []byte{0xe5, 0x32, 0x98, 0x5a, 0x72, 0x28, 0xc1, 0x41, 0x83, 0x8e, 0xd9, 0x89, 0x14, 0xe9, 0xb7, 0xff})

	firstProcedure := procedure("CrashFunction", 0x10, 0x20, 0x1110)
	outerSite, outerAnnotation := inlineSite(0x1000, 0, 0x20)
	block := blockScope()
	innerSite, _ := inlineSite(0x1001, 4, 8)
	deepSite, _ := inlineSite(0x1002, 4, 2)
	outerOffset := len(firstProcedure)
	blockOffset := outerOffset + len(outerSite)
	innerOffset := blockOffset + len(block)
	deepOffset := innerOffset + len(innerSite)
	deepEndOffset := deepOffset + len(deepSite)
	innerEndOffset := deepEndOffset + len(inlineSiteEnd())
	blockEndOffset := innerEndOffset + len(inlineSiteEnd())
	outerEndOffset := blockEndOffset + len(inlineSiteEnd())
	procedureEndOffset := outerEndOffset + len(inlineSiteEnd())
	setScopeOffsets(firstProcedure, 0, uint32(procedureEndOffset+4))
	setScopeOffsets(outerSite, 4, uint32(outerEndOffset+4))
	setScopeOffsets(block, uint32(outerOffset+4), uint32(blockEndOffset+4))
	setScopeOffsets(innerSite, uint32(blockOffset+4), uint32(innerEndOffset+4))
	setScopeOffsets(deepSite, uint32(innerOffset+4), uint32(deepEndOffset+4))
	symbols := append([]byte(nil), firstProcedure...)
	symbols = append(symbols, outerSite...)
	symbols = append(symbols, block...)
	symbols = append(symbols, innerSite...)
	symbols = append(symbols, deepSite...)
	symbols = append(symbols, inlineSiteEnd()...)
	symbols = append(symbols, inlineSiteEnd()...)
	symbols = append(symbols, codeViewRecord(0x0006, nil)...)
	symbols = append(symbols, inlineSiteEnd()...)
	symbols = append(symbols, codeViewRecord(0x0006, nil)...)
	secondProcedureOffset := len(symbols)
	secondProcedure := procedure("OtherFunction", 0x80, 0x10, 0x1147)
	secondProcedureEndOffset := secondProcedureOffset + len(secondProcedure)
	setScopeOffsets(secondProcedure, 0, uint32(secondProcedureEndOffset+4))
	symbols = append(symbols, secondProcedure...)
	symbols = append(symbols, codeViewRecord(0x114f, nil)...)

	c13, _, inlineeOffset := inlineSourceLines()
	symbolBytes := 4 + len(symbols)
	module := append([]byte{4, 0, 0, 0}, symbols...)
	module = append(module, c13...)
	moduleInfo := moduleInfoRecord(5, symbolBytes, len(c13), "fixture.obj", "fixture.obj")

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
	binary.LittleEndian.PutUint16(optional[10:], 6)
	dbi = append(dbi, optional...)

	section := make([]byte, 40)
	copy(section, ".text\x00\x00\x00")
	binary.LittleEndian.PutUint32(section[8:], 0x100)
	binary.LittleEndian.PutUint32(section[12:], 0x1000)
	binary.LittleEndian.PutUint32(section[16:], 0x100)
	binary.LittleEndian.PutUint32(section[36:], 0x60000020)

	streams := [][]byte{nil, info, nil, dbi, inlineIDStream(), module, section}
	data, blocks := writeMSF(blockSize, streams)
	moduleBase := blocks[5][0] * blockSize
	fixture := Fixture{
		Data: data, DBIBlock: blocks[3][0], InfoBlock: blocks[1][0], ModuleBlock: blocks[5][0],
		FirstProcedureLength: len(firstProcedure), C13Start: moduleBase + symbolBytes,
		InlineSiteOffset:          moduleBase + 4 + outerOffset,
		InlineAnnotationOffset:    moduleBase + 4 + outerOffset + outerAnnotation,
		InlineEndOffset:           moduleBase + 4 + deepEndOffset + 2,
		InlineeLinesOffset:        moduleBase + symbolBytes + inlineeOffset,
		IPIStreamSizeOffset:       3*blockSize + 4 + 4*4,
		ProcedureEndPointerOffset: moduleBase + 4 + 8,
		ProcedureEndOffset:        moduleBase + 4 + procedureEndOffset,
		SecondProcedureOffset:     moduleBase + 4 + secondProcedureOffset,
	}
	return fixture
}

func WithPublicOnlySymbols() Fixture {
	const blockSize = 512
	info := make([]byte, 28)
	binary.LittleEndian.PutUint32(info, 20000404)
	binary.LittleEndian.PutUint32(info[4:], 1)
	binary.LittleEndian.PutUint32(info[8:], 1)
	copy(info[12:], []byte{0xe5, 0x32, 0x98, 0x5a, 0x72, 0x28, 0xc1, 0x41, 0x83, 0x8e, 0xd9, 0x89, 0x14, 0xe9, 0xb7, 0xff})

	moduleInfo := moduleInfoRecord(^uint16(0), 0, 0, "fixture.obj", "fixture.obj")
	sectionMap := make([]byte, 4+2*20)
	binary.LittleEndian.PutUint16(sectionMap, 2)
	binary.LittleEndian.PutUint16(sectionMap[2:], 2)
	for i, frame := range []uint16{1, 2} {
		entry := sectionMap[4+i*20 : 4+(i+1)*20]
		binary.LittleEndian.PutUint16(entry, 1|4|8)
		binary.LittleEndian.PutUint16(entry[6:], frame)
		binary.LittleEndian.PutUint16(entry[8:], ^uint16(0))
		binary.LittleEndian.PutUint16(entry[10:], ^uint16(0))
		binary.LittleEndian.PutUint32(entry[12:], []uint32{0x20, 0}[i])
		binary.LittleEndian.PutUint32(entry[16:], 0x60)
	}

	const optionalHeaderBytes = 11 * 2
	dbi := make([]byte, 64)
	binary.LittleEndian.PutUint32(dbi, ^uint32(0))
	binary.LittleEndian.PutUint32(dbi[4:], 19990903)
	binary.LittleEndian.PutUint32(dbi[8:], 1)
	binary.LittleEndian.PutUint16(dbi[12:], 2) // GSI stream, unused by the public-symbol scan
	binary.LittleEndian.PutUint16(dbi[16:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[20:], 4) // SymRecordStream (GSS records)
	binary.LittleEndian.PutUint32(dbi[24:], uint32(len(moduleInfo)))
	binary.LittleEndian.PutUint32(dbi[32:], uint32(len(sectionMap)))
	binary.LittleEndian.PutUint32(dbi[48:], optionalHeaderBytes)
	binary.LittleEndian.PutUint16(dbi[56:], 1<<1)
	binary.LittleEndian.PutUint16(dbi[58:], 0x8664)
	dbi = append(dbi, moduleInfo...)
	dbi = append(dbi, sectionMap...)
	optional := bytes.Repeat([]byte{0xff}, optionalHeaderBytes)
	binary.LittleEndian.PutUint16(optional[10:], 5)
	dbi = append(dbi, optional...)

	sections := make([]byte, 2*40)
	for i, sectionName := range []string{".text", ".text2"} {
		section := sections[i*40 : (i+1)*40]
		copy(section, sectionName)
		binary.LittleEndian.PutUint32(section[8:], 0x80)
		binary.LittleEndian.PutUint32(section[12:], []uint32{0x1000, 0x1080}[i])
		binary.LittleEndian.PutUint32(section[16:], 0x80)
		binary.LittleEndian.PutUint32(section[36:], 0x60000020)
	}

	publics := append(publicSymbol("PublicFirst", 3, 0x10, 1), publicSymbol("PublicSecond", 3, 0x30, 1)...)
	publics = append(publics, publicSymbol("GlobalVariable", 1, 0x40, 1)...)
	streams := [][]byte{nil, info, nil, dbi, publics, sections}
	data, blocks := writeMSF(blockSize, streams)
	return Fixture{
		Data: data, DBIBlock: blocks[3][0], InfoBlock: blocks[1][0], PublicSymbolsBlock: blocks[4][0],
		SectionMapStart: blocks[3][0]*blockSize + 64 + len(moduleInfo),
	}
}

func publicSymbol(name string, flags uint32, offset uint32, segment uint16) []byte {
	body := make([]byte, 10+len(name)+1)
	binary.LittleEndian.PutUint32(body, flags)
	binary.LittleEndian.PutUint32(body[4:], offset)
	binary.LittleEndian.PutUint16(body[8:], segment)
	copy(body[10:], name)
	total := 4 + len(body)
	padding := (4 - total%4) % 4
	record := make([]byte, total+padding)
	binary.LittleEndian.PutUint16(record, uint16(len(record)-2))
	binary.LittleEndian.PutUint16(record[2:], 0x110e)
	copy(record[4:], body)
	for i := 0; i < padding; i++ {
		record[total+i] = byte(0xf1 + i)
	}
	return record
}

func inlineSite(inlinee uint32, start, length byte) ([]byte, int) {
	annotations := []byte{1, 0, 6, 0, 4, length, 0, 0}
	if start != 0 {
		annotations[1] = start
	}
	body := make([]byte, 12, 12+len(annotations))
	binary.LittleEndian.PutUint32(body[8:], inlinee)
	body = append(body, annotations...)
	return codeViewRecord(0x114d, body), 4 + 12
}

func blockScope() []byte {
	body := make([]byte, 18)
	body[16] = 1
	return codeViewRecord(0x1103, append(body, 0))
}

func inlineSiteEnd() []byte {
	return codeViewRecord(0x114e, nil)
}

func codeViewRecord(kind uint16, body []byte) []byte {
	var record bytes.Buffer
	binary.Write(&record, binary.LittleEndian, uint16(2+len(body)))
	binary.Write(&record, binary.LittleEndian, kind)
	record.Write(body)
	return record.Bytes()
}

func setScopeOffsets(record []byte, parent, end uint32) {
	binary.LittleEndian.PutUint32(record[4:], parent)
	binary.LittleEndian.PutUint32(record[8:], end)
}

func inlineSourceLines() ([]byte, int, int) {
	paths := [][]byte{[]byte("C:\\src\\crash.cc\x00"), []byte("C:\\include\\inline.hpp\x00"), []byte("C:\\include\\inner.hpp\x00"), []byte("C:\\include\\deep.hpp\x00")}
	var stringsData []byte
	nameOffsets := make([]uint32, len(paths))
	for i, path := range paths {
		nameOffsets[i] = uint32(len(stringsData))
		stringsData = append(stringsData, path...)
	}
	stringsData = append([]byte{0}, stringsData...)
	for i := range nameOffsets {
		nameOffsets[i]++
	}

	checksums := make([]byte, 4*24)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(checksums[i*24:], nameOffsets[i])
		checksums[i*24+4] = 16
		checksums[i*24+5] = 1
	}
	line := make([]byte, 48)
	binary.LittleEndian.PutUint32(line, 0x10)
	binary.LittleEndian.PutUint16(line[4:], 1)
	binary.LittleEndian.PutUint16(line[6:], 1)
	binary.LittleEndian.PutUint32(line[8:], 0x20)
	binary.LittleEndian.PutUint32(line[16:], 2)
	binary.LittleEndian.PutUint32(line[20:], 36)
	binary.LittleEndian.PutUint32(line[28:], 42)
	binary.LittleEndian.PutUint32(line[32:], 0x10)
	binary.LittleEndian.PutUint32(line[36:], 43)
	binary.LittleEndian.PutUint16(line[40:], 1)
	binary.LittleEndian.PutUint16(line[42:], 3)
	binary.LittleEndian.PutUint16(line[44:], 1)
	binary.LittleEndian.PutUint16(line[46:], 4)

	inlinees := make([]byte, 4+3*12)
	binary.LittleEndian.PutUint32(inlinees, 0)
	binary.LittleEndian.PutUint32(inlinees[4:], 0x1000)
	binary.LittleEndian.PutUint32(inlinees[8:], 24)
	binary.LittleEndian.PutUint32(inlinees[12:], 100)
	binary.LittleEndian.PutUint32(inlinees[16:], 0x1001)
	binary.LittleEndian.PutUint32(inlinees[20:], 48)
	binary.LittleEndian.PutUint32(inlinees[24:], 200)
	binary.LittleEndian.PutUint32(inlinees[28:], 0x1002)
	binary.LittleEndian.PutUint32(inlinees[32:], 72)
	binary.LittleEndian.PutUint32(inlinees[36:], 300)

	var c13 bytes.Buffer
	appendSubsection(&c13, 0xf2, line)
	appendSubsection(&c13, 0xf4, checksums)
	appendSubsection(&c13, 0xf3, stringsData)
	inlineeOffset := c13.Len() + 8
	appendSubsection(&c13, 0xf6, inlinees)
	return c13.Bytes(), len(line), inlineeOffset
}

func inlineIDStream() []byte {
	first := inlineIDRecord("OuterInline", 0x1601)
	second := inlineIDRecord("InnerInline", 0x1602)
	third := inlineIDRecord("DeepInline", 0x1601)
	stream := make([]byte, 56, 56+len(first)+len(second)+len(third))
	binary.LittleEndian.PutUint32(stream, 20040203)
	binary.LittleEndian.PutUint32(stream[4:], 56)
	binary.LittleEndian.PutUint32(stream[8:], 0x1000)
	binary.LittleEndian.PutUint32(stream[12:], 0x1003)
	binary.LittleEndian.PutUint32(stream[16:], uint32(len(first)+len(second)+len(third)))
	stream = append(stream, first...)
	stream = append(stream, second...)
	stream = append(stream, third...)
	return stream
}

func inlineIDRecord(name string, kind uint16) []byte {
	body := make([]byte, 8)
	body = append(body, name...)
	body = append(body, 0)
	for padding := (4 - len(body)%4) % 4; padding > 0; padding-- {
		body = append(body, byte(0xf0+padding))
	}
	return codeViewRecord(kind, body)
}

func build(includeLines, globalStrings, multipleModules bool, ec, extra []byte) Fixture {
	const blockSize = 512
	info := make([]byte, 28)
	binary.LittleEndian.PutUint32(info, 20000404)
	binary.LittleEndian.PutUint32(info[4:], 1)
	binary.LittleEndian.PutUint32(info[8:], 1)
	copy(info[12:], []byte{0xe5, 0x32, 0x98, 0x5a, 0x72, 0x28, 0xc1, 0x41, 0x83, 0x8e, 0xd9, 0x89, 0x14, 0xe9, 0xb7, 0xff})

	firstProcedure := procedure("CrashFunction", 0x10, 0x20, 0x1110)
	secondProcedure := procedure("OtherFunction", 0x80, 0x10, 0x1147)
	symbols := append([]byte(nil), firstProcedure...)
	symbols = append(symbols, secondProcedure...)
	if extra != nil {
		binary.LittleEndian.PutUint32(extra[8:], uint32(4+len(symbols)+len(extra)))
		symbols = append(symbols, extra...)
		symbols = append(symbols, 2, 0, 6, 0)
	}
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
	binary.LittleEndian.PutUint32(dbi[52:], uint32(len(ec)))
	dbi = append(dbi, moduleInfo...)
	dbi = append(dbi, ec...)
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
