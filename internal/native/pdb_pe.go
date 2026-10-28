package native

import (
	"bytes"
	"encoding/binary"
)

const maxPDBPEDebugRecords = 4096

type pdbPESectionExtent struct {
	virtualAddress uint64
	virtualSize    uint64
	rawOffset      uint64
	rawSize        uint64
}

// parsePDBCompanionPE reads only the identity and section metadata needed by a
// legacy PDB. The image itself never supplies symbols or an executable object.
func parsePDBCompanionPE(data []byte) (string, []pdbSection, bool) {
	if len(data) < 64 || len(data) > maxPDBBytes || !bytes.Equal(data[:2], []byte("MZ")) {
		return "", nil, false
	}
	peOffset := uint64(binary.LittleEndian.Uint32(data[60:]))
	if peOffset < 64 || !pdbPEFileExtent(data, peOffset, 24) || !bytes.Equal(data[peOffset:peOffset+4], []byte{'P', 'E', 0, 0}) {
		return "", nil, false
	}
	coff := data[peOffset+4 : peOffset+24]
	sectionCount := uint64(binary.LittleEndian.Uint16(coff[2:]))
	optionalSize := uint64(binary.LittleEndian.Uint16(coff[16:]))
	optionalOffset := peOffset + 24
	if sectionCount == 0 || sectionCount > maxPDBSections || optionalSize < 2 || !pdbPEFileExtent(data, optionalOffset, optionalSize) {
		return "", nil, false
	}
	optional := data[optionalOffset : optionalOffset+optionalSize]
	var directoriesOffset uint64
	switch binary.LittleEndian.Uint16(optional) {
	case 0x10b: // PE32.
		directoriesOffset = 96
	case 0x20b: // PE32+.
		directoriesOffset = 112
	default:
		return "", nil, false
	}
	if optionalSize < directoriesOffset {
		return "", nil, false
	}
	directoryCount := uint64(binary.LittleEndian.Uint32(optional[directoriesOffset-4:]))
	if directoryCount <= 6 || directoryCount > (optionalSize-directoriesOffset)/8 {
		return "", nil, false
	}
	headersSize := uint64(binary.LittleEndian.Uint32(optional[60:]))
	sectionsOffset := optionalOffset + optionalSize
	sectionsSize := sectionCount * 40
	if !pdbPEFileExtent(data, sectionsOffset, sectionsSize) || headersSize < sectionsOffset+sectionsSize || headersSize > uint64(len(data)) {
		return "", nil, false
	}
	sectionData := data[sectionsOffset : sectionsOffset+sectionsSize]
	sections, ok := parsePDBSections(sectionData)
	if !ok {
		return "", nil, false
	}
	extents := make([]pdbPESectionExtent, len(sections))
	for i, section := range sections {
		header := sectionData[i*40 : (i+1)*40]
		extent := pdbPESectionExtent{
			virtualAddress: uint64(section.virtualAddr),
			virtualSize:    uint64(max(section.virtualSize, section.rawSize)),
			rawOffset:      uint64(binary.LittleEndian.Uint32(header[20:])),
			rawSize:        uint64(section.rawSize),
		}
		if extent.virtualSize > uint64(1)<<32-extent.virtualAddress || extent.rawSize != 0 && !pdbPEFileExtent(data, extent.rawOffset, extent.rawSize) {
			return "", nil, false
		}
		extents[i] = extent
	}
	debugDirectory := optional[directoriesOffset+6*8 : directoriesOffset+7*8]
	debugRVA := uint64(binary.LittleEndian.Uint32(debugDirectory))
	debugSize := uint64(binary.LittleEndian.Uint32(debugDirectory[4:]))
	if debugRVA == 0 || debugSize == 0 || debugSize%28 != 0 || debugSize/28 > maxPDBPEDebugRecords {
		return "", nil, false
	}
	debugOffset, ok := pdbPERangeFileOffset(data, headersSize, extents, debugRVA, debugSize)
	if !ok {
		return "", nil, false
	}
	debugID := ""
	for offset := debugOffset; offset < debugOffset+debugSize; offset += 28 {
		record := data[offset : offset+28]
		if binary.LittleEndian.Uint32(record[12:]) != 2 { // IMAGE_DEBUG_TYPE_CODEVIEW.
			continue
		}
		size := uint64(binary.LittleEndian.Uint32(record[16:]))
		rva := uint64(binary.LittleEndian.Uint32(record[20:]))
		pointer := uint64(binary.LittleEndian.Uint32(record[24:]))
		if size < 4 || !pdbPEFileExtent(data, pointer, size) {
			return "", nil, false
		}
		if rva != 0 {
			mappedOffset, ok := pdbPERangeFileOffset(data, headersSize, extents, rva, size)
			if !ok || mappedOffset != pointer {
				return "", nil, false
			}
		}
		codeView := data[pointer : pointer+size]
		if !bytes.Equal(codeView[:4], []byte("NB10")) {
			continue
		}
		// Use the shared NB10 framing and identity contract: the path must
		// terminate in NUL; neither its spelling nor dwOffset selects a file.
		identity := DebugKey(codeViewDebugID(codeView))
		if identity == "" || debugID != "" && debugID != identity {
			return "", nil, false
		}
		debugID = identity
	}
	if debugID == "" {
		return "", nil, false
	}
	return debugID, sections, true
}

func pdbPEFileExtent(data []byte, offset, size uint64) bool {
	return offset <= uint64(len(data)) && size <= uint64(len(data))-offset
}

// pdbPERangeFileOffset requires the complete range to belong to exactly one
// file-backed RVA region. Virtual zero-fill and overlapping mappings cannot
// identify file bytes, even if one candidate alone could supply the range.
func pdbPERangeFileOffset(data []byte, headersSize uint64, sections []pdbPESectionExtent, rva, size uint64) (uint64, bool) {
	if size == 0 || rva >= uint64(1)<<32 || size > uint64(1)<<32-rva {
		return 0, false
	}
	end := rva + size
	var offset uint64
	found := false
	if rva < headersSize {
		if end > headersSize {
			return 0, false
		}
		offset, found = rva, true
	}
	for _, section := range sections {
		sectionEnd := section.virtualAddress + section.virtualSize
		if end <= section.virtualAddress || rva >= sectionEnd || section.virtualSize == 0 {
			continue
		}
		if found || rva < section.virtualAddress || end > sectionEnd {
			return 0, false
		}
		delta := rva - section.virtualAddress
		if delta > section.rawSize || size > section.rawSize-delta {
			return 0, false
		}
		offset, found = section.rawOffset+delta, true
	}
	return offset, found && pdbPEFileExtent(data, offset, size)
}
