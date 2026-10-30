package native

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"testing"
)

func arm32SignalMinidump(pc, sp uint32, platform uint32, ranges []arm64SignalRange) []byte {
	data := buildARM32Minidump(368)
	thread := minidumpStreamRVA(data, streamThreadList)
	context := binary.LittleEndian.Uint32(data[thread+48:])
	binary.LittleEndian.PutUint32(data[context+48:], 0)
	binary.LittleEndian.PutUint32(data[context+56:], sp)
	binary.LittleEndian.PutUint32(data[context+60:], 0)
	binary.LittleEndian.PutUint32(data[context+64:], pc)
	binary.LittleEndian.PutUint32(data[thread+36:], 0)
	exception := minidumpStreamRVA(data, streamException)
	binary.LittleEndian.PutUint64(data[exception+24:], uint64(pc))
	system := make([]byte, 56)
	binary.LittleEndian.PutUint16(system, cpuArchARM)
	binary.LittleEndian.PutUint32(system[20:], platform)
	systemRVA := uint32(len(data))
	data = append(data, system...)
	memory := make([]byte, 16+len(ranges)*16)
	binary.LittleEndian.PutUint64(memory, uint64(len(ranges)))
	memRVA := uint32(len(data))
	data = append(data, memory...)
	binary.LittleEndian.PutUint64(data[memRVA+8:], uint64(len(data)))
	for i, r := range ranges {
		off := memRVA + 16 + uint32(i)*16
		binary.LittleEndian.PutUint64(data[off:], r.address)
		binary.LittleEndian.PutUint64(data[off+8:], uint64(len(r.data)))
		data = append(data, r.data...)
	}
	directory := uint32(len(data))
	data = append(data, data[32:80]...)
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
	entry := directory + 48
	binary.LittleEndian.PutUint32(data[entry:], streamMemory64)
	binary.LittleEndian.PutUint32(data[entry+4:], uint32(len(memory)))
	binary.LittleEndian.PutUint32(data[entry+8:], memRVA)
	return data
}

func TestLinuxARM32SignalFallbackFromMinidump(t *testing.T) {
	const tramp = 0x60001000
	const initialSP = 0x5000
	const base = 0x400000
	for _, tc := range []struct {
		name, mode string
		rt, oabi   bool
		savedSP    uint32
		want       []uint64
	}{
		{"non RT EABI", "", false, false, 0x9000, []uint64{tramp, base + 0x1100, base + 0x1220}},
		{"RT EABI", "", true, false, 0x9000, []uint64{tramp, base + 0x1100, base + 0x1220}},
		{"non RT OABI", "", false, true, 0x9000, []uint64{tramp, base + 0x1100, base + 0x1220}},
		{"RT OABI", "", true, true, 0x9000, []uint64{tramp, base + 0x1100, base + 0x1220}},
		{"downward disjoint", "", false, false, 0x3000, []uint64{tramp, base + 0x1100, base + 0x1220}},
		{"missing r4", "missing r4", false, false, 0x9000, []uint64{tramp, base + 0x1100}},
		{"missing SP", "missing sp", false, false, 0x9000, []uint64{tramp}},
		{"missing PC", "missing pc", false, false, 0x9000, []uint64{tramp}},
		{"missing CPSR", "missing cpsr", false, false, 0x9000, []uint64{tramp}},
		{"invalid marker", "marker", false, false, 0x9000, nil},
		{"missing marker", "missing marker", false, false, 0x9000, nil},
		{"invalid signal", "signal", true, false, 0x9000, nil},
		{"historical RT pointer", "old rt", true, false, 0x9000, nil},
		{"thumb CPSR", "thumb", false, false, 0x9000, []uint64{tramp, base + 0x1100, base + 0x1220}},
		{"Jazelle CPSR", "jazelle", false, false, 0x9000, []uint64{tramp}},
		{"big endian CPSR", "big endian", false, false, 0x9000, []uint64{tramp}},
		{"unaligned saved PC", "unaligned saved", false, false, 0x9000, []uint64{tramp}},
		{"unaligned current PC", "unaligned", false, false, 0x9000, nil},
		{"wrong OS", "os", false, false, 0x9000, nil},
		{"short instructions", "short", false, false, 0x9000, nil},
		{"wrong svc", "svc", false, true, 0x9000, nil},
		{"zero PC", "zero", false, false, 0x9000, []uint64{tramp}},
		{"repeated PC", "repeat", false, false, 0x9000, []uint64{tramp}},
		{"overlapping stacks", "overlap", false, false, 0x4ff0, []uint64{tramp}},
		{"32 bit overflow", "overflow", false, false, 0x9000, nil},
		{"DWARF precedence", "dwarf", false, false, 0x9000, []uint64{base + 0x1010}},
		{"EHABI CANTUNWIND precedence", "ehabi", false, false, 0x9000, []uint64{base + 0x1010}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pc := uint32(tramp)
			if tc.mode == "dwarf" || tc.mode == "ehabi" {
				pc = base + 0x1010
			}
			if tc.mode == "unaligned" {
				pc++
			}
			sp := uint32(initialSP)
			if tc.mode == "overflow" {
				sp = 0xffffffd0
			}
			off := 32
			nr := uint32(119)
			marker := uint32(0x5ac3c35a)
			if tc.rt {
				off = 160
				nr = 173
				marker = 11
			}
			signal := make([]byte, 256)
			binary.LittleEndian.PutUint32(signal, marker)
			binary.LittleEndian.PutUint32(signal[off+4*4:], tc.savedSP+0x30)
			binary.LittleEndian.PutUint32(signal[off+13*4:], tc.savedSP)
			savedPC := uint32(base + 0x1100)
			if tc.mode == "zero" {
				savedPC = 0
			}
			if tc.mode == "repeat" {
				savedPC = pc
			}
			if tc.mode == "unaligned saved" {
				savedPC++
			}
			binary.LittleEndian.PutUint32(signal[off+15*4:], savedPC)
			if tc.mode == "thumb" {
				binary.LittleEndian.PutUint32(signal[off+16*4:], 0x20)
			}
			if tc.mode == "jazelle" {
				binary.LittleEndian.PutUint32(signal[off+16*4:], 0x01000000)
			}
			if tc.mode == "big endian" {
				binary.LittleEndian.PutUint32(signal[off+16*4:], 0x200)
			}
			if tc.mode == "marker" || tc.mode == "signal" {
				binary.LittleEndian.PutUint32(signal, 0)
			}
			if tc.mode == "old rt" {
				binary.LittleEndian.PutUint32(signal, initialSP+8)
			}
			code := make([]byte, 8)
			binary.LittleEndian.PutUint32(code, 0xe3a07000|nr)
			svc := uint32(0xef000000)
			if tc.oabi {
				svc |= 0x900000 | nr
			}
			if tc.mode == "svc" {
				svc++
			}
			binary.LittleEndian.PutUint32(code[4:], svc)
			if tc.mode == "short" {
				code = code[:4]
			}
			stack := make([]byte, 0x100)
			binary.LittleEndian.PutUint32(stack[0x30:], base+0x1220)
			ranges := []arm64SignalRange{{uint64(pc), code}, {uint64(initialSP), signal}, {uint64(tc.savedSP), stack}}
			if tc.mode == "overflow" {
				ranges = []arm64SignalRange{{uint64(pc), code}, {uint64(sp), make([]byte, 16)}, {uint64(tc.savedSP), stack}}
			}
			if tc.mode == "missing marker" {
				ranges = []arm64SignalRange{{uint64(pc), code}, {uint64(initialSP) + 4, signal[4:]}, {uint64(tc.savedSP), stack}}
			}
			missing := -1
			switch tc.mode {
			case "missing r4":
				missing = off + 4*4
			case "missing sp":
				missing = off + 13*4
			case "missing pc":
				missing = off + 15*4
			case "missing cpsr":
				missing = off + 16*4
			}
			if missing >= 0 {
				ranges = []arm64SignalRange{{uint64(pc), code}, {uint64(initialSP), signal[:missing]}, {uint64(initialSP) + uint64(missing+4), signal[missing+4:]}, {uint64(tc.savedSP), stack}}
			}
			platform := uint32(0x8201)
			if tc.mode == "os" {
				platform = 2
			}
			crash, err := ParseMinidump(arm32SignalMinidump(pc, sp, platform, ranges))
			if err != nil {
				t.Fatal(err)
			}
			tables := UnwindSet{}
			for _, section := range [][]byte{buildArchDebugFrame(4, 14, 4, 4, 0x7c, 0x1100, 0x100), buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1200, 0x100)} {
				_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
				if table == nil {
					t.Fatal("ARM32 CFI ignored")
				}
				tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
			}
			if tc.mode == "dwarf" {
				_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1000, 0x100)))
				tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
			}
			if tc.mode == "ehabi" {
				_, table := ParseObjectUnwind("test_app32.pdb", buildARM32EHABIELF(false, 1))
				if table == nil {
					t.Fatal("EHABI ignored")
				}
				tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
			}
			frames := UnwindCFI(&crash, tables)
			if len(frames) != len(tc.want) {
				t.Fatalf("frames=%+v; want PCs %x", frames, tc.want)
			}
			for i, w := range tc.want {
				if frames[i].Instruction != w {
					t.Fatalf("frame %d=%#x want %#x", i, frames[i].Instruction, w)
				}
			}
		})
	}
}

func TestLinuxARM32SignalRecoveredIntegerRegisterMapping(t *testing.T) {
	const tramp = 0x60001000
	const sp = 0x5000
	const savedSP = 0x9000
	const base = 0x400000
	for _, rt := range []bool{false, true} {
		for reg := byte(0); reg <= 14; reg++ {
			t.Run(fmt.Sprintf("rt_%t_r%d", rt, reg), func(t *testing.T) {
				off := 32
				nr := uint32(119)
				marker := uint32(0x5ac3c35a)
				if rt {
					off = 160
					nr = 173
					marker = 11
				}
				signal := make([]byte, 256)
				binary.LittleEndian.PutUint32(signal, marker)
				for r := byte(0); r <= 14; r++ {
					binary.LittleEndian.PutUint32(signal[off+int(r)*4:], savedSP+0x40+uint32(r)*4)
				}
				binary.LittleEndian.PutUint32(signal[off+13*4:], savedSP)
				binary.LittleEndian.PutUint32(signal[off+15*4:], base+0x1100)
				code := make([]byte, 8)
				binary.LittleEndian.PutUint32(code, 0xe3a07000|nr)
				binary.LittleEndian.PutUint32(code[4:], 0xef000000)
				stack := make([]byte, 0x100)
				binary.LittleEndian.PutUint32(stack, base+0x1220+uint32(reg)*4)
				for r := byte(0); r <= 14; r++ {
					binary.LittleEndian.PutUint32(stack[0x40+int(r)*4:], base+0x1220+uint32(r)*4)
				}
				crash, err := ParseMinidump(arm32SignalMinidump(tramp, sp, 0x8201, []arm64SignalRange{{tramp, code}, {sp, signal}, {savedSP, stack}}))
				if err != nil {
					t.Fatal(err)
				}
				tables := UnwindSet{}
				for _, section := range [][]byte{buildArchDebugFrame(4, 14, reg, 4, 0x7c, 0x1100, 0x100), buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1200, 0x100)} {
					_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
					if table == nil {
						t.Fatal("ARM32 CFI ignored")
					}
					tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
				}
				frames := UnwindCFI(&crash, tables)
				want := []uint64{tramp, base + 0x1100, base + 0x1220 + uint64(reg)*4}
				if len(frames) < len(want) {
					t.Fatalf("r%d frames=%+v want %x", reg, frames, want)
				}
				for i, pc := range want {
					if frames[i].Instruction != pc {
						t.Fatalf("r%d frame %d=%#x want %#x", reg, i, frames[i].Instruction, pc)
					}
				}
			})
		}
	}
}
