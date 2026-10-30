package native

import (
	"debug/elf"
	"encoding/binary"
	"testing"
)

type arm64SignalRange struct {
	address uint64
	data    []byte
}

// Construct actual Minidump stream descriptors, including Linux SystemInfo and
// Memory64List, so the signal recognizer must use bytes read by the parser.
func arm64SignalMinidump(pc, sp uint64, platform uint32, ranges []arm64SignalRange) []byte {
	data := buildARM64Minidump(272, cpuArchARM64, 0)
	system := make([]byte, 56)
	binary.LittleEndian.PutUint16(system, cpuArchARM64)
	binary.LittleEndian.PutUint32(system[20:], platform)
	systemRVA := uint32(len(data))
	data = append(data, system...)
	thread := minidumpStreamRVA(data, streamThreadList)
	context := binary.LittleEndian.Uint32(data[thread+48:])
	binary.LittleEndian.PutUint64(data[context+256:], sp)
	binary.LittleEndian.PutUint64(data[context+264:], pc)
	binary.LittleEndian.PutUint64(data[context+240:], 0)
	binary.LittleEndian.PutUint32(data[thread+36:], 0)
	// Rewrite the exception address to match the initial context.
	exception := minidumpStreamRVA(data, streamException)
	binary.LittleEndian.PutUint64(data[exception+24:], pc)
	memory := make([]byte, 16+16*len(ranges))
	binary.LittleEndian.PutUint64(memory, uint64(len(ranges)))
	memoryRVA := uint32(len(data))
	data = append(data, memory...)
	binary.LittleEndian.PutUint64(data[memoryRVA+8:], uint64(len(data)))
	for i, r := range ranges {
		off := memoryRVA + 16 + uint32(i)*16
		binary.LittleEndian.PutUint64(data[off:], r.address)
		binary.LittleEndian.PutUint64(data[off+8:], uint64(len(r.data)))
		data = append(data, r.data...)
	}
	// Relocate the directory, leaving all old RVAs untouched.
	directory := uint32(len(data))
	data = append(data, data[32:32+4*12]...)
	data = append(data, make([]byte, 12)...)
	binary.LittleEndian.PutUint32(data[8:], 5)
	binary.LittleEndian.PutUint32(data[12:], directory)
	for i := uint32(0); i < 4; i++ {
		entry := directory + i*12
		if binary.LittleEndian.Uint32(data[entry:]) == streamSystemInfo {
			binary.LittleEndian.PutUint32(data[entry+4:], uint32(len(system)))
			binary.LittleEndian.PutUint32(data[entry+8:], systemRVA)
		}
	}
	entry := directory + 4*12
	binary.LittleEndian.PutUint32(data[entry:], streamMemory64)
	binary.LittleEndian.PutUint32(data[entry+4:], uint32(len(memory)))
	binary.LittleEndian.PutUint32(data[entry+8:], memoryRVA)
	return data
}

func TestLinuxARM64SignalFallbackFromMinidump(t *testing.T) {
	const base uint64 = 0x100000000
	const trampoline uint64 = 0x200001000
	const signalSP uint64 = 0x5000
	for _, tc := range []struct {
		name    string
		savedSP uint64
		mode    string
		want    []uint64
	}{
		{"upward and no trampoline module", 0x9000, "", []uint64{trampoline, base + 0x1100, base + 0x1220}},
		{"downward disjoint stacks", 0x3000, "", []uint64{trampoline, base + 0x1100, base + 0x1220}},
		{"instructions in separate ranges", 0x9000, "split code", []uint64{trampoline, base + 0x1100, base + 0x1220}},
		{"missing X19", 0x9000, "missing register", []uint64{trampoline, base + 0x1100}},
		{"no CFI for interrupted frame", 0x9000, "no cfi", []uint64{trampoline, base + 0x1100}},
		{"missing saved SP", 0x9000, "missing sp", []uint64{trampoline}},
		{"missing saved PC", 0x9000, "missing pc", []uint64{trampoline}},
		{"truncated instructions", 0x9000, "short code", nil},
		{"missing instructions", 0x9000, "no code", nil},
		{"wrong instructions", 0x9000, "wrong code", nil},
		{"wrong platform", 0x9000, "wrong platform", nil},
		{"zero PC", 0x9000, "zero pc", []uint64{trampoline}},
		{"repeated PC", 0x9000, "repeated pc", []uint64{trampoline}},
		{"overlapping stacks", 0x4ff0, "overlap", []uint64{trampoline}},
		{"overlapping stacks reversed", 0x4ff0, "overlap reversed", []uint64{trampoline}},
		{"signal address overflow", 0x9000, "overflow", []uint64{trampoline}},
		{"CFI takes precedence", 0x9000, "cfi", []uint64{base + 0x1010}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pc := trampoline
			currentSP := signalSP
			if tc.mode == "overflow" {
				currentSP = ^uint64(0) - 100
			}
			if tc.mode == "cfi" {
				pc = base + 0x1010
			}
			signal := make([]byte, 576)
			binary.LittleEndian.PutUint64(signal[312+19*8:], tc.savedSP+0x30)
			binary.LittleEndian.PutUint64(signal[560:], tc.savedSP)
			savedPC := base + 0x1100
			if tc.mode == "zero pc" {
				savedPC = 0
			}
			if tc.mode == "repeated pc" {
				savedPC = pc
			}
			binary.LittleEndian.PutUint64(signal[568:], savedPC)
			code := make([]byte, 8)
			binary.LittleEndian.PutUint32(code, 0xd2801168)
			binary.LittleEndian.PutUint32(code[4:], 0xd4000001)
			if tc.mode == "short code" {
				code = code[:4]
			}
			if tc.mode == "no code" {
				code = nil
			}
			if tc.mode == "wrong code" {
				code[0] ^= 1
			}
			callerStack := make([]byte, 0x100)
			binary.LittleEndian.PutUint64(callerStack[0x30:], base+0x1220)
			ranges := []arm64SignalRange{{pc, code}, {signalSP, signal}, {tc.savedSP, callerStack}}
			if tc.mode == "missing register" {
				ranges = []arm64SignalRange{{pc, code}, {signalSP, signal[:464]}, {signalSP + 472, signal[472:]}, {tc.savedSP, callerStack}}
			}
			if tc.mode == "missing sp" {
				ranges = []arm64SignalRange{{pc, code}, {signalSP, signal[:560]}, {signalSP + 568, signal[568:]}, {tc.savedSP, callerStack}}
			}
			if tc.mode == "missing pc" {
				ranges = []arm64SignalRange{{pc, code}, {signalSP, signal[:568]}, {tc.savedSP, callerStack}}
			}
			if tc.mode == "overlap reversed" {
				ranges = []arm64SignalRange{{pc, code}, {tc.savedSP, callerStack}, {signalSP, signal}}
			}
			if tc.mode == "split code" {
				ranges = []arm64SignalRange{{pc, code[:4]}, {pc + 4, code[4:]}, {signalSP, signal}, {tc.savedSP, callerStack}}
			}
			if tc.mode == "overflow" {
				ranges = []arm64SignalRange{{pc, code}, {currentSP, make([]byte, 16)}, {tc.savedSP, callerStack}}
			}
			platform := uint32(0x8201)
			if tc.mode == "wrong platform" {
				platform = 2
			}
			crash, err := ParseMinidump(arm64SignalMinidump(pc, currentSP, platform, ranges))
			if err != nil {
				t.Fatal(err)
			}
			caller := buildArchDebugFrame(8, 30, 19, 8, 0x78, 0x1100, 0x100)
			last := buildArchDebugFrame(8, 30, 31, 8, 0x78, 0x1200, 0x100)
			tables := UnwindSet{}
			for _, section := range [][]byte{caller, last} {
				if tc.mode == "no cfi" {
					break
				}
				_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, section))
				if table == nil {
					t.Fatal("caller ELF CFI ignored")
				}
				tables["test_arm64.pdb"] = append(tables["test_arm64.pdb"], table)
			}
			if tc.mode == "cfi" {
				first := buildArchDebugFrame(8, 30, 31, 8, 0x78, 0x1000, 0x100)
				_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, first))
				if table == nil {
					t.Fatal("trampoline ELF CFI ignored")
				}
				tables["test_arm64.pdb"] = append(tables["test_arm64.pdb"], table)
			}
			frames := UnwindCFI(&crash, tables)
			if len(frames) != len(tc.want) {
				t.Fatalf("frames=%+v; want PCs %x", frames, tc.want)
			}
			for i, pc := range tc.want {
				if frames[i].Instruction != pc {
					t.Fatalf("frame %d=%#x; want %#x", i, frames[i].Instruction, pc)
				}
			}
		})
	}
}
