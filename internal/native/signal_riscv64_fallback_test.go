package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"testing"
)

// Reuse only the serialized stream builder; replace its ARM64 context with the
// public Crashpad RISC-V64 context, retaining captured Memory64 ranges.
func riscvSignalMinidump(pc, sp uint64, platform uint32, ranges []arm64SignalRange) []byte {
	data := arm64SignalMinidump(pc, sp, platform, ranges)
	system := minidumpStreamRVA(data, streamSystemInfo)
	binary.LittleEndian.PutUint16(data[system:], cpuArchRISCV64)
	thread := minidumpStreamRVA(data, streamThreadList)
	context := make([]byte, 528)
	binary.LittleEndian.PutUint32(context, 0x08000001)
	binary.LittleEndian.PutUint32(context[4:], 1)
	binary.LittleEndian.PutUint64(context[8:], pc)
	binary.LittleEndian.PutUint64(context[24:], sp) // x2
	rva := uint32(len(data))
	data = append(data, context...)
	binary.LittleEndian.PutUint32(data[thread+44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(data[thread+48:], rva)
	return data
}

func riscvSignalEntryCFI(mode string) []byte {
	var section bytes.Buffer
	write := func(record []byte) {
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
	}
	// A signal CIE with an explicit SP-based CFA, but normally no return rule.
	augmentation := []byte{'z', 'S', 0}
	if mode == "ordinary cfi" {
		augmentation = []byte{'z', 0}
	}
	cie := append([]byte{0, 0, 0, 0, 1}, augmentation...)
	cie = append(cie, 1, 0x78, 1, 0, 0x0c, 2, 8)
	if mode == "explicit cfi" {
		cie = append(cie, 0x05, 1, 1)
	}
	if mode == "undefined cfi" {
		cie = append(cie, 0x07, 1)
	}
	write(cie)
	var fde bytes.Buffer
	_ = binary.Write(&fde, binary.LittleEndian, uint32(section.Len()+4))
	_ = binary.Write(&fde, binary.LittleEndian, uint64(0x1010))
	_ = binary.Write(&fde, binary.LittleEndian, uint64(0x10))
	_ = fde.WriteByte(0)
	write(fde.Bytes())
	return section.Bytes()
}

func TestLinuxRISCV64SignalFallbackFromMinidump(t *testing.T) {
	const base uint64 = 0x100000000
	const trampoline uint64 = 0x200001000
	const sp uint64 = 0x5000
	for _, tc := range []struct {
		name, mode string
		savedSP    uint64
		want       []uint64
	}{
		{"upward without module", "", 0x9000, []uint64{trampoline, base + 0x1100, base + 0x1220}},
		{"downward disjoint", "", 0x3000, []uint64{trampoline, base + 0x1100, base + 0x1220}},
		{"split instruction ranges", "split", 0x9000, []uint64{trampoline, base + 0x1100, base + 0x1220}},
		{"missing x8", "missing x8", 0x9000, []uint64{trampoline, base + 0x1100}},
		{"x0 stays zero", "x0", 0x9000, []uint64{trampoline, base + 0x1100}},
		{"missing x2", "missing x2", 0x9000, []uint64{trampoline}},
		{"missing PC", "missing pc", 0x9000, []uint64{trampoline}},
		{"zero PC", "zero pc", 0x9000, []uint64{trampoline}},
		{"repeated PC", "repeated pc", 0x9000, []uint64{trampoline}},
		{"short code", "short", 0x9000, nil},
		{"wrong code", "wrong code", 0x9000, nil},
		{"wrong OS", "wrong os", 0x9000, nil},
		{"overlap", "overlap", 0x4ff0, []uint64{trampoline}},
		{"overlap reversed", "overlap reversed", 0x4ff0, []uint64{trampoline}},
		{"overflow", "overflow", 0x9000, []uint64{trampoline}},
		{"signal CFI without return rule", "signal cfi", 0x9000, []uint64{base + 0x1010, base + 0x1100, base + 0x1220}},
		{"signal CFI explicit return takes precedence", "explicit cfi", 0x9000, []uint64{base + 0x1010}},
		{"signal CFI undefined return takes precedence", "undefined cfi", 0x9000, []uint64{base + 0x1010}},
		{"ordinary CFI without return takes precedence", "ordinary cfi", 0x9000, []uint64{base + 0x1010}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pc := trampoline
			if tc.mode == "signal cfi" || tc.mode == "explicit cfi" || tc.mode == "undefined cfi" || tc.mode == "ordinary cfi" {
				pc = base + 0x1010
			}
			initialSP := sp
			if tc.mode == "overflow" {
				initialSP = ^uint64(0) - 100
			}
			signal := make([]byte, 560)
			savedPC := base + 0x1100
			if tc.mode == "zero pc" {
				savedPC = 0
			}
			if tc.mode == "repeated pc" {
				savedPC = pc
			}
			binary.LittleEndian.PutUint64(signal[304:], savedPC)
			binary.LittleEndian.PutUint64(signal[304+2*8:], tc.savedSP)
			binary.LittleEndian.PutUint64(signal[304+8*8:], tc.savedSP+0x30)
			code := make([]byte, 8)
			binary.LittleEndian.PutUint32(code, 0x08b00893)
			binary.LittleEndian.PutUint32(code[4:], 0x00000073)
			if tc.mode == "short" {
				code = code[:4]
			}
			if tc.mode == "wrong code" {
				code[0] ^= 1
			}
			stack := make([]byte, 0x100)
			binary.LittleEndian.PutUint64(stack[0x30:], base+0x1220)
			ranges := []arm64SignalRange{{pc, code}, {sp, signal}, {tc.savedSP, stack}}
			if tc.mode == "split" {
				ranges = []arm64SignalRange{{pc, code[:4]}, {pc + 4, code[4:]}, {sp, signal}, {tc.savedSP, stack}}
			}
			if tc.mode == "missing x8" {
				ranges = []arm64SignalRange{{pc, code}, {sp, signal[:368]}, {sp + 376, signal[376:]}, {tc.savedSP, stack}}
			}
			if tc.mode == "missing x2" {
				ranges = []arm64SignalRange{{pc, code}, {sp, signal[:320]}, {sp + 328, signal[328:]}, {tc.savedSP, stack}}
			}
			if tc.mode == "missing pc" {
				ranges = []arm64SignalRange{{pc, code}, {sp, signal[:304]}, {sp + 312, signal[312:]}, {tc.savedSP, stack}}
			}
			if tc.mode == "overlap reversed" {
				ranges = []arm64SignalRange{{pc, code}, {tc.savedSP, stack}, {sp, signal}}
			}
			if tc.mode == "overflow" {
				ranges = []arm64SignalRange{{pc, code}, {initialSP, make([]byte, 16)}, {tc.savedSP, stack}}
			}
			platform := uint32(0x8201)
			if tc.mode == "wrong os" {
				platform = 2
			}
			crash, err := ParseMinidump(riscvSignalMinidump(pc, initialSP, platform, ranges))
			if err != nil {
				t.Fatal(err)
			}
			tables := UnwindSet{}
			caller := buildArchDebugFrame(8, 1, 8, 8, 0x78, 0x1100, 0x100)
			if tc.mode == "x0" {
				caller = buildArchDebugFrame(8, 0, 8, 8, 0x78, 0x1100, 0x100)
				idx := bytes.Index(caller, []byte{0x05, 0, 1})
				if idx < 0 {
					t.Fatal("no return rule")
				}
				copy(caller[idx:idx+3], []byte{0x08, 0, 0})
			}
			for _, section := range [][]byte{caller, buildArchDebugFrame(8, 1, 2, 8, 0x78, 0x1200, 0x100)} {
				_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_RISCV, section))
				if table == nil {
					t.Fatal("RISC-V caller ELF CFI ignored")
				}
				tables["test_arm64.pdb"] = append(tables["test_arm64.pdb"], table)
			}
			if tc.mode == "signal cfi" || tc.mode == "explicit cfi" || tc.mode == "undefined cfi" || tc.mode == "ordinary cfi" {
				object := buildELF64DebugFrame(elf.EM_RISCV, riscvSignalEntryCFI(tc.mode))
				name := bytes.Index(object, []byte(".debug_frame\x00"))
				if name < 0 {
					t.Fatal("missing section name")
				}
				copy(object[name:], []byte(".eh_frame\x00"))
				_, table := ParseObjectUnwind("test_arm64.pdb", object)
				if table == nil {
					t.Fatal("signal ELF CFI ignored")
				}
				tables["test_arm64.pdb"] = append(tables["test_arm64.pdb"], table)
			}
			frames := UnwindCFI(&crash, tables)
			if len(frames) != len(tc.want) {
				t.Fatalf("frames=%+v; want PCs %x", frames, tc.want)
			}
			for i, want := range tc.want {
				if frames[i].Instruction != want {
					t.Fatalf("frame %d=%#x want %#x", i, frames[i].Instruction, want)
				}
			}
		})
	}
}

func TestLinuxRISCV64SignalRecoveredIntegerRegisterMapping(t *testing.T) {
	const base uint64 = 0x100000000
	const trampoline uint64 = 0x200001000
	const signalSP uint64 = 0x5000
	const savedSP uint64 = 0x9000
	for register := byte(1); register <= 31; register++ {
		t.Run(fmt.Sprintf("x%d", register), func(t *testing.T) {
			signal := make([]byte, 560)
			binary.LittleEndian.PutUint64(signal[304:], base+0x1100)
			for reg := byte(1); reg <= 31; reg++ {
				binary.LittleEndian.PutUint64(signal[304+int(reg)*8:], savedSP+0x40+uint64(reg)*8)
			}
			binary.LittleEndian.PutUint64(signal[304+2*8:], savedSP)
			code := make([]byte, 8)
			binary.LittleEndian.PutUint32(code, 0x08b00893)
			binary.LittleEndian.PutUint32(code[4:], 0x00000073)
			stack := make([]byte, 0x200)
			binary.LittleEndian.PutUint64(stack, base+0x1220+uint64(register)*4)
			for reg := byte(1); reg <= 31; reg++ {
				binary.LittleEndian.PutUint64(stack[0x40+int(reg)*8:], base+0x1220+uint64(reg)*4)
			}
			data := riscvSignalMinidump(trampoline, signalSP, 0x8201, []arm64SignalRange{{trampoline, code}, {signalSP, signal}, {savedSP, stack}})
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			tables := UnwindSet{}
			for _, section := range [][]byte{buildArchDebugFrame(8, 1, register, 8, 0x78, 0x1100, 0x100), buildArchDebugFrame(8, 1, 2, 8, 0x78, 0x1200, 0x100)} {
				_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_RISCV, section))
				if table == nil {
					t.Fatal("RISC-V ELF CFI ignored")
				}
				tables["test_arm64.pdb"] = append(tables["test_arm64.pdb"], table)
			}
			frames := UnwindCFI(&crash, tables)
			want := []uint64{trampoline, base + 0x1100, base + 0x1220 + uint64(register)*4}
			if len(frames) < len(want) {
				t.Fatalf("x%d frames=%+v; want first PCs %x", register, frames, want)
			}
			for i, pc := range want {
				if frames[i].Instruction != pc {
					t.Fatalf("x%d frame %d=%#x; want %#x", register, i, frames[i].Instruction, pc)
				}
			}
		})
	}
}
