package native

import (
	"bytes"
	"encoding/binary"
	"sort"
)

const maxPDBC11References = 1_000_000

type pdbSourceBudget struct {
	lines, files, mappings  int
	c11Bytes, c11References int
}

type pdbC11Range struct {
	start, end uint64
}

// parsePDBC11 reads the historical ST-name profile. Other C11 filename
// profiles are selected outside this reader, rather than guessed from bytes.
func parsePDBC11(data []byte, sections []pdbSection, procedures []SymbolFunction, budget *pdbSourceBudget) ([]SymbolFunction, bool) {
	if len(data) < 4 || len(data)%4 != 0 || len(data) > maxPDBBytes-budget.c11Bytes {
		return nil, false
	}
	budget.c11Bytes += len(data)
	fileCount := int(binary.LittleEndian.Uint16(data))
	segmentCount := int(binary.LittleEndian.Uint16(data[2:]))
	rootEnd := (uint64(4) + uint64(fileCount)*4 + uint64(segmentCount)*10 + 3) &^ uint64(3)
	if rootEnd > uint64(len(data)) || fileCount > maxPDBSourceFiles-budget.files || segmentCount > maxPDBC11References-budget.c11References {
		return nil, false
	}
	budget.files += fileCount
	budget.c11References += segmentCount
	roots, unknownRoots, ok := pdbC11RootRanges(data, fileCount, segmentCount, sections)
	if !ok {
		return nil, false
	}
	index := newPDBFunctionIndex(procedures)
	var sourceFunctions []SymbolFunction
	for fileIndex := 0; fileIndex < fileCount; fileIndex++ {
		fileOffset := uint64(binary.LittleEndian.Uint32(data[4+fileIndex*4:]))
		if fileOffset%4 != 0 || fileOffset+4 > uint64(len(data)) {
			return nil, false
		}
		tableCount := int(binary.LittleEndian.Uint16(data[fileOffset:]))
		nameOffset := fileOffset + 4 + uint64(tableCount)*12
		if nameOffset >= uint64(len(data)) || tableCount > maxPDBC11References-budget.c11References {
			return nil, false
		}
		budget.c11References += tableCount
		nameLength := uint64(data[nameOffset])
		nameEnd := nameOffset + 1 + nameLength
		if nameLength == 0 || (nameEnd+3)&^uint64(3) > uint64(len(data)) {
			return nil, false
		}
		name := data[nameOffset+1 : nameEnd]
		if bytes.IndexByte(name, 0) >= 0 {
			return nil, false
		}
		filename := string(name)
		for _, value := range name {
			if value >= 128 {
				// The historical MBCS code page is not identified by this data.
				// Still validate every referenced table before retaining functions.
				filename = ""
				break
			}
		}
		fileRanges := fileOffset + 4 + uint64(tableCount)*4
		for tableIndex := 0; tableIndex < tableCount; tableIndex++ {
			tableOffset := uint64(binary.LittleEndian.Uint32(data[fileOffset+4+uint64(tableIndex)*4:]))
			if tableOffset%4 != 0 || tableOffset+4 > uint64(len(data)) {
				return nil, false
			}
			segment := binary.LittleEndian.Uint16(data[tableOffset:])
			pairCount := int(binary.LittleEndian.Uint16(data[tableOffset+2:]))
			tableEnd := (tableOffset + 4 + uint64(pairCount)*6 + 3) &^ uint64(3)
			if tableEnd > uint64(len(data)) || pairCount > maxPDBSourceLines-budget.lines {
				return nil, false
			}
			budget.lines += pairCount
			rangeOffset := fileRanges + uint64(tableIndex)*8
			start := binary.LittleEndian.Uint32(data[rangeOffset:])
			last := binary.LittleEndian.Uint32(data[rangeOffset+4:])
			unknownFile := start == 0 && last == 0
			union := roots[segment]
			if segment == 0 || int(segment) > len(sections) || sections[segment-1].invalidMap || !unknownRoots[segment] && len(union) == 0 {
				return nil, false
			}
			section := sections[segment-1]
			var contribution pdbC11Range
			if !unknownFile {
				var ok bool
				contribution, ok = pdbC11SectionRange(segment, start, last, sections)
				if !ok {
					return nil, false
				}
				covering := sort.Search(len(union), func(i int) bool { return union[i].end > contribution.start })
				if !unknownRoots[segment] && (covering == len(union) || union[covering].start > contribution.start || union[covering].end < contribution.end) {
					return nil, false
				}
			}
			emit := filename != "" && section.flags&pdbPESectionExecute != 0
			var lines []pdbC13Line
			if emit {
				lines = make([]pdbC13Line, 0, pairCount)
			}
			var previous uint32
			for pairIndex := 0; pairIndex < pairCount; pairIndex++ {
				offset := binary.LittleEndian.Uint32(data[tableOffset+4+uint64(pairIndex)*4:])
				line := binary.LittleEndian.Uint16(data[tableOffset+4+uint64(pairCount)*4+uint64(pairIndex)*2:])
				if pairIndex != 0 && offset < previous {
					return nil, false
				}
				if unknownFile {
					if _, ok := pdbC11SectionRange(segment, offset, offset, sections); !ok {
						return nil, false
					}
					covering := sort.Search(len(union), func(i int) bool { return union[i].end > uint64(offset) })
					if !unknownRoots[segment] && (covering == len(union) || union[covering].start > uint64(offset)) {
						return nil, false
					}
				} else if uint64(offset) < contribution.start || uint64(offset) >= contribution.end {
					return nil, false
				}
				previous = offset
				if emit {
					lines = append(lines, pdbC13Line{offset: offset - start, line: int(line), file: filename})
				}
			}
			if emit {
				var mapped []SymbolFunction
				var ok bool
				if unknownFile {
					mapped, ok = pdbC11UnknownFileLines(lines, section, index, union, unknownRoots[segment], &budget.mappings)
				} else {
					mapped, ok = pdbC13FunctionsForLines(lines, uint32(contribution.end-contribution.start), section, start, index, &budget.mappings)
				}
				if !ok {
					return nil, false
				}
				sourceFunctions = append(sourceFunctions, mapped...)
			}
		}
	}
	return sourceFunctions, true
}

// Unknown file bounds do not authorize source attribution to a procedure
// which contains no line record of its own.
func pdbC11UnknownFileLines(lines []pdbC13Line, section pdbSection, procedures *pdbFunctionIndex, roots []pdbC11Range, unknownRoot bool, mappings *int) ([]SymbolFunction, bool) {
	ends := make([]uint64, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		ends[i] = uint64(max(section.virtualSize, section.rawSize))
		if i+1 < len(lines) {
			if lines[i+1].offset > lines[i].offset {
				ends[i] = uint64(lines[i+1].offset)
			} else {
				ends[i] = ends[i+1]
			}
		}
	}
	var result []SymbolFunction
	for i, line := range lines {
		if line.line <= 0 {
			continue
		}
		end := ends[i]
		if !unknownRoot {
			covering := sort.Search(len(roots), func(j int) bool { return roots[j].end > uint64(line.offset) })
			if covering == len(roots) || roots[covering].start > uint64(line.offset) {
				return nil, false
			}
			end = min(end, roots[covering].end)
		}
		startRVA := uint64(section.virtualAddr) + uint64(line.offset)
		endRVA := uint64(section.virtualAddr) + end
		matches := procedures.overlaps(startRVA, startRVA+1)
		if len(matches) > maxPDBLineMappings-*mappings {
			return nil, false
		}
		*mappings += len(matches)
		for _, procedure := range matches {
			end := min(endRVA, procedure.End)
			if startRVA < end {
				result = append(result, SymbolFunction{Start: startRVA, End: end, Name: procedure.Name, File: line.file, Line: line.line, procedureStart: procedure.Start})
			}
		}
	}
	return result, true
}

func pdbC11SectionRange(segment uint16, start, last uint32, sections []pdbSection) (pdbC11Range, bool) {
	if segment == 0 || int(segment) > len(sections) || start > last {
		return pdbC11Range{}, false
	}
	section := sections[segment-1]
	if section.invalidMap {
		return pdbC11Range{}, false
	}
	end := uint64(last) + 1
	if end > uint64(max(section.virtualSize, section.rawSize)) || uint64(section.virtualAddr)+end > uint64(1)<<32 {
		return pdbC11Range{}, false
	}
	return pdbC11Range{start: uint64(start), end: end}, true
}

func pdbC11RootRanges(data []byte, fileCount, segmentCount int, sections []pdbSection) (map[uint16][]pdbC11Range, map[uint16]bool, bool) {
	roots := make(map[uint16][]pdbC11Range)
	unknown := make(map[uint16]bool)
	rangesOffset := 4 + fileCount*4
	segmentsOffset := rangesOffset + segmentCount*8
	for i := 0; i < segmentCount; i++ {
		segment := binary.LittleEndian.Uint16(data[segmentsOffset+i*2:])
		start := binary.LittleEndian.Uint32(data[rangesOffset+i*8:])
		last := binary.LittleEndian.Uint32(data[rangesOffset+i*8+4:])
		interval, ok := pdbC11SectionRange(segment, start, last, sections)
		if !ok {
			return nil, nil, false
		}
		if start == 0 && last == 0 {
			unknown[segment] = true
			continue
		}
		roots[segment] = append(roots[segment], interval)
	}
	for segment, intervals := range roots {
		sort.Slice(intervals, func(i, j int) bool {
			if intervals[i].start != intervals[j].start {
				return intervals[i].start < intervals[j].start
			}
			return intervals[i].end < intervals[j].end
		})
		merged := intervals[:0]
		for _, interval := range intervals {
			if len(merged) > 0 && interval.start <= merged[len(merged)-1].end {
				merged[len(merged)-1].end = max(merged[len(merged)-1].end, interval.end)
			} else {
				merged = append(merged, interval)
			}
		}
		roots[segment] = merged
	}
	return roots, unknown, true
}
