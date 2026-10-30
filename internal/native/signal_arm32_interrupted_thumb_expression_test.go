package native

import (
	"debug/elf"
	"encoding/binary"
	"testing"
)

func TestLinuxARM32InterruptedThumbRejectsCFIExpressions(t *testing.T) {
	const base = uint32(0x400000)
	const trampoline = uint32(0x60001000)
	const sp = uint32(0x5000)
	const savedSP = uint32(0x9000)
	const savedPC = base + 0x1104
	const caller = base + 0x1220
	cases := []struct {
		name        string
		instruction []byte
	}{
		{"CFA expression", []byte{0x0f, 2, 0x7d, 4}},                                   // DW_OP_breg13 + 4
		{"return expression", []byte{0x10, 14, 2, 0x7d, 0}},                            // location at SP
		{"return value expression", []byte{0x16, 14, 5, 0x03, 0x20, 0x12, 0x40, 0x00}}, // DW_OP_addr caller
	}
	for _, tc := range cases {
		for _, thumb := range []bool{false, true} {
			name := tc.name + "/ARM"
			if thumb {
				name = tc.name + "/Thumb"
			}
			t.Run(name, func(t *testing.T) {
				signal := make([]byte, 256)
				binary.LittleEndian.PutUint32(signal, 0x5ac3c35a)
				binary.LittleEndian.PutUint32(signal[32+13*4:], savedSP)
				binary.LittleEndian.PutUint32(signal[32+15*4:], savedPC)
				if thumb {
					binary.LittleEndian.PutUint32(signal[32+16*4:], 0x20)
				}
				code := make([]byte, 8)
				binary.LittleEndian.PutUint32(code, 0xe3a07077)
				binary.LittleEndian.PutUint32(code[4:], 0xef000000)
				stack := make([]byte, 16)
				binary.LittleEndian.PutUint32(stack, caller)
				crash, err := ParseMinidump(arm32SignalMinidump(trampoline, sp, 0x8201, []arm64SignalRange{{uint64(trampoline), code}, {uint64(sp), signal}, {uint64(savedSP), stack}}))
				if err != nil {
					t.Fatal(err)
				}
				section := buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1100, 0x100)
				fde := 4 + int(binary.LittleEndian.Uint32(section))
				length := binary.LittleEndian.Uint32(section[fde:])
				binary.LittleEndian.PutUint32(section[fde:], length+uint32(len(tc.instruction)))
				section = append(section, tc.instruction...)
				_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
				if table == nil {
					t.Fatal("ARM32 CFI ignored")
				}
				_, nextTable := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1200, 0x100)))
				if nextTable == nil {
					t.Fatal("caller CFI ignored")
				}
				frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table, nextTable}})
				want := []uint64{uint64(trampoline), uint64(savedPC)}
				if !thumb {
					want = append(want, uint64(caller))
				}
				if len(frames) != len(want) {
					t.Fatalf("frames=%+v; want PCs %x", frames, want)
				}
				for i, pc := range want {
					if frames[i].Instruction != pc {
						t.Fatalf("frame %d=%#x; want %#x", i, frames[i].Instruction, pc)
					}
				}
			})
		}
	}
}
