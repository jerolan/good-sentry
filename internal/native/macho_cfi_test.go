package native

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"os"
	"testing"
)

func TestMachODSYMDebugFrameUnwinds(t *testing.T) {
	data, err := os.ReadFile("testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	key, table := ParseObjectUnwind("macho-dsym-amd64", data)
	if table == nil {
		t.Fatal("Mach-O dSYM __debug_frame was ignored")
	}
	if len(table.entries) != 1 || table.entries[0].start != 0xf6a || table.entries[0].end != 0xf81 {
		t.Fatalf("Mach-O __debug_frame entries: %+v", table.entries)
	}
	const base = uint64(0x100000000)
	const stackBase = uint64(0x200000)
	stack := make([]byte, 32)
	binary.LittleEndian.PutUint64(stack[0:], base+0xf6b)
	binary.LittleEndian.PutUint64(stack[8:], base+0xf6c)
	crash := Crash{
		RIP: base + 0xf6a, RSP: stackBase, RBP: stackBase, Arch: "amd64", PointerSize: 8,
		Modules: []Module{{Name: "macho-dsym-amd64", Base: base, Size: 0x2000}},
		stack:   []memoryRange{{start: stackBase, data: stack}},
	}
	frames := UnwindCFI(&crash, UnwindSet{key: {table}})
	want := []uint64{base + 0xf6a, base + 0xf6b, base + 0xf6c}
	if len(frames) != len(want) {
		t.Fatalf("Mach-O CFI frames=%+v want=%x", frames, want)
	}
	for index, address := range want {
		if frames[index].Instruction != address {
			t.Fatalf("Mach-O CFI frame %d=%#x want=%#x", index, frames[index].Instruction, address)
		}
	}
}

func buildMachO64DebugFrame(sectionData []byte) []byte {
	const (
		headerSize     = 32
		segmentSize    = 72
		sectionSize    = 80
		sectionFileOff = 0x400
	)
	loadSize := segmentSize + sectionSize
	data := make([]byte, sectionFileOff+len(sectionData))
	binary.LittleEndian.PutUint32(data[0:], macho.Magic64)
	binary.LittleEndian.PutUint32(data[4:], 0x0100000c) // CPU_TYPE_ARM64
	binary.LittleEndian.PutUint32(data[8:], 0)
	binary.LittleEndian.PutUint32(data[12:], 0xa) // MH_DSYM
	binary.LittleEndian.PutUint32(data[16:], 1)
	binary.LittleEndian.PutUint32(data[20:], uint32(loadSize))
	command := headerSize
	binary.LittleEndian.PutUint32(data[command:], 0x19) // LC_SEGMENT_64
	binary.LittleEndian.PutUint32(data[command+4:], uint32(loadSize))
	copy(data[command+8:command+24], "__DWARF")
	binary.LittleEndian.PutUint64(data[command+24:], 0)
	binary.LittleEndian.PutUint64(data[command+32:], 0x2000)
	binary.LittleEndian.PutUint64(data[command+40:], 0)
	binary.LittleEndian.PutUint64(data[command+48:], uint64(len(data)))
	binary.LittleEndian.PutUint32(data[command+56:], 7)
	binary.LittleEndian.PutUint32(data[command+60:], 7)
	binary.LittleEndian.PutUint32(data[command+64:], 1)
	section := command + segmentSize
	copy(data[section:section+16], "__debug_frame")
	copy(data[section+16:section+32], "__DWARF")
	binary.LittleEndian.PutUint64(data[section+32:], 0x1010)
	binary.LittleEndian.PutUint64(data[section+40:], uint64(len(sectionData)))
	binary.LittleEndian.PutUint32(data[section+48:], sectionFileOff)
	binary.LittleEndian.PutUint32(data[section+52:], 3)
	copy(data[sectionFileOff:], sectionData)
	return data
}

func TestMachOARM64DebugFrameUnwinds(t *testing.T) {
	object := buildMachO64DebugFrame(buildArchDebugFrame(8, 30, 31, 8, 0x78, 0x1010, 0x60))
	if _, err := macho.NewFile(bytes.NewReader(object)); err != nil {
		t.Fatal(err)
	}
	_, table := ParseObjectUnwind("test_arm64.pdb", object)
	if table == nil {
		t.Fatal("Mach-O ARM64 __debug_frame was ignored")
	}
	crash, err := ParseMinidump(buildARM64Minidump(272, cpuArchARM64, 0))
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(crash.stack[0].data, 0x100001050)
	frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table}})
	want := []uint64{0x100001010, 0x100001050}
	if len(frames) != len(want) {
		t.Fatalf("Mach-O ARM64 frames=%+v want=%x", frames, want)
	}
	for index, address := range want {
		if frames[index].Instruction != address {
			t.Fatalf("Mach-O ARM64 frame %d=%#x want=%#x", index, frames[index].Instruction, address)
		}
	}
}
