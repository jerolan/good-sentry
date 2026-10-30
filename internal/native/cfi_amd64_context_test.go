package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

// The first frame uses captured RBX and restores R12; its caller uses R12.
func amd64ContextDebugFrame(missingR12Rule bool) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) {
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
	}
	appendRecord([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, 0x78, 16})
	appendFDE := func(start uint64, instructions []byte) {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0))
		_ = binary.Write(&record, binary.LittleEndian, start)
		_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		_, _ = record.Write(instructions)
		appendRecord(record.Bytes())
	}
	first := []byte{0x0c, 3, 16}
	if !missingR12Rule {
		first = append(first, 0x05, 12, 2) // R12 = *(CFA-16)
	}
	first = append(first, 0x05, 16, 1) // RIP = *(CFA-8)
	appendFDE(0x1000, first)
	appendFDE(0x1100, []byte{0x0c, 12, 8, 0x05, 16, 1})
	appendFDE(0x1200, []byte{0x0c, 7, 8, 0x05, 16, 1})
	return section.Bytes()
}

func TestAMD64MinidumpIntegerContextELFCFI(t *testing.T) {
	const base = uint64(0x140000000)
	for _, test := range []struct {
		name          string
		flags         uint32
		extension     int
		missingMemory bool
		missingRule   bool
		want          []uint64
	}{
		{"integer context", 0x100002, 0, false, false, []uint64{base + 0x1010, base + 0x1120, base + 0x1220}},
		{"integer context with XSTATE", 0x100042, 144, false, false, []uint64{base + 0x1010, base + 0x1120, base + 0x1220}},
		{"integer flag absent", 0x100001, 0, false, false, nil},
		{"integer bit without amd64 architecture flag", 0x2, 0, false, false, nil},
		{"wrong architecture flag", 0x10002, 0, false, false, nil},
		{"flags zero", 0, 0, false, false, nil},
		{"missing captured memory", 0x100002, 0, true, false, []uint64{base + 0x1010}},
		{"missing caller register rule", 0x100002, 0, false, true, []uint64{base + 0x1010}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := buildAMD64MinidumpWithContext(1232+test.extension, test.extension != 0)
			thread := minidumpStreamRVA(data, streamThreadList)
			stack := binary.LittleEndian.Uint32(data[thread+40:])
			context := binary.LittleEndian.Uint32(data[thread+48:])
			binary.LittleEndian.PutUint32(data[context+48:], test.flags)
			binary.LittleEndian.PutUint64(data[context+144:], 0x200010) // RBX
			binary.LittleEndian.PutUint64(data[context+216:], 0x200030) // R12; must not leak to caller
			if test.missingMemory {
				binary.LittleEndian.PutUint64(data[context+144:], 0x201000)
			}
			binary.LittleEndian.PutUint64(data[stack+0x10:], 0x200030)
			binary.LittleEndian.PutUint64(data[stack+0x18:], base+0x1120)
			binary.LittleEndian.PutUint64(data[stack+0x30:], base+0x1220)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			_, table := ParseObjectUnwind("test_app.pdb", buildELF64DebugFrame(elf.EM_X86_64, amd64ContextDebugFrame(test.missingRule)))
			if table == nil {
				t.Fatal("AMD64 ELF CFI was ignored")
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_app.pdb": {table}})
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

func TestAMD64MinidumpIntegerContextRegisterMapping(t *testing.T) {
	const base = uint64(0x140000000)
	data := buildAMD64Minidump()
	thread := minidumpStreamRVA(data, streamThreadList)
	context := binary.LittleEndian.Uint32(data[thread+48:])
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	binary.LittleEndian.PutUint32(data[context+48:], 0x100002)
	registers := []struct {
		dwarf  byte
		offset uint32
	}{
		{0, 120}, {1, 136}, {2, 128}, {3, 144}, {4, 168}, {5, 176},
		{8, 184}, {9, 192}, {10, 200}, {11, 208}, {12, 216}, {13, 224}, {14, 232}, {15, 240},
	}
	for i, reg := range registers {
		binary.LittleEndian.PutUint64(data[context+reg.offset:], uint64(0x200010+i*8))
		binary.LittleEndian.PutUint64(data[stack+0x10+uint32(i)*8:], base+0x1120+uint64(i)*4)
	}
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	for index, reg := range registers {
		frame := buildArchDebugFrame(8, 16, reg.dwarf, 8, 0x78, 0x1010, 0x60)
		_, table := ParseObjectUnwind("test_app.pdb", buildELF64DebugFrame(elf.EM_X86_64, frame))
		if table == nil {
			t.Fatal("AMD64 ELF CFI was ignored")
		}
		caller := buildArchDebugFrame(8, 16, 7, 8, 0x78, 0x1100, 0x100)
		_, callerTable := ParseObjectUnwind("test_app.pdb", buildELF64DebugFrame(elf.EM_X86_64, caller))
		if callerTable == nil {
			t.Fatal("AMD64 caller CFI was ignored")
		}
		frames := UnwindCFI(&crash, UnwindSet{"test_app.pdb": {table, callerTable}})
		want := base + 0x1120 + uint64(index)*4
		if len(frames) < 2 || frames[1].Instruction != want {
			t.Errorf("DWARF register %d: frames=%+v; want caller %#x", reg.dwarf, frames, want)
		}
	}
}
