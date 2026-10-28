package native

import (
	"encoding/binary"
	"encoding/hex"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxSymCacheBytes           = 64 << 20
	maxSymCacheEntries         = 1_000_000
	symCacheHeaderSize         = 72
	symCachePrefixSize         = 8
	symCacheFileSizeV7         = 12
	symCacheFileSizeV9         = 24
	symCacheFunctionSize       = 16
	symCacheSourceLocationSize = 16
	symCacheRangeSize          = 4
	symCacheNoIndex            = ^uint32(0)
)

type symCacheFile struct {
	path string
}

type symCacheFunction struct {
	name string
}

type symCacheSourceLocation struct {
	file     uint32
	line     uint32
	function uint32
	parent   uint32
	sentinel bool
}

// parseSymCache reads the public SymCache v7-v9 tables needed for physical
// frame symbolication. Variable information is extent-checked but not decoded.
func parseSymCache(data []byte) (string, []SymbolFunction, bool) {
	if len(data) < symCachePrefixSize+symCacheHeaderSize || len(data) > maxSymCacheBytes || string(data[:4]) != "SYMC" {
		return "", nil, false
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version < 7 || version > 9 {
		return "", nil, false
	}
	header := data[symCachePrefixSize : symCachePrefixSize+symCacheHeaderSize]
	debugID, ok := parseSymCacheDebugID(header[:32])
	if !ok {
		return "", nil, false
	}
	fileCount := binary.LittleEndian.Uint32(header[36:40])
	functionCount := binary.LittleEndian.Uint32(header[40:44])
	locationCount := binary.LittleEndian.Uint32(header[44:48])
	rangeCount := binary.LittleEndian.Uint32(header[48:52])
	stringBytes := binary.LittleEndian.Uint32(header[52:56])
	if fileCount > maxSymCacheEntries || functionCount > maxSymCacheEntries || locationCount > maxSymCacheEntries || rangeCount > maxSymCacheEntries || rangeCount > locationCount {
		return "", nil, false
	}

	cursor := symCachePrefixSize + symCacheHeaderSize
	fileSize := uint64(symCacheFileSizeV7)
	if version == 9 {
		fileSize = symCacheFileSizeV9
	}
	filesOffset, next, ok := symCacheSection(data, cursor, fileCount, fileSize)
	if !ok {
		return "", nil, false
	}
	functionsOffset, next, ok := symCacheSection(data, next, functionCount, symCacheFunctionSize)
	if !ok {
		return "", nil, false
	}
	locationsOffset, next, ok := symCacheSection(data, next, locationCount, symCacheSourceLocationSize)
	if !ok {
		return "", nil, false
	}
	rangesOffset, next, ok := symCacheSection(data, next, rangeCount, symCacheRangeSize)
	if !ok {
		return "", nil, false
	}
	stringsOffset, _, ok := symCacheSection(data, next, stringBytes, 1)
	if !ok {
		return "", nil, false
	}
	stringTable := data[stringsOffset : stringsOffset+int(stringBytes)]
	stringsByOffset, ok := parseSymCacheStrings(stringTable, version)
	if !ok {
		return "", nil, false
	}

	files := make([]symCacheFile, fileCount)
	for i := range files {
		record := data[filesOffset+i*int(fileSize) : filesOffset+(i+1)*int(fileSize)]
		compDir, valid := symCacheString(stringsByOffset, binary.LittleEndian.Uint32(record[0:4]), true)
		if !valid {
			return "", nil, false
		}
		directory, valid := symCacheString(stringsByOffset, binary.LittleEndian.Uint32(record[4:8]), true)
		if !valid {
			return "", nil, false
		}
		name, valid := symCacheString(stringsByOffset, binary.LittleEndian.Uint32(record[8:12]), false)
		if !valid {
			return "", nil, false
		}
		if version == 9 {
			for offset := 12; offset < symCacheFileSizeV9; offset += 4 {
				if _, valid := symCacheString(stringsByOffset, binary.LittleEndian.Uint32(record[offset:offset+4]), true); !valid {
					return "", nil, false
				}
			}
		}
		files[i].path = symCacheFullPath(compDir, directory, name)
	}

	functions := make([]symCacheFunction, functionCount)
	for i := range functions {
		record := data[functionsOffset+i*symCacheFunctionSize : functionsOffset+(i+1)*symCacheFunctionSize]
		name, valid := symCacheString(stringsByOffset, binary.LittleEndian.Uint32(record[0:4]), false)
		if !valid {
			return "", nil, false
		}
		if _, valid := symCacheString(stringsByOffset, binary.LittleEndian.Uint32(record[4:8]), true); !valid {
			return "", nil, false
		}
		functions[i].name = name
	}

	locations := make([]symCacheSourceLocation, locationCount)
	for i := range locations {
		record := data[locationsOffset+i*symCacheSourceLocationSize : locationsOffset+(i+1)*symCacheSourceLocationSize]
		location := symCacheSourceLocation{
			file:     binary.LittleEndian.Uint32(record[0:4]),
			line:     binary.LittleEndian.Uint32(record[4:8]),
			function: binary.LittleEndian.Uint32(record[8:12]),
			parent:   binary.LittleEndian.Uint32(record[12:16]),
		}
		location.sentinel = location.file == symCacheNoIndex && location.line == symCacheNoIndex && location.function == symCacheNoIndex && location.parent == symCacheNoIndex
		if !location.sentinel {
			if (location.file != symCacheNoIndex && location.file >= fileCount) ||
				(location.function != symCacheNoIndex && location.function >= functionCount) ||
				(location.parent != symCacheNoIndex && location.parent >= locationCount) {
				return "", nil, false
			}
		}
		locations[i] = location
	}
	if !symCacheInlineChainsValid(locations) {
		return "", nil, false
	}

	ranges := make([]uint32, rangeCount)
	for i := range ranges {
		ranges[i] = binary.LittleEndian.Uint32(data[rangesOffset+i*symCacheRangeSize : rangesOffset+(i+1)*symCacheRangeSize])
		if i > 0 && ranges[i] <= ranges[i-1] {
			return "", nil, false
		}
	}

	if version == 9 {
		variableHeader := binary.LittleEndian.Uint16(header[56:58])
		if variableHeader > 1 {
			return "", nil, false
		}
		if variableHeader == 1 {
			cursor := symCacheAlign8(stringsOffset + int(stringBytes))
			if cursor > len(data) || len(data)-cursor < 12 {
				return "", nil, false
			}
			variableCount := binary.LittleEndian.Uint32(data[cursor : cursor+4])
			locationInfoCount := binary.LittleEndian.Uint32(data[cursor+4 : cursor+8])
			typeCount := binary.LittleEndian.Uint32(data[cursor+8 : cursor+12])
			if variableCount > maxSymCacheEntries || locationInfoCount > maxSymCacheEntries || typeCount > maxSymCacheEntries {
				return "", nil, false
			}
			cursor += 12
			_, cursor, ok = symCacheSection(data, cursor, functionCount, 8)
			if !ok {
				return "", nil, false
			}
			_, cursor, ok = symCacheSection(data, cursor, variableCount, 20)
			if !ok {
				return "", nil, false
			}
			_, cursor, ok = symCacheSection(data, cursor, locationInfoCount, 16)
			if !ok {
				return "", nil, false
			}
			if _, _, ok = symCacheSection(data, cursor, typeCount, 16); !ok {
				return "", nil, false
			}
		}
	}

	functionsOut := make([]SymbolFunction, 0, len(ranges))
	locationStart := int(locationCount - rangeCount)
	for i, start := range ranges {
		location := locations[locationStart+i]
		if location.sentinel || location.function == symCacheNoIndex {
			continue
		}
		end := uint64(1) << 32
		if i+1 < len(ranges) {
			end = uint64(ranges[i+1])
		}
		line := -1
		if location.line != 0 && location.line != symCacheNoIndex {
			if uint64(location.line) > uint64(^uint(0)>>1) {
				return "", nil, false
			}
			line = int(location.line)
		}
		file := ""
		if location.file != symCacheNoIndex {
			file = files[location.file].path
		}
		functionsOut = append(functionsOut, SymbolFunction{
			Start: uint64(start), End: end, Name: functions[location.function].name, File: file, Line: line,
		})
	}
	return DebugKey(debugID), functionsOut, true
}

func parseSymCacheDebugID(raw []byte) (string, bool) {
	if len(raw) != 32 || !symCacheBytesAreZero(raw[20:31]) {
		return "", false
	}
	var id string
	switch raw[31] {
	case 0:
		id = hex.EncodeToString(raw[:16])
	case 1:
		if !symCacheBytesAreZero(raw[4:16]) {
			return "", false
		}
		timestamp := strconv.FormatUint(uint64(binary.BigEndian.Uint32(raw[:4])), 16)
		id = strings.Repeat("0", 8-len(timestamp)) + timestamp
	default:
		return "", false
	}
	appendix := binary.LittleEndian.Uint32(raw[16:20])
	if appendix != 0 || raw[31] == 1 {
		id += strconv.FormatUint(uint64(appendix), 16)
	}
	return id, true
}

func symCacheBytesAreZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func symCacheSection(data []byte, cursor int, count uint32, elementSize uint64) (int, int, bool) {
	start := symCacheAlign8(cursor)
	if start < cursor || start > len(data) {
		return 0, 0, false
	}
	length := uint64(count) * elementSize
	if elementSize != 0 && length/elementSize != uint64(count) || length > uint64(len(data)-start) {
		return 0, 0, false
	}
	end := start + int(length)
	return start, end, true
}

func symCacheAlign8(cursor int) int {
	if cursor > int(^uint(0)>>1)-7 {
		return int(^uint(0) >> 1)
	}
	return (cursor + 7) &^ 7
}

func parseSymCacheStrings(table []byte, version uint32) (map[uint32]string, bool) {
	stringsByOffset := make(map[uint32]string)
	for offset := 0; offset < len(table); {
		start := offset
		var length uint64
		if version == 7 {
			if len(table)-offset < 4 {
				return nil, false
			}
			length = uint64(binary.LittleEndian.Uint32(table[offset : offset+4]))
			offset += 4
		} else {
			var ok bool
			length, offset, ok = readSymCacheULEB128(table, offset)
			if !ok {
				return nil, false
			}
		}
		if length > uint64(len(table)-offset) || uint64(start) > uint64(^uint32(0)) {
			return nil, false
		}
		end := offset + int(length)
		value := table[offset:end]
		if !utf8.Valid(value) || len(stringsByOffset) >= maxSymCacheEntries {
			return nil, false
		}
		stringsByOffset[uint32(start)] = string(value)
		offset = end
	}
	return stringsByOffset, true
}

func readSymCacheULEB128(data []byte, offset int) (uint64, int, bool) {
	var value uint64
	for i := 0; i < 5; i++ {
		if offset >= len(data) {
			return 0, offset, false
		}
		current := data[offset]
		offset++
		if i == 4 && current&0xf0 != 0 {
			return 0, offset, false
		}
		value |= uint64(current&0x7f) << (7 * i)
		if current&0x80 == 0 {
			if i > 0 && value < uint64(1)<<(7*i) {
				return 0, offset, false
			}
			return value, offset, true
		}
	}
	return 0, offset, false
}

func symCacheString(stringsByOffset map[uint32]string, offset uint32, optional bool) (string, bool) {
	if offset == symCacheNoIndex {
		return "", optional
	}
	value, ok := stringsByOffset[offset]
	return value, ok
}

func symCacheInlineChainsValid(locations []symCacheSourceLocation) bool {
	state := make([]uint8, len(locations))
	chain := make([]int, 0, 8)
	for index := range locations {
		if state[index] != 0 {
			continue
		}
		chain = chain[:0]
		current := uint32(index)
		for current != symCacheNoIndex {
			position := int(current)
			if state[position] == 1 {
				return false
			}
			if state[position] == 2 {
				break
			}
			state[position] = 1
			chain = append(chain, position)
			if locations[position].sentinel {
				break
			}
			current = locations[position].parent
		}
		for _, position := range chain {
			state[position] = 2
		}
	}
	return true
}

func symCacheFullPath(compDir, directory, name string) string {
	full := symCacheJoinPath(compDir, directory)
	full = symCacheJoinPath(full, name)
	if full == "" || (strings.HasPrefix(full, "<") && strings.HasSuffix(full, ">")) {
		return full
	}
	windows := symCacheIsWindowsPath(full)
	unc := windows && (strings.HasPrefix(full, `\\`) || strings.HasPrefix(full, "//"))
	cleanInput := strings.ReplaceAll(full, `\`, "/")
	if unc {
		cleanInput = strings.TrimLeft(cleanInput, "/")
	}
	cleaned := path.Clean(cleanInput)
	if cleaned == "." {
		cleaned = ""
	}
	if windows {
		cleaned = strings.ReplaceAll(cleaned, "/", `\`)
		if unc {
			cleaned = `\\` + cleaned
		}
	}
	return cleaned
}

func symCacheJoinPath(base, other string) string {
	if len(other) >= 2 && other[0] == '<' && other[len(other)-1] == '>' {
		return other
	}
	if base == "" || symCacheIsAbsoluteWindowsPath(other) || strings.HasPrefix(other, "/") {
		return other
	}
	if other == "" {
		return base
	}
	if strings.HasPrefix(other, `\`) {
		if symCacheIsAbsoluteWindowsPath(base) && len(base) >= 2 {
			return base[:2] + other
		}
		return other
	}
	separator := "/"
	if symCacheIsWindowsPath(base) || symCacheIsWindowsPath(other) {
		separator = `\`
	}
	return strings.TrimRight(base, `/\`) + separator + strings.TrimLeft(other, `/\`)
}

func symCacheIsWindowsPath(value string) bool {
	return symCacheIsAbsoluteWindowsPath(value) || strings.Contains(value, `\`)
}

func symCacheIsAbsoluteWindowsPath(value string) bool {
	if strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//") {
		return true
	}
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (len(value) == 2 || value[2] == '/' || value[2] == '\\')
}
