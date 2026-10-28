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
	maxPDBPublicFunctions = 200_000
	maxPDBSymbolRecords   = 1_000_000
	maxPDBProcedureName   = 4096
	maxPDBC13Subsections  = 65_536
	maxPDBSourceLines     = 1_000_000
	maxPDBSourceFiles     = 65_536
	maxPDBLineMappings    = 1_000_000
	maxPDBNamedCapacity   = 65_536
	maxPDBInlineSites     = 100_000
	maxPDBInlineDepth     = 128
	maxPDBInlineRecords   = 1_000_000
	maxPDBInlineAnnots    = 1_000_000
	pdbStreamAbsent       = ^uint32(0)
	pdbModuleStreamAbsent = ^uint16(0)
	pdbPublicSymbolKind   = 0x110e
	pdbPublicFunctionFlag = 1 << 1
	pdbPublicKnownFlags   = 0x0f
	pdbSectionMapExecute  = 1 << 2
	pdbSectionMapSelector = 1 << 8
	pdbSectionMapAbsolute = 1 << 9
	pdbSectionMapGroup    = 1 << 10
	pdbPESectionExecute   = 0x20000000
	pdbDBIOldVersion      = ^uint32(0)
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
	legacyMap   bool
	invalidMap  bool
}

type pdbSectionMapEntry struct {
	flags         uint16
	overlay       uint16
	group         uint16
	frame         uint16
	offset        uint32
	sectionLength uint32
}

type pdbPublicFunction struct {
	section       int
	start         uint64
	descriptorEnd uint64
	name          string
}

type pdbProcedure struct {
	segment uint16
	offset  uint32
	size    uint32
	parent  uint32
	end     uint32
	name    string
}

type pdbSymbolScope struct {
	startOffset    uint32
	parentOffset   uint32
	endOffset      uint32
	opener         uint16
	endKind        uint16
	procedure      int
	inlineSite     int
	inlineAncestor int
	inlineDepth    int
}

func parsePDBSymbols(data []byte) (string, []SymbolFunction, bool) {
	return parsePDBSymbolsWithCompanions(data, nil)
}

func parsePDBSymbolsWithCompanions(data []byte, companions pdbCompanions) (string, []SymbolFunction, bool) {
	if len(data) > maxPDBBytes {
		return "", nil, false
	}
	var msf *pdbMSFFile
	var ok bool
	switch {
	case bytes.HasPrefix(data, msf7Magic):
		msf, ok = parsePDBMSF(data)
	case bytes.HasPrefix(data, msf2Magic):
		msf, ok = parsePDBMSF2(data)
	}
	if !ok {
		return "", nil, false
	}
	info, ok := msf.readStream(1)
	if !ok || pdbInfoHeaderSize(info) == 0 {
		return "", nil, false
	}
	age := binary.LittleEndian.Uint32(info[8:])
	dbi, ok := msf.readStream(3)
	if !ok || len(dbi) < 24 {
		return "", nil, false
	}
	debugID := pdbInfoDebugID(info)
	if debugID == "" {
		return "", nil, false
	}
	version := pdbDBIOldVersion
	moduleStart := 24
	var moduleSize, sectionContributionSize, sectionMapSize, sourceInfoSize, typeServerSize, optionalHeaderSize, ecSize int
	if binary.LittleEndian.Uint32(dbi) == ^uint32(0) {
		if len(dbi) < 64 {
			return "", nil, false
		}
		version = binary.LittleEndian.Uint32(dbi[4:])
		dbiAge := binary.LittleEndian.Uint32(dbi[8:])
		if !knownDBIVersion(version) || dbiAge != age && !(version == 19960307 && dbiAge == 0) {
			return "", nil, false
		}
		moduleStart = 64
		moduleSize, sectionContributionSize, sectionMapSize, sourceInfoSize, typeServerSize, optionalHeaderSize, ecSize, ok = pdbDBISubstreamSizes(dbi)
	} else {
		if binary.LittleEndian.Uint32(info) >= 19960307 {
			return "", nil, false
		}
		moduleSize, sectionContributionSize, sectionMapSize, sourceInfoSize, ok = pdbOldDBISubstreamSizes(dbi)
	}
	if !ok {
		return "", nil, false
	}
	optionalStart := moduleStart + moduleSize + sectionContributionSize + sectionMapSize + sourceInfoSize + typeServerSize + ecSize
	var sections []pdbSection
	sectionStreamIndex := pdbModuleStreamAbsent
	if optionalHeaderSize != 0 {
		if optionalHeaderSize < 12 || optionalHeaderSize%2 != 0 {
			return "", nil, false
		}
		sectionStreamIndex = binary.LittleEndian.Uint16(dbi[optionalStart+5*2:])
	}
	if (version == 19960307 || version == pdbDBIOldVersion) && sectionStreamIndex == pdbModuleStreamAbsent {
		candidate, found := companions[DebugKey(debugID)]
		if !found || candidate.ambiguous {
			return "", nil, false
		}
		sections = candidate.sections
	} else {
		if optionalHeaderSize == 0 {
			return "", nil, false
		}
		sectionsData, ok := msf.readStream(sectionStreamIndex)
		if !ok {
			return "", nil, false
		}
		sections, ok = parsePDBSections(sectionsData)
		if !ok {
			return "", nil, false
		}
	}
	sectionMapStart := moduleStart + moduleSize + sectionContributionSize
	sectionMapData := dbi[sectionMapStart : sectionMapStart+sectionMapSize]
	if version == 19960307 || version == pdbDBIOldVersion {
		sections, ok = directPDBLegacySections(sectionMapData, sections)
		if !ok {
			return "", nil, false
		}
	}
	moduleData := dbi[moduleStart : moduleStart+moduleSize]
	functions, ok := parsePDBModulesVersion(msf, moduleData, sections, info, version)
	if !ok {
		return "", nil, false
	}
	if version != pdbDBIOldVersion && binary.LittleEndian.Uint16(dbi[56:])&(1<<1) != 0 {
		publicFunctions, ok := parsePDBPublicFunctions(msf, binary.LittleEndian.Uint16(dbi[20:]), sectionMapData, sections)
		if !ok {
			return "", nil, false
		}
		functions = append(functions, publicFunctions...)
	}
	if len(functions) == 0 {
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

func parsePDBSections(data []byte) ([]pdbSection, bool) {
	if len(data) == 0 || len(data)%40 != 0 || len(data)/40 > maxPDBSections {
		return nil, false
	}
	sections := make([]pdbSection, len(data)/40)
	for i := range sections {
		header := data[i*40 : (i+1)*40]
		sections[i] = pdbSection{
			virtualSize: binary.LittleEndian.Uint32(header[8:]),
			virtualAddr: binary.LittleEndian.Uint32(header[12:]),
			rawSize:     binary.LittleEndian.Uint32(header[16:]),
			flags:       binary.LittleEndian.Uint32(header[36:]),
		}
	}
	return sections, true
}

// Only the observed direct mapping is admitted here. Other logical mappings
// require their own verified translation rather than guessed PE addresses.
func directPDBLegacySections(data []byte, physical []pdbSection) ([]pdbSection, bool) {
	entries, ok := parsePDBSectionMap(data)
	if !ok {
		return nil, false
	}
	logicalCount := int(binary.LittleEndian.Uint16(data[2:]))
	sections := make([]pdbSection, logicalCount)
	for i := range sections {
		entry := entries[i]
		sections[i] = pdbSection{legacyMap: true, invalidMap: true}
		if i >= len(physical) || entry.frame != uint16(i+1) || entry.offset != 0 || entry.overlay != 0 || entry.group != 0 || entry.sectionLength == 0 || entry.flags&(pdbSectionMapAbsolute|pdbSectionMapGroup) != 0 {
			continue
		}
		section := physical[i]
		section.virtualSize = min(section.virtualSize, entry.sectionLength)
		section.rawSize = min(section.rawSize, entry.sectionLength)
		section.legacyMap = true
		sections[i] = section
	}
	return sections, true
}

func parsePDBMSF(data []byte) (*pdbMSFFile, bool) {
	if len(data) < 56 || len(data) > maxPDBBytes || !bytes.HasPrefix(data, msf7Magic) {
		return nil, false
	}
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
		blockCounts[i] = uint32((uint64(size) + uint64(blockSize) - 1) / uint64(blockSize))
		if i == 0 {
			// Stream 0 is an obsolete directory snapshot, not an active stream.
			streams[i].absent = true
			continue
		}
		declaredBytes += uint64(size)
		if declaredBytes > maxPDBBytes {
			return nil, false
		}
		streams[i].size = size
	}
	directoryOffset := 4 + int(streamCount)*4
	for i := range streams {
		count := blockCounts[i]
		if uint64(directoryOffset)+uint64(count)*4 > uint64(len(directory)) {
			return nil, false
		}
		if i == 0 {
			directoryOffset += int(count) * 4
			continue
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

func pdbOldDBISubstreamSizes(dbi []byte) (module, contribution, sectionMap, source int, ok bool) {
	if len(dbi) < 24 {
		return 0, 0, 0, 0, false
	}
	var total uint64 = 24
	var sizes [4]int
	for i := range sizes {
		size := binary.LittleEndian.Uint32(dbi[8+i*4:])
		total += uint64(size)
		if total > uint64(len(dbi)) {
			return 0, 0, 0, 0, false
		}
		sizes[i] = int(size)
	}
	return sizes[0], sizes[1], sizes[2], sizes[3], total == uint64(len(dbi))
}

func parsePDBPublicFunctions(msf *pdbMSFFile, symbolRecordStream uint16, sectionMapData []byte, sections []pdbSection) ([]SymbolFunction, bool) {
	sectionMap, ok := parsePDBSectionMap(sectionMapData)
	if !ok {
		return nil, false
	}
	globalSymbols, ok := msf.readStream(symbolRecordStream)
	if !ok || len(globalSymbols)%4 != 0 {
		return nil, false
	}
	publicFunctions := make([]pdbPublicFunction, 0)
	for offset, records := 0, 0; offset < len(globalSymbols); records++ {
		if len(globalSymbols)-offset < 4 || records == maxPDBSymbolRecords || offset%4 != 0 {
			return nil, false
		}
		recordLength := int(binary.LittleEndian.Uint16(globalSymbols[offset:]))
		totalLength := recordLength + 2
		if recordLength < 2 || totalLength%4 != 0 || totalLength > len(globalSymbols)-offset {
			return nil, false
		}
		record := globalSymbols[offset : offset+totalLength]
		kind := binary.LittleEndian.Uint16(record[2:])
		if kind == pdbPublicSymbolKind {
			body := record[4:]
			if len(body) < 11 {
				return nil, false
			}
			nameBytes := body[10:]
			nameLength := bytes.IndexByte(nameBytes, 0)
			if nameLength < 1 || nameLength > maxPDBProcedureName {
				return nil, false
			}
			padding := nameBytes[nameLength+1:]
			if len(padding) > 3 {
				return nil, false
			}
			for i, value := range padding {
				if value != byte(0xf1+i) {
					return nil, false
				}
			}
			flags := binary.LittleEndian.Uint32(body)
			if flags&^uint32(pdbPublicKnownFlags) != 0 {
				return nil, false
			}
			if flags&pdbPublicFunctionFlag != 0 {
				if len(publicFunctions) == maxPDBPublicFunctions {
					return nil, false
				}
				segment := binary.LittleEndian.Uint16(body[8:])
				if segment == 0 || int(segment) > len(sectionMap) {
					return nil, false
				}
				mapping := sectionMap[segment-1]
				unsupportedFlags := uint16(pdbSectionMapSelector | pdbSectionMapAbsolute | pdbSectionMapGroup)
				if mapping.flags&pdbSectionMapExecute == 0 || mapping.flags&unsupportedFlags != 0 || mapping.frame == 0 || int(mapping.frame) > len(sections) {
					return nil, false
				}
				sectionIndex := int(mapping.frame - 1)
				section := sections[sectionIndex]
				sectionSize := max(section.virtualSize, section.rawSize)
				if section.flags&pdbPESectionExecute == 0 || mapping.offset > sectionSize || mapping.sectionLength > sectionSize-mapping.offset {
					return nil, false
				}
				symbolOffset := binary.LittleEndian.Uint32(body[4:])
				if symbolOffset >= mapping.sectionLength {
					return nil, false
				}
				start := uint64(section.virtualAddr) + uint64(mapping.offset) + uint64(symbolOffset)
				descriptorEnd := uint64(section.virtualAddr) + uint64(mapping.offset) + uint64(mapping.sectionLength)
				if descriptorEnd > uint64(1)<<32 || start >= descriptorEnd {
					return nil, false
				}
				publicFunctions = append(publicFunctions, pdbPublicFunction{
					section: sectionIndex, start: start, descriptorEnd: descriptorEnd, name: string(nameBytes[:nameLength]),
				})
			}
		}
		offset += totalLength
	}
	return inferredPDBPublicFunctions(publicFunctions), true
}

func parsePDBSectionMap(data []byte) ([]pdbSectionMapEntry, bool) {
	if len(data) < 4 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint16(data))
	logicalCount := int(binary.LittleEndian.Uint16(data[2:]))
	if count == 0 || count > maxPDBSections || logicalCount > count || len(data) != 4+count*20 {
		return nil, false
	}
	entries := make([]pdbSectionMapEntry, count)
	for i := range entries {
		record := data[4+i*20 : 4+(i+1)*20]
		entries[i] = pdbSectionMapEntry{
			flags: binary.LittleEndian.Uint16(record), overlay: binary.LittleEndian.Uint16(record[2:]),
			group: binary.LittleEndian.Uint16(record[4:]), frame: binary.LittleEndian.Uint16(record[6:]),
			offset: binary.LittleEndian.Uint32(record[12:]), sectionLength: binary.LittleEndian.Uint32(record[16:]),
		}
	}
	return entries, true
}

func inferredPDBPublicFunctions(publicFunctions []pdbPublicFunction) []SymbolFunction {
	sort.Slice(publicFunctions, func(i, j int) bool {
		if publicFunctions[i].section != publicFunctions[j].section {
			return publicFunctions[i].section < publicFunctions[j].section
		}
		if publicFunctions[i].start != publicFunctions[j].start {
			return publicFunctions[i].start < publicFunctions[j].start
		}
		return publicFunctions[i].name < publicFunctions[j].name
	})
	functions := make([]SymbolFunction, 0, len(publicFunctions))
	for groupStart := 0; groupStart < len(publicFunctions); {
		groupEnd := groupStart + 1
		for groupEnd < len(publicFunctions) && publicFunctions[groupEnd].section == publicFunctions[groupStart].section && publicFunctions[groupEnd].start == publicFunctions[groupStart].start {
			groupEnd++
		}
		if groupEnd < len(publicFunctions) && publicFunctions[groupEnd].section == publicFunctions[groupStart].section {
			nextStart := publicFunctions[groupEnd].start
			for i := groupStart; i < groupEnd; i++ {
				publicFunctions[i].descriptorEnd = min(publicFunctions[i].descriptorEnd, nextStart)
			}
		}
		for i := groupStart; i < groupEnd; i++ {
			function := publicFunctions[i]
			if function.descriptorEnd > function.start {
				functions = append(functions, SymbolFunction{Start: function.start, End: function.descriptorEnd, Name: function.name, Line: -1, publicFallback: true})
			}
		}
		groupStart = groupEnd
	}
	return functions
}

func parsePDBModules(msf *pdbMSFFile, moduleInfo []byte, sections []pdbSection, pdbInfo []byte) ([]SymbolFunction, bool) {
	return parsePDBModulesVersion(msf, moduleInfo, sections, pdbInfo, 0)
}

func parsePDBModulesVersion(msf *pdbMSFFile, moduleInfo []byte, sections []pdbSection, pdbInfo []byte, version uint32) ([]SymbolFunction, bool) {
	var functions []SymbolFunction
	modules, procedures, records := 0, 0, 0
	sourceBudget := pdbSourceBudget{}
	inlineSites, inlineRecords, inlineAnnotations, inlineeRecords := 0, 0, 0, 0
	var inlineNames map[uint32]string
	for offset := 0; offset < len(moduleInfo); {
		prefixSize := 64
		if version == 19960307 || version == pdbDBIOldVersion {
			prefixSize = 48
		}
		if len(moduleInfo)-offset < prefixSize || modules == maxPDBModules {
			return nil, false
		}
		modules++
		streamOffset := 34
		if version == 19960307 || version == pdbDBIOldVersion {
			streamOffset = 26
		}
		moduleStreamIndex := binary.LittleEndian.Uint16(moduleInfo[offset+streamOffset:])
		symbolBytes := binary.LittleEndian.Uint32(moduleInfo[offset+streamOffset+2:])
		c11Bytes := binary.LittleEndian.Uint32(moduleInfo[offset+streamOffset+6:])
		otherBytes := binary.LittleEndian.Uint32(moduleInfo[offset+streamOffset+10:])
		var c13Bytes, fpoBytes uint32
		if version == 19960307 || version == pdbDBIOldVersion {
			fpoBytes = otherBytes
		} else {
			c13Bytes = otherBytes
		}
		nameStart := offset + prefixSize
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
			if symbolBytes != 0 || c11Bytes != 0 || c13Bytes != 0 || fpoBytes != 0 {
				return nil, false
			}
			continue
		}
		moduleStream, ok := msf.readStream(moduleStreamIndex)
		streamEnd := uint64(symbolBytes) + uint64(c11Bytes) + uint64(c13Bytes) + uint64(fpoBytes)
		if !ok || symbolBytes < 4 || streamEnd > uint64(len(moduleStream)) || (c11Bytes != 0 && c13Bytes != 0) {
			return nil, false
		}
		signature := binary.LittleEndian.Uint32(moduleStream)
		if version == pdbDBIOldVersion && signature != 1 ||
			version != pdbDBIOldVersion && !knownCodeViewSignature(signature) ||
			signature == 2 && c13Bytes != 0 {
			return nil, false
		}
		moduleFunctionsStart := len(functions)
		var moduleInlineSites []pdbInlineSite
		if !parsePDBModuleSymbolsWithInlineSites(moduleStream[4:symbolBytes], sections, &functions, &procedures, &records, &moduleInlineSites, &inlineSites, signature) {
			return nil, false
		}
		moduleProceduresEnd := len(functions)
		if c11Bytes > 0 && (signature == 1 || signature == 2) && pdbInfoHeaderSize(pdbInfo) == 12 {
			c11End := uint64(symbolBytes) + uint64(c11Bytes)
			lines, ok := parsePDBC11(moduleStream[symbolBytes:c11End], sections, functions[moduleFunctionsStart:moduleProceduresEnd], &sourceBudget)
			if !ok {
				return nil, false
			}
			functions = append(functions, lines...)
		}
		if c13Bytes > 0 {
			c13Start := uint64(symbolBytes) + uint64(c11Bytes)
			c13End := c13Start + uint64(c13Bytes)
			lines, checksums, inlinees, ok := parsePDBC13WithInlinees(moduleStream[c13Start:c13End], msf, pdbInfo, sections, functions[moduleFunctionsStart:moduleProceduresEnd], &sourceBudget.lines, &sourceBudget.files, &sourceBudget.mappings, &inlineeRecords)
			if !ok {
				return nil, false
			}
			functions = append(functions, lines...)
			if len(moduleInlineSites) != 0 {
				if inlineNames == nil {
					inlineStream, ok := msf.readStream(4)
					if !ok {
						return nil, false
					}
					inlineNames, ok = parsePDBInlineIDNames(inlineStream, &inlineRecords)
					if !ok {
						return nil, false
					}
				}
				if !attachPDBInlineSites(functions[moduleFunctionsStart:], moduleFunctionsStart, moduleProceduresEnd-moduleFunctionsStart, moduleInlineSites, checksums, inlinees, inlineNames, &inlineAnnotations) {
					return nil, false
				}
			}
		} else if len(moduleInlineSites) != 0 {
			return nil, false
		}
		thunks, ok := parsePDBThunks(moduleStream[4:symbolBytes], sections, &procedures, signature)
		if !ok {
			return nil, false
		}
		functions = append(functions, thunks...)
	}
	return functions, true
}

type pdbC13Line struct {
	offset uint32
	line   int
	file   string
}

func parsePDBC13(data []byte, msf *pdbMSFFile, pdbInfo []byte, sections []pdbSection, functions []SymbolFunction, sourceLineCount, sourceFileCount, lineMappingCount *int) ([]SymbolFunction, bool) {
	lines, _, _, ok := parsePDBC13WithInlinees(data, msf, pdbInfo, sections, functions, sourceLineCount, sourceFileCount, lineMappingCount, new(int))
	return lines, ok
}

func parsePDBC13WithInlinees(data []byte, msf *pdbMSFFile, pdbInfo []byte, sections []pdbSection, functions []SymbolFunction, sourceLineCount, sourceFileCount, lineMappingCount, inlineeRecordCount *int) ([]SymbolFunction, map[uint32]string, map[uint32]pdbInlinee, bool) {
	if len(data)%4 != 0 {
		return nil, nil, nil, false
	}
	var lineSubsections [][]byte
	var inlineeSubsections [][]byte
	var checksumData []byte
	var localStringTable []byte
	checksumSeen, stringTableSeen, inlineeSeen := false, false, false
	subsections := 0
	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 || subsections == maxPDBC13Subsections {
			return nil, nil, nil, false
		}
		subsections++
		rawKind := binary.LittleEndian.Uint32(data[offset:])
		size := binary.LittleEndian.Uint32(data[offset+4:])
		start := uint64(offset) + 8
		end := start + uint64(size)
		paddedEnd := (end + 3) &^ uint64(3)
		if end > uint64(len(data)) || paddedEnd > uint64(len(data)) {
			return nil, nil, nil, false
		}
		if rawKind&0x80000000 == 0 {
			payload := data[start:end]
			switch rawKind {
			case 0xf2: // DEBUG_S_LINES
				lineSubsections = append(lineSubsections, payload)
			case 0xf3: // DEBUG_S_STRINGTABLE
				if stringTableSeen {
					return nil, nil, nil, false
				}
				stringTableSeen, localStringTable = true, payload
			case 0xf4: // DEBUG_S_FILECHKSMS
				if checksumSeen {
					return nil, nil, nil, false
				}
				checksumSeen, checksumData = true, payload
			case 0xf6: // DEBUG_S_INLINEELINES
				if inlineeSeen {
					return nil, nil, nil, false
				}
				inlineeSeen = true
				inlineeSubsections = append(inlineeSubsections, payload)
			}
		}
		offset = int(paddedEnd)
	}

	if !checksumSeen {
		if len(lineSubsections) != 0 || len(inlineeSubsections) != 0 {
			return nil, nil, nil, false
		}
		if stringTableSeen && !validPDBStringTable(localStringTable) {
			return nil, nil, nil, false
		}
		return nil, nil, nil, true
	}
	stringTable := localStringTable
	if stringTableSeen {
		if !validPDBStringTable(stringTable) {
			return nil, nil, nil, false
		}
	} else {
		var ok bool
		stringTable, ok = pdbGlobalStringTable(msf, pdbInfo)
		if !ok {
			return nil, nil, nil, false
		}
	}
	checksums, ok := parsePDBC13Checksums(checksumData, stringTable, sourceFileCount)
	if !ok {
		return nil, nil, nil, false
	}
	inlinees := make(map[uint32]pdbInlinee)
	for _, subsection := range inlineeSubsections {
		entries, ok := parsePDBC13InlineeLines(subsection, checksums, inlineeRecordCount)
		if !ok {
			return nil, nil, nil, false
		}
		for id, entry := range entries {
			if _, exists := inlinees[id]; exists {
				return nil, nil, nil, false
			}
			inlinees[id] = entry
		}
	}
	index := newPDBFunctionIndex(functions)
	var sourceFunctions []SymbolFunction
	for _, subsection := range lineSubsections {
		lines, ok := parsePDBC13Lines(subsection, checksums, sections, index, sourceLineCount, lineMappingCount)
		if !ok {
			return nil, nil, nil, false
		}
		sourceFunctions = append(sourceFunctions, lines...)
	}
	return sourceFunctions, checksums, inlinees, true
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
				sourceFunctions = append(sourceFunctions, SymbolFunction{Start: start, End: end, Name: function.Name, File: line.file, Line: line.line, procedureStart: function.Start})
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
			if uint64(id) < uint64(len(strings)) && strings[id] == 0 && strings[id-1] == 0 {
				continue
			}
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
	headerSize := pdbInfoHeaderSize(info)
	if headerSize == 0 || len(info) < headerSize+4 || binary.LittleEndian.Uint32(info) == 19941610 {
		return 0, false
	}
	stringBytes := binary.LittleEndian.Uint32(info[headerSize:])
	stringsStart := uint64(headerSize + 4)
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
	return parsePDBModuleSymbolsWithInlineSites(data, sections, functions, procedures, records, nil, nil, 4)
}

func parsePDBModuleSymbolsWithInlineSites(data []byte, sections []pdbSection, functions *[]SymbolFunction, procedures, records *int, sites *[]pdbInlineSite, siteCount *int, signature uint32) bool {
	currentProcedure := -1
	strictScopes := false
	scopeStackInvalid := false
	var scopes []pdbSymbolScope
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
		body := data[offset+4 : offset+2+length]
		// Scope pointers are relative to the symbol substream, including its
		// four-byte CodeView signature. data begins immediately after it.
		recordOffset := uint32(offset + 4)
		if kind == 0x114d {
			if sites == nil || siteCount == nil || currentProcedure < 0 || len(body) < 12 || *siteCount == maxPDBInlineSites {
				return false
			}
			if !strictScopes {
				if scopeStackInvalid || !validPDBInlineScopeStack(scopes, uint32(len(data))+4) {
					return false
				}
				strictScopes = true
			}
			if len(scopes) == 0 || scopes[len(scopes)-1].procedure != currentProcedure || !validPDBScopeChild(scopes, recordOffset, binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:]), 0x114d, 0x114e, uint32(len(data))+4) {
				return false
			}
			parentScope := scopes[len(scopes)-1]
			parent, inlineDepth := parentScope.inlineAncestor, parentScope.inlineDepth
			if inlineDepth == maxPDBInlineDepth {
				return false
			}
			index := len(*sites)
			*sites = append(*sites, pdbInlineSite{
				parent: parent, procedure: currentProcedure, procedureStart: (*functions)[currentProcedure].Start,
				inlinee: binary.LittleEndian.Uint32(body[8:]), annotations: append([]byte(nil), body[12:]...),
			})
			(*siteCount)++
			scopes = append(scopes, pdbSymbolScope{
				startOffset: recordOffset, parentOffset: binary.LittleEndian.Uint32(body), endOffset: binary.LittleEndian.Uint32(body[4:]),
				opener: kind, endKind: 0x114e, procedure: currentProcedure, inlineSite: index,
				inlineAncestor: index, inlineDepth: inlineDepth + 1,
			})
		} else if endKind, isEnd := pdbScopeEndKind(kind); isEnd {
			if length != 2 {
				return false
			}
			if len(scopes) == 0 || scopes[len(scopes)-1].endOffset != recordOffset || scopes[len(scopes)-1].endKind != endKind {
				if strictScopes {
					return false
				}
				scopeStackInvalid = true
			} else {
				closed := scopes[len(scopes)-1]
				scopes = scopes[:len(scopes)-1]
				if pdbProcedureScope(closed.opener) {
					currentProcedure = -1
				}
			}
		}
		if isPDBProcedureKind(kind) {
			if signature == 1 && kind != 0x0204 && kind != 0x0205 ||
				signature == 2 && kind != 0x100a && kind != 0x100b ||
				signature != 1 && (kind == 0x0204 || kind == 0x0205) {
				return false
			}
			if strictScopes && len(scopes) != 0 {
				return false
			}
			if !strictScopes {
				scopes = nil
				scopeStackInvalid = false
			}
			currentProcedure = -1
			if *procedures == maxPDBProcedures {
				return false
			}
			(*procedures)++
			procedure, ok := parsePDBProcedure(body, kind)
			if !ok {
				return false
			}
			if procedure.segment == 0 || int(procedure.segment) > len(sections) {
				return false
			}
			section := sections[procedure.segment-1]
			if section.invalidMap || section.legacyMap && procedure.size > 0 && (procedure.offset >= max(section.virtualSize, section.rawSize) || procedure.size > max(section.virtualSize, section.rawSize)-procedure.offset) {
				return false
			}
			if procedure.size > 0 {
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
					currentProcedure = len(*functions) - 1
				}
			}
			if strictScopes && (procedure.parent != 0 || procedure.end <= recordOffset || procedure.end >= uint32(len(data))+4) {
				return false
			}
			scopes = append(scopes, pdbSymbolScope{
				startOffset: recordOffset, parentOffset: procedure.parent, endOffset: procedure.end,
				opener: kind, endKind: pdbProcedureEndKind(kind), procedure: currentProcedure, inlineSite: -1, inlineAncestor: -1,
			})
		} else if endKind, isScope := pdbScopeOpenerEndKind(kind); isScope && kind != 0x114d {
			if len(body) < 8 {
				return false
			}
			parentOffset, endOffset := binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:])
			if strictScopes && !validPDBScopeChild(scopes, recordOffset, parentOffset, endOffset, kind, endKind, uint32(len(data))+4) {
				return false
			}
			inlineAncestor, inlineDepth := -1, 0
			if len(scopes) > 0 {
				inlineAncestor = scopes[len(scopes)-1].inlineAncestor
				inlineDepth = scopes[len(scopes)-1].inlineDepth
			}
			scopes = append(scopes, pdbSymbolScope{
				startOffset: recordOffset, parentOffset: parentOffset, endOffset: endOffset,
				opener: kind, endKind: endKind, procedure: currentProcedure, inlineSite: -1,
				inlineAncestor: inlineAncestor, inlineDepth: inlineDepth,
			})
		}
		offset += length + 2
	}
	return !strictScopes || len(scopes) == 0
}

func validPDBInlineScopeStack(scopes []pdbSymbolScope, symbolBytes uint32) bool {
	if len(scopes) == 0 {
		return false
	}
	root := scopes[0]
	if !isPDBProcedureKind(root.opener) || root.parentOffset != 0 || root.procedure < 0 || root.inlineSite >= 0 ||
		root.endKind != pdbProcedureEndKind(root.opener) || root.endOffset <= root.startOffset || root.endOffset >= symbolBytes {
		return false
	}
	for i, scope := range scopes {
		if i == 0 {
			continue
		}
		parent := scopes[i-1]
		wantEndKind, ok := pdbScopeOpenerEndKind(scope.opener)
		if !ok || scope.parentOffset != parent.startOffset || scope.procedure != root.procedure ||
			scope.endKind != wantEndKind || scope.startOffset <= parent.startOffset ||
			scope.endOffset <= scope.startOffset || scope.endOffset >= parent.endOffset || scope.endOffset >= symbolBytes {
			return false
		}
	}
	return true
}

func validPDBScopeChild(scopes []pdbSymbolScope, start, parent, end uint32, opener, endKind uint16, symbolBytes uint32) bool {
	if len(scopes) == 0 {
		return false
	}
	parentScope := scopes[len(scopes)-1]
	wantEndKind, ok := pdbScopeOpenerEndKind(opener)
	return ok && wantEndKind == endKind && parentScope.startOffset == parent &&
		start > parentScope.startOffset && end > start && end < parentScope.endOffset && end < symbolBytes
}

func pdbScopeEndKind(kind uint16) (uint16, bool) {
	switch kind {
	case 0x0006, 0x114e, 0x114f:
		return kind, true
	default:
		return 0, false
	}
}

func pdbScopeOpenerEndKind(kind uint16) (uint16, bool) {
	switch kind {
	case 0x1102, 0x1103, 0x1104:
		return 0x0006, true
	case 0x114d:
		return 0x114e, true
	default:
		return 0, false
	}
}

func pdbProcedureScope(kind uint16) bool {
	return isPDBProcedureKind(kind)
}

func pdbProcedureEndKind(kind uint16) uint16 {
	switch kind {
	case 0x1146, 0x1147, 0x1156:
		return 0x114f
	default:
		return 0x0006
	}
}

func parsePDBProcedure(data []byte, kind uint16) (pdbProcedure, bool) {
	var procedure pdbProcedure
	nameOffset, addressOffset, segmentOffset := 35, 28, 32
	if kind == 0x0204 || kind == 0x0205 {
		nameOffset, addressOffset, segmentOffset = 33, 24, 28
	}
	if len(data) <= nameOffset {
		return procedure, false
	}
	procedure.size = binary.LittleEndian.Uint32(data[12:])
	procedure.parent = binary.LittleEndian.Uint32(data)
	procedure.end = binary.LittleEndian.Uint32(data[4:])
	procedure.offset = binary.LittleEndian.Uint32(data[addressOffset:])
	procedure.segment = binary.LittleEndian.Uint16(data[segmentOffset:])
	name := data[nameOffset:]
	if kind == 0x100a || kind == 0x100b || kind == 0x0204 || kind == 0x0205 {
		length := int(name[0])
		if length == 0 || length > len(name)-1 {
			return pdbProcedure{}, false
		}
		procedure.name = string(name[1 : 1+length])
		return procedure, true
	}
	end := bytes.IndexByte(name, 0)
	if end < 0 || end == 0 || end > maxPDBProcedureName {
		return pdbProcedure{}, false
	}
	procedure.name = string(name[:end])
	return procedure, true
}

func isPDBProcedureKind(kind uint16) bool {
	switch kind {
	case 0x0204, 0x0205, 0x100a, 0x100b, 0x110f, 0x1110, 0x1146, 0x1147, 0x1155, 0x1156:
		return true
	default:
		return false
	}
}

func knownCodeViewSignature(signature uint32) bool {
	return signature == 2 || signature == 4
}

func pdbInfoDebugID(info []byte) string {
	headerSize := pdbInfoHeaderSize(info)
	if headerSize == 0 {
		return ""
	}
	if headerSize == 12 {
		codeView := make([]byte, 17)
		copy(codeView, "NB10")
		copy(codeView[8:16], info[4:12])
		return DebugKey(codeViewDebugID(codeView))
	}
	codeView := make([]byte, 24)
	copy(codeView, "RSDS")
	copy(codeView[4:20], info[12:28])
	binary.LittleEndian.PutUint32(codeView[20:], binary.LittleEndian.Uint32(info[8:]))
	return DebugKey(codeViewID(codeView))
}

func pdbInfoHeaderSize(info []byte) int {
	if len(info) < 12 || !knownPDBInfoVersion(binary.LittleEndian.Uint32(info)) {
		return 0
	}
	if binary.LittleEndian.Uint32(info) < 20000404 {
		return 12
	}
	if len(info) < 28 {
		return 0
	}
	return 28
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
