package native

import (
	"bytes"
	"encoding/binary"
)

// parsePDBThunks is a second bounded pass over a validated module's
// symbol stream. Its ranges are appended only after source-line ownership has
// been determined from procedures in the same module.
func parsePDBThunks(data []byte, sections []pdbSection, procedures *int, signature uint32) ([]SymbolFunction, bool) {
	var result []SymbolFunction
	for offset := 0; offset < len(data); {
		if len(data)-offset < 4 {
			return nil, false
		}
		length := int(binary.LittleEndian.Uint16(data[offset:]))
		if length < 2 || length > len(data)-offset-2 {
			return nil, false
		}
		kind := binary.LittleEndian.Uint16(data[offset+2:])
		body := data[offset+4 : offset+2+length]
		offset += length + 2
		legacy := signature == 1 || signature == 2
		if legacy && kind != 0x0206 || !legacy && kind != 0x1102 {
			continue
		}
		if len(body) < 21 {
			return nil, false
		}
		if legacy && body[20] != 0 || !legacy && body[20] > 6 {
			continue
		}
		if len(body) < 22 {
			return nil, false
		}
		var name string
		if legacy {
			n := int(body[21])
			if n == 0 || n > len(body)-22 {
				return nil, false
			}
			tail := body[22+n:]
			if len(tail) > 3 || bytes.Count(tail, []byte{0}) != len(tail) {
				return nil, false
			}
			name = string(body[22 : 22+n])
		} else {
			n := bytes.IndexByte(body[21:], 0)
			if n <= 0 || n > maxPDBProcedureName {
				return nil, false
			}
			name = string(body[21 : 21+n])
		}
		if *procedures == maxPDBProcedures {
			return nil, false
		}
		(*procedures)++
		address := binary.LittleEndian.Uint32(body[12:])
		segment := binary.LittleEndian.Uint16(body[16:])
		size := uint32(binary.LittleEndian.Uint16(body[18:]))
		if segment == 0 || int(segment) > len(sections) {
			return nil, false
		}
		section := sections[segment-1]
		sectionSize := max(section.virtualSize, section.rawSize)
		if section.invalidMap || section.legacyMap && size > 0 && (address >= sectionSize || size > sectionSize-address) {
			return nil, false
		}
		if size == 0 || section.flags&pdbPESectionExecute == 0 {
			continue
		}
		if address >= sectionSize || size > sectionSize-address {
			return nil, false
		}
		start := uint64(section.virtualAddr) + uint64(address)
		end := start + uint64(size)
		if end > uint64(1)<<32 || end <= start {
			return nil, false
		}
		result = append(result, SymbolFunction{Start: start, End: end, Name: name, Line: -1})
	}
	return result, true
}
