package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"testing"
)

// The signal context saves an instruction address, not a tagged return address.
func TestLinuxARM32SignalInterruptedThumb(t *testing.T) {
	const base = uint32(0x400000)
	const tramp = uint32(0x60001000)
	const sp = uint32(0x5000)
	const savedSP = uint32(0x9000)
	const savedPC = base + 0x1102
	const caller = base + 0x1220
	for _, rt := range []bool{false, true} {
		for _, thumbRestorer := range []bool{false, true} {
			for _, mode := range []string{"valid", "no cfi", "jazelle", "big endian", "odd PC", "missing r4"} {
				t.Run(fmt.Sprintf("rt_%t/thumb_restorer_%t/%s", rt, thumbRestorer, mode), func(t *testing.T) {
					offset, nr, marker := 32, uint32(119), uint32(0x5ac3c35a)
					if rt {
						offset, nr, marker = 160, 173, 11
					}
					signal := make([]byte, 256)
					binary.LittleEndian.PutUint32(signal, marker)
					binary.LittleEndian.PutUint32(signal[offset+4*4:], savedSP+0x30)
					binary.LittleEndian.PutUint32(signal[offset+13*4:], savedSP)
					pc := savedPC
					if mode == "odd PC" {
						pc++
					}
					binary.LittleEndian.PutUint32(signal[offset+15*4:], pc)
					cpsr := uint32(0x20)
					if mode == "jazelle" {
						cpsr |= 0x01000000
					}
					if mode == "big endian" {
						cpsr |= 0x200
					}
					binary.LittleEndian.PutUint32(signal[offset+16*4:], cpsr)
					code := make([]byte, 8)
					restorerPC := tramp
					if thumbRestorer {
						restorerPC |= 1
						binary.LittleEndian.PutUint32(code, 0xdf002700|nr)
						code = code[:4]
					} else {
						binary.LittleEndian.PutUint32(code, 0xe3a07000|nr)
						binary.LittleEndian.PutUint32(code[4:], 0xef000000)
					}
					stack := make([]byte, 0x100)
					binary.LittleEndian.PutUint32(stack[0x30:], caller)
					// A second FDE has a plausible caller but must not be used for this slice.
					binary.LittleEndian.PutUint32(stack, base+0x1320)
					ranges := []arm64SignalRange{{uint64(tramp), code}, {uint64(sp), signal}, {uint64(savedSP), stack}}
					if mode == "missing r4" {
						missing := offset + 4*4
						ranges = []arm64SignalRange{{uint64(tramp), code}, {uint64(sp), signal[:missing]}, {uint64(sp) + uint64(missing+4), signal[missing+4:]}, {uint64(savedSP), stack}}
					}
					crash, err := ParseMinidump(arm32SignalMinidump(restorerPC, sp, 0x8201, ranges))
					if err != nil {
						t.Fatal(err)
					}
					tables := UnwindSet{}
					if mode != "no cfi" {
						for _, section := range [][]byte{buildArchDebugFrame(4, 14, 4, 4, 0x7c, 0x1102, 0x100), buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1200, 0x100)} {
							_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
							if table == nil {
								t.Fatal("ARM32 CFI ignored")
							}
							tables["test_app32.pdb"] = append(tables["test_app32.pdb"], table)
						}
					}
					frames := UnwindCFI(&crash, tables)
					want := []uint64{uint64(restorerPC), uint64(savedPC), uint64(caller)}
					switch mode {
					case "no cfi", "missing r4":
						want = want[:2]
					case "jazelle", "big endian", "odd PC":
						want = want[:1]
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
}

// EXIDX_CANTUNWIND covers the preceding instruction; only an exact lookup
// reaches the second entry at the interrupted instruction.
func TestLinuxARM32InterruptedThumbEHABIExactPC(t *testing.T) {
	const base = uint32(0x400000)
	const trampoline = uint32(0x60001000)
	const sp = uint32(0x5000)
	const savedSP = uint32(0x9000)
	const savedPC = base + 0x1102
	const caller = base + 0x1220
	signal := make([]byte, 256)
	binary.LittleEndian.PutUint32(signal, 0x5ac3c35a)
	binary.LittleEndian.PutUint32(signal[32+13*4:], savedSP)
	binary.LittleEndian.PutUint32(signal[32+15*4:], savedPC)
	binary.LittleEndian.PutUint32(signal[32+16*4:], 0x20)
	code := make([]byte, 8)
	binary.LittleEndian.PutUint32(code, 0xe3a07077)
	binary.LittleEndian.PutUint32(code[4:], 0xef000000)
	stack := make([]byte, 16)
	binary.LittleEndian.PutUint32(stack, caller)
	crash, err := ParseMinidump(arm32SignalMinidump(trampoline, sp, 0x8201, []arm64SignalRange{{uint64(trampoline), code}, {uint64(sp), signal}, {uint64(savedSP), stack}}))
	if err != nil {
		t.Fatal(err)
	}
	object := buildARM32EHABIELF(false, 1)
	f, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	index := f.Section(".ARM.exidx")
	if index == nil {
		t.Fatal("missing ARM exidx")
	}
	data := object[index.Offset : index.Offset+index.Size]
	binary.LittleEndian.PutUint32(data[8:], arm32TestPrel31(0x2008, 0x1102))
	binary.LittleEndian.PutUint32(data[12:], 0x808400b0)
	_, table := ParseObjectUnwind("test_app32.pdb", object)
	if table == nil {
		t.Fatal("ARM32 EHABI ignored")
	}
	frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
	want := []uint64{uint64(trampoline), uint64(savedPC), uint64(caller)}
	if len(frames) != len(want) {
		t.Fatalf("frames=%+v; want PCs %x", frames, want)
	}
	for i, pc := range want {
		if frames[i].Instruction != pc {
			t.Fatalf("frame %d=%#x; want %#x", i, frames[i].Instruction, pc)
		}
	}
	binary.LittleEndian.PutUint32(data[12:], 1) // EXIDX_CANTUNWIND at saved PC.
	_, table = ParseObjectUnwind("test_app32.pdb", object)
	if table == nil {
		t.Fatal("ARM32 EHABI ignored with CANTUNWIND")
	}
	frames = UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
	if len(frames) != 2 || frames[1].Instruction != uint64(savedPC) {
		t.Fatalf("CANTUNWIND frames=%+v; want restored PC only", frames)
	}

}

func TestLinuxARM32InterruptedThumbFrameLimit(t *testing.T) {
	const base = uint32(0x400000)
	const trampoline = uint32(0x60001000)
	const sp = uint32(0x5000)
	const savedSP = uint32(0x9000)
	const savedPC = base + 0x1302
	for _, previous := range []int{62, 63} {
		t.Run(fmt.Sprintf("restorer_at_frame_%d", previous+1), func(t *testing.T) {
			stack := make([]byte, 1024)
			for i := 0; i < previous-1; i++ {
				binary.LittleEndian.PutUint32(stack[i*4:], base+0x1004+uint32(i*4))
			}
			binary.LittleEndian.PutUint32(stack[(previous-1)*4:], trampoline)
			signal := stack[previous*4:]
			binary.LittleEndian.PutUint32(signal, 0x5ac3c35a)
			binary.LittleEndian.PutUint32(signal[32+13*4:], savedSP)
			binary.LittleEndian.PutUint32(signal[32+15*4:], savedPC)
			binary.LittleEndian.PutUint32(signal[32+16*4:], 0x20)
			code := make([]byte, 8)
			binary.LittleEndian.PutUint32(code, 0xe3a07077)
			binary.LittleEndian.PutUint32(code[4:], 0xef000000)
			crash, err := ParseMinidump(arm32SignalMinidump(base+0x1000, sp, 0x8201, []arm64SignalRange{{uint64(trampoline), code}, {uint64(sp), stack}, {uint64(savedSP), make([]byte, 16)}}))
			if err != nil {
				t.Fatal(err)
			}
			section := buildArchDebugFrame(4, 14, 13, 4, 0x7c, 0x1000, 0x200)
			_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_ARM, section))
			if table == nil {
				t.Fatal("ARM32 CFI ignored")
			}
			frames, recovered := UnwindCFIWithSignalRecovery(&crash, UnwindSet{"test_app32.pdb": {table}})
			if len(frames) != 64 {
				t.Fatalf("got %d frames; want 64", len(frames))
			}
			if got := frames[previous].Instruction; got != uint64(trampoline) {
				t.Fatalf("restorer frame = %#x", got)
			}
			if previous == 62 && (frames[63].Instruction != uint64(savedPC) || !recovered) {
				t.Fatalf("restored frame = %+v, recovered=%t", frames[63], recovered)
			}
			if previous == 63 && recovered {
				t.Fatal("reported recovery without room for the restored PC")
			}
		})
	}
}
