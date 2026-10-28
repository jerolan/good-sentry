package native

import (
	"bytes"
	"encoding/binary"
)

var msf2Magic = []byte("Microsoft C/C++ program database 2.00\r\n\x1aJG\x00\x00")

// Small MSF has direct u16 directory pages and eight-byte stream entries.
// Obsolete pointer fields are serialized process addresses, never file offsets.
func parsePDBMSF2(data []byte) (*pdbMSFFile, bool) {
	if len(data) < 60 || len(data) > maxPDBBytes || !bytes.HasPrefix(data, msf2Magic) {
		return nil, false
	}
	blockSize := binary.LittleEndian.Uint32(data[44:])
	var fpmPages uint32
	switch blockSize {
	case 1024:
		fpmPages = 8
	case 2048:
		fpmPages = 4
	case 4096:
		fpmPages = 1
	default:
		return nil, false
	}
	freeMap := uint32(binary.LittleEndian.Uint16(data[48:]))
	blockCount := uint32(binary.LittleEndian.Uint16(data[50:]))
	directorySize := binary.LittleEndian.Uint32(data[52:])
	if freeMap != 1 && freeMap != 1+fpmPages || blockCount <= 2*fpmPages || uint64(blockCount)*uint64(blockSize) != uint64(len(data)) || directorySize == 0 || directorySize > maxPDBBytes {
		return nil, false
	}
	directoryBlockCount := (directorySize + blockSize - 1) / blockSize
	if uint64(60)+uint64(directoryBlockCount)*2 > uint64(blockSize) {
		return nil, false
	}
	usedBlocks := map[uint32]struct{}{0: {}}
	for page := uint32(1); page <= 2*fpmPages; page++ {
		usedBlocks[page] = struct{}{}
	}
	directory := make([]byte, directorySize)
	for i := uint32(0); i < directoryBlockCount; i++ {
		page := uint32(binary.LittleEndian.Uint16(data[60+i*2:]))
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
	streamCount := uint32(binary.LittleEndian.Uint16(directory))
	if streamCount < 4 || streamCount > maxPDBStreams || uint64(4)+uint64(streamCount)*8 > uint64(len(directory)) {
		return nil, false
	}
	streams := make([]pdbMSFStream, streamCount)
	blockCounts := make([]uint32, streamCount)
	var declaredBytes uint64
	for i := range streams {
		size := binary.LittleEndian.Uint32(directory[4+i*8:])
		if size == pdbStreamAbsent {
			streams[i].absent = true
			continue
		}
		blockCounts[i] = uint32((uint64(size) + uint64(blockSize) - 1) / uint64(blockSize))
		if i == 0 {
			// Its old directory pages may have been reused by current streams.
			streams[i].absent = true
			continue
		}
		declaredBytes += uint64(size)
		if declaredBytes > maxPDBBytes {
			return nil, false
		}
		streams[i].size = size
	}
	directoryOffset := 4 + int(streamCount)*8
	for i := range streams {
		count := blockCounts[i]
		if uint64(directoryOffset)+uint64(count)*2 > uint64(len(directory)) {
			return nil, false
		}
		if i == 0 {
			directoryOffset += int(count) * 2
			continue
		}
		streams[i].blocks = make([]uint32, count)
		for j := range streams[i].blocks {
			page := uint32(binary.LittleEndian.Uint16(directory[directoryOffset:]))
			directoryOffset += 2
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
