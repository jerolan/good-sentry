package native

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func buildMIPS32ContextMinidump(size int, arch uint16, flags, padding uint32) []byte {
	data := buildBreakpadMIPS64Minidump(size, arch, flags, padding)
	module := minidumpStreamRVA(data, streamModuleList)
	binary.LittleEndian.PutUint64(data[module+4:], 0x400000)
	exception := minidumpStreamRVA(data, streamException)
	binary.LittleEndian.PutUint64(data[exception+24:], 0x401010)
	thread := minidumpStreamRVA(data, streamThreadList)
	context := binary.LittleEndian.Uint32(data[thread+48:])
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	if size >= 320 {
		binary.LittleEndian.PutUint64(data[context+312:], 0x401010)
	}
	if size >= 264 {
		binary.LittleEndian.PutUint64(data[context+8+31*8:], 0x401190)
	}
	// Plausible 32-bit frame records and return slots must not invent callers.
	binary.LittleEndian.PutUint32(data[stack:], 0x401150)
	binary.LittleEndian.PutUint32(data[stack+0x20:], 0)
	binary.LittleEndian.PutUint32(data[stack+0x24:], 0x401180)
	return data
}

func TestMIPS32ELMinidumpContextAndSymbolication(t *testing.T) {
	for _, flags := range []uint32{0x40002, 0x40006, 0x4000a, 0x4000e} {
		for _, padding := range []uint32{0, 0xffffffff, 0x12345678} {
			t.Run(fmt.Sprintf("flags_%x_padding_%x", flags, padding), func(t *testing.T) {
				data := buildMIPS32ContextMinidump(600, 1, flags, padding)
				thread := minidumpStreamRVA(data, streamThreadList)
				context := binary.LittleEndian.Uint32(data[thread+48:])
				binary.LittleEndian.PutUint64(data[context+8+5*8:], 0xdeadbeef87654321)
				crash, err := ParseMinidump(data)
				if err != nil {
					t.Fatal(err)
				}
				if crash.Arch != "mips32el" || crash.PointerSize != 4 || crash.RIP != 0x401010 || crash.RSP != 0x200000 || crash.RBP != 0x200020 || crash.LR != 0x401190 {
					t.Fatalf("context arch=%s ptr=%d PC=%#x SP=%#x FP=%#x LR=%#x", crash.Arch, crash.PointerSize, crash.RIP, crash.RSP, crash.RBP, crash.LR)
				}
				if len(crash.registers) != 32 || crash.registers[0] != 0 || crash.registers[5] != 0xdeadbeef87654321 {
					t.Fatalf("registers=%v", crash.registers)
				}
				if len(crash.Frames) != 1 || crash.Frames[0].Instruction != crash.RIP {
					t.Fatalf("invented caller: %+v", crash.Frames)
				}
				key, symbols := ParseBreakpad("test_app.pdb", []byte("MODULE linux mips TEST test_app.pdb\nFILE 1 source.c\nFUNC 1010 20 0 crash_mips\n1010 20 42 1\n"))
				Symbolicate(&crash, SymbolSet{key: symbols})
				if crash.Frames[0].Function != "crash_mips" || crash.Frames[0].File != "source.c" || crash.Frames[0].Line != 42 {
					t.Fatalf("initial frame not symbolicated: %+v", crash.Frames[0])
				}
			})
		}
	}
}

func TestMIPS32ELRejectsHighAddressFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset uint32
	}{{"EPC", 312}, {"SP", 8 + 29*8}, {"FP", 8 + 30*8}, {"LR", 8 + 31*8}} {
		t.Run(tc.name, func(t *testing.T) {
			data := buildMIPS32ContextMinidump(600, 1, 0x4000e, 0)
			thread := minidumpStreamRVA(data, streamThreadList)
			context := binary.LittleEndian.Uint32(data[thread+48:])
			old := binary.LittleEndian.Uint64(data[context+tc.offset:])
			binary.LittleEndian.PutUint64(data[context+tc.offset:], old|0x100000000)
			if _, err := ParseMinidump(data); err == nil {
				t.Fatal("address exceeds 32 bits but was accepted")
			}
		})
	}
}

func TestMIPS32ELRejectsInvalidContexts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		arch  uint16
		flags uint32
	}{
		{"short", 599, 1, 0x4000e}, {"long", 601, 1, 0x4000e}, {"Crashpad MIPS64 size", 640, 1, 0x4000e},
		{"AMD64 architecture", 600, cpuArchAMD64, 0x4000e}, {"ARM64 architecture", 600, cpuArchARM64, 0x4000e}, {"MIPS64 architecture", 600, 0x8004, 0x4000e},
		{"MIPS64 flags", 600, 1, 0x8000e}, {"zero flags", 600, 1, 0}, {"missing integer", 600, 1, 0x40000}, {"missing architecture", 600, 1, 2}, {"unknown flag", 600, 1, 0x40012},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseMinidump(buildMIPS32ContextMinidump(tc.size, tc.arch, tc.flags, 0)); err == nil {
				t.Fatal("invalid MIPS32 context accepted")
			}
		})
	}
}

func TestMIPS32AndMIPS64ELContextsRemainDistinct(t *testing.T) {
	mips32, err := ParseMinidump(buildMIPS32ContextMinidump(600, 1, 0x4000e, 0xffffffff))
	if err != nil {
		t.Fatal(err)
	}
	mips64, err := ParseMinidump(buildBreakpadMIPS64Minidump(600, 0x8004, 0x8000e, 0xffffffff))
	if err != nil {
		t.Fatal(err)
	}
	if mips32.PointerSize != 4 || mips64.PointerSize != 8 || mips32.RIP != 0x401010 || mips64.RIP != 0x140001010 {
		t.Fatalf("MIPS32=%+v MIPS64=%+v", mips32, mips64)
	}
}
