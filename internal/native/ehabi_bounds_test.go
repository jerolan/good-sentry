package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

func buildARM32EHABIBound(entryCount, additionalWords int) []byte {
	const (
		textAddress  = uint32(0x1000)
		exidxAddress = uint32(0x2000)
		extabAddress = uint32(0x300000)
	)
	sectionNames := []byte("\x00.text\x00.ARM.exidx\x00.ARM.extab\x00.shstrtab\x00")
	nameOffset := func(name string) uint32 {
		return uint32(bytes.Index(sectionNames, append([]byte(name), 0)))
	}
	textSize := uint32(0x100)
	if needed := uint32(entryCount * 4); needed > textSize {
		textSize = needed
	}
	textData := make([]byte, textSize)
	indexData := make([]byte, entryCount*8)
	entrySize := 4 + additionalWords*4
	extabData := make([]byte, entryCount*entrySize)
	for index := 0; index < entryCount; index++ {
		place := exidxAddress + uint32(index*8)
		binary.LittleEndian.PutUint32(indexData[index*8:], arm32TestPrel31(uint64(place), uint64(textAddress+uint32(index*4))))
		if additionalWords == 0 {
			binary.LittleEndian.PutUint32(indexData[index*8+4:], 1)
			continue
		}
		programAddress := extabAddress + uint32(index*entrySize)
		binary.LittleEndian.PutUint32(indexData[index*8+4:], arm32TestPrel31(uint64(place+4), uint64(programAddress)))
		word := uint32(0x81000000) | uint32(additionalWords)<<16
		binary.LittleEndian.PutUint32(extabData[index*entrySize:], word)
	}

	const (
		elfHeaderSize     = 52
		sectionHeaderSize = 40
	)
	shstrOffset := uint32(elfHeaderSize)
	textOffset := (shstrOffset + uint32(len(sectionNames)) + 3) &^ 3
	indexOffset := textOffset + uint32(len(textData))
	extabOffset := indexOffset + uint32(len(indexData))
	sectionTableOffset := (extabOffset + uint32(len(extabData)) + 3) &^ 3
	data := make([]byte, sectionTableOffset+5*sectionHeaderSize)
	copy(data[shstrOffset:], sectionNames)
	copy(data[textOffset:], textData)
	copy(data[indexOffset:], indexData)
	copy(data[extabOffset:], extabData)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS32), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_ARM))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[32:], sectionTableOffset)
	binary.LittleEndian.PutUint16(data[40:], elfHeaderSize)
	binary.LittleEndian.PutUint16(data[46:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[48:], 5)
	binary.LittleEndian.PutUint16(data[50:], 4)
	writeSection := func(index int, name, kind, flags, address, offset, size, link, alignment, entrySize uint32) {
		start := sectionTableOffset + uint32(index)*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], name)
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint32(data[start+8:], flags)
		binary.LittleEndian.PutUint32(data[start+12:], address)
		binary.LittleEndian.PutUint32(data[start+16:], offset)
		binary.LittleEndian.PutUint32(data[start+20:], size)
		binary.LittleEndian.PutUint32(data[start+24:], link)
		binary.LittleEndian.PutUint32(data[start+32:], alignment)
		binary.LittleEndian.PutUint32(data[start+36:], entrySize)
	}
	writeSection(1, nameOffset(".text"), uint32(elf.SHT_PROGBITS), uint32(elf.SHF_ALLOC|elf.SHF_EXECINSTR), textAddress, textOffset, uint32(len(textData)), 0, 4, 0)
	writeSection(2, nameOffset(".ARM.exidx"), 0x70000001, uint32(elf.SHF_ALLOC)|0x80, exidxAddress, indexOffset, uint32(len(indexData)), 1, 4, 8)
	writeSection(3, nameOffset(".ARM.extab"), uint32(elf.SHT_PROGBITS), uint32(elf.SHF_ALLOC), extabAddress, extabOffset, uint32(len(extabData)), 0, 4, 0)
	writeSection(4, nameOffset(".shstrtab"), uint32(elf.SHT_STRTAB), 0, 0, shstrOffset, uint32(len(sectionNames)), 0, 1, 0)
	return data
}

func TestARM32EHABIEntryBound(t *testing.T) {
	if _, table := ParseObjectUnwind("arm32-bound.so", buildARM32EHABIBound(262144, 0)); table == nil {
		t.Fatal("262144 .ARM.exidx entries were rejected")
	}
	if _, table := ParseObjectUnwind("arm32-bound.so", buildARM32EHABIBound(262145, 0)); table != nil {
		t.Fatal("262145 .ARM.exidx entries were accepted")
	}
}

func TestARM32EHABIOperationByteBound(t *testing.T) {
	if _, table := ParseObjectUnwind("arm32-bound.so", buildARM32EHABIBound(16000, 255)); table == nil {
		t.Fatal("under-16MiB EHABI operations were rejected")
	}
	if _, table := ParseObjectUnwind("arm32-bound.so", buildARM32EHABIBound(16500, 255)); table != nil {
		t.Fatal("over-16MiB EHABI operations were accepted")
	}
}
