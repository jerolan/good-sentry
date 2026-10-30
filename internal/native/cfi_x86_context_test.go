package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

// Two ordinary x86 frames: the first uses the captured EBX for its CFA and
// restores ESI; the caller then uses that recovered ESI as its CFA base.
func x86ContextDebugFrame(missingESIRule bool) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) {
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
	}
	appendRecord([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, 0x7c, 8})
	appendFDE := func(start uint32, instructions []byte) {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0))
		_ = binary.Write(&record, binary.LittleEndian, start)
		_ = binary.Write(&record, binary.LittleEndian, uint32(0x100))
		_, _ = record.Write(instructions)
		appendRecord(record.Bytes())
	}
	first := []byte{0x0c, 3, 8}
	if !missingESIRule {
		first = append(first, 0x05, 6, 2) // ESI = *(CFA-8)
	}
	first = append(first, 0x05, 8, 1) // EIP = *(CFA-4)
	appendFDE(0x1000, first)
	appendFDE(0x1100, []byte{0x0c, 6, 4, 0x05, 8, 1})
	appendFDE(0x1200, []byte{0x0c, 4, 4, 0x05, 8, 1})
	return section.Bytes()
}

func TestX86MinidumpIntegerContextELFCFI(t *testing.T) {
	for _, test := range []struct {
		name      string
		flags     uint32
		extension int
		missing   bool
		want      []uint64
	}{
		{"integer context", 0x10002, 0, false, []uint64{0x401010, 0x401120, 0x401220}},
		{"integer context with XSTATE", 0x10042, 144, false, []uint64{0x401010, 0x401120, 0x401220}},
		{"integer flag absent", 0x10001, 0, false, nil},
		{"integer bit without x86 architecture flag", 0x2, 0, false, nil},
		{"flags zero", 0, 0, false, nil},
		{"missing captured memory", 0x10002, 0, true, []uint64{0x401010}},
		{"missing caller register rule", 0x10002, 0, false, []uint64{0x401010}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := buildX86MinidumpWithContext(716+test.extension, test.extension != 0)
			thread := minidumpStreamRVA(data, streamThreadList)
			stack := binary.LittleEndian.Uint32(data[thread+40:])
			context := binary.LittleEndian.Uint32(data[thread+48:])
			binary.LittleEndian.PutUint32(data[context:], test.flags)
			// Crashpad x86 context order: EDI, ESI, EBX, EDX, ECX, EAX.
			for i, value := range []uint32{0x5555, 0x6666, 0x200010, 0x2222, 0x1111, 0xaaaa} {
				binary.LittleEndian.PutUint32(data[context+156+uint32(i)*4:], value)
			}
			if test.missing {
				binary.LittleEndian.PutUint32(data[context+164:], 0x201000)
			}
			if test.name == "missing caller register rule" {
				// Retaining the initial ESI would fabricate a caller from this slot.
				binary.LittleEndian.PutUint32(data[context+160:], 0x200030)
			}
			binary.LittleEndian.PutUint32(data[stack+0x10:], 0x200030)
			binary.LittleEndian.PutUint32(data[stack+0x14:], 0x401120)
			binary.LittleEndian.PutUint32(data[stack+0x30:], 0x401220)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_386, x86ContextDebugFrame(test.name == "missing caller register rule")))
			if table == nil {
				t.Fatal("x86 ELF CFI was ignored")
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
			if len(frames) != len(test.want) {
				t.Fatalf("frames = %+v; want PCs %x", frames, test.want)
			}
			for i, want := range test.want {
				if frames[i].Instruction != want {
					t.Fatalf("frame %d PC = %#x; want %#x", i, frames[i].Instruction, want)
				}
			}
		})
	}
}

func TestX86MinidumpIntegerContextRegisterMapping(t *testing.T) {
	data := buildX86MinidumpWithContext(716, false)
	thread := minidumpStreamRVA(data, streamThreadList)
	context := binary.LittleEndian.Uint32(data[thread+48:])
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	binary.LittleEndian.PutUint32(data[context:], 0x10002)
	for i := uint32(0); i < 6; i++ {
		binary.LittleEndian.PutUint32(data[context+156+i*4:], 0x200010+i*4)
		binary.LittleEndian.PutUint32(data[stack+0x10+i*4:], 0x401120+i*4)
	}
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	for index, register := range []byte{7, 6, 3, 2, 1, 0} {
		frame := buildArchDebugFrame(4, 8, register, 4, 0x7c, 0x1010, 0x60)
		_, table := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_386, frame))
		if table == nil {
			t.Fatal("x86 ELF CFI was ignored")
		}
		caller := buildArchDebugFrame(4, 8, 4, 4, 0x7c, 0x1100, 0x100)
		_, callerTable := ParseObjectUnwind("test_app32.pdb", buildELF32DebugFrame(elf.EM_386, caller))
		if callerTable == nil {
			t.Fatal("x86 caller CFI was ignored")
		}
		frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table, callerTable}})
		want := uint64(0x401120 + index*4)
		if len(frames) < 2 || frames[1].Instruction != want {
			t.Errorf("DWARF register %d: frames=%+v; want caller %#x", register, frames, want)
		}
	}
}
