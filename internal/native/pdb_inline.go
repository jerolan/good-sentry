package native

import (
	"bytes"
	"encoding/binary"
)

type pdbInlineSiteSet struct {
	sites []pdbInlineSite
}

type pdbInlineSite struct {
	parent         int
	procedure      int
	procedureStart uint64
	inlinee        uint32
	name           string
	depth          int
	annotations    []byte
	ranges         []pdbInlineRange
}

type pdbInlineRange struct {
	start, end uint32
	file       string
	line       int
}

type pdbInlinee struct {
	fileID uint32
	line   uint32
}

func parsePDBInlineIDNames(data []byte, recordCount *int) (map[uint32]string, bool) {
	const headerSize = 56
	if len(data) < headerSize {
		return nil, false
	}
	headerBytes := binary.LittleEndian.Uint32(data[4:])
	firstIndex := binary.LittleEndian.Uint32(data[8:])
	lastIndex := binary.LittleEndian.Uint32(data[12:])
	recordBytes := binary.LittleEndian.Uint32(data[16:])
	if headerBytes < headerSize || uint64(headerBytes) > uint64(len(data)) || firstIndex < 0x1000 || lastIndex < firstIndex || lastIndex-firstIndex > maxPDBInlineRecords || recordBytes%4 != 0 || uint64(headerBytes)+uint64(recordBytes) > uint64(len(data)) || uint64(*recordCount)+uint64(lastIndex-firstIndex) > maxPDBInlineRecords {
		return nil, false
	}
	position := int(headerBytes)
	end := position + int(recordBytes)
	names := make(map[uint32]string)
	for index := firstIndex; index < lastIndex; index++ {
		if end-position < 4 {
			return nil, false
		}
		length := int(binary.LittleEndian.Uint16(data[position:]))
		if length < 2 || length > end-position-2 {
			return nil, false
		}
		kind := binary.LittleEndian.Uint16(data[position+2:])
		if kind == 0x1601 || kind == 0x1602 {
			body := data[position+4 : position+2+length]
			if len(body) < 9 {
				return nil, false
			}
			nameStart := 8
			nameEnd := bytes.IndexByte(body[nameStart:], 0)
			if nameEnd <= 0 || nameEnd > maxPDBProcedureName || !validPDBTypePadding(body[nameStart+nameEnd+1:]) {
				return nil, false
			}
			names[index] = string(body[nameStart : nameStart+nameEnd])
		}
		position += length + 2
		(*recordCount)++
	}
	if position != end {
		return nil, false
	}
	return names, true
}

func validPDBTypePadding(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if len(data) > 15 || data[0] != byte(0xf0+len(data)) {
		return false
	}
	for i := range data {
		if data[i] != byte(0xf0+len(data)-i) {
			return false
		}
	}
	return true
}

func parsePDBC13InlineeLines(data []byte, checksums map[uint32]string, recordCount *int) (map[uint32]pdbInlinee, bool) {
	if len(data) < 4 {
		return nil, false
	}
	signature := binary.LittleEndian.Uint32(data)
	if signature > 1 {
		return nil, false
	}
	entries := make(map[uint32]pdbInlinee)
	for position := 4; position < len(data); {
		if len(data)-position < 12 || *recordCount == maxPDBInlineRecords {
			return nil, false
		}
		id := binary.LittleEndian.Uint32(data[position:])
		fileID := binary.LittleEndian.Uint32(data[position+4:])
		line := binary.LittleEndian.Uint32(data[position+8:])
		if id < 0x1000 || line == 0 {
			return nil, false
		}
		if _, ok := checksums[fileID]; !ok {
			return nil, false
		}
		if _, exists := entries[id]; exists {
			return nil, false
		}
		entries[id] = pdbInlinee{fileID: fileID, line: line}
		(*recordCount)++
		position += 12
		if signature == 1 {
			if len(data)-position < 4 {
				return nil, false
			}
			extraCount := binary.LittleEndian.Uint32(data[position:])
			position += 4
			if extraCount > maxPDBSourceFiles || uint64(extraCount)*4 > uint64(len(data)-position) {
				return nil, false
			}
			for i := uint32(0); i < extraCount; i++ {
				fileID = binary.LittleEndian.Uint32(data[position:])
				if _, ok := checksums[fileID]; !ok {
					return nil, false
				}
				position += 4
			}
		}
	}
	return entries, true
}

func attachPDBInlineSites(moduleFunctions []SymbolFunction, moduleFunctionsStart, procedureCount int, sites []pdbInlineSite, checksums map[uint32]string, inlinees map[uint32]pdbInlinee, names map[uint32]string, annotationCount *int) bool {
	resolved := make([]pdbInlineSite, len(sites))
	procedureHasSites := make([]bool, procedureCount)
	for i, site := range sites {
		procedureIndex := site.procedure - moduleFunctionsStart
		if procedureIndex < 0 || procedureIndex >= procedureCount || site.parent >= i || site.parent < -1 {
			return false
		}
		procedure := moduleFunctions[procedureIndex]
		inlinee, ok := inlinees[site.inlinee]
		if !ok {
			return false
		}
		name, ok := names[site.inlinee]
		if !ok || name == "" || procedure.End-procedure.Start > uint64(^uint32(0)) {
			return false
		}
		ranges, ok := parsePDBInlineAnnotations(site.annotations, inlinee, checksums, uint32(procedure.End-procedure.Start), annotationCount)
		if !ok || len(ranges) == 0 {
			return false
		}
		depth := 1
		if site.parent >= 0 {
			parent := resolved[site.parent]
			if parent.procedure != site.procedure || parent.procedureStart != procedure.Start || parent.depth == maxPDBInlineDepth || !pdbInlineRangesCover(parent.ranges, ranges) {
				return false
			}
			depth = parent.depth + 1
		}
		resolved[i] = pdbInlineSite{
			parent: site.parent, procedure: site.procedure, procedureStart: procedure.Start,
			inlinee: site.inlinee, name: name, depth: depth, annotations: site.annotations, ranges: ranges,
		}
		procedureHasSites[procedureIndex] = true
	}
	set := &pdbInlineSiteSet{sites: resolved}
	for i := 0; i < procedureCount; i++ {
		if procedureHasSites[i] {
			moduleFunctions[i].inlineSites = set
		}
	}
	return true
}

func pdbInlineRangesCover(parent, children []pdbInlineRange) bool {
	for i, current := range parent {
		if current.end <= current.start || (i > 0 && current.start < parent[i-1].end) {
			return false
		}
	}
	parentIndex := 0
	for i, child := range children {
		if child.end <= child.start || (i > 0 && child.start < children[i-1].end) {
			return false
		}
		position := child.start
		for parentIndex < len(parent) && parent[parentIndex].end <= position {
			parentIndex++
		}
		index := parentIndex
		for position < child.end {
			if index == len(parent) || parent[index].start > position || parent[index].end <= position {
				break
			}
			position = min(child.end, parent[index].end)
			if position < child.end {
				index++
			}
		}
		if position != child.end {
			return false
		}
		parentIndex = index
	}
	return true
}

func parsePDBInlineAnnotations(data []byte, inlinee pdbInlinee, checksums map[uint32]string, procedureSize uint32, annotationCount *int) ([]pdbInlineRange, bool) {
	var codeOffset uint32
	var base, end *uint32
	lineOffset := int64(0)
	currentLine, nextLine := int64(0), int64(0)
	hasCurrentLine, hasNextLine := false, false
	currentFile, nextFile := inlinee.fileID, uint32(0)
	hasNextFile := false
	var ranges []pdbInlineRange
	position := 0
	updateCodeOffset := func(delta uint32) bool {
		if base == nil {
			value := codeOffset
			base = &value
		} else if end == nil {
			value := *base + delta
			if value < *base || value > procedureSize {
				return false
			}
			end = &value
		}
		return true
	}
	updateLineOffset := func(delta int64) bool {
		value := lineOffset + delta
		if value < -int64(inlinee.line) || value > int64(^uint32(0))-int64(inlinee.line) {
			return false
		}
		lineOffset = value
		if base == nil || !hasCurrentLine {
			currentLine, hasCurrentLine = lineOffset, true
		} else {
			nextLine, hasNextLine = lineOffset, true
		}
		return true
	}
	flushRange := func() bool {
		if base == nil || end == nil || *end <= *base || *end > procedureSize {
			return false
		}
		file, ok := checksums[currentFile]
		line := int64(inlinee.line) + currentLine
		if !ok || line <= 0 || line > int64(^uint(0)>>1) {
			return false
		}
		ranges = append(ranges, pdbInlineRange{start: *base, end: *end, file: file, line: int(line)})
		value := *end
		base, end = &value, nil
		if hasNextLine {
			currentLine, hasNextLine = nextLine, false
		}
		if hasNextFile {
			currentFile, hasNextFile = nextFile, false
		}
		return true
	}
	for position < len(data) {
		op, ok := readPDBCompressedAnnotation(data, &position)
		if !ok {
			return nil, false
		}
		if op == 0 {
			for _, padding := range data[position:] {
				if padding != 0 {
					return nil, false
				}
			}
			position = len(data)
			break
		}
		if *annotationCount == maxPDBInlineAnnots {
			return nil, false
		}
		(*annotationCount)++
		var u1, u2 uint32
		var s1 int64
		switch op {
		case 1, 2, 3, 4, 5, 7, 8, 9, 13:
			u1, ok = readPDBCompressedAnnotation(data, &position)
		case 6, 10:
			u1, ok = readPDBCompressedAnnotation(data, &position)
			s1 = decodePDBSignedAnnotation(u1)
		case 11:
			u1, ok = readPDBCompressedAnnotation(data, &position)
			s1 = decodePDBSignedAnnotation(u1 >> 4)
			u1 &= 0xf
		case 12:
			u1, ok = readPDBCompressedAnnotation(data, &position)
			if ok {
				u2, ok = readPDBCompressedAnnotation(data, &position)
			}
		default:
			return nil, false
		}
		if !ok {
			return nil, false
		}
		switch op {
		case 1, 2, 3:
			if u1 > ^uint32(0)-codeOffset {
				return nil, false
			}
			codeOffset += u1
			if !updateCodeOffset(u1) {
				return nil, false
			}
		case 4:
			if !updateCodeOffset(u1) {
				return nil, false
			}
		case 5:
			if _, exists := checksums[u1]; !exists {
				return nil, false
			}
			if base == nil {
				currentFile = u1
			} else {
				nextFile, hasNextFile = u1, true
			}
		case 6:
			if !updateLineOffset(s1) {
				return nil, false
			}
		case 11:
			if u1 > ^uint32(0)-codeOffset {
				return nil, false
			}
			codeOffset += u1
			if !updateCodeOffset(u1) || !updateLineOffset(s1) {
				return nil, false
			}
		case 12:
			if u2 > ^uint32(0)-codeOffset {
				return nil, false
			}
			codeOffset += u2
			if !updateCodeOffset(u2) || !updateCodeOffset(u1) {
				return nil, false
			}
		}
		if end != nil {
			if !flushRange() {
				return nil, false
			}
		}
	}
	if end != nil && !flushRange() {
		return nil, false
	}
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end {
			return nil, false
		}
	}
	return ranges, len(ranges) != 0
}

func readPDBCompressedAnnotation(data []byte, position *int) (uint32, bool) {
	if *position < 0 || *position >= len(data) {
		return 0, false
	}
	first := data[*position]
	(*position)++
	if first&0x80 == 0 {
		return uint32(first), true
	}
	if first&0xc0 == 0x80 {
		if len(data)-*position < 1 {
			return 0, false
		}
		value := uint32(first&0x3f)<<8 | uint32(data[*position])
		(*position)++
		return value, true
	}
	if first&0xe0 == 0xc0 {
		if len(data)-*position < 3 {
			return 0, false
		}
		value := uint32(first&0x1f)<<24 | uint32(data[*position])<<16 | uint32(data[*position+1])<<8 | uint32(data[*position+2])
		*position += 3
		return value, true
	}
	return 0, false
}

func decodePDBSignedAnnotation(value uint32) int64 {
	if value&1 != 0 {
		return -int64(value >> 1)
	}
	return int64(value >> 1)
}

func pdbInlineFramesAt(set *pdbInlineSiteSet, procedureStart uint64, offset uint64) []InlineFrame {
	if set == nil || offset > uint64(^uint32(0)) {
		return nil
	}
	active := make([]*pdbInlineRange, len(set.sites))
	deepest := -1
	for i, site := range set.sites {
		if site.procedureStart != procedureStart {
			continue
		}
		for j := range site.ranges {
			candidate := &site.ranges[j]
			if uint32(offset) >= candidate.start && uint32(offset) < candidate.end {
				active[i] = candidate
				if deepest < 0 || site.depth > set.sites[deepest].depth || site.depth == set.sites[deepest].depth && candidate.end-candidate.start < active[deepest].end-active[deepest].start {
					deepest = i
				}
				break
			}
		}
	}
	if deepest < 0 {
		return nil
	}
	var chain []InlineFrame
	for index := deepest; index >= 0; index = set.sites[index].parent {
		if active[index] == nil || len(chain) == maxPDBInlineDepth {
			return nil
		}
		site := set.sites[index]
		chain = append(chain, InlineFrame{Function: site.name, File: active[index].file, Line: active[index].line})
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain
}
