package native

import (
	"debug/elf"
	"encoding/binary"
	"testing"
)

func TestLinuxARM32InterruptedThumbCFIAddressBounds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		savedSP, r4 uint32
		slot        uint64
		align       byte
		factor      []byte
		wantCaller  bool
	}{
		{"valid", 0x9000, 0x9000, 0x9008, 4, []byte{1}, true},
		{"slot beyond address space", 0xffffff00, 0xfffffff8, 0x100000000, 4, []byte{1}, false},
		{"slot crosses address space", 0xffffff00, 0xfffffff6, 0xfffffffe, 4, []byte{1}, false},
		{"negative offset wraps", 0x9000, 0x9000, 0, 0x7c, []byte{0x82, 0x48}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const tramp = 0x60001000
			code := make([]byte, 8)
			binary.LittleEndian.PutUint32(code, 0xe3a07077)
			binary.LittleEndian.PutUint32(code[4:], 0xef000000)
			signal := make([]byte, 100)
			binary.LittleEndian.PutUint32(signal, 0x5ac3c35a)
			binary.LittleEndian.PutUint32(signal[32+4*4:], tc.r4)
			binary.LittleEndian.PutUint32(signal[32+13*4:], tc.savedSP)
			binary.LittleEndian.PutUint32(signal[32+15*4:], 0x401102)
			binary.LittleEndian.PutUint32(signal[32+16*4:], 0x20)
			slot := make([]byte, 4)
			binary.LittleEndian.PutUint32(slot, 0x401220)
			crash, err := ParseMinidump(arm32SignalMinidump(tramp, 0x5000, 0x8201, []arm64SignalRange{{tramp, code}, {0x5000, signal}, {tc.slot, slot}}))
			if err != nil {
				t.Fatal(err)
			}
			section := buildArchDebugFrame(4, 14, 4, 4, tc.align, 0x1102, 0x100)
			// Replace the unsigned factor in the CIE return-address rule.
			if len(tc.factor) > 1 {
				section = append(append(append([]byte{}, section[:18]...), tc.factor...), section[19:]...)
				binary.LittleEndian.PutUint32(section, binary.LittleEndian.Uint32(section)+uint32(len(tc.factor)-1))
			} else {
				section[18] = tc.factor[0]
			}
			_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
			if table == nil {
				t.Fatal("CFI ignored")
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
			want := 2
			if tc.wantCaller {
				want = 3
			}
			if len(frames) != want {
				t.Fatalf("frames=%+v, want %d", frames, want)
			}
			if tc.wantCaller && frames[2].Instruction != 0x401220 {
				t.Fatalf("caller=%#x", frames[2].Instruction)
			}
		})
	}
}
