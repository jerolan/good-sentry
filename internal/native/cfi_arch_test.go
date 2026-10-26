package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

func buildELF32DebugFrame(machine elf.Machine, sectionData []byte) []byte {
	const (
		elfHeaderSize     = 52
		sectionHeaderSize = 40
	)
	sectionNames := []byte("\x00.shstrtab\x00.debug_frame\x00")
	sectionNamesOffset := elfHeaderSize
	sectionDataOffset := (sectionNamesOffset + len(sectionNames) + 3) &^ 3
	sectionTableOffset := (sectionDataOffset + len(sectionData) + 3) &^ 3
	data := make([]byte, sectionTableOffset+3*sectionHeaderSize)
	copy(data[sectionNamesOffset:], sectionNames)
	copy(data[sectionDataOffset:], sectionData)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS32), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(data[18:], uint16(machine))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[32:], uint32(sectionTableOffset))
	binary.LittleEndian.PutUint16(data[40:], elfHeaderSize)
	binary.LittleEndian.PutUint16(data[46:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[48:], 3)
	binary.LittleEndian.PutUint16(data[50:], 1)
	writeSection := func(index int, name, kind, offset, size, alignment uint32) {
		start := sectionTableOffset + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], name)
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint32(data[start+16:], offset)
		binary.LittleEndian.PutUint32(data[start+20:], size)
		binary.LittleEndian.PutUint32(data[start+32:], alignment)
	}
	writeSection(1, 1, uint32(elf.SHT_STRTAB), uint32(sectionNamesOffset), uint32(len(sectionNames)), 1)
	writeSection(2, 11, uint32(elf.SHT_PROGBITS), uint32(sectionDataOffset), uint32(len(sectionData)), 4)
	return data
}

func buildELF64DebugFrame(machine elf.Machine, sectionData []byte) []byte {
	const (
		elfHeaderSize     = 64
		sectionHeaderSize = 64
	)
	sectionNames := []byte("\x00.shstrtab\x00.debug_frame\x00")
	sectionNamesOffset := elfHeaderSize
	sectionDataOffset := (sectionNamesOffset + len(sectionNames) + 7) &^ 7
	sectionTableOffset := (sectionDataOffset + len(sectionData) + 7) &^ 7
	data := make([]byte, sectionTableOffset+3*sectionHeaderSize)
	copy(data[sectionNamesOffset:], sectionNames)
	copy(data[sectionDataOffset:], sectionData)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(data[18:], uint16(machine))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint64(data[40:], uint64(sectionTableOffset))
	binary.LittleEndian.PutUint16(data[52:], elfHeaderSize)
	binary.LittleEndian.PutUint16(data[58:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[60:], 3)
	binary.LittleEndian.PutUint16(data[62:], 1)
	writeSection := func(index int, name, kind, offset, size, alignment uint32) {
		start := sectionTableOffset + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], name)
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint64(data[start+24:], uint64(offset))
		binary.LittleEndian.PutUint64(data[start+32:], uint64(size))
		binary.LittleEndian.PutUint64(data[start+48:], uint64(alignment))
	}
	writeSection(1, 1, uint32(elf.SHT_STRTAB), uint32(sectionNamesOffset), uint32(len(sectionNames)), 1)
	writeSection(2, 11, uint32(elf.SHT_PROGBITS), uint32(sectionDataOffset), uint32(len(sectionData)), 8)
	return data
}

func buildArchDebugFrame(addressSize int, returnReg, cfaReg, cfaOffset, dataAlign byte, start uint64, size uint64) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) int {
		origin := section.Len()
		var length [4]byte
		binary.LittleEndian.PutUint32(length[:], uint32(len(record)))
		section.Write(length[:])
		section.Write(record)
		return origin
	}
	cie := []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, dataAlign, returnReg, 0x0c, cfaReg, cfaOffset, 0x05, returnReg, 1}
	cieOffset := appendRecord(cie)
	var fde bytes.Buffer
	fde.Write([]byte{byte(cieOffset), byte(cieOffset >> 8), byte(cieOffset >> 16), byte(cieOffset >> 24)})
	if addressSize == 4 {
		var field [4]byte
		binary.LittleEndian.PutUint32(field[:], uint32(start))
		fde.Write(field[:])
		binary.LittleEndian.PutUint32(field[:], uint32(size))
		fde.Write(field[:])
	} else {
		var field [8]byte
		binary.LittleEndian.PutUint64(field[:], start)
		fde.Write(field[:])
		binary.LittleEndian.PutUint64(field[:], size)
		fde.Write(field[:])
	}
	appendRecord(fde.Bytes())
	return section.Bytes()
}

func TestX86ELFFrameUnwinds(t *testing.T) {
	crash, err := ParseMinidump(buildX86Minidump())
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.stack) == 0 {
		t.Fatal("x86 fixture has no captured stack")
	}
	binary.LittleEndian.PutUint32(crash.stack[0].data, 0x00401050)
	_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_386, buildArchDebugFrame(4, 8, 4, 4, 0x7c, 0x1010, 0x60)))
	if table == nil {
		t.Fatal("x86 ELF .debug_frame was ignored")
	}
	frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
	want := []uint64{0x00401010, 0x00401050}
	if len(frames) != len(want) {
		t.Fatalf("x86 CFI frames=%+v want=%x", frames, want)
	}
	for index, address := range want {
		if frames[index].Instruction != address {
			t.Fatalf("x86 CFI frame %d=%#x want=%#x", index, frames[index].Instruction, address)
		}
	}
}

func TestARM64ELFFrameUnwinds(t *testing.T) {
	crash, err := ParseMinidump(buildARM64Minidump(272, cpuArchARM64, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.stack) == 0 {
		t.Fatal("ARM64 fixture has no captured stack")
	}
	binary.LittleEndian.PutUint64(crash.stack[0].data, 0x100001050)
	_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, buildArchDebugFrame(8, 30, 31, 8, 0x78, 0x1010, 0x60)))
	if table == nil {
		t.Fatal("ARM64 ELF .debug_frame was ignored")
	}
	frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table}})
	want := []uint64{0x100001010, 0x100001050}
	if len(frames) != len(want) {
		t.Fatalf("ARM64 CFI frames=%+v want=%x", frames, want)
	}
	for index, address := range want {
		if frames[index].Instruction != address {
			t.Fatalf("ARM64 CFI frame %d=%#x want=%#x", index, frames[index].Instruction, address)
		}
	}
}
