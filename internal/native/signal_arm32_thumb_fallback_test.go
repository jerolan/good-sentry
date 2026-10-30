package native

import (
	"debug/elf"
	"encoding/binary"
	"testing"
)

// An ARM handler unwinds to a tagged Thumb signal restorer. The interrupted
// state remains ARM; this is not a test of unwinding an interrupted Thumb PC.
func TestLinuxARM32ThumbSignalRestorerFromMinidump(t *testing.T) {
	const base = 0x400000
	const handler = base + 0x1010
	const handlerSP = 0x5000
	const signalSP = handlerSP + 4
	const savedSP = 0x9000
	for _, tc := range []struct {
		name, mode string
		rt         bool
		want       []uint64
	}{
		{"non RT Thumb, halfword aligned", "", false, []uint64{handler, 0x60001003, base + 0x1100, base + 0x1220}},
		{"RT Thumb, halfword aligned", "", true, []uint64{handler, 0x60001003, base + 0x1100, base + 0x1220}},
		{"missing opcode byte", "short", false, []uint64{handler}},
		{"wrong opcode", "wrong", false, []uint64{handler}},
		{"untagged return pointer", "untagged", false, []uint64{handler}},
		{"invalid frame marker", "marker", false, []uint64{handler}},
		{"interrupted Thumb CPSR", "cpsr", false, []uint64{handler, 0x60001003, base + 0x1100, base + 0x1220}},
		{"trampoline DWARF takes precedence", "dwarf", false, []uint64{handler, base + 0x1083}},
		{"trampoline EHABI CANTUNWIND takes precedence", "ehabi", false, []uint64{handler, base + 0x1083}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tagged := uint32(0x60001003)
			if tc.mode == "dwarf" || tc.mode == "ehabi" {
				tagged = base + 0x1083
			}
			if tc.mode == "untagged" {
				tagged &^= 1
			}
			actual := tagged &^ 1
			handlerStack := make([]byte, 4)
			binary.LittleEndian.PutUint32(handlerStack, tagged)
			offset := 32
			marker := uint32(0x5ac3c35a)
			op := uint32(0xdf002777)
			if tc.rt {
				offset = 160
				marker = 11
				op = 0xdf0027ad
			}
			signal := make([]byte, 256)
			binary.LittleEndian.PutUint32(signal, marker)
			if tc.mode == "marker" {
				binary.LittleEndian.PutUint32(signal, 0)
			}
			binary.LittleEndian.PutUint32(signal[offset+4*4:], savedSP+0x30)
			binary.LittleEndian.PutUint32(signal[offset+13*4:], savedSP)
			binary.LittleEndian.PutUint32(signal[offset+15*4:], base+0x1100)
			if tc.mode == "cpsr" {
				binary.LittleEndian.PutUint32(signal[offset+16*4:], 0x20)
			}
			code := make([]byte, 4)
			binary.LittleEndian.PutUint32(code, op)
			if tc.mode == "short" {
				code = code[:3]
			}
			if tc.mode == "wrong" {
				code[1] ^= 1
			}
			stack := make([]byte, 0x100)
			binary.LittleEndian.PutUint32(stack[0x30:], base+0x1220)
			crash, err := ParseMinidump(arm32SignalMinidump(handler, handlerSP, 0x8201, []arm64SignalRange{{handlerSP, handlerStack}, {signalSP, signal}, {uint64(actual), code}, {savedSP, stack}}))
			if err != nil {
				t.Fatal(err)
			}
			tables := UnwindSet{}
			for _, section := range [][]byte{buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1000, 0x40), buildArchDebugFrame(4, 14, 4, 4, 0x7c, 0x1100, 0x100), buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1200, 0x100)} {
				_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
				if table == nil {
					t.Fatal("ARM CFI ignored")
				}
				tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
			}
			if tc.mode == "dwarf" {
				_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1082, 2)))
				if table == nil {
					t.Fatal("restorer CFI ignored")
				}
				tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
			}
			if tc.mode == "ehabi" {
				_, table := ParseObjectUnwind("test_app32.pdb", buildARM32EHABIELF(false, 1))
				if table == nil {
					t.Fatal("restorer EHABI ignored")
				}
				tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
			}
			frames := UnwindCFI(&crash, tables)
			if len(frames) != len(tc.want) {
				t.Fatalf("frames=%+v; want %x", frames, tc.want)
			}
			for i, want := range tc.want {
				if frames[i].Instruction != want {
					t.Fatalf("frame %d=%#x; want %#x", i, frames[i].Instruction, want)
				}
			}
		})
	}
}
