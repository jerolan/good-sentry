package native

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
)

// These records are the original CIE and the main.leaf/main.caller FDEs from
// Go cross-compiled mipsle and mips64le executables, decoded by readelf.
// Their addresses and bytes are not fabricated by the implementation.
func TestMIPSELFSingleCFITransition(t *testing.T) {
	for _, arch := range []struct {
		name               string
		width              int
		base, leaf, caller uint64
		file               string
	}{
		{"mips32el", 4, 0x400000, 0xc0910, 0xc0954, "testdata/mips-cfi-oracle/mipsle.debug_frame"},
		{"mips64el", 8, 0x140000000, 0xc0a20, 0xc0a64, "testdata/mips-cfi-oracle/mips64le.debug_frame"},
	} {
		for _, tc := range []struct {
			name, mode string
			caller     bool
			wantCaller bool
		}{
			{"leaf equal SP", "", false, true}, {"stack saved return", "", true, true},
			{"stack memory absent", "missing", true, false}, {"return zero", "zero", true, false},
			{"return repeated", "repeat", true, false}, {"different module identity", "identity", true, false},
			{"wrong ELF class", "class", true, false}, {"wrong ELF architecture", "machine", true, false},
			{"big endian ELF", "endian", true, false}, {"MIPS16 ELF", "mips16", true, false},
			{"microMIPS ELF", "micromips", true, false}, {"decreasing SP", "decreasing", false, false},
		} {
			t.Run(arch.name+"/"+tc.name, func(t *testing.T) {
				var data []byte
				if arch.width == 4 {
					data = buildMIPS32ContextMinidump(600, 1, 0x4000e, 0)
				} else {
					data = buildMIPS64ContextMinidump(640, 0x8004, 0x8000e)
				}
				thread := minidumpStreamRVA(data, streamThreadList)
				context := binary.LittleEndian.Uint32(data[thread+48:])
				stack := binary.LittleEndian.Uint32(data[thread+40:])
				module := minidumpStreamRVA(data, streamModuleList)
				binary.LittleEndian.PutUint32(data[module+12:], 0x100000)
				pc := arch.base + arch.leaf
				if tc.caller {
					pc = arch.base + arch.caller
				}
				if arch.width == 4 {
					binary.LittleEndian.PutUint64(data[context+312:], pc)
				} else {
					binary.LittleEndian.PutUint64(data[context+336:], pc)
				}
				ret := arch.base + arch.leaf + 4
				if !tc.caller {
					ret = arch.base + arch.caller
				}
				if tc.mode == "zero" {
					ret = 0
				}
				if tc.mode == "repeat" {
					ret = pc
				}
				binary.LittleEndian.PutUint64(data[context+8+31*8:], ret)
				if arch.width == 4 {
					binary.LittleEndian.PutUint32(data[stack:], uint32(ret))
				} else {
					binary.LittleEndian.PutUint64(data[stack:], ret)
				}
				if tc.mode == "missing" {
					binary.LittleEndian.PutUint32(data[thread+36:], 0)
				}
				crash, err := ParseMinidump(data)
				if err != nil {
					t.Fatal(err)
				}
				section, err := os.ReadFile(arch.file)
				if err != nil {
					t.Fatal(err)
				}
				if tc.mode == "decreasing" {
					section = buildArchDebugFrame(arch.width, 31, 29, 0, 0x7c, arch.leaf, 0x18)
					pos := bytes.Index(section, []byte{0x0c, 29, 0, 0x05, 31, 1})
					if pos < 0 {
						t.Fatal("no fixture rules")
					}
					copy(section[pos:pos+6], []byte{0x12, 29, 1, 0x08, 31, 0})
				}
				objectWidth := arch.width
				if tc.mode == "class" {
					objectWidth = 12 - arch.width
				}
				machine := elf.EM_MIPS
				if tc.mode == "machine" {
					machine = elf.EM_X86_64
				}
				var object []byte
				if objectWidth == 4 {
					object = buildELF32DebugFrame(machine, section)
				} else {
					object = buildELF64DebugFrame(machine, section)
				}
				if tc.mode == "endian" {
					object[5] = byte(elf.ELFDATA2MSB)
				}
				if tc.mode == "mips16" || tc.mode == "micromips" {
					off := 36
					if objectWidth == 8 {
						off = 48
					}
					flag := uint32(0x04000000)
					if tc.mode == "micromips" {
						flag = 0x02000000
					}
					binary.LittleEndian.PutUint32(object[off:], flag)
				}
				key, table := ParseObjectUnwind("test_app.pdb", object)
				tables := UnwindSet{}
				if table != nil {
					if tc.mode == "identity" {
						key = "different-project-object"
					}
					tables[key] = []*UnwindTable{table}
				}
				frames := UnwindCFI(&crash, tables)
				if tc.wantCaller {
					if len(frames) != 2 || frames[0].Instruction != pc || frames[1].Instruction != ret {
						t.Fatalf("frames=%+v; want captured PC %#x caller %#x", frames, pc, ret)
					}
				} else if len(frames) > 1 {
					t.Fatalf("unexpected caller %+v", frames)
				}
			})
		}
	}
}

// Arithmetic must stay within the captured architecture's address width before
// it is used as a CFA, memory location or return address.
func TestMIPSELFTransitionRejectsAddressOverflow(t *testing.T) {
	for _, arch := range []struct {
		name  string
		width int
		max   uint64
	}{{"mips32el", 4, 0xffffffff}, {"mips64el", 8, ^uint64(0)}} {
		for _, tc := range []struct {
			name, mode string
			sp         uint64
		}{
			{"negative CFA wraps", "cfa negative", 4},
			{"positive CFA wraps", "cfa positive", arch.max - 3},
			{"return offset wraps", "return offset", 8},
			{"return value offset wraps", "return value", 8},
		} {
			t.Run(arch.name+"/"+tc.name, func(t *testing.T) {
				var data []byte
				if arch.width == 4 {
					data = buildMIPS32ContextMinidump(600, 1, 0x4000e, 0)
				} else {
					data = buildMIPS64ContextMinidump(640, 0x8004, 0x8000e)
				}
				thread := minidumpStreamRVA(data, streamThreadList)
				ctx := binary.LittleEndian.Uint32(data[thread+48:])
				binary.LittleEndian.PutUint64(data[ctx+8+29*8:], tc.sp)
				crash, err := ParseMinidump(data)
				if err != nil {
					t.Fatal(err)
				}
				// A wrapped negative address is made readable, so only bounds validation
				// can prevent a plausible but fabricated return PC.
				endRange := make([]byte, arch.width)
				if arch.width == 4 {
					binary.LittleEndian.PutUint32(endRange, 0x401120)
				} else {
					binary.LittleEndian.PutUint64(endRange, 0x140001120)
				}
				crash.stack = append(crash.stack, memoryRange{start: arch.max - 15, data: endRange})
				align := byte(0x7c)
				if arch.width == 8 {
					align = 0x78
				}
				section := buildArchDebugFrame(arch.width, 31, 29, 0, align, 0x1000, 0x100)
				pos := bytes.Index(section, []byte{0x0c, 29, 0, 0x05, 31, 1})
				if pos < 0 {
					t.Fatal("no fixture rules")
				}
				switch tc.mode {
				case "cfa negative":
					copy(section[pos:pos+6], []byte{0x12, 29, 1, 0x08, 31, 0}) // SP + (-width), return same r31
				case "cfa positive":
					copy(section[pos:pos+6], []byte{0x0c, 29, 8, 0x08, 31, 0})
				case "return offset":
					copy(section[pos:pos+6], []byte{0x0c, 29, 0, 0x05, 31, 3})
				case "return value":
					copy(section[pos:pos+6], []byte{0x0c, 29, 0, 0x14, 31, 3})
				}
				var object []byte
				if arch.width == 4 {
					object = buildELF32DebugFrame(elf.EM_MIPS, section)
				} else {
					object = buildELF64DebugFrame(elf.EM_MIPS, section)
				}
				key, table := ParseObjectUnwind("test_app.pdb", object)
				if table == nil {
					t.Fatal("MIPS CFI ignored")
				}
				frames := UnwindCFI(&crash, UnwindSet{key: {table}})
				if len(frames) > 1 {
					t.Fatalf("overflow yielded caller: %+v", frames)
				}
			})
		}
	}
}

func TestMIPSELFTransitionRejectsMissingCFA(t *testing.T) {
	for _, arch := range []struct {
		name  string
		width int
	}{{"mips32el", 4}, {"mips64el", 8}} {
		t.Run(arch.name, func(t *testing.T) {
			var data []byte
			if arch.width == 4 {
				data = buildMIPS32ContextMinidump(600, 1, 0x4000e, 0)
			} else {
				data = buildMIPS64ContextMinidump(640, 0x8004, 0x8000e)
			}
			thread := minidumpStreamRVA(data, streamThreadList)
			ctx := binary.LittleEndian.Uint32(data[thread+48:])
			binary.LittleEndian.PutUint64(data[ctx+8+29*8:], 0)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			section := buildArchDebugFrame(arch.width, 31, 29, 0, 0x7c, 0x1000, 0x100)
			pos := bytes.Index(section, []byte{0x0c, 29, 0, 0x05, 31, 1})
			if pos < 0 {
				t.Fatal("no fixture rules")
			}
			copy(section[pos:pos+6], []byte{0, 0, 0, 0x08, 31, 0}) // no CFA instruction; valid same-value RA
			var object []byte
			if arch.width == 4 {
				object = buildELF32DebugFrame(elf.EM_MIPS, section)
			} else {
				object = buildELF64DebugFrame(elf.EM_MIPS, section)
			}
			key, table := ParseObjectUnwind("test_app.pdb", object)
			if table == nil {
				t.Fatal("MIPS CFI ignored")
			}
			frames := UnwindCFI(&crash, UnwindSet{key: {table}})
			if len(frames) > 1 {
				t.Fatalf("CFA absent but caller emitted: %+v", frames)
			}
		})
	}
}

// Oversized unsigned factors must not wrap into a small valid saved-register
// offset. The positive control uses the same opcode with a factor of zero.
func TestMIPSELFTransitionRejectsOverflowingUnsignedCFIFactors(t *testing.T) {
	for _, arch := range []struct {
		name   string
		width  int
		factor uint64
	}{{"mips32el", 4, 1 << 62}, {"mips64el", 8, 1 << 61}} {
		for _, op := range []struct {
			name     string
			code     byte
			extended bool
		}{{"primary_offset", 0x9f, false}, {"offset_extended", 0x05, true}, {"val_offset", 0x14, true}, {"GNU_negative_offset", 0x2f, true}} {
			for _, overflow := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/overflow_%t", arch.name, op.name, overflow), func(t *testing.T) {
					var data []byte
					if arch.width == 4 {
						data = buildMIPS32ContextMinidump(600, 1, 0x4000e, 0)
					} else {
						data = buildMIPS64ContextMinidump(640, 0x8004, 0x8000e)
					}
					thread := minidumpStreamRVA(data, streamThreadList)
					stack := binary.LittleEndian.Uint32(data[thread+40:])
					ret := uint64(0x401120)
					if arch.width == 8 {
						ret = 0x140001120
					}
					if arch.width == 4 {
						binary.LittleEndian.PutUint32(data[stack:], uint32(ret))
					} else {
						binary.LittleEndian.PutUint64(data[stack:], ret)
					}
					crash, err := ParseMinidump(data)
					if err != nil {
						t.Fatal(err)
					}
					var section bytes.Buffer
					record := func(b []byte) {
						_ = binary.Write(&section, binary.LittleEndian, uint32(len(b)))
						_, _ = section.Write(b)
					}
					align := byte(0x7c)
					if arch.width == 8 {
						align = 0x78
					}
					record([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, align, 31, 0x0c, 29, 0})
					var fde bytes.Buffer
					_ = binary.Write(&fde, binary.LittleEndian, uint32(0))
					if arch.width == 4 {
						_ = binary.Write(&fde, binary.LittleEndian, uint32(0x1000))
						_ = binary.Write(&fde, binary.LittleEndian, uint32(0x100))
					} else {
						_ = binary.Write(&fde, binary.LittleEndian, uint64(0x1000))
						_ = binary.Write(&fde, binary.LittleEndian, uint64(0x100))
					}
					_ = fde.WriteByte(op.code)
					if op.extended {
						_ = fde.WriteByte(31)
					}
					factor := uint64(0)
					if overflow {
						factor = arch.factor
					}
					var encoded [10]byte
					n := binary.PutUvarint(encoded[:], factor)
					_, _ = fde.Write(encoded[:n])
					record(fde.Bytes())
					var object []byte
					if arch.width == 4 {
						object = buildELF32DebugFrame(elf.EM_MIPS, section.Bytes())
					} else {
						object = buildELF64DebugFrame(elf.EM_MIPS, section.Bytes())
					}
					key, table := ParseObjectUnwind("test_app.pdb", object)
					frames := []Frame(nil)
					if table != nil {
						frames = UnwindCFI(&crash, UnwindSet{key: {table}})
					}
					if overflow {
						if len(frames) > 1 {
							t.Fatalf("overflowed factor yielded caller: %+v", frames)
						}
					} else {
						if op.code == 0x14 {
							ret = 0x200000
						}
						if len(frames) != 2 || frames[1].Instruction != ret {
							t.Fatalf("valid zero factor frames=%+v; want caller %#x", frames, ret)
						}
					}
				})
			}
		}
	}
}

func TestMIPSELFTransitionRejectsOversizedUnsignedCFA(t *testing.T) {
	for _, arch := range []struct {
		name  string
		width int
	}{{"mips32el", 4}, {"mips64el", 8}} {
		for _, op := range []byte{0x0c, 0x0e} {
			for _, overflow := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/op_%x/overflow_%t", arch.name, op, overflow), func(t *testing.T) {
					var data []byte
					if arch.width == 4 {
						data = buildMIPS32ContextMinidump(600, 1, 0x4000e, 0)
					} else {
						data = buildMIPS64ContextMinidump(640, 0x8004, 0x8000e)
					}
					thread := minidumpStreamRVA(data, streamThreadList)
					ctx := binary.LittleEndian.Uint32(data[thread+48:])
					binary.LittleEndian.PutUint64(data[ctx+8+29*8:], 8)
					crash, err := ParseMinidump(data)
					if err != nil {
						t.Fatal(err)
					}
					var section bytes.Buffer
					write := func(b []byte) {
						_ = binary.Write(&section, binary.LittleEndian, uint32(len(b)))
						_, _ = section.Write(b)
					}
					write([]byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, 0x7c, 31, 0x0c, 29, 0, 0x08, 31})
					var fde bytes.Buffer
					_ = binary.Write(&fde, binary.LittleEndian, uint32(0))
					if arch.width == 4 {
						_ = binary.Write(&fde, binary.LittleEndian, uint32(0x1000))
						_ = binary.Write(&fde, binary.LittleEndian, uint32(0x100))
					} else {
						_ = binary.Write(&fde, binary.LittleEndian, uint64(0x1000))
						_ = binary.Write(&fde, binary.LittleEndian, uint64(0x100))
					}
					_ = fde.WriteByte(op)
					if op == 0x0c {
						_ = fde.WriteByte(29)
					}
					value := uint64(0)
					if overflow {
						value = ^uint64(0) - 15
					}
					var encoded [10]byte
					n := binary.PutUvarint(encoded[:], value)
					_, _ = fde.Write(encoded[:n])
					write(fde.Bytes())
					var object []byte
					if arch.width == 4 {
						object = buildELF32DebugFrame(elf.EM_MIPS, section.Bytes())
					} else {
						object = buildELF64DebugFrame(elf.EM_MIPS, section.Bytes())
					}
					key, table := ParseObjectUnwind("test_app.pdb", object)
					var frames []Frame
					if table != nil {
						frames = UnwindCFI(&crash, UnwindSet{key: {table}})
					}
					if overflow {
						if len(frames) > 1 {
							t.Fatalf("oversized CFA offset yielded caller: %+v", frames)
						}
					} else if len(frames) != 2 || frames[1].Instruction != crash.LR {
						t.Fatalf("valid CFA offset did not unwind: %+v", frames)
					}
				})
			}
		}
	}
}
