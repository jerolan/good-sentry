package native

import (
	"bytes"
	"encoding/binary"
)

// Legacy public names have no size; only a verified direct executable section
// supplies the boundary used by the existing public fallback policy.
func parsePDBLegacyPublicFunctions(msf *pdbMSFFile, streamIndex uint16, mapData []byte, sections []pdbSection) ([]SymbolFunction, bool) {
	var sectionMap []pdbSectionMapEntry
	if len(mapData) > 0 {
		var ok bool
		sectionMap, ok = parsePDBSectionMap(mapData)
		if !ok {
			return nil, false
		}
		sectionMap = sectionMap[:binary.LittleEndian.Uint16(mapData[2:])]
	}
	data, ok := msf.readStream(streamIndex)
	if !ok || len(data)%4 != 0 {
		return nil, false
	}
	var public []pdbPublicFunction
	for offset, records := 0, 0; offset < len(data); records++ {
		if len(data)-offset < 4 || records == maxPDBSymbolRecords || offset%4 != 0 {
			return nil, false
		}
		length := int(binary.LittleEndian.Uint16(data[offset:]))
		total := length + 2
		if length < 2 || total%4 != 0 || total > len(data)-offset {
			return nil, false
		}
		kind := binary.LittleEndian.Uint16(data[offset+2:])
		body := data[offset+4 : offset+total]
		offset += total
		if kind == 0x0400 || kind == 0x0401 || kind == 0x0403 {
			if offset >= len(data) {
				return nil, false
			}
			extra := (int(data[offset]) + 1 + 3) &^ 3
			if extra > len(data)-offset {
				return nil, false
			}
			offset += extra
			continue
		}
		if kind != 0x1009 && kind != 0x0203 {
			continue
		}
		var flags, relative uint32
		var segment uint16
		var nameOffset int
		if kind == 0x1009 {
			if len(body) < 11 {
				return nil, false
			}
			flags = binary.LittleEndian.Uint32(body)
			relative = binary.LittleEndian.Uint32(body[4:])
			segment = binary.LittleEndian.Uint16(body[8:])
			nameOffset = 10
		} else {
			if len(body) < 9 {
				return nil, false
			}
			relative = binary.LittleEndian.Uint32(body)
			segment = binary.LittleEndian.Uint16(body[4:])
			// The 16-bit field is a type index, not public flags.
			nameOffset = 8
		}
		n := int(body[nameOffset])
		if n == 0 || n > len(body)-nameOffset-1 || flags & ^uint32(pdbPublicKnownFlags) != 0 {
			return nil, false
		}
		name := body[nameOffset+1 : nameOffset+1+n]
		tail := body[nameOffset+1+n:]
		if len(tail) > 3 || bytes.Count(tail, []byte{0}) != len(tail) {
			return nil, false
		}
		if flags&0xc != 0 || len(sectionMap) == 0 {
			continue
		}
		if segment == 0 || int(segment) > len(sectionMap) {
			return nil, false
		}
		mapping := sectionMap[segment-1]
		if mapping.flags&pdbSectionMapExecute == 0 {
			continue
		}
		if int(segment) > len(sections) || mapping.frame != segment || mapping.offset != 0 || mapping.overlay != 0 || mapping.group != 0 || mapping.flags&(pdbSectionMapAbsolute|pdbSectionMapGroup) != 0 {
			continue
		}
		section := sections[segment-1]
		if section.invalidMap || section.flags&pdbPESectionExecute == 0 {
			continue
		}
		sectionSize := max(section.virtualSize, section.rawSize)
		if mapping.sectionLength == 0 || mapping.sectionLength > sectionSize || relative >= mapping.sectionLength {
			return nil, false
		}
		start := uint64(section.virtualAddr) + uint64(relative)
		end := uint64(section.virtualAddr) + uint64(mapping.sectionLength)
		if end > uint64(1)<<32 || start >= end || len(public) == maxPDBPublicFunctions {
			return nil, false
		}
		public = append(public, pdbPublicFunction{section: int(segment) - 1, start: start, descriptorEnd: end, name: string(name)})
	}
	return inferredPDBPublicFunctions(public), true
}
