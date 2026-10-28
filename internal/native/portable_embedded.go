package native

import (
	"bytes"
	"compress/flate"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"io"
)

// embeddedPortablePDB returns the uncompressed metadata and its conventional
// PE CodeView alias, after checking the image's debug records.
const maxEmbeddedPortableExpansion = 128 * 1024 * 1024

func embeddedPortablePDB(data []byte, expansion *int) ([]byte, string, bool) {
	if len(data) < 64 || len(data) > maxPDBBytes || !bytes.Equal(data[:2], []byte("MZ")) {
		return nil, "", false
	}
	peOffset := uint64(binary.LittleEndian.Uint32(data[60:]))
	if peOffset < 64 || !pdbPEFileExtent(data, peOffset, 24) || !bytes.Equal(data[peOffset:peOffset+4], []byte{'P', 'E', 0, 0}) {
		return nil, "", false
	}
	coff := data[peOffset+4 : peOffset+24]
	count := uint64(binary.LittleEndian.Uint16(coff[2:]))
	size := uint64(binary.LittleEndian.Uint16(coff[16:]))
	optionalOffset := peOffset + 24
	if count == 0 || count > maxPDBSections || size < 2 || !pdbPEFileExtent(data, optionalOffset, size) {
		return nil, "", false
	}
	optional := data[optionalOffset : optionalOffset+size]
	var directoryOffset uint64
	switch binary.LittleEndian.Uint16(optional) {
	case 0x10b:
		directoryOffset = 96
	case 0x20b:
		directoryOffset = 112
	default:
		return nil, "", false
	}
	if size < directoryOffset {
		return nil, "", false
	}
	directories := uint64(binary.LittleEndian.Uint32(optional[directoryOffset-4:]))
	if directories <= 6 || directories > (size-directoryOffset)/8 {
		return nil, "", false
	}
	headersSize := uint64(binary.LittleEndian.Uint32(optional[60:]))
	sectionsOffset := optionalOffset + size
	if !pdbPEFileExtent(data, sectionsOffset, count*40) || headersSize < sectionsOffset+count*40 || headersSize > uint64(len(data)) {
		return nil, "", false
	}
	sectionData := data[sectionsOffset : sectionsOffset+count*40]
	sections, ok := parsePDBSections(sectionData)
	if !ok {
		return nil, "", false
	}
	extents := make([]pdbPESectionExtent, len(sections))
	for i, section := range sections {
		header := sectionData[i*40 : (i+1)*40]
		extent := pdbPESectionExtent{virtualAddress: uint64(section.virtualAddr), virtualSize: uint64(max(section.virtualSize, section.rawSize)), rawOffset: uint64(binary.LittleEndian.Uint32(header[20:])), rawSize: uint64(section.rawSize)}
		if extent.virtualSize > uint64(1)<<32-extent.virtualAddress || extent.rawSize != 0 && !pdbPEFileExtent(data, extent.rawOffset, extent.rawSize) {
			return nil, "", false
		}
		extents[i] = extent
	}
	directory := optional[directoryOffset+6*8 : directoryOffset+7*8]
	rva, length := uint64(binary.LittleEndian.Uint32(directory)), uint64(binary.LittleEndian.Uint32(directory[4:]))
	if rva == 0 || length == 0 || length%28 != 0 || length/28 > maxPDBPEDebugRecords {
		return nil, "", false
	}
	start, ok := pdbPERangeFileOffset(data, headersSize, extents, rva, length)
	if !ok {
		return nil, "", false
	}
	var embedded []byte
	var cvID, alias string
	var checksums [][]byte
	for pos := start; pos < start+length; pos += 28 {
		record := data[pos : pos+28]
		kind := binary.LittleEndian.Uint32(record[12:])
		if kind != 2 && kind != 17 && kind != 19 {
			continue
		}
		major, minor := binary.LittleEndian.Uint16(record[8:]), binary.LittleEndian.Uint16(record[10:])
		if kind == 2 && minor != 0x504d {
			continue
		}
		n, virtual, pointer := uint64(binary.LittleEndian.Uint32(record[16:])), uint64(binary.LittleEndian.Uint32(record[20:])), uint64(binary.LittleEndian.Uint32(record[24:]))
		if n == 0 || !pdbPEFileExtent(data, pointer, n) {
			return nil, "", false
		}
		if virtual != 0 {
			mapped, valid := pdbPERangeFileOffset(data, headersSize, extents, virtual, n)
			if !valid || mapped != pointer {
				return nil, "", false
			}
		}
		body := data[pointer : pointer+n]
		switch kind {
		case 2:
			if major != 0x100 || len(body) < 25 || !bytes.Equal(body[:4], []byte("RSDS")) || binary.LittleEndian.Uint32(body[20:]) != 1 || bytes.IndexByte(body[24:], 0) < 0 {
				return nil, "", false
			}
			idBytes := make([]byte, 20)
			copy(idBytes, body[4:20])
			binary.LittleEndian.PutUint32(idBytes[16:], binary.LittleEndian.Uint32(record[4:]))
			id := portableIdentity(idBytes)
			binary.LittleEndian.PutUint32(idBytes[16:], 1)
			currentAlias := portableIdentity(idBytes)
			if currentAlias == "" || cvID != "" && (cvID != id || alias != currentAlias) {
				return nil, "", false
			}
			cvID, alias = id, currentAlias
		case 17:
			if embedded != nil || minor != 0x100 || binary.LittleEndian.Uint32(record[4:]) != 0 || len(body) < 9 || !bytes.Equal(body[:4], []byte("MPDB")) {
				return nil, "", false
			}
			expanded := binary.LittleEndian.Uint32(body[4:])
			if expanded == 0 || expanded > maxPDBBytes {
				return nil, "", false
			}
			if int(expanded) > maxEmbeddedPortableExpansion-*expansion {
				*expansion = maxEmbeddedPortableExpansion + 1
				return nil, "", false
			}
			*expansion += int(expanded)
			input := bytes.NewReader(body[8:])
			reader := flate.NewReader(input)
			decoded, err := io.ReadAll(io.LimitReader(reader, int64(expanded)+1))
			closeErr := reader.Close()
			if err != nil || closeErr != nil || len(decoded) != int(expanded) || input.Len() != 0 {
				return nil, "", false
			}
			embedded = decoded
		case 19:
			if major != 1 || minor != 0 {
				return nil, "", false
			}
			checksums = append(checksums, body)
		}
	}
	if embedded == nil {
		return nil, "", false
	}
	streams, ok := portableStreams(embedded)
	if !ok || len(streams["#Pdb"]) < 32 {
		return nil, "", false
	}
	id := portableIdentity(streams["#Pdb"])
	if cvID != "" && cvID != id {
		return nil, "", false
	}
	if len(checksums) > 0 {
		idOffset, valid := embeddedPDBIDOffset(embedded)
		if !valid {
			return nil, "", false
		}
		for _, checksum := range checksums {
			separator := bytes.IndexByte(checksum, 0)
			if separator < 0 {
				return nil, "", false
			}
			var h hash.Hash
			switch string(checksum[:separator]) {
			case "SHA256":
				h = sha256.New()
			case "SHA1":
				h = sha1.New()
			default:
				return nil, "", false
			}
			h.Write(embedded[:idOffset])
			h.Write(make([]byte, 20))
			h.Write(embedded[idOffset+20:])
			if !bytes.Equal(h.Sum(nil), checksum[separator+1:]) {
				return nil, "", false
			}
		}
	}
	return embedded, alias, true
}

// portableStreams already checked these extents; find the ID's absolute offset
// to calculate the checksum without copying or modifying the PDB.
func embeddedPDBIDOffset(data []byte) (int, bool) {
	version := int(binary.LittleEndian.Uint32(data[12:]))
	pos := 16 + version
	count := int(binary.LittleEndian.Uint16(data[pos+2:]))
	pos += 4
	for i := 0; i < count; i++ {
		offset := int(binary.LittleEndian.Uint32(data[pos:]))
		pos += 8
		end := bytes.IndexByte(data[pos:], 0)
		if end < 0 {
			return 0, false
		}
		name := string(data[pos : pos+end])
		pos = (pos + end + 1 + 3) &^ 3
		if name == "#Pdb" {
			if offset > len(data)-20 {
				return 0, false
			}
			return offset, true
		}
	}
	return 0, false
}
