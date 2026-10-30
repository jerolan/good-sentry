package native

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Linux i386 vDSO rt_sigreturn CFI, as observed in vdso-cfi.txt: CFA,
// EBX and EIP come from the context at ESP+188, ESP+192 and ESP+216.
func linuxI386SignalCFI(signal bool, espRule string) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) int {
		start := section.Len()
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
		return start
	}
	appendCIE := func(augmentation string) int {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0))
		_ = record.WriteByte(1)
		_, _ = record.WriteString(augmentation + "\x00")
		_, _ = record.Write([]byte{1, 0x7c, 8, 1, 0})
		return appendRecord(record.Bytes())
	}
	appendFDE := func(cie, start int, instructions []byte) {
		var record bytes.Buffer
		idOffset := section.Len() + 4
		_ = binary.Write(&record, binary.LittleEndian, uint32(idOffset-cie))
		_ = binary.Write(&record, binary.LittleEndian, uint32(start))
		_ = binary.Write(&record, binary.LittleEndian, uint32(0x100))
		_ = record.WriteByte(0)
		_, _ = record.Write(instructions)
		appendRecord(record.Bytes())
	}
	augmentation := "zR"
	if signal {
		augmentation = "zRS"
	}
	cie := appendCIE(augmentation)
	instructions := []byte{
		0x0f, 4, 0x74, 0xbc, 0x01, 0x06, // CFA = *(ESP + 188)
		0x10, 3, 3, 0x74, 0xc0, 0x01, // EBX = *(ESP + 192)
		0x10, 8, 3, 0x74, 0xd8, 0x01, // EIP = *(ESP + 216)
	}
	if espRule == "different" {
		instructions = append(instructions, 0x10, 4, 3, 0x74, 0xc8, 0x01) // ESP = *(ESP + 200)
	} else if espRule == "missing" {
		instructions = append(instructions, 0x10, 4, 3, 0x74, 0xdc, 0x01) // outside the capture
	}
	appendFDE(cie, 0x1000, instructions)
	ordinary := appendCIE("zR")
	caller := []byte{0x0c, 3, 4, 0x05, 8, 1} // CFA = EBX + 4; EIP = *(CFA - 4)
	if espRule == "different" {
		caller = []byte{0x0c, 4, 4, 0x05, 8, 1} // caller depends on the explicit ESP
	}
	appendFDE(ordinary, 0x2000, caller)
	appendFDE(ordinary, 0x27ff, []byte{0x0c, 4, 4, 0x05, 8, 1})
	return section.Bytes()
}

func TestLinuxI386SignalContextRecoversRegistersAcrossCapturedStacks(t *testing.T) {
	for _, test := range []struct {
		name                 string
		signal               bool
		contextBase, savedSP uint64
		want                 []uint64
	}{
		{"alternate stack below interrupted stack", true, 0x4000, 0x8000, []uint64{0x1001, 0x2000, 0x2800}},
		{"alternate stack above interrupted stack", true, 0x9000, 0x8000, []uint64{0x1001, 0x2000, 0x2800}},
		{"missing saved return address", true, 0x9000, 0x8000, []uint64{0x1001}},
		{"missing general register stops later CFI", true, 0x4000, 0x8000, []uint64{0x1001}},
		{"missing interrupted stack stops backwards switch", true, 0x9000, 0x8000, []uint64{0x1001}},
		{"unreadable explicit stack rule stops", true, 0x9000, 0x8000, []uint64{0x1001}},
		{"explicit stack rule differs from CFA", true, 0x9000, 0x8000, []uint64{0x1001, 0x2000, 0x2800}},
		{"ordinary frame cannot switch backwards", false, 0x9000, 0x8000, []uint64{0x1001}},
		{"signal cannot move backwards within one range", true, 0x8050, 0x8000, []uint64{0x1001}},
		{"overlapping captured ranges are not distinct stacks", true, 0x81f0, 0x8000, []uint64{0x1001}},
	} {
		t.Run(test.name, func(t *testing.T) {
			espRule := ""
			if test.name == "unreadable explicit stack rule stops" {
				espRule = "missing"
			} else if test.name == "explicit stack rule differs from CFA" {
				espRule = "different"
			}
			table, err := parseCFISection(linuxI386SignalCFI(test.signal, espRule), 0, true, 4)
			if err != nil {
				t.Fatal(err)
			}
			context := make([]byte, 220)
			binary.LittleEndian.PutUint32(context[188:], uint32(test.savedSP))
			binary.LittleEndian.PutUint32(context[192:], uint32(test.savedSP))
			binary.LittleEndian.PutUint32(context[216:], 0x2000)
			if espRule == "different" {
				binary.LittleEndian.PutUint32(context[200:], uint32(test.savedSP-4))
			}
			if test.name == "missing saved return address" {
				context = context[:216]
			}
			stack := make([]byte, 512)
			binary.LittleEndian.PutUint32(stack, 0x2800)
			ranges := []memoryRange{{start: test.savedSP, data: stack}, {start: test.contextBase, data: context}}
			if espRule == "different" {
				ranges[0].start = test.savedSP - 4
			}
			if test.name == "missing general register stops later CFI" {
				// Split the capture so CFA and EIP are present, but EBX is absent.
				ranges = []memoryRange{{start: test.savedSP, data: stack}, {start: test.contextBase + 188, data: context[188:192]}, {start: test.contextBase + 216, data: context[216:]}}
			}
			if test.name == "missing interrupted stack stops backwards switch" {
				ranges = ranges[1:]
			}
			if test.name == "signal cannot move backwards within one range" {
				copy(stack[test.contextBase-test.savedSP:], context)
				ranges = ranges[:1]
			}
			if test.name == "overlapping captured ranges are not distinct stacks" {
				stack = make([]byte, 0x1f0)
				binary.LittleEndian.PutUint32(stack, 0x2800)
				contextRange := make([]byte, 16+len(context))
				copy(contextRange[16:], context)
				ranges = []memoryRange{{start: test.savedSP, data: stack}, {start: test.contextBase - 16, data: contextRange}}
			}
			crash := Crash{RIP: 0x1001, RSP: test.contextBase, Arch: "x86", PointerSize: 4,
				Modules: []Module{{Name: "signal", Size: 0x3000}}, stack: ranges}
			frames := UnwindCFI(&crash, UnwindSet{"signal": {table}})
			if len(frames) != len(test.want) {
				t.Fatalf("frames=%+v; want PCs %x", frames, test.want)
			}
			for i := range frames {
				if frames[i].Instruction != test.want[i] {
					t.Fatalf("frame %d PC=%x; want %x", i, frames[i].Instruction, test.want[i])
				}
			}
		})
	}
}
