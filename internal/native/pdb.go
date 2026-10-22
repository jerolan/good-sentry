package native

import (
	"bytes"
	"encoding/binary"
	"sort"
)

const (
	maxPDBBytes           = 64 << 20
	maxPDBStreams         = 4096
	maxPDBModules         = 4096
	maxPDBSections        = 512
	maxPDBProcedures      = 200_000
	maxPDBSymbolRecords   = 1_000_000
	maxPDBProcedureName   = 4096
	maxPDBC13Subsections  = 65_536
	maxPDBSourceLines     = 1_000_000
	maxPDBSourceFiles     = 65_536
	maxPDBLineMappings    = 1_000_000
	maxPDBNamedCapacity   = 65_536
	pdbHeaderSize         = 32
	pdbStreamAbsent       = ^uint32(0)
	pdbModuleStreamAbsent = ^uint16(0)
)

var msf7Magic = []byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS\x00\x00\x00")

type pdbMSFStream struct {
	size   uint32
	absent bool
	blocks []uint32
}

type pdbMSFFile struct {
	data      []byte
	blockSize uint32
	streams   []pdbMSFStream
}

type pdbSection struct {
	virtualSize uint32
	virtualAddr uint32
	rawSize     uint32
	flags       uint32
}

type pdbProcedure struct {
	segment uint16
	offset  uint32
	size    uint32
	name    string
}

func parsePDBSymbols(data []byte) (string, []SymbolFunction, bool) {
	if len(data) < pdbHeaderSize || len(data) > maxPDBBytes || !bytes.Equal(data[:pdbHeaderSize], msf7Magic) {
		return "", nil, false
	}
	msf, ok := parsePDBMSF(data)
	if !ok {
		return "", nil, false
	}
	info, ok := msf.readStream(1)
	if !ok || len(info) < 28 || !knownPDBInfoVersion(binary.LittleEndian.Uint32(info)) {
		return "", nil, false
	}
	age := binary.LittleEndian.Uint32(info[8:])
	dbi, ok := msf.readStream(3)
	if !ok || len(dbi) < 64 || int32(binary.LittleEndian.Uint32(dbi)) != -1 || !knownDBIVersion(binary.LittleEndian.Uint32(dbi[4:])) || binary.LittleEndian.Uint32(dbi[8:]) != age {
		return "", nil, false
	}
	debugID := pdbInfoDebugID(info)
	if debugID == "" {
		return "", nil, false
	}
	moduleSize, sectionContributionSize, sectionMapSize, sourceInfoSize, typeServerSize, optionalHeaderSize, _, ok := pdbDBISubstreamSizes(dbi)
	if !ok {
		return "", nil, false
	}
	if optionalHeaderSize < 12 || optionalHeaderSize%2 != 0 {
		return "", nil, false
	}
	moduleStart := 64
	optionalStart := moduleStart + moduleSize + sectionContributionSize + sectionMapSize + sourceInfoSize + typeServerSize
	sectionStreamIndex := binary.LittleEndian.Uint16(dbi[optionalStart+5*2:])
	sectionsData, ok := msf.readStream(sectionStreamIndex)
	if !ok || len(sectionsData) == 0 || len(sectionsData)%40 != 0 || len(sectionsData)/40 > maxPDBSections {
		return "", nil, false
	}
	sections := make([]pdbSection, len(sectionsData)/40)
	for i := range sections {
		header := sectionsData[i*40 : (i+1)*40]
		sections[i] = pdbSection{
			virtualSize: binary.LittleEndian.Uint32(header[8:]),
			virtualAddr: binary.LittleEndian.Uint32(header[12:]),
			rawSize:     binary.LittleEndian.Uint32(header[16:]),
			flags:       binary.LittleEndian.Uint32(header[36:]),
		}
	}
	moduleData := dbi[moduleStart : moduleStart+moduleSize]
	functions, ok := parsePDBModules(msf, moduleData, sections, info)
	if !ok || len(functions) == 0 {
		return "", nil, false
	}
	sort.SliceStable(functions, func(i, j int) bool {
		if functions[i].Start != functions[j].Start {
			return functions[i].Start < functions[j].Start
		}
		if functions[i].End != functions[j].End {
			return functions[i].End < functions[j].End
		}
		return functions[i].Name < functions[j].Name
	})
	return debugID, functions, true
}

func parsePDBMSF(data []byte) (*pdbMSFFile, bool) {
	blockSize := binary.LittleEndian.Uint32(data[32:])
	switch blockSize {
	case 512, 1024, 2048, 4096:
	default:
		return nil, false
	}
	freeMap := binary.LittleEndian.Uint32(data[36:])
	blockCount := binary.LittleEndian.Uint32(data[40:])
	directorySize := binary.LittleEndian.Uint32(data[44:])
	blockMap := binary.LittleEndian.Uint32(data[52:])
	if freeMap != 1 && freeMap != 2 || blockCount == 0 || uint64(blockCount)*uint64(blockSize) != uint64(len(data)) || directorySize == 0 || directorySize > maxPDBBytes {
		return nil, false
	}
	otherFreeMap := uint32(3) - freeMap
	directoryBlockCount := (directorySize + blockSize - 1) / blockSize
	if blockCount < 3 || directoryBlockCount == 0 || directoryBlockCount > blockSize/4 || blockMap == 0 || blockMap >= blockCount {
		return nil, false
	}
	usedBlocks := map[uint32]struct{}{0: {}}
	for start, alternate := uint64(freeMap), uint64(otherFreeMap); start < uint64(blockCount) || alternate < uint64(blockCount); start, alternate = start+uint64(blockSize), alternate+uint64(blockSize) {
		for _, page := range []uint64{start, alternate} {
			if page >= uint64(blockCount) {
				continue
			}
			if _, exists := usedBlocks[uint32(page)]; exists {
				return nil, false
			}
			usedBlocks[uint32(page)] = struct{}{}
		}
	}
	if _, exists := usedBlocks[blockMap]; exists {
		return nil, false
	}
	usedBlocks[blockMap] = struct{}{}
	directory := make([]byte, directorySize)
	mapBase := uint64(blockMap) * uint64(blockSize)
	for i := uint32(0); i < directoryBlockCount; i++ {
		position := mapBase + uint64(i)*4
		if position+4 > uint64(len(data)) {
			return nil, false
		}
		page := binary.LittleEndian.Uint32(data[position:])
		if page >= blockCount {
			return nil, false
		}
		if _, exists := usedBlocks[page]; exists {
			return nil, false
		}
		usedBlocks[page] = struct{}{}
		copyStart := uint64(i) * uint64(blockSize)
		copyEnd := min(copyStart+uint64(blockSize), uint64(directorySize))
		pageStart := uint64(page) * uint64(blockSize)
		copy(directory[copyStart:copyEnd], data[pageStart:pageStart+copyEnd-copyStart])
	}
	if len(directory) < 4 {
		return nil, false
	}
	streamCount := binary.LittleEndian.Uint32(directory)
	if streamCount < 4 || streamCount > maxPDBStreams || uint64(4)+uint64(streamCount)*4 > uint64(len(directory)) {
		return nil, false
	}
	streams := make([]pdbMSFStream, streamCount)
	blockCounts := make([]uint32, streamCount)
	var declaredBytes uint64
	for i := range streams {
		size := binary.LittleEndian.Uint32(directory[4+i*4:])
		if size == pdbStreamAbsent {
			streams[i].absent = true
			continue
		}
		declaredBytes += uint64(size)
		if declaredBytes > maxPDBBytes {
			return nil, false
		}
		streams[i].size = size
		blockCounts[i] = uint32((uint64(size) + uint64(blockSize) - 1) / uint64(blockSize))
	}
	directoryOffset := 4 + int(streamCount)*4
	for i := range streams {
		count := blockCounts[i]
		if uint64(directoryOffset)+uint64(count)*4 > uint64(len(directory)) {
			return nil, false
		}
		streams[i].blocks = make([]uint32, count)
		for j := range streams[i].blocks {
			page := binary.LittleEndian.Uint32(directory[directoryOffset:])
			directoryOffset += 4
			if page >= blockCount {
				return nil, false
			}
			if _, exists := usedBlocks[page]; exists {
				return nil, false
			}
			usedBlocks[page] = struct{}{}
			streams[i].blocks[j] = page
		}
	}
	if directoryOffset != len(directory) {
		return nil, false
	}
	return &pdbMSFFile{data: data, blockSize: blockSize, streams: streams}, true
}

func (f *pdbMSFFile) readStream(index uint16) ([]byte, bool) {
	if int(index) >= len(f.streams) || f.streams[index].absent {
		return nil, false
	}
	stream := f.streams[index]
	data := make([]byte, stream.size)
	for i, page := range stream.blocks {
		start := i * int(f.blockSize)
		end := min(start+int(f.blockSize), len(data))
		pageStart := uint64(page) * uint64(f.blockSize)
		copy(data[start:end], f.data[pageStart:pageStart+uint64(end-start)])
	}
	return data, true
}

func pdbDBISubstreamSizes(dbi []byte) (module, contribution, sectionMap, source, typeServer, optional, ec int, ok bool) {
	fields := []int{24, 28, 32, 36, 40, 48, 52}
	sizes := make([]int, len(fields))
	var total uint64 = 64
	for i, offset := range fields {
		size := binary.LittleEndian.Uint32(dbi[offset:])
		total += uint64(size)
		if total > uint64(len(dbi)) {
			return 0, 0, 0, 0, 0, 0, 0, false
		}
		sizes[i] = int(size)
	}
	if total != uint64(len(dbi)) {
		return 0, 0, 0, 0, 0, 0, 0, false
	}
	return sizes[0], sizes[1], sizes[2], sizes[3], sizes[4], sizes[5], sizes[6], true
}

func parsePDBModules(msf *pdbMSFFile, moduleInfo []byte, sections []pdbSection, pdbInfo []byte) ([]SymbolFunction, bool) {
	var functions []SymbolFunction
	modules, procedures, records := 0, 0, 0
	sourceLines := 0
	sourceFiles, lineMappings := 0, 0
	for offset := 0; offset < len(moduleInfo); {
		if len(moduleInfo)-offset < 64 || modules == maxPDBModules {
			return nil, false
		}
		modules++
		moduleStreamIndex := binary.LittleEndian.Uint16(moduleInfo[offset+34:])
		symbolBytes := binary.LittleEndian.Uint32(moduleInfo[offset+36:])
		c11Bytes := binary.LittleEndian.Uint32(moduleInfo[offset+40:])
		c13Bytes := binary.LittleEndian.Uint32(moduleInfo[offset+44:])
		nameStart := offset + 64
		nameEnd := bytes.IndexByte(moduleInfo[nameStart:], 0)
		if nameEnd < 0 {
			return nil, false
		}
		objectStart := nameStart + nameEnd + 1
		objectEnd := bytes.IndexByte(moduleInfo[objectStart:], 0)
		if objectEnd < 0 {
			return nil, false
		}
		next := objectStart + objectEnd + 1
		next = (next + 3) &^ 3
		if next > len(moduleInfo) {
			return nil, false
		}
		offset = next
		if moduleStreamIndex == pdbModuleStreamAbsent {
			if symbolBytes != 0 || c11Bytes != 0 || c13Bytes != 0 {
				return nil, false
			}
			continue
		}
		moduleStream, ok := msf.readStream(moduleStreamIndex)
		streamEnd := uint64(symbolBytes) + uint64(c11Bytes) + uint64(c13Bytes)
		if !ok || symbolBytes < 4 || streamEnd > uint64(len(moduleStream)) || (c11Bytes != 0 && c13Bytes != 0) || !knownCodeViewSignature(binary.LittleEndian.Uint32(moduleStream)) {
			return nil, false
		}
		moduleFunctionsStart := len(functions)
		if !parsePDBModuleSymbols(moduleStream[4:symbolBytes], sections, &functions, &procedures, &records) {
			return nil, false
		}
		if c13Bytes > 0 {
			c13Start := uint64(symbolBytes) + uint64(c11Bytes)
			c13End := c13Start + uint64(c13Bytes)
			lines, ok := parsePDBC13(moduleStream[c13Start:c13End], msf, pdbInfo, sections, functions[moduleFunctionsStart:], &sourceLines, &sourceFiles, &lineMappings)
			if !ok {
				return nil, false
			}
			functions = append(functions, lines...)
		}
	}
	return functions, true
}

type pdbC13Line struct {
	offset uint32
	line   int
	file   string
}

func parsePDBC13(data []byte, msf *pdbMSFFile, pdbInfo []byte, sections []pdbSection, functions []SymbolFunction, sourceLineCount, sourceFileCount, lineMappingCount *int) ([]SymbolFunction, bool) {
	if len(data)%4 != 0 {
		return nil, false
	}
	var lineSubsections [][]byte
	var checksumData []byte
	var localStringTable []byte
	checksumSeen, stringTableSeen := false, false
	subsections := 0
	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 || subsections == maxPDBC13Subsections {
			return nil, false
		}
		subsections++
		rawKind := binary.LittleEndian.Uint32(data[offset:])
		size := binary.LittleEndian.Uint32(data[offset+4:])
		start := uint64(offset) + 8
		end := start + uint64(size)
		paddedEnd := (end + 3) &^ uint64(3)
		if end > uint64(len(data)) || paddedEnd > uint64(len(data)) {
			return nil, false
		}
		if rawKind&0x80000000 == 0 {
			payload := data[start:end]
			switch rawKind {
			case 0xf2: // DEBUG_S_LINES
				lineSubsections = append(lineSubsections, payload)
			case 0xf3: // DEBUG_S_STRINGTABLE
				if stringTableSeen {
					return nil, false
				}
				stringTableSeen, localStringTable = true, payload
			case 0xf4: // DEBUG_S_FILECHKSMS
				if checksumSeen {
					return nil, false
				}
				checksumSeen, checksumData = true, payload
			}
		}
		offset = int(paddedEnd)
	}

	if !checksumSeen {
		if len(lineSubsections) != 0 {
			return nil, false
		}
		if stringTableSeen && !validPDBStringTable(localStringTable) {
			return nil, false
		}
		return nil, true
	}
	stringTable := localStringTable
	if stringTableSeen {
		if !validPDBStringTable(stringTable) {
			return nil, false
		}
	} else {
		var ok bool
		stringTable, ok = pdbGlobalStringTable(msf, pdbInfo)
		if !ok {
			return nil, false
		}
	}
	checksums, ok := parsePDBC13Checksums(checksumData, stringTable, sourceFileCount)
	if !ok {
		return nil, false
	}
	index := newPDBFunctionIndex(functions)
	var sourceFunctions []SymbolFunction
	for _, subsection := range lineSubsections {
		lines, ok := parsePDBC13Lines(subsection, checksums, sections, index, sourceLineCount, lineMappingCount)
		if !ok {
			return nil, false
		}
		sourceFunctions = append(sourceFunctions, lines...)
	}
	return sourceFunctions, true
}

func parsePDBC13Checksums(data, stringTable []byte, sourceFileCount *int) (map[uint32]string, bool) {
	checksums := make(map[uint32]string)
	for offset := 0; offset < len(data); {
		if len(data)-offset < 6 || *sourceFileCount == maxPDBSourceFiles {
			return nil, false
		}
		fileNameOffset := binary.LittleEndian.Uint32(data[offset:])
		checksumSize, checksumKind := int(data[offset+4]), data[offset+5]
		expectedSize := -1
		switch checksumKind {
		case 0:
			expectedSize = 0
		case 1:
			expectedSize = 16
		case 2:
			expectedSize = 20
		case 3:
			expectedSize = 32
		default:
			return nil, false
		}
		if checksumSize != expectedSize {
			return nil, false
		}
		end := uint64(offset) + 6 + uint64(checksumSize)
		paddedEnd := (end + 3) &^ uint64(3)
		if paddedEnd > uint64(len(data)) {
			return nil, false
		}
		name, ok := pdbStringAt(stringTable, fileNameOffset)
		if !ok {
			return nil, false
		}
		checksums[uint32(offset)] = name
		*sourceFileCount = *sourceFileCount + 1
		offset = int(paddedEnd)
	}
	return checksums, true
}

type pdbFunctionNode struct {
	function SymbolFunction
	maxEnd   uint64
	left     *pdbFunctionNode
	right    *pdbFunctionNode
}

type pdbFunctionIndex struct {
	root *pdbFunctionNode
}

func newPDBFunctionIndex(functions []SymbolFunction) *pdbFunctionIndex {
	ordered := append([]SymbolFunction(nil), functions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Start != ordered[j].Start {
			return ordered[i].Start < ordered[j].Start
		}
		if ordered[i].End != ordered[j].End {
			return ordered[i].End < ordered[j].End
		}
		return ordered[i].Name < ordered[j].Name
	})
	return &pdbFunctionIndex{root: buildPDBFunctionTree(ordered)}
}

func buildPDBFunctionTree(functions []SymbolFunction) *pdbFunctionNode {
	if len(functions) == 0 {
		return nil
	}
	middle := len(functions) / 2
	node := &pdbFunctionNode{function: functions[middle]}
	node.left = buildPDBFunctionTree(functions[:middle])
	node.right = buildPDBFunctionTree(functions[middle+1:])
	node.maxEnd = node.function.End
	if node.left != nil {
		node.maxEnd = max(node.maxEnd, node.left.maxEnd)
	}
	if node.right != nil {
		node.maxEnd = max(node.maxEnd, node.right.maxEnd)
	}
	return node
}

func (index *pdbFunctionIndex) overlaps(start, end uint64) []SymbolFunction {
	var matches []SymbolFunction
	queryPDBFunctionTree(index.root, start, end, &matches)
	return matches
}

func queryPDBFunctionTree(node *pdbFunctionNode, start, end uint64, matches *[]SymbolFunction) {
	if node == nil || node.maxEnd <= start {
		return
	}
	queryPDBFunctionTree(node.left, start, end, matches)
	if node.function.Start < end && node.function.End > start {
		*matches = append(*matches, node.function)
	}
	if node.function.Start < end {
		queryPDBFunctionTree(node.right, start, end, matches)
	}
}

func parsePDBC13Lines(data []byte, checksums map[uint32]string, sections []pdbSection, functions *pdbFunctionIndex, sourceLineCount, lineMappingCount *int) ([]SymbolFunction, bool) {
	if len(data) < 12 {
		return nil, false
	}
	sectionOffset := binary.LittleEndian.Uint32(data)
	segment := binary.LittleEndian.Uint16(data[4:])
	flags := binary.LittleEndian.Uint16(data[6:])
	codeSize := binary.LittleEndian.Uint32(data[8:])
	if segment == 0 || int(segment) > len(sections) || flags&^uint16(1) != 0 || codeSize == 0 {
		return nil, false
	}
	section := sections[segment-1]
	sectionSize := max(section.virtualSize, section.rawSize)
	if section.flags&0x20000000 == 0 || sectionOffset >= sectionSize || codeSize > sectionSize-sectionOffset {
		return nil, false
	}
	hasColumns := flags&1 != 0
	var sourceFunctions []SymbolFunction
	for offset := 12; offset < len(data); {
		if len(data)-offset < 12 {
			return nil, false
		}
		nameIndex := binary.LittleEndian.Uint32(data[offset:])
		lineCount := binary.LittleEndian.Uint32(data[offset+4:])
		blockSize := binary.LittleEndian.Uint32(data[offset+8:])
		lineRecordSize := uint64(8)
		if hasColumns {
			lineRecordSize += 4
		}
		required := uint64(12) + uint64(lineCount)*lineRecordSize
		if blockSize < 12 || blockSize%4 != 0 || uint64(blockSize) > uint64(len(data)-offset) || required > uint64(blockSize) || uint64(*sourceLineCount)+uint64(lineCount) > maxPDBSourceLines {
			return nil, false
		}
		file, exists := checksums[nameIndex]
		if !exists {
			return nil, false
		}
		*sourceLineCount += int(lineCount)
		lineOffset := offset + 12
		var previous uint32
		lines := make([]pdbC13Line, 0, lineCount)
		for i := uint32(0); i < lineCount; i++ {
			entry := data[lineOffset:]
			codeOffset := binary.LittleEndian.Uint32(entry)
			lineInfo := binary.LittleEndian.Uint32(entry[4:])
			if codeOffset >= codeSize || (i != 0 && codeOffset < previous) {
				return nil, false
			}
			previous = codeOffset
			lineNumber := int(lineInfo & 0x00ffffff)
			lines = append(lines, pdbC13Line{offset: codeOffset, line: lineNumber, file: file})
			lineOffset += 8
		}
		if hasColumns {
			columnsEnd := lineOffset + int(lineCount)*4
			for columnOffset := lineOffset; columnOffset < columnsEnd; columnOffset += 4 {
				_ = binary.LittleEndian.Uint16(data[columnOffset:])
				_ = binary.LittleEndian.Uint16(data[columnOffset+2:])
			}
		}
		mapped, ok := pdbC13FunctionsForLines(lines, codeSize, section, sectionOffset, functions, lineMappingCount)
		if !ok {
			return nil, false
		}
		sourceFunctions = append(sourceFunctions, mapped...)
		offset += int(blockSize)
	}
	return sourceFunctions, true
}

func pdbC13FunctionsForLines(lines []pdbC13Line, codeSize uint32, section pdbSection, sectionOffset uint32, functions *pdbFunctionIndex, lineMappingCount *int) ([]SymbolFunction, bool) {
	if len(lines) == 0 {
		return nil, true
	}
	lineEnds := make([]uint32, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		lineEnds[i] = codeSize
		if i+1 < len(lines) {
			if lines[i+1].offset > lines[i].offset {
				lineEnds[i] = lines[i+1].offset
			} else {
				lineEnds[i] = lineEnds[i+1]
			}
		}
	}
	var sourceFunctions []SymbolFunction
	for i, line := range lines {
		endOffset := lineEnds[i]
		if line.line <= 0 || endOffset <= line.offset {
			continue
		}
		lineStart := uint64(section.virtualAddr) + uint64(sectionOffset) + uint64(line.offset)
		lineEnd := uint64(section.virtualAddr) + uint64(sectionOffset) + uint64(endOffset)
		matches := functions.overlaps(lineStart, lineEnd)
		if len(matches) > maxPDBLineMappings-*lineMappingCount {
			return nil, false
		}
		*lineMappingCount += len(matches)
		for _, function := range matches {
			start := max(lineStart, function.Start)
			end := min(lineEnd, function.End)
			if start < end {
				sourceFunctions = append(sourceFunctions, SymbolFunction{Start: start, End: end, Name: function.Name, File: line.file, Line: line.line})
			}
		}
	}
	return sourceFunctions, true
}

func pdbGlobalStringTable(msf *pdbMSFFile, pdbInfo []byte) ([]byte, bool) {
	streamIndex, ok := pdbNamedStreamIndex(pdbInfo, msf)
	if !ok {
		return nil, false
	}
	stream, ok := msf.readStream(streamIndex)
	if !ok || len(stream) < 16 || binary.LittleEndian.Uint32(stream) != 0xeffeeffe {
		return nil, false
	}
	hashVersion := binary.LittleEndian.Uint32(stream[4:])
	if hashVersion != 1 && hashVersion != 2 {
		return nil, false
	}
	stringBytes := binary.LittleEndian.Uint32(stream[8:])
	stringsEnd := uint64(12) + uint64(stringBytes)
	if stringsEnd+8 > uint64(len(stream)) {
		return nil, false
	}
	strings := stream[12:stringsEnd]
	hashCount := binary.LittleEndian.Uint32(stream[stringsEnd:])
	hashEnd := stringsEnd + 4 + uint64(hashCount)*4
	if hashCount > maxPDBSourceLines || hashEnd+4 != uint64(len(stream)) || !validPDBStringTable(strings) {
		return nil, false
	}
	for offset := stringsEnd + 4; offset < hashEnd; offset += 4 {
		id := binary.LittleEndian.Uint32(stream[offset:])
		if id != 0 {
			if _, ok := pdbStringAt(strings, id); !ok {
				return nil, false
			}
		}
	}
	if binary.LittleEndian.Uint32(stream[hashEnd:]) > maxPDBSourceLines {
		return nil, false
	}
	return strings, true
}

func pdbNamedStreamIndex(info []byte, msf *pdbMSFFile) (uint16, bool) {
	if len(info) < 32 {
		return 0, false
	}
	stringBytes := binary.LittleEndian.Uint32(info[28:])
	stringsStart := uint64(32)
	stringsEnd := stringsStart + uint64(stringBytes)
	if stringsEnd+8 > uint64(len(info)) {
		return 0, false
	}
	strings := info[stringsStart:stringsEnd]
	position := int(stringsEnd)
	size, ok := readPDBU32(info, &position)
	if !ok {
		return 0, false
	}
	capacity, ok := readPDBU32(info, &position)
	if !ok || size > maxPDBStreams || capacity == 0 || capacity > maxPDBNamedCapacity || size > capacity {
		return 0, false
	}
	present, ok := readPDBBitVector(info, &position, capacity)
	if !ok {
		return 0, false
	}
	deleted, ok := readPDBBitVector(info, &position, capacity)
	if !ok {
		return 0, false
	}
	presentCount := 0
	for bucket := uint32(0); bucket < capacity; bucket++ {
		isPresent := pdbBit(present, bucket)
		isDeleted := pdbBit(deleted, bucket)
		if isPresent && isDeleted {
			return 0, false
		}
		if isPresent {
			presentCount++
		}
	}
	if uint32(presentCount) != size || uint64(position)+uint64(size)*8 > uint64(len(info)) {
		return 0, false
	}
	var namesIndex uint32
	found := false
	for bucket := uint32(0); bucket < capacity; bucket++ {
		if !pdbBit(present, bucket) {
			continue
		}
		nameOffset := binary.LittleEndian.Uint32(info[position:])
		streamIndex := binary.LittleEndian.Uint32(info[position+4:])
		position += 8
		name, ok := pdbStringAt(strings, nameOffset)
		if !ok || streamIndex >= uint32(len(msf.streams)) || msf.streams[streamIndex].absent {
			return 0, false
		}
		if name == "/names" {
			if found {
				return 0, false
			}
			namesIndex, found = streamIndex, true
		}
	}
	if (len(info)-position)%4 != 0 || !found || namesIndex > 0xffff {
		return 0, false
	}
	return uint16(namesIndex), true
}

func readPDBBitVector(data []byte, position *int, capacity uint32) ([]uint32, bool) {
	count, ok := readPDBU32(data, position)
	maxWords := (uint64(capacity) + 31) / 32
	if !ok || uint64(count) > maxWords || uint64(*position)+uint64(count)*4 > uint64(len(data)) {
		return nil, false
	}
	words := make([]uint32, count)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(data[*position:])
		*position += 4
	}
	if len(words) > 0 && uint64(len(words)) == maxWords && capacity%32 != 0 {
		validMask := uint32(1)<<(capacity%32) - 1
		if words[len(words)-1]&^validMask != 0 {
			return nil, false
		}
	}
	return words, true
}

func pdbBit(words []uint32, index uint32) bool {
	word := index / 32
	return int(word) < len(words) && words[word]&(uint32(1)<<(index%32)) != 0
}

func readPDBU32(data []byte, position *int) (uint32, bool) {
	if *position < 0 || len(data)-*position < 4 {
		return 0, false
	}
	value := binary.LittleEndian.Uint32(data[*position:])
	*position += 4
	return value, true
}

func validPDBStringTable(data []byte) bool {
	if len(data) == 0 || data[0] != 0 {
		return false
	}
	for offset := 0; offset < len(data); {
		end := bytes.IndexByte(data[offset:], 0)
		if end < 0 {
			return false
		}
		offset += end + 1
	}
	return true
}

func pdbStringAt(table []byte, offset uint32) (string, bool) {
	if uint64(offset) >= uint64(len(table)) || (offset > 0 && table[offset-1] != 0) {
		return "", false
	}
	value := table[offset:]
	end := bytes.IndexByte(value, 0)
	if end <= 0 || end > maxPDBProcedureName {
		return "", false
	}
	return string(value[:end]), true
}

func parsePDBModuleSymbols(data []byte, sections []pdbSection, functions *[]SymbolFunction, procedures, records *int) bool {
	for offset := 0; offset < len(data); {
		if len(data)-offset < 4 || *records == maxPDBSymbolRecords {
			return false
		}
		length := int(binary.LittleEndian.Uint16(data[offset:]))
		if length < 2 || length > len(data)-offset-2 {
			return false
		}
		(*records)++
		kind := binary.LittleEndian.Uint16(data[offset+2:])
		if isPDBProcedureKind(kind) {
			if *procedures == maxPDBProcedures {
				return false
			}
			(*procedures)++
			procedure, ok := parsePDBProcedure(data[offset+4 : offset+2+length])
			if !ok {
				return false
			}
			if procedure.segment == 0 || int(procedure.segment) > len(sections) {
				return false
			}
			if procedure.size == 0 {
				offset += length + 2
				continue
			}
			section := sections[procedure.segment-1]
			sectionSize := max(section.virtualSize, section.rawSize)
			if section.flags&0x20000000 != 0 {
				if procedure.offset >= sectionSize || procedure.size > sectionSize-procedure.offset {
					return false
				}
				start := uint64(section.virtualAddr) + uint64(procedure.offset)
				end := start + uint64(procedure.size)
				if end > uint64(1)<<32 || end <= start {
					return false
				}
				*functions = append(*functions, SymbolFunction{Start: start, End: end, Name: procedure.name, Line: -1})
			}
		}
		offset += length + 2
	}
	return true
}

func parsePDBProcedure(data []byte) (pdbProcedure, bool) {
	var procedure pdbProcedure
	if len(data) < 36 {
		return procedure, false
	}
	procedure.size = binary.LittleEndian.Uint32(data[12:])
	procedure.offset = binary.LittleEndian.Uint32(data[28:])
	procedure.segment = binary.LittleEndian.Uint16(data[32:])
	name := data[35:]
	end := bytes.IndexByte(name, 0)
	if end < 0 || end == 0 || end > maxPDBProcedureName {
		return pdbProcedure{}, false
	}
	procedure.name = string(name[:end])
	return procedure, true
}

func isPDBProcedureKind(kind uint16) bool {
	switch kind {
	case 0x110f, 0x1110, 0x1146, 0x1147, 0x1155, 0x1156:
		return true
	default:
		return false
	}
}

func knownCodeViewSignature(signature uint32) bool {
	return signature == 4
}

func pdbInfoDebugID(info []byte) string {
	if len(info) < 28 {
		return ""
	}
	codeView := make([]byte, 24)
	copy(codeView, "RSDS")
	copy(codeView[4:20], info[12:28])
	binary.LittleEndian.PutUint32(codeView[20:], binary.LittleEndian.Uint32(info[8:]))
	return DebugKey(codeViewID(codeView))
}

func knownPDBInfoVersion(version uint32) bool {
	switch version {
	case 19941610, 19950623, 19950814, 19960307, 19970604, 19990604, 20000404, 20030901, 20091201, 20140508:
		return true
	default:
		return false
	}
}

func knownDBIVersion(version uint32) bool {
	switch version {
	case 930803, 19960307, 19970606, 19990903, 20091201:
		return true
	default:
		return false
	}
}
