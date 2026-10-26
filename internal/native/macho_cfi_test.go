package native

import (
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
