package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

// The first ordinary frame recovers a register from its stack. The caller
// needs that register to compute its CFA, even with no initial integer flags.
func recoveredX86DebugFrame(pointerSize int, missingRule, missingMemory bool) []byte {
	var section bytes.Buffer
	write := func(record []byte) {
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
	}
	sp, recovered, ret, align := byte(4), byte(6), byte(8), byte(0x7c)
	if pointerSize == 8 {
		sp, recovered, ret, align = 7, 12, 16, 0x78
	}
	write([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, align, ret})
	frame := func(start uint64, instructions []byte) {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0))
		if pointerSize == 4 {
			_ = binary.Write(&record, binary.LittleEndian, uint32(start))
			_ = binary.Write(&record, binary.LittleEndian, uint32(0x100))
		} else {
			_ = binary.Write(&record, binary.LittleEndian, start)
			_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		}
		_, _ = record.Write(instructions)
		write(record.Bytes())
	}
	first := []byte{0x0c, sp, byte(2 * pointerSize)}
	if !missingRule {
		offset := byte(2)
		if missingMemory {
			offset = 32 // The register's slot lies outside captured stack memory.
		}
		first = append(first, 0x05, recovered, offset)
	}
	first = append(first, 0x05, ret, 1)
	frame(0x1000, first)
	frame(0x1100, []byte{0x0c, recovered, byte(pointerSize), 0x05, ret, 1})
	frame(0x1200, []byte{0x0c, sp, byte(pointerSize), 0x05, ret, 1})
	return section.Bytes()
}

func TestX86AndAMD64CFIPropagateRecoveredRegisterWithoutIntegerFlags(t *testing.T) {
	for _, arch := range []struct {
		name        string
		pointerSize int
		base        uint64
		module      string
	}{
		{"x86", 4, 0x400000, "test_app32.pdb"},
		{"amd64", 8, 0x140000000, "test_app.pdb"},
	} {
		for _, control := range []struct {
			name                       string
			missingRule, missingMemory bool
		}{
			{"recovered", false, false},
			{"missing rule", true, false},
			{"missing memory", false, true},
		} {
			t.Run(arch.name+"/"+control.name, func(t *testing.T) {
				var data []byte
				if arch.pointerSize == 4 {
					data = buildX86Minidump()
				} else {
					data = buildAMD64Minidump()
				}
				thread := minidumpStreamRVA(data, streamThreadList)
				stack := binary.LittleEndian.Uint32(data[thread+40:])
				putPointer := func(offset uint32, value uint64) {
					if arch.pointerSize == 4 {
						binary.LittleEndian.PutUint32(data[stack+offset:], uint32(value))
					} else {
						binary.LittleEndian.PutUint64(data[stack+offset:], value)
					}
				}
				putPointer(0, 0x200030)
				putPointer(uint32(arch.pointerSize), arch.base+0x1120)
				putPointer(0x30, arch.base+0x1220)
				crash, err := ParseMinidump(data)
				if err != nil {
					t.Fatal(err)
				}
				section := recoveredX86DebugFrame(arch.pointerSize, control.missingRule, control.missingMemory)
				var object []byte
				if arch.pointerSize == 4 {
					object = buildELF32DebugFrame(elf.EM_386, section)
				} else {
					object = buildELF64DebugFrame(elf.EM_X86_64, section)
				}
				_, table := ParseObjectUnwind(arch.module, object)
				if table == nil {
					t.Fatal("ELF CFI was ignored")
				}
				frames := UnwindCFI(&crash, UnwindSet{arch.module: {table}})
				want := []uint64{arch.base + 0x1010}
				if !control.missingRule && !control.missingMemory {
					want = append(want, arch.base+0x1120, arch.base+0x1220)
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
