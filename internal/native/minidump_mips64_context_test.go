package native

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func buildMIPS64ContextMinidump(size int, architecture uint16, flags uint64) []byte {
	data := buildAMD64Minidump()
	system := minidumpStreamRVA(data, streamSystemInfo)
	binary.LittleEndian.PutUint16(data[system:], architecture)
	thread := minidumpStreamRVA(data, streamThreadList)
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	binary.LittleEndian.PutUint64(data[stack:], 0x200020)
	binary.LittleEndian.PutUint64(data[stack+8:], 0x140001150)
	binary.LittleEndian.PutUint64(data[stack+0x20:], 0)
	binary.LittleEndian.PutUint64(data[stack+0x28:], 0x140001180)
	context := make([]byte, size)
	if size >= 8 {
		binary.LittleEndian.PutUint64(context, flags)
	}
	for reg := 0; reg < 32; reg++ {
		if 8+(reg+1)*8 <= size {
			binary.LittleEndian.PutUint64(context[8+reg*8:], 0x1000+uint64(reg))
		}
	}
	if size >= 264 {
		binary.LittleEndian.PutUint64(context[8+29*8:], 0x200000)
		binary.LittleEndian.PutUint64(context[8+30*8:], 0x200020)
		binary.LittleEndian.PutUint64(context[8+31*8:], 0x140001190)
	}
	if size >= 344 {
		binary.LittleEndian.PutUint64(context[336:], 0x140001010)
	}
	rva := uint32(len(data))
	data = append(data, context...)
	binary.LittleEndian.PutUint32(data[thread+44:], uint32(size))
	binary.LittleEndian.PutUint32(data[thread+48:], rva)
	return data
}

func TestMIPS64ELMinidumpContextAndSymbolication(t *testing.T) {
	for _, flags := range []uint64{0x80002, 0x80006, 0x8000a, 0x8000e} {
		t.Run(fmt.Sprintf("flags_%x", flags), func(t *testing.T) {
			crash, err := ParseMinidump(buildMIPS64ContextMinidump(640, 0x8004, flags))
			if err != nil {
				t.Fatal(err)
			}
			if crash.Arch != "mips64el" || crash.PointerSize != 8 || crash.RIP != 0x140001010 || crash.RSP != 0x200000 || crash.RBP != 0x200020 || crash.LR != 0x140001190 {
				t.Fatalf("context arch=%s ptr=%d PC=%#x SP=%#x FP=%#x LR=%#x", crash.Arch, crash.PointerSize, crash.RIP, crash.RSP, crash.RBP, crash.LR)
			}
			if len(crash.registers) != 32 || crash.registers[0] != 0 {
				t.Fatalf("registers=%v", crash.registers)
			}
			for reg := uint32(1); reg < 29; reg++ {
				if crash.registers[reg] != 0x1000+uint64(reg) {
					t.Fatalf("r%d=%#x", reg, crash.registers[reg])
				}
			}
			if len(crash.Frames) != 1 || crash.Frames[0].Instruction != crash.RIP {
				t.Fatalf("invented caller: %+v", crash.Frames)
			}
			key, symbols := ParseBreakpad("test_app.pdb", []byte("MODULE linux mips64 TEST test_app.pdb\nFILE 1 source.c\nFUNC 1010 20 0 crash_mips\n1010 20 42 1\n"))
			Symbolicate(&crash, SymbolSet{key: symbols})
			if crash.Frames[0].Function != "crash_mips" || crash.Frames[0].File != "source.c" || crash.Frames[0].Line != 42 {
				t.Fatalf("initial frame was not symbolicated: %+v", crash.Frames[0])
			}
		})
	}
}

func TestMIPS64ELMinidumpRejectsInvalidContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		arch  uint16
		flags uint64
	}{
		{"truncated breakpad layout", 599, 0x8004, 0x80002},
		{"short", 639, 0x8004, 0x80002},
		{"long", 641, 0x8004, 0x80002},
		{"wrong system architecture", 640, cpuArchAMD64, 0x80002},
		{"MIPS context declared ARM64", 640, cpuArchARM64, 0x80002},
		{"zero flags", 640, 0x8004, 0},
		{"integer without architecture", 640, 0x8004, 2},
		{"architecture without integer", 640, 0x8004, 0x80000},
		{"unknown flag", 640, 0x8004, 0x80012},
		{"unknown high flag", 640, 0x8004, 0x100080002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseMinidump(buildMIPS64ContextMinidump(tc.size, tc.arch, tc.flags)); err == nil {
				t.Fatal("invalid MIPS64EL context accepted")
			}
		})
	}
}
