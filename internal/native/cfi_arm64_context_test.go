package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

// The interrupted frame uses X19 as its CFA and restores X20 for its caller.
func arm64ContextDebugFrame(missingRule, sameReturn bool) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) {
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
	}
	appendRecord([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, 0x78, 30})
	appendFDE := func(start uint64, instructions []byte) {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0))
		_ = binary.Write(&record, binary.LittleEndian, start)
		_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		_, _ = record.Write(instructions)
		appendRecord(record.Bytes())
	}
	first := []byte{0x0c, 19, 16}
	if !missingRule {
		first = append(first, 0x05, 20, 2) // X20 = *(CFA-16)
	}
	if sameReturn {
		first = append(first, 0x08, 30) // Return address = X30
	} else {
		first = append(first, 0x05, 30, 1) // Return address = *(CFA-8)
	}
	appendFDE(0x1000, first)
	appendFDE(0x1100, []byte{0x0c, 20, 8, 0x05, 30, 1})
	appendFDE(0x1200, []byte{0x0c, 31, 8, 0x05, 30, 1})
	return section.Bytes()
}

func TestARM64MinidumpIntegerContextELFCFI(t *testing.T) {
	const base = uint64(0x100000000)
	const stackBase = uint64(0x16f000000)
	for _, test := range []struct {
		name                                   string
		size                                   int
		arch                                   uint16
		flags                                  uint64
		missingMemory, missingRule, sameReturn bool
		want                                   []uint64
	}{
		{"modern integer", 272, cpuArchARM64, 0x400002, false, false, false, []uint64{base + 0x1010, base + 0x1120, base + 0x1220}},
		{"modern integer and control", 272, cpuArchARM64, 0x400003, false, false, true, []uint64{base + 0x1010, base + 0x1120, base + 0x1220}},
		{"legacy integer", 796, cpuArchARM64Old, 0x80000002, false, false, true, []uint64{base + 0x1010, base + 0x1120, base + 0x1220}},
		{"legacy full", 796, cpuArchARM64Old, 0x80000006, false, false, true, []uint64{base + 0x1010, base + 0x1120, base + 0x1220}},
		{"zero flags", 272, cpuArchARM64, 0, false, false, false, nil},
		{"control without integer", 272, cpuArchARM64, 0x400001, false, false, true, nil},
		{"integer without architecture", 272, cpuArchARM64, 2, false, false, false, nil},
		{"missing memory", 272, cpuArchARM64, 0x400002, true, false, false, []uint64{base + 0x1010}},
		{"missing recovered X20 rule", 272, cpuArchARM64, 0x400002, false, true, false, []uint64{base + 0x1010}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := buildARM64Minidump(test.size, test.arch, test.flags)
			thread := minidumpStreamRVA(data, streamThreadList)
			stack := binary.LittleEndian.Uint32(data[thread+40:])
			context := binary.LittleEndian.Uint32(data[thread+48:])
			x19 := stackBase + 0x10
			if test.missingMemory {
				x19 = stackBase + 0x1000
			}
			binary.LittleEndian.PutUint64(data[context+8+19*8:], x19)
			binary.LittleEndian.PutUint64(data[context+8+20*8:], stackBase+0x40) // Must not leak to the caller.
			binary.LittleEndian.PutUint64(data[context+8+30*8:], base+0x1120)
			binary.LittleEndian.PutUint64(data[stack+0x10:], stackBase+0x30)
			binary.LittleEndian.PutUint64(data[stack+0x18:], base+0x1120)
			binary.LittleEndian.PutUint64(data[stack+0x30:], base+0x1220)
			binary.LittleEndian.PutUint64(data[stack+0x40:], base+0x1330)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, arm64ContextDebugFrame(test.missingRule, test.sameReturn)))
			if table == nil {
				t.Fatal("ARM64 ELF CFI was ignored")
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table}})
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

func TestARM64MinidumpControlContextReturnRegister(t *testing.T) {
	const base = uint64(0x100000000)
	for _, test := range []struct {
		name  string
		flags uint64
		want  bool
	}{
		{"control", 0x400001, true},
		{"integer only", 0x400002, false},
		{"control without architecture", 1, false},
		{"flags zero", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := buildARM64Minidump(272, cpuArchARM64, test.flags)
			thread := minidumpStreamRVA(data, streamThreadList)
			context := binary.LittleEndian.Uint32(data[thread+48:])
			stack := binary.LittleEndian.Uint32(data[thread+40:])
			binary.LittleEndian.PutUint64(data[context+8+30*8:], base+0x1120)
			binary.LittleEndian.PutUint64(data[stack+8:], 0)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			// This independent FDE needs SP, not the integer-register group.
			var replacement bytes.Buffer
			_ = binary.Write(&replacement, binary.LittleEndian, uint32(9))
			_, _ = replacement.Write([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, 0x78, 30})
			var fde bytes.Buffer
			_ = binary.Write(&fde, binary.LittleEndian, uint32(0))
			_ = binary.Write(&fde, binary.LittleEndian, uint64(0x1000))
			_ = binary.Write(&fde, binary.LittleEndian, uint64(0x100))
			_, _ = fde.Write([]byte{0x0c, 31, 8, 0x08, 30})
			_ = binary.Write(&replacement, binary.LittleEndian, uint32(fde.Len()))
			_, _ = replacement.Write(fde.Bytes())
			fde.Reset()
			_ = binary.Write(&fde, binary.LittleEndian, uint32(0))
			_ = binary.Write(&fde, binary.LittleEndian, uint64(0x1100))
			_ = binary.Write(&fde, binary.LittleEndian, uint64(0x100))
			_, _ = fde.Write([]byte{0x0c, 31, 8, 0x05, 30, 1})
			_ = binary.Write(&replacement, binary.LittleEndian, uint32(fde.Len()))
			_, _ = replacement.Write(fde.Bytes())
			section := replacement.Bytes()
			_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, section))
			if table == nil {
				t.Fatal("ARM64 ELF CFI was ignored")
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table}})
			if test.want {
				if len(frames) != 2 || frames[1].Instruction != base+0x1120 {
					t.Fatalf("frames=%+v; want caller %#x", frames, base+0x1120)
				}
			} else if len(frames) != 1 {
				t.Fatalf("frames=%+v; want only the crashing frame", frames)
			}
		})
	}
}

func TestARM64MinidumpIntegerRegisterMapping(t *testing.T) {
	const base = uint64(0x100000000)
	const stackBase = uint64(0x16f000000)
	for _, format := range []struct {
		name  string
		size  int
		arch  uint16
		flags uint64
	}{
		{"modern", 272, cpuArchARM64, 0x400002},
		{"legacy", 796, cpuArchARM64Old, 0x80000002},
	} {
		t.Run(format.name, func(t *testing.T) {
			data := buildARM64Minidump(format.size, format.arch, format.flags)
			thread := minidumpStreamRVA(data, streamThreadList)
			context := binary.LittleEndian.Uint32(data[thread+48:])
			stack := binary.LittleEndian.Uint32(data[thread+40:])
			for reg := uint32(0); reg <= 28; reg++ {
				binary.LittleEndian.PutUint64(data[context+8+reg*8:], stackBase+0x10+uint64(reg)*8)
				binary.LittleEndian.PutUint64(data[stack+0x10+reg*8:], base+0x1120+uint64(reg)*4)
			}
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			caller := buildArchDebugFrame(8, 30, 31, 8, 0x78, 0x1100, 0x100)
			_, callerTable := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, caller))
			if callerTable == nil {
				t.Fatal("ARM64 caller CFI was ignored")
			}
			for reg := byte(0); reg <= 28; reg++ {
				frame := buildArchDebugFrame(8, 30, reg, 8, 0x78, 0x1010, 0x60)
				_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, frame))
				if table == nil {
					t.Fatal("ARM64 ELF CFI was ignored")
				}
				frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table, callerTable}})
				want := base + 0x1120 + uint64(reg)*4
				if len(frames) < 2 || frames[1].Instruction != want {
					t.Errorf("DWARF X%d: frames=%+v; want caller %#x", reg, frames, want)
				}
			}
		})
	}
}

func TestARM64MinidumpIntegerContextMachOCFI(t *testing.T) {
	const base = uint64(0x100000000)
	const stackBase = uint64(0x16f000000)
	data := buildARM64Minidump(272, cpuArchARM64, 0x400002)
	thread := minidumpStreamRVA(data, streamThreadList)
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	context := binary.LittleEndian.Uint32(data[thread+48:])
	binary.LittleEndian.PutUint64(data[context+8+19*8:], stackBase+0x10)
	binary.LittleEndian.PutUint64(data[stack+0x10:], stackBase+0x30)
	binary.LittleEndian.PutUint64(data[stack+0x18:], base+0x1120)
	binary.LittleEndian.PutUint64(data[stack+0x30:], base+0x1220)
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	_, table := ParseObjectUnwind("test_arm64.pdb", buildMachO64DebugFrame(arm64ContextDebugFrame(false, false)))
	if table == nil {
		t.Fatal("ARM64 Mach-O CFI was ignored")
	}
	frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table}})
	want := []uint64{base + 0x1010, base + 0x1120, base + 0x1220}
	if len(frames) != len(want) {
		t.Fatalf("frames=%+v; want PCs %x", frames, want)
	}
	for i, pc := range want {
		if frames[i].Instruction != pc {
			t.Fatalf("frame %d=%#x; want %#x", i, frames[i].Instruction, pc)
		}
	}
}

func TestARM64CFIPropagatesRecoveredRegisterWithoutInitialIntegerFlags(t *testing.T) {
	const base = uint64(0x100000000)
	const stackBase = uint64(0x16f000000)
	data := buildARM64Minidump(272, cpuArchARM64, 0)
	thread := minidumpStreamRVA(data, streamThreadList)
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	binary.LittleEndian.PutUint64(data[stack:], stackBase+0x30)
	binary.LittleEndian.PutUint64(data[stack+8:], base+0x1120)
	binary.LittleEndian.PutUint64(data[stack+0x30:], base+0x1220)
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	var section bytes.Buffer
	write := func(record []byte) {
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
	}
	write([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, 0x78, 30})
	frame := func(start uint64, instructions []byte) {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0))
		_ = binary.Write(&record, binary.LittleEndian, start)
		_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		_, _ = record.Write(instructions)
		write(record.Bytes())
	}
	frame(0x1000, []byte{0x0c, 31, 16, 0x05, 19, 2, 0x05, 30, 1}) // recover X19 from SP
	frame(0x1100, []byte{0x0c, 19, 8, 0x05, 30, 1})
	frame(0x1200, []byte{0x0c, 31, 8, 0x05, 30, 1})
	_, table := ParseObjectUnwind("test_arm64.pdb", buildELF64DebugFrame(elf.EM_AARCH64, section.Bytes()))
	if table == nil {
		t.Fatal("ARM64 ELF CFI was ignored")
	}
	frames := UnwindCFI(&crash, UnwindSet{"test_arm64.pdb": {table}})
	want := []uint64{base + 0x1010, base + 0x1120, base + 0x1220}
	if len(frames) != len(want) {
		t.Fatalf("frames=%+v; want PCs %x", frames, want)
	}
	for i, pc := range want {
		if frames[i].Instruction != pc {
			t.Fatalf("frame %d=%#x; want %#x", i, frames[i].Instruction, pc)
		}
	}
}
