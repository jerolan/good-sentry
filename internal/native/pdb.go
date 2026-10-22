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
	functions, ok := parsePDBModules(msf, moduleData, sections)
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

func parsePDBModules(msf *pdbMSFFile, moduleInfo []byte, sections []pdbSection) ([]SymbolFunction, bool) {
	var functions []SymbolFunction
	modules, procedures, records := 0, 0, 0
	for offset := 0; offset < len(moduleInfo); {
		if len(moduleInfo)-offset < 64 || modules == maxPDBModules {
			return nil, false
		}
		modules++
		moduleStreamIndex := binary.LittleEndian.Uint16(moduleInfo[offset+34:])
		symbolBytes := binary.LittleEndian.Uint32(moduleInfo[offset+36:])
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
			if symbolBytes != 0 {
				return nil, false
			}
			continue
		}
		moduleStream, ok := msf.readStream(moduleStreamIndex)
		if !ok || symbolBytes < 4 || uint64(symbolBytes) > uint64(len(moduleStream)) || !knownCodeViewSignature(binary.LittleEndian.Uint32(moduleStream)) {
			return nil, false
		}
		if !parsePDBModuleSymbols(moduleStream[4:symbolBytes], sections, &functions, &procedures, &records) {
			return nil, false
		}
	}
	return functions, true
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
