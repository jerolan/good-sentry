package native

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func buildBreakpadMIPS64Minidump(size int, arch uint16, flags, padding uint32) []byte {
	data := buildMIPS64ContextMinidump(size, arch, uint64(flags))
	thread := minidumpStreamRVA(data, streamThreadList)
	context := binary.LittleEndian.Uint32(data[thread+48:])
	if size >= 8 {
		binary.LittleEndian.PutUint32(data[context+4:], padding)
	}
	if size >= 320 {
		binary.LittleEndian.PutUint64(data[context+312:], 0x140001010)
	}
	if size >= 344 {
		binary.LittleEndian.PutUint64(data[context+336:], 0x140001080)
	}
	return data
}

func TestBreakpadMIPS64ContextAndSymbolication(t *testing.T) {
	for _, flags := range []uint32{0x80002, 0x80006, 0x8000a, 0x8000e} {
		for _, padding := range []uint32{0, 0xffffffff, 0x12345678} {
			t.Run(fmt.Sprintf("flags_%x_padding_%x", flags, padding), func(t *testing.T) {
				crash, err := ParseMinidump(buildBreakpadMIPS64Minidump(600, 0x8004, flags, padding))
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
}

func TestBreakpadMIPS64RejectsInvalidContexts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		arch  uint16
		flags uint32
	}{
		{"short", 599, 0x8004, 0x8000e}, {"long", 601, 0x8004, 0x8000e},
		{"wrong AMD64 architecture", 600, cpuArchAMD64, 0x8000e}, {"wrong ARM64 architecture", 600, cpuArchARM64, 0x8000e},
		{"wrong MIPS32 architecture", 600, 1, 0x8000e}, {"MIPS32 flags", 600, 0x8004, 0x4000e},
		{"zero flags", 600, 0x8004, 0}, {"integer without architecture", 600, 0x8004, 2}, {"missing integer", 600, 0x8004, 0x80000}, {"unknown flag", 600, 0x8004, 0x80012},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseMinidump(buildBreakpadMIPS64Minidump(tc.size, tc.arch, tc.flags, 0)); err == nil {
				t.Fatal("invalid Breakpad MIPS64 context accepted")
			}
		})
	}
}

func TestBreakpadAndCrashpadMIPS64DifferentEPCOffsets(t *testing.T) {
	breakpad, err := ParseMinidump(buildBreakpadMIPS64Minidump(600, 0x8004, 0x8000e, 0x12345678))
	if err != nil {
		t.Fatal(err)
	}
	crashpadBytes := buildMIPS64ContextMinidump(640, 0x8004, 0x8000e)
	thread := minidumpStreamRVA(crashpadBytes, streamThreadList)
	context := binary.LittleEndian.Uint32(crashpadBytes[thread+48:])
	binary.LittleEndian.PutUint64(crashpadBytes[context+312:], 0x140001080)
	crashpad, err := ParseMinidump(crashpadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if breakpad.RIP != 0x140001010 || crashpad.RIP != breakpad.RIP {
		t.Fatalf("Breakpad PC=%#x Crashpad PC=%#x", breakpad.RIP, crashpad.RIP)
	}
}
