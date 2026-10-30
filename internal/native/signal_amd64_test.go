package native

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// These CFI expressions match Linux/amd64's rt_sigreturn context: saved RBX,
// RSP and RIP are at offsets 128, 160 and 168 from the signal-frame RSP.
func linuxAMD64SignalCFI(signal bool, rspRule string) []byte {
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
		_, _ = record.Write([]byte{1, 0x78, 16, 1, 0})
		return appendRecord(record.Bytes())
	}
	appendFDE := func(cie, start int, instructions []byte) {
		var record bytes.Buffer
		idOffset := section.Len() + 4
		_ = binary.Write(&record, binary.LittleEndian, uint32(idOffset-cie))
		_ = binary.Write(&record, binary.LittleEndian, uint64(start))
		_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		_ = record.WriteByte(0)
		_, _ = record.Write(instructions)
		appendRecord(record.Bytes())
	}
	augmentation := "zR"
	if signal {
		augmentation = "zRS"
	}
	sigCIE := appendCIE(augmentation)
	signalInstructions := []byte{
		0x0f, 4, 0x77, 0xa0, 0x01, 0x06, // CFA = *(RSP + 160)
		0x10, 3, 3, 0x77, 0x80, 0x01, // RBX = *(RSP + 128)
		0x10, 16, 3, 0x77, 0xa8, 0x01, // RIP = *(RSP + 168)
	}
	rspOffset := byte(0xa0) // RSP = *(RSP + 160), as in glibc
	if rspRule == "different" {
		rspOffset = 0x98 // RSP = *(RSP + 152)
	} else if rspRule == "missing" {
		rspOffset = 0xb0 // RSP = *(RSP + 176), outside the captured context
	}
	signalInstructions = append(signalInstructions, 0x10, 7, 3, 0x77, rspOffset, 0x01)
	appendFDE(sigCIE, 0x1000, signalInstructions)
	ordinaryCIE := appendCIE("zR")
	instructions := []byte{0x0c, 3, 8, 0x05, 16, 1} // CFA = RBX + 8; RIP = *(CFA - 8)
	if rspRule == "different" {
		instructions = []byte{0x0c, 7, 8, 0x05, 16, 1} // CFA = RSP + 8
	}
	appendFDE(ordinaryCIE, 0x2000, instructions)
	appendFDE(ordinaryCIE, 0x27ff, []byte{0x0c, 7, 8, 0x05, 16, 1}) // CFA = RSP + 8
	return section.Bytes()
}

func TestLinuxAMD64SignalContextRecoversRegistersAcrossCapturedStacks(t *testing.T) {
	for _, test := range []struct {
		name        string
		signal      bool
		contextBase uint64
		savedSP     uint64
		short       bool
		overlap     bool
		want        []uint64
	}{
		{"alternate stack below interrupted stack", true, 0x4000, 0x8000, false, false, []uint64{0x1001, 0x2000, 0x2800}},
		{"alternate stack above interrupted stack", true, 0x9000, 0x8000, false, false, []uint64{0x1001, 0x2000, 0x2800}},
		{"missing saved return address", true, 0x9000, 0x8000, true, false, []uint64{0x1001}},
		{"missing general register stops later CFI", true, 0x4000, 0x8000, false, false, []uint64{0x1001}},
		{"missing interrupted stack stops backwards switch", true, 0x9000, 0x8000, false, false, []uint64{0x1001}},
		{"unreadable explicit stack rule stops", true, 0x9000, 0x8000, false, false, []uint64{0x1001}},
		{"explicit stack rule differs from CFA", true, 0x9000, 0x8000, false, false, []uint64{0x1001, 0x2000, 0x2800}},
		{"ordinary frame cannot switch backwards", false, 0x9000, 0x8000, false, false, []uint64{0x1001}},
		{"signal cannot move backwards within one range", true, 0x8050, 0x8000, false, false, []uint64{0x1001}},
		{"overlapping captured ranges are not distinct stacks", true, 0x81f0, 0x8000, false, true, []uint64{0x1001}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rspRule := "same"
			if test.name == "unreadable explicit stack rule stops" {
				rspRule = "missing"
			} else if test.name == "explicit stack rule differs from CFA" {
				rspRule = "different"
			}
			table, err := parseCFISection(linuxAMD64SignalCFI(test.signal, rspRule), 0, true, 8)
			if err != nil {
				t.Fatal(err)
			}
			context := make([]byte, 176)
			binary.LittleEndian.PutUint64(context[128:], test.savedSP)
			binary.LittleEndian.PutUint64(context[160:], test.savedSP)
			binary.LittleEndian.PutUint64(context[168:], 0x2000)
			if rspRule == "different" {
				binary.LittleEndian.PutUint64(context[152:], test.savedSP-8)
			}
			if test.short {
				context = context[:168]
			}
			stack := make([]byte, 256)
			binary.LittleEndian.PutUint64(stack, 0x2800)
			ranges := []memoryRange{{start: test.savedSP, data: stack}, {start: test.contextBase, data: context}}
			if rspRule == "different" {
				ranges[0].start = test.savedSP - 8
			}
			if test.name == "missing general register stops later CFI" {
				ranges[1] = memoryRange{start: test.contextBase + 160, data: context[160:]}
			}
			if test.name == "missing interrupted stack stops backwards switch" {
				ranges = ranges[1:]
			}
			if test.name == "signal cannot move backwards within one range" {
				copy(stack[test.contextBase-test.savedSP:], context)
				ranges = ranges[:1]
			}
			if test.overlap {
				stack = make([]byte, 0x1f0)
				binary.LittleEndian.PutUint64(stack, 0x2800)
				contextRange := make([]byte, 16+len(context))
				copy(contextRange[16:], context)
				ranges = []memoryRange{{start: test.savedSP, data: stack}, {start: test.contextBase - 16, data: contextRange}}
			}
			crash := Crash{RIP: 0x1001, RSP: test.contextBase, Arch: "amd64", PointerSize: 8,
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
