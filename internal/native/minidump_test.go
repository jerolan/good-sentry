package native

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func buildAMD64Minidump() []byte {
	return buildAMD64MinidumpWithContext(1232, false)
}

func buildAMD64MinidumpWithContext(contextSize int, xstate bool) []byte {
	const (
		moduleBase = uint64(0x0000000140000000)
		stackBase  = uint64(0x0000000000200000)
	)
	context := make([]byte, contextSize)
	if xstate {
		binary.LittleEndian.PutUint32(context[48:], 0x0010005f)
	}
	binary.LittleEndian.PutUint64(context[152:], stackBase)
	binary.LittleEndian.PutUint64(context[160:], stackBase)
	binary.LittleEndian.PutUint64(context[248:], moduleBase+0x1010)
	stack := make([]byte, 0x100)
	binary.LittleEndian.PutUint64(stack[8:], moduleBase+0x1050)
	system := make([]byte, 2)
	binary.LittleEndian.PutUint16(system, cpuArchAMD64)
	modules := make([]byte, 4+108)
	binary.LittleEndian.PutUint32(modules, 1)
	binary.LittleEndian.PutUint64(modules[4:], moduleBase)
	binary.LittleEndian.PutUint32(modules[12:], 0x2000)
	binary.LittleEndian.PutUint32(modules[20:], 0x1234)
	threads := make([]byte, 4+48)
	binary.LittleEndian.PutUint32(threads, 1)
	binary.LittleEndian.PutUint32(threads[4:], 7)
	exception := make([]byte, 32)
	binary.LittleEndian.PutUint32(exception, 7)
	binary.LittleEndian.PutUint32(exception[8:], 0xC0000005)
	binary.LittleEndian.PutUint64(exception[24:], moduleBase+0x1010)
	streams := [][]byte{system, threads, modules, exception}
	headerSize := 32 + len(streams)*12
	buf := make([]byte, headerSize)
	type location struct{ typ, rva, size uint32 }
	locations := make([]location, 0, len(streams))
	appendData := func(data []byte) uint32 { rva := uint32(len(buf)); buf = append(buf, data...); return rva }
	stackRVA := appendData(stack)
	contextRVA := appendData(context)
	name := encodeTestUTF16("C:\\test\\test_app.pdb")
	nameRVA := appendData(name)
	binary.LittleEndian.PutUint32(modules[4+20:], nameRVA)
	for i, data := range streams {
		if i == 1 {
			binary.LittleEndian.PutUint64(threads[28:], stackBase)
			binary.LittleEndian.PutUint32(threads[36:], uint32(len(stack)))
			binary.LittleEndian.PutUint32(threads[40:], stackRVA)
			binary.LittleEndian.PutUint32(threads[44:], uint32(len(context)))
			binary.LittleEndian.PutUint32(threads[48:], contextRVA)
		}
		locations = append(locations, location{uint32(i + 3), appendData(data), uint32(len(data))})
	}
	// Replace sequential indices with the actual public stream ids.
	locations[0].typ, locations[1].typ, locations[2].typ, locations[3].typ = streamSystemInfo, streamThreadList, streamModuleList, streamException
	binary.LittleEndian.PutUint32(buf[0:], minidumpSignature)
	binary.LittleEndian.PutUint16(buf[4:], minidumpVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(streams)))
	binary.LittleEndian.PutUint32(buf[12:], 32)
	for i, loc := range locations {
		off := 32 + i*12
		binary.LittleEndian.PutUint32(buf[off:], loc.typ)
		binary.LittleEndian.PutUint32(buf[off+4:], loc.size)
		binary.LittleEndian.PutUint32(buf[off+8:], loc.rva)
	}
	return buf
}

func encodeTestUTF16(value string) []byte {
	out := make([]byte, 4+len(value)*2)
	binary.LittleEndian.PutUint32(out, uint32(len(value)*2))
	for i, r := range []rune(value) {
		binary.LittleEndian.PutUint16(out[4+i*2:], uint16(r))
	}
	return out
}

func TestParseMinidumpAndBreakpadSymbols(t *testing.T) {
	crash, err := ParseMinidump(buildAMD64Minidump())
	if err != nil {
		t.Fatal(err)
	}
	if crash.ThreadID != 7 || crash.Code != 0xC0000005 || len(crash.Modules) != 1 || len(crash.Frames) < 2 || crash.Frames[0].Instruction != 0x140001010 || crash.Frames[1].Instruction != 0x140001050 {
		t.Fatalf("parsed crash: %+v", crash)
	}
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 CrashFunction\n1010 20 42 1\n")
	_, functions := ParseBreakpad("test_app.pdb", symbol)
	Symbolicate(&crash, SymbolSet{moduleKey(crash.Modules[0].Name): functions})
	if crash.Frames[0].Function != "CrashFunction" || crash.Frames[0].File != "C:\\src\\crash.cc" || crash.Frames[0].Line != 42 {
		t.Fatalf("symbolication: %+v", crash.Frames[0])
	}
}

func TestParseMinidumpRejectsArchitectureContextMismatches(t *testing.T) {
	tests := []struct {
		name         string
		build        func() []byte
		declaredArch uint16
	}{
		{name: "AMD64 context declared as x86", build: buildAMD64Minidump, declaredArch: 0},
		{name: "x86 context declared as AMD64", build: buildX86Minidump, declaredArch: cpuArchAMD64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.build()
			count := binary.LittleEndian.Uint32(data[8:])
			directory := binary.LittleEndian.Uint32(data[12:])
			for i := uint32(0); i < count; i++ {
				entry := int(directory + i*12)
				if binary.LittleEndian.Uint32(data[entry:]) != streamSystemInfo {
					continue
				}
				systemRVA := binary.LittleEndian.Uint32(data[entry+8:])
				binary.LittleEndian.PutUint16(data[systemRVA:], test.declaredArch)
				if _, err := ParseMinidump(data); err == nil {
					t.Fatalf("accepted context whose declared architecture is %d", test.declaredArch)
				}
				return
			}
			t.Fatal("minidump has no SystemInfo stream")
		})
	}
}

func TestParseAMD64MinidumpXStateExtension(t *testing.T) {
	for _, extensionSize := range []int{144, 16384} {
		t.Run(fmt.Sprintf("extension_%d", extensionSize), func(t *testing.T) {
			data := buildAMD64MinidumpWithContext(1232+extensionSize, true)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			if crash.Arch != "amd64" || crash.RIP != 0x0000000140001010 || len(crash.Frames) < 2 || crash.Frames[1].Instruction != 0x0000000140001050 {
				t.Fatalf("parsed extended AMD64 context: %+v", crash)
			}
		})
	}
}

func TestParseAMD64MinidumpRejectsInvalidXStateExtensions(t *testing.T) {
	tests := []struct {
		name       string
		extension  int
		withXState bool
	}{
		{name: "extension without XSTATE flag", extension: 128},
		{name: "extension exceeds bound", extension: 16385, withXState: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := buildAMD64MinidumpWithContext(1232+test.extension, test.withXState)
			if _, err := ParseMinidump(data); err == nil {
				t.Fatalf("accepted AMD64 XSTATE extension of %d bytes with flag=%v", test.extension, test.withXState)
			}
		})
	}
}

func TestParseBreakpadOptionalMultipleSymbolMarker(t *testing.T) {
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 PreviousFunction\n1010 20 42 1\nFUNC m 1030 20 0 Folded Function Name\n1030 20 17 1\nPUBLIC m 1050 0 Folded Public Name\n1050 1 18 1\nFUNC m 1060 nothex 0 Malformed Function\n1070 1 19 1\n")
	_, functions := ParseBreakpad("test_app.pdb", symbol)
	if len(functions) != 5 {
		t.Fatalf("parsed %d symbol rows, want five valid FUNC/PUBLIC rows: %+v", len(functions), functions)
	}
	byIdentity := make(map[string]SymbolFunction, len(functions))
	for _, function := range functions {
		byIdentity[fmt.Sprintf("%x:%s:%d", function.Start, function.Name, function.Line)] = function
	}
	for _, want := range []struct {
		key  string
		end  uint64
		file string
	}{
		{key: "1010:PreviousFunction:-1", end: 0x1030},
		{key: "1010:PreviousFunction:42", end: 0x1030, file: `C:\src\crash.cc`},
		{key: "1030:Folded Function Name:-1", end: 0x1050},
		{key: "1030:Folded Function Name:17", end: 0x1050, file: `C:\src\crash.cc`},
		{key: "1050:Folded Public Name:-1", end: ^uint64(0)},
	} {
		function, ok := byIdentity[want.key]
		if !ok || function.End != want.end || function.File != want.file {
			t.Errorf("symbol %q: got %+v, present=%t; want end=%#x file=%q", want.key, function, ok, want.end, want.file)
		}
	}
	for _, leaked := range []string{
		"1030:PreviousFunction:17",
		"1050:PreviousFunction:18",
		"1070:PreviousFunction:19",
	} {
		if function, ok := byIdentity[leaked]; ok {
			t.Errorf("orphan source row leaked onto the previous function: %+v", function)
		}
	}
}

func TestBreakpadPublicSymbolsInferModuleBoundedRanges(t *testing.T) {
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nPUBLIC m 1000 0 First Public\nPUBLIC 1000 0 Duplicate Public\nPUBLIC m 1010 0 Next Public\nFUNC 1020 20 0 Exact Function\nPUBLIC 1025 0 Public Without Source\nFILE 1 C:\\src\\crash.cc\nFUNC 1040 20 0 Exact Function With Source\n1040 20 42 1\nPUBLIC 1045 0 Public With Source\nPUBLIC m 1060 0 Final Public\n")
	_, functions := ParseBreakpad("test_app.pdb", symbol)
	module := Module{Name: "test_app.pdb", Base: 0x100000, Size: 0x2000}
	offsets := []uint64{0x1000, 0x100f, 0x1010, 0x101f, 0x1020, 0x1025, 0x103f, 0x1040, 0x105f, 0x1060, 0x1fff, 0x2000}
	want := []struct {
		function string
		file     string
		line     int
	}{
		{function: "First Public", line: -1},
		{function: "First Public", line: -1},
		{function: "Next Public", line: -1},
		{function: "Next Public", line: -1},
		{function: "Exact Function", line: -1},
		{function: "Exact Function", line: -1},
		{function: "Exact Function", line: -1},
		{function: "Exact Function With Source", file: `C:\src\crash.cc`, line: 42},
		{function: "Exact Function With Source", file: `C:\src\crash.cc`, line: 42},
		{function: "Final Public", line: -1},
		{function: "Final Public", line: -1},
		{},
	}
	crash := Crash{Modules: []Module{module}}
	for _, offset := range offsets {
		crash.Frames = append(crash.Frames, Frame{Instruction: module.Base + offset, Module: module.Name})
	}
	Symbolicate(&crash, SymbolSet{moduleKey(module.Name): functions})
	for i, expected := range want {
		frame := crash.Frames[i]
		if frame.Function != expected.function || frame.File != expected.file || frame.Line != expected.line {
			t.Errorf("frame at module offset %#x: got function=%q file=%q line=%d, want %+v", offsets[i], frame.Function, frame.File, frame.Line, expected)
		}
	}
}

func buildX86Minidump() []byte {
	return buildX86MinidumpWithContext(716, false)
}

func buildX86MinidumpWithContext(contextSize int, xstate bool) []byte {
	const moduleBase = uint32(0x00400000)
	const stackBase = uint32(0x00200000)
	context := make([]byte, contextSize)
	if xstate {
		binary.LittleEndian.PutUint32(context, 0x0001005f)
	}
	binary.LittleEndian.PutUint32(context[180:], stackBase)
	binary.LittleEndian.PutUint32(context[184:], moduleBase+0x1010)
	binary.LittleEndian.PutUint32(context[196:], stackBase)
	stack := make([]byte, 0x100)
	binary.LittleEndian.PutUint32(stack[4:], moduleBase+0x1050)
	system := make([]byte, 2)
	binary.LittleEndian.PutUint16(system, 0)
	modules := make([]byte, 4+108)
	binary.LittleEndian.PutUint32(modules, 1)
	binary.LittleEndian.PutUint64(modules[4:], uint64(moduleBase))
	binary.LittleEndian.PutUint32(modules[12:], 0x2000)
	threads := make([]byte, 4+48)
	binary.LittleEndian.PutUint32(threads, 1)
	binary.LittleEndian.PutUint32(threads[4:], 3)
	exception := make([]byte, 32)
	binary.LittleEndian.PutUint32(exception, 3)
	binary.LittleEndian.PutUint32(exception[8:], 0xC000001D)
	binary.LittleEndian.PutUint64(exception[24:], uint64(moduleBase+0x1010))
	streams := [][]byte{system, threads, modules, exception}
	headerSize := 32 + len(streams)*12
	buf := make([]byte, headerSize)
	type location struct{ typ, rva, size uint32 }
	locations := make([]location, 0, len(streams))
	appendData := func(data []byte) uint32 { rva := uint32(len(buf)); buf = append(buf, data...); return rva }
	stackRVA, contextRVA := appendData(stack), appendData(context)
	nameRVA := appendData(encodeTestUTF16("C:\\test\\test_app32.pdb"))
	binary.LittleEndian.PutUint32(modules[4+20:], nameRVA)
	binary.LittleEndian.PutUint64(threads[28:], uint64(stackBase))
	binary.LittleEndian.PutUint32(threads[36:], uint32(len(stack)))
	binary.LittleEndian.PutUint32(threads[40:], stackRVA)
	binary.LittleEndian.PutUint32(threads[44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(threads[48:], contextRVA)
	types := []uint32{streamSystemInfo, streamThreadList, streamModuleList, streamException}
	for i, data := range streams {
		locations = append(locations, location{types[i], appendData(data), uint32(len(data))})
	}
	binary.LittleEndian.PutUint32(buf, minidumpSignature)
	binary.LittleEndian.PutUint16(buf[4:], minidumpVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(streams)))
	binary.LittleEndian.PutUint32(buf[12:], 32)
	for i, loc := range locations {
		off := 32 + i*12
		binary.LittleEndian.PutUint32(buf[off:], loc.typ)
		binary.LittleEndian.PutUint32(buf[off+4:], loc.size)
		binary.LittleEndian.PutUint32(buf[off+8:], loc.rva)
	}
	return buf
}

func TestParseX86MinidumpFromContextShape(t *testing.T) {
	crash, err := ParseMinidump(buildX86Minidump())
	if err != nil {
		t.Fatal(err)
	}
	if crash.Arch != "x86" || crash.PointerSize != 4 || crash.ThreadID != 3 || crash.RIP != 0x00401010 || len(crash.Frames) != 2 || crash.Frames[1].Instruction != 0x00401050 {
		t.Fatalf("x86 crash: %+v", crash)
	}
}

func TestParseX86MinidumpXStateExtension(t *testing.T) {
	for _, extensionSize := range []int{144, 16384} {
		t.Run(fmt.Sprintf("extension_%d", extensionSize), func(t *testing.T) {
			data := buildX86MinidumpWithContext(716+extensionSize, true)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			if crash.Arch != "x86" || crash.RIP != 0x00401010 || len(crash.Frames) < 2 || crash.Frames[1].Instruction != 0x00401050 {
				t.Fatalf("parsed extended x86 context: %+v", crash)
			}
		})
	}
}

func TestParseX86MinidumpRejectsInvalidXStateExtensions(t *testing.T) {
	tests := []struct {
		name       string
		extension  int
		withXState bool
		wrongArch  bool
	}{
		{name: "extension without XSTATE flag", extension: 144},
		{name: "extension exceeds bound", extension: 16385, withXState: true},
		{name: "context architecture mismatch", extension: 144, withXState: true, wrongArch: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := buildX86MinidumpWithContext(716+test.extension, test.withXState)
			if test.wrongArch && !setMinidumpSystemInfoArchitecture(data, cpuArchAMD64) {
				t.Fatal("minidump has no SystemInfo stream")
			}
			if _, err := ParseMinidump(data); err == nil {
				t.Fatalf("accepted x86 XSTATE extension of %d bytes with flag=%v and wrongArch=%v", test.extension, test.withXState, test.wrongArch)
			}
		})
	}
}

func setMinidumpSystemInfoArchitecture(data []byte, architecture uint16) bool {
	count := binary.LittleEndian.Uint32(data[8:])
	directory := binary.LittleEndian.Uint32(data[12:])
	for i := uint32(0); i < count; i++ {
		entry := int(directory + i*12)
		if binary.LittleEndian.Uint32(data[entry:]) == streamSystemInfo {
			systemRVA := binary.LittleEndian.Uint32(data[entry+8:])
			binary.LittleEndian.PutUint16(data[systemRVA:], architecture)
			return true
		}
	}
	return false
}

func buildARM32Minidump(contextSize int) []byte {
	data := buildX86Minidump()
	systemRVA := minidumpStreamRVA(data, streamSystemInfo)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	binary.LittleEndian.PutUint16(data[systemRVA:], cpuArchARM)
	context := make([]byte, contextSize)
	if contextSize >= 68 {
		for register := uint32(0); register < 16; register++ {
			binary.LittleEndian.PutUint32(context[4+4*register:], 0x1000+register)
		}
		binary.LittleEndian.PutUint32(context[48:], 0x00200000) // r11, frame pointer
		binary.LittleEndian.PutUint32(context[56:], 0x00200000) // r13, stack pointer
		binary.LittleEndian.PutUint32(context[60:], 0x00401050) // r14, link register
		binary.LittleEndian.PutUint32(context[64:], 0x00401010) // r15, program counter
	}
	contextRVA := uint32(len(data))
	data = append(data, context...)
	binary.LittleEndian.PutUint32(data[threadRVA+44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(data[threadRVA+48:], contextRVA)
	return data
}

func minidumpStreamRVA(data []byte, streamType uint32) uint32 {
	count := binary.LittleEndian.Uint32(data[8:])
	directory := binary.LittleEndian.Uint32(data[12:])
	for i := uint32(0); i < count; i++ {
		entry := directory + i*12
		if binary.LittleEndian.Uint32(data[entry:]) == streamType {
			return binary.LittleEndian.Uint32(data[entry+8:])
		}
	}
	return 0
}

func TestParseARM32MinidumpContext(t *testing.T) {
	crash, err := ParseMinidump(buildARM32Minidump(368))
	if err != nil {
		t.Fatal(err)
	}
	if crash.Arch != "arm" || crash.PointerSize != 4 || crash.RIP != 0x00401010 || crash.RSP != 0x00200000 || crash.RBP != 0x00200000 || crash.LR != 0x00401050 {
		t.Fatalf("ARM32 crash context: %+v", crash)
	}
	if len(crash.Frames) != 2 || crash.Frames[1].Instruction != 0x00401050 {
		t.Fatalf("ARM32 frame-pointer chain: %+v", crash.Frames)
	}
	for register := uint32(0); register < 11; register++ {
		if got, want := crash.registers[register], uint64(0x1000+register); got != want {
			t.Fatalf("ARM32 r%d = %#x; want %#x", register, got, want)
		}
	}
	for register, want := range map[uint32]uint64{11: 0x00200000, 12: 0x100c, 13: 0x00200000, 14: 0x00401050, 15: 0x00401010} {
		if got := crash.registers[register]; got != want {
			t.Fatalf("ARM32 r%d = %#x; want %#x", register, got, want)
		}
	}
}

func TestARM32CFIUnwindsUsingRestoredRegisters(t *testing.T) {
	data := buildARM32Minidump(368)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	stackRVA := binary.LittleEndian.Uint32(data[threadRVA+40:])
	binary.LittleEndian.PutUint32(data[stackRVA:], 0x00200020)    // caller r4
	binary.LittleEndian.PutUint32(data[stackRVA+4:], 0x00401100)  // first return address
	binary.LittleEndian.PutUint32(data[stackRVA+28:], 0x00401200) // second return address
	binary.LittleEndian.PutUint32(data[stackRVA+32:], 0)          // end of captured chain
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name          string
		savedR4Offset int64
		want          []uint64
	}{
		{name: "restore callee-saved r4", savedR4Offset: -8, want: []uint64{0x00401010, 0x00401100, 0x00401200}},
		{name: "unavailable saved r4 stops", savedR4Offset: -256, want: []uint64{0x00401010}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, table := ParseObjectUnwind("test_app32.pdb", buildARM32ELFWithCFI(test.savedR4Offset))
			if table == nil {
				t.Fatal("ARM32 ELF CFI was not parsed")
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
			if len(frames) != len(test.want) {
				t.Fatalf("ARM32 CFI frames = %+v; want instruction addresses %x", frames, test.want)
			}
			for i := range test.want {
				if frames[i].Instruction != test.want[i] {
					t.Fatalf("ARM32 CFI frame %d = %#x; want %#x", i, frames[i].Instruction, test.want[i])
				}
			}
		})
	}
}

func TestARM32EHABIUnwindsInlineAndExtabTables(t *testing.T) {
	for _, test := range []struct {
		name   string
		object []byte
	}{
		{name: "inline compact entry", object: buildARM32EHABIELF(false, 0x808400b0)},
		{name: "extab long entry", object: buildARM32EHABIELF(true, 0x81008400)},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := buildARM32Minidump(368)
			threadRVA := minidumpStreamRVA(data, streamThreadList)
			stackRVA := binary.LittleEndian.Uint32(data[threadRVA+40:])
			binary.LittleEndian.PutUint32(data[stackRVA:], 0x00401104)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			_, table := ParseObjectUnwind("arm32-ehabi.so", test.object)
			if table == nil {
				t.Fatal("ARM32 EHABI table was not parsed")
			}
			entry, ok := table.arm32EHABIAt(0x1010)
			if !ok {
				t.Fatal("ARM32 EHABI function entry was not found")
			}
			restored, callerSP, returnAddress, ok := unwindARM32EHABI(entry.program, crash.registers, crash.stack)
			if !ok || callerSP != 0x00200004 || returnAddress != 0x00401104 || restored[14] != returnAddress || restored[15] != returnAddress {
				t.Fatalf("ARM32 EHABI caller state = regs[%d]=%#x regs[%d]=%#x SP=%#x PC=%#x ok=%v", 14, restored[14], 15, restored[15], callerSP, returnAddress, ok)
			}
			frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
			want := []uint64{0x00401010, 0x00401104}
			if len(frames) != len(want) {
				t.Fatalf("ARM32 EHABI frames = %+v; want instruction addresses %x", frames, want)
			}
			for i := range want {
				if frames[i].Instruction != want[i] {
					t.Fatalf("ARM32 EHABI frame %d = %#x; want %#x", i, frames[i].Instruction, want[i])
				}
			}
		})
	}
}

func TestARM32EHABIFailsClosed(t *testing.T) {
	t.Run("unavailable restore memory", func(t *testing.T) {
		data := buildARM32Minidump(368)
		crash, err := ParseMinidump(data)
		if err != nil {
			t.Fatal(err)
		}
		crash.RSP = 0x00300000
		crash.registers[13] = crash.RSP
		_, table := ParseObjectUnwind("arm32-ehabi.so", buildARM32EHABIELF(false, 0x808400b0))
		frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
		if len(frames) != 1 || frames[0].Instruction != crash.RIP {
			t.Fatalf("unavailable EHABI restore produced frames %+v", frames)
		}
	})

	t.Run("malformed index section", func(t *testing.T) {
		object := buildARM32EHABIELF(false, 0x808400b0)
		file, err := elf.NewFile(bytes.NewReader(object))
		if err != nil {
			t.Fatal(err)
		}
		index := file.Section(".ARM.exidx")
		if index == nil {
			t.Fatal("fixture has no .ARM.exidx section")
		}
		sectionHeaders := binary.LittleEndian.Uint32(object[32:])
		binary.LittleEndian.PutUint32(object[int(sectionHeaders)+2*40+20:], 7)
		if _, table := ParseObjectUnwind("arm32-ehabi.so", object); table != nil {
			t.Fatal("truncated .ARM.exidx section produced an unwind table")
		}
	})

	t.Run("unsupported instruction", func(t *testing.T) {
		data := buildARM32Minidump(368)
		crash, err := ParseMinidump(data)
		if err != nil {
			t.Fatal(err)
		}
		_, table := ParseObjectUnwind("arm32-ehabi.so", buildARM32EHABIELF(false, 0x80b30000))
		if table == nil {
			t.Fatal("valid EHABI table was not parsed")
		}
		frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
		if len(frames) != 1 || frames[0].Instruction != crash.RIP {
			t.Fatalf("unsupported EHABI instruction produced frames %+v", frames)
		}
	})

	t.Run("function prel31 outside executable section", func(t *testing.T) {
		object := buildARM32EHABIELF(false, 0x808400b0)
		file, err := elf.NewFile(bytes.NewReader(object))
		if err != nil {
			t.Fatal(err)
		}
		index := file.Section(".ARM.exidx")
		binary.LittleEndian.PutUint32(object[index.Offset:], arm32TestPrel31(index.Addr, 0x5000))
		if _, table := ParseObjectUnwind("arm32-ehabi.so", object); table != nil {
			t.Fatal("out-of-range function prel31 produced an unwind table")
		}
	})

	t.Run("unsorted function entries", func(t *testing.T) {
		object := buildARM32EHABIELF(false, 0x808400b0)
		file, err := elf.NewFile(bytes.NewReader(object))
		if err != nil {
			t.Fatal(err)
		}
		index := file.Section(".ARM.exidx")
		binary.LittleEndian.PutUint32(object[index.Offset+8:], arm32TestPrel31(index.Addr+8, 0x1000))
		if _, table := ParseObjectUnwind("arm32-ehabi.so", object); table != nil {
			t.Fatal("unsorted .ARM.exidx entries produced an unwind table")
		}
	})

	t.Run("function prel31 high bit", func(t *testing.T) {
		object := buildARM32EHABIELF(false, 0x808400b0)
		file, err := elf.NewFile(bytes.NewReader(object))
		if err != nil {
			t.Fatal(err)
		}
		index := file.Section(".ARM.exidx")
		binary.LittleEndian.PutUint32(object[index.Offset:], binary.LittleEndian.Uint32(object[index.Offset:])|0x80000000)
		if _, table := ParseObjectUnwind("arm32-ehabi.so", object); table != nil {
			t.Fatal("invalid high-bit function prel31 produced an unwind table")
		}
	})

	t.Run("generic personality", func(t *testing.T) {
		object := buildARM32EHABIELF(true, 0x00008400)
		if _, table := ParseObjectUnwind("arm32-ehabi.so", object); table != nil {
			t.Fatal("generic personality produced an unwind table")
		}
	})

	t.Run("reserved compact personality", func(t *testing.T) {
		object := buildARM32EHABIELF(true, 0x83840000)
		if _, table := ParseObjectUnwind("arm32-ehabi.so", object); table != nil {
			t.Fatal("reserved compact personality produced an unwind table")
		}
	})

	t.Run("overlong ULEB operand", func(t *testing.T) {
		data := buildARM32Minidump(368)
		crash, err := ParseMinidump(data)
		if err != nil {
			t.Fatal(err)
		}
		object := buildARM32EHABIELF(true, 0x810200b2, 0x80808080, 0x80000000)
		_, table := ParseObjectUnwind("arm32-ehabi.so", object)
		if table == nil {
			t.Fatal("valid compact table was not parsed")
		}
		frames := UnwindCFI(&crash, UnwindSet{"test_app32.pdb": {table}})
		if len(frames) != 1 || frames[0].Instruction != crash.RIP {
			t.Fatalf("overlong ULEB operand produced frames %+v", frames)
		}
	})
}

func TestARM32EHABIVSPAndCoreRegisterOperations(t *testing.T) {
	for _, test := range []struct {
		name         string
		program      []byte
		stackPointer uint64
		framePointer uint64
		memoryOffset uint64
		stackValues  []uint32
		wantNextSP   uint64
		wantR0       uint64
		wantR4       uint64
		wantReturnPC uint64
	}{
		{name: "positive vsp adjustment", program: []byte{0x01, 0x84, 0x00, 0xb0}, stackPointer: 0x00200000, memoryOffset: 8, stackValues: []uint32{0x00401104}, wantNextSP: 0x0020000c, wantReturnPC: 0x00401104},
		{name: "negative vsp adjustment", program: []byte{0x41, 0x84, 0x00, 0xb0}, stackPointer: 0x00200010, memoryOffset: 8, stackValues: []uint32{0x00401108}, wantNextSP: 0x0020000c, wantReturnPC: 0x00401108},
		{name: "vsp from r4", program: []byte{0x94, 0x84, 0x00, 0xb0}, stackPointer: 0x00200000, framePointer: 0x00200008, memoryOffset: 8, stackValues: []uint32{0x0040110c}, wantNextSP: 0x0020000c, wantR4: 0x00200008, wantReturnPC: 0x0040110c},
		{name: "pop r4 and lr", program: []byte{0x84, 0x01, 0xb0}, stackPointer: 0x00200000, stackValues: []uint32{0x44556677, 0x00401110}, wantNextSP: 0x00200008, wantR4: 0x44556677, wantReturnPC: 0x00401110},
		{name: "pop low register", program: []byte{0xb1, 0x01, 0xb0}, stackPointer: 0x00200000, stackValues: []uint32{0x11223344}, wantNextSP: 0x00200004, wantR0: 0x11223344, wantReturnPC: 0x00401050},
		{name: "large vsp adjustment", program: []byte{0xb2, 0x00, 0x84, 0x00, 0xb0}, stackPointer: 0x00200000, memoryOffset: 0x204, stackValues: []uint32{0x00401114}, wantNextSP: 0x00200208, wantReturnPC: 0x00401114},
	} {
		t.Run(test.name, func(t *testing.T) {
			registers := map[uint32]uint64{13: test.stackPointer, 14: 0x00401050}
			if test.framePointer != 0 {
				registers[4] = test.framePointer
			}
			memory := memoryRange{start: 0x00200000, data: make([]byte, 0x500)}
			for i, value := range test.stackValues {
				binary.LittleEndian.PutUint32(memory.data[int(test.memoryOffset)+i*4:], value)
			}
			next, nextSP, returnPC, ok := unwindARM32EHABI(test.program, registers, []memoryRange{memory})
			if !ok || nextSP != test.wantNextSP || next[13] != test.wantNextSP || returnPC != test.wantReturnPC || next[15] != test.wantReturnPC || next[0] != test.wantR0 || next[4] != test.wantR4 {
				t.Fatalf("EHABI state: r0=%#x r4=%#x SP=%#x PC=%#x ok=%v", next[0], next[4], nextSP, returnPC, ok)
			}
		})
	}
}

func buildARM32EHABIELF(useExtab bool, firstEntry uint32, extabTail ...uint32) []byte {
	const (
		textAddress  = uint32(0x1000)
		textSize     = uint32(0x300)
		exidxAddress = uint32(0x2000)
		extabAddress = uint32(0x3000)
	)
	sectionNames := []byte("\x00.text\x00.ARM.exidx\x00.ARM.extab\x00.shstrtab\x00")
	nameOffset := func(name string) uint32 {
		return uint32(bytes.Index(sectionNames, append([]byte(name), 0)))
	}
	textData := make([]byte, textSize)
	indexData := make([]byte, 16)
	binary.LittleEndian.PutUint32(indexData, arm32TestPrel31(uint64(exidxAddress), uint64(textAddress)))
	if useExtab {
		binary.LittleEndian.PutUint32(indexData[4:], arm32TestPrel31(uint64(exidxAddress+4), uint64(extabAddress)))
	} else {
		binary.LittleEndian.PutUint32(indexData[4:], firstEntry)
	}
	binary.LittleEndian.PutUint32(indexData[8:], arm32TestPrel31(uint64(exidxAddress+8), uint64(textAddress+0x100)))
	binary.LittleEndian.PutUint32(indexData[12:], 1) // EXIDX_CANTUNWIND
	var extabData []byte
	if useExtab {
		extabData = make([]byte, 4+4*len(extabTail))
		binary.LittleEndian.PutUint32(extabData, firstEntry)
		for i, word := range extabTail {
			binary.LittleEndian.PutUint32(extabData[4+i*4:], word)
		}
	}

	const (
		elfHeaderSize     = 52
		sectionHeaderSize = 40
	)
	shstrOffset := elfHeaderSize
	textOffset := (shstrOffset + len(sectionNames) + 3) &^ 3
	indexOffset := textOffset + len(textData)
	extabOffset := indexOffset + len(indexData)
	sectionTableOffset := (extabOffset + len(extabData) + 3) &^ 3
	data := make([]byte, sectionTableOffset+5*sectionHeaderSize)
	copy(data[shstrOffset:], sectionNames)
	copy(data[textOffset:], textData)
	copy(data[indexOffset:], indexData)
	copy(data[extabOffset:], extabData)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS32), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_ARM))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[32:], uint32(sectionTableOffset))
	binary.LittleEndian.PutUint16(data[40:], elfHeaderSize)
	binary.LittleEndian.PutUint16(data[46:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[48:], 5)
	binary.LittleEndian.PutUint16(data[50:], 4)
	writeSection := func(index int, name, kind, flags, address, offset, size, link, alignment, entrySize uint32) {
		start := sectionTableOffset + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], name)
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint32(data[start+8:], flags)
		binary.LittleEndian.PutUint32(data[start+12:], address)
		binary.LittleEndian.PutUint32(data[start+16:], offset)
		binary.LittleEndian.PutUint32(data[start+20:], size)
		binary.LittleEndian.PutUint32(data[start+24:], link)
		binary.LittleEndian.PutUint32(data[start+32:], alignment)
		binary.LittleEndian.PutUint32(data[start+36:], entrySize)
	}
	writeSection(1, nameOffset(".text"), uint32(elf.SHT_PROGBITS), uint32(elf.SHF_ALLOC|elf.SHF_EXECINSTR), textAddress, uint32(textOffset), uint32(len(textData)), 0, 4, 0)
	writeSection(2, nameOffset(".ARM.exidx"), 0x70000001, uint32(elf.SHF_ALLOC)|0x80, exidxAddress, uint32(indexOffset), uint32(len(indexData)), 1, 4, 8)
	writeSection(3, nameOffset(".ARM.extab"), uint32(elf.SHT_PROGBITS), uint32(elf.SHF_ALLOC), extabAddress, uint32(extabOffset), uint32(len(extabData)), 0, 4, 0)
	writeSection(4, nameOffset(".shstrtab"), uint32(elf.SHT_STRTAB), 0, 0, uint32(shstrOffset), uint32(len(sectionNames)), 0, 1, 0)
	return data
}

func arm32TestPrel31(place, target uint64) uint32 {
	return uint32(int64(target)-int64(place)) & 0x7fffffff
}

func buildARM32ELFWithCFI(savedR4Offset int64) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) int {
		start := section.Len()
		var length [4]byte
		binary.LittleEndian.PutUint32(length[:], uint32(len(record)))
		section.Write(length[:])
		section.Write(record)
		return start
	}
	appendCIE := func(cfaRegister, cfaOffset byte, r4OffsetFactor byte) int {
		var record bytes.Buffer
		var cieID [4]byte
		binary.LittleEndian.PutUint32(cieID[:], 0xffffffff)
		record.Write(cieID[:])
		record.Write([]byte{1, 0, 1, 0x7c, 14}) // version, augmentation, code/data alignment, return register
		record.Write([]byte{0x0c, cfaRegister, cfaOffset})
		record.Write([]byte{0x05, 14, 1}) // r14 is saved at CFA-4
		if r4OffsetFactor != 0 {
			record.Write([]byte{0x05, 4, r4OffsetFactor})
		}
		return appendRecord(record.Bytes())
	}
	appendFDE := func(cieOffset int, start uint32) {
		var record bytes.Buffer
		var field [4]byte
		for _, value := range []uint32{uint32(cieOffset), start, 0x20} {
			binary.LittleEndian.PutUint32(field[:], value)
			record.Write(field[:])
		}
		appendRecord(record.Bytes())
	}
	first := appendCIE(13, 8, byte(-savedR4Offset/4))
	appendFDE(first, 0x1000)
	second := appendCIE(4, 0, 0) // r4 defaults to same-value under the ARM ABI
	appendFDE(second, 0x10f0)
	third := appendCIE(13, 4, 0)
	appendFDE(third, 0x11f0)
	return buildARM32ELFWithSection(section.Bytes())
}

func buildARM32ELFWithSection(sectionData []byte) []byte {
	const (
		elfHeaderSize     = 52
		sectionHeaderSize = 40
	)
	sectionNames := []byte("\x00.shstrtab\x00.debug_frame\x00")
	sectionNamesOffset := elfHeaderSize
	sectionDataOffset := (sectionNamesOffset + len(sectionNames) + 3) &^ 3
	sectionTableOffset := (sectionDataOffset + len(sectionData) + 3) &^ 3
	data := make([]byte, sectionTableOffset+3*sectionHeaderSize)
	copy(data[sectionNamesOffset:], sectionNames)
	copy(data[sectionDataOffset:], sectionData)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS32), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_ARM))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[32:], uint32(sectionTableOffset))
	binary.LittleEndian.PutUint16(data[40:], elfHeaderSize)
	binary.LittleEndian.PutUint16(data[46:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[48:], 3)
	binary.LittleEndian.PutUint16(data[50:], 1)
	writeSection := func(index int, name, kind, offset, size, alignment uint32) {
		start := sectionTableOffset + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], name)
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint32(data[start+16:], offset)
		binary.LittleEndian.PutUint32(data[start+20:], size)
		binary.LittleEndian.PutUint32(data[start+32:], alignment)
	}
	writeSection(1, 1, uint32(elf.SHT_STRTAB), uint32(sectionNamesOffset), uint32(len(sectionNames)), 1)
	writeSection(2, 11, uint32(elf.SHT_PROGBITS), uint32(sectionDataOffset), uint32(len(sectionData)), 4)
	return data
}

func TestParseARM32MinidumpUsesLinkRegisterWithoutFramePointer(t *testing.T) {
	data := buildARM32Minidump(368)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	stackRVA := binary.LittleEndian.Uint32(data[threadRVA+40:])
	binary.LittleEndian.PutUint32(data[stackRVA:], 0x00401060)
	contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
	binary.LittleEndian.PutUint32(data[contextRVA+48:], 0)
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.Frames) != 2 || crash.Frames[1].Instruction != crash.LR {
		t.Fatalf("ARM32 link-register fallback: %+v, LR %#x", crash.Frames, crash.LR)
	}
}

func TestParseARM32MinidumpDoesNotGuessFromStackPointer(t *testing.T) {
	data := buildARM32Minidump(368)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	stackRVA := binary.LittleEndian.Uint32(data[threadRVA+40:])
	binary.LittleEndian.PutUint32(data[stackRVA:], 0x00401060)
	contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
	binary.LittleEndian.PutUint32(data[contextRVA+48:], 0)
	binary.LittleEndian.PutUint32(data[contextRVA+60:], 0)
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.Frames) != 1 {
		t.Fatalf("ARM32 stack-pointer guess: %+v", crash.Frames)
	}
}

func TestParseARM32MinidumpRejectsUnsupportedContextShape(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "truncated context", data: buildARM32Minidump(360)},
		{name: "unpadded context", data: buildARM32Minidump(364)},
		{name: "architecture mismatch", data: func() []byte {
			data := buildARM32Minidump(368)
			systemRVA := minidumpStreamRVA(data, streamSystemInfo)
			binary.LittleEndian.PutUint16(data[systemRVA:], cpuArchAMD64)
			return data
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseMinidump(test.data); err == nil {
				t.Fatal("unsupported ARM32 context was accepted")
			}
		})
	}
}

func TestParseARM64Minidump(t *testing.T) {
	const moduleBase = uint64(0x0000000100000000)
	const stackBase = uint64(0x000000016f000000)
	context := make([]byte, 912)
	binary.LittleEndian.PutUint64(context[240:], stackBase)
	binary.LittleEndian.PutUint64(context[264:], moduleBase+0x1010)
	binary.LittleEndian.PutUint64(context[256:], stackBase)
	stack := make([]byte, 0x100)
	binary.LittleEndian.PutUint64(stack[8:], moduleBase+0x1050)
	system := make([]byte, 2)
	binary.LittleEndian.PutUint16(system, 12)
	modules := make([]byte, 4+108)
	binary.LittleEndian.PutUint32(modules, 1)
	binary.LittleEndian.PutUint64(modules[4:], moduleBase)
	binary.LittleEndian.PutUint32(modules[12:], 0x2000)
	threads := make([]byte, 4+48)
	binary.LittleEndian.PutUint32(threads, 1)
	binary.LittleEndian.PutUint32(threads[4:], 9)
	exception := make([]byte, 32)
	binary.LittleEndian.PutUint32(exception, 9)
	binary.LittleEndian.PutUint32(exception[8:], 0xC0000005)
	binary.LittleEndian.PutUint64(exception[24:], moduleBase+0x1010)
	data := [][]byte{system, threads, modules, exception}
	types := []uint32{streamSystemInfo, streamThreadList, streamModuleList, streamException}
	buf := make([]byte, 32+len(data)*12)
	appendData := func(value []byte) uint32 { rva := uint32(len(buf)); buf = append(buf, value...); return rva }
	stackRVA, contextRVA := appendData(stack), appendData(context)
	nameRVA := appendData(encodeTestUTF16("C:\\test\\test_arm64.pdb"))
	binary.LittleEndian.PutUint32(modules[4+20:], nameRVA)
	binary.LittleEndian.PutUint64(threads[28:], stackBase)
	binary.LittleEndian.PutUint32(threads[36:], uint32(len(stack)))
	binary.LittleEndian.PutUint32(threads[40:], stackRVA)
	binary.LittleEndian.PutUint32(threads[44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(threads[48:], contextRVA)
	binary.LittleEndian.PutUint32(buf, minidumpSignature)
	binary.LittleEndian.PutUint16(buf[4:], minidumpVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(data)))
	binary.LittleEndian.PutUint32(buf[12:], 32)
	for i, value := range data {
		off := 32 + i*12
		binary.LittleEndian.PutUint32(buf[off:], types[i])
		binary.LittleEndian.PutUint32(buf[off+4:], uint32(len(value)))
		binary.LittleEndian.PutUint32(buf[off+8:], appendData(value))
	}
	crash, err := ParseMinidump(buf)
	if err != nil {
		t.Fatal(err)
	}
	if crash.Arch != "arm64" || crash.ThreadID != 9 || crash.RIP != moduleBase+0x1010 || len(crash.Frames) != 2 || crash.Frames[1].Instruction != moduleBase+0x1050 {
		t.Fatalf("arm64 crash: %+v", crash)
	}
}

const (
	testRISCV64MinidumpArchitecture = uint16(0x8006)
	testRISCV64ModuleBase           = uint64(0x0000000140000000)
	testRISCV64StackBase            = uint64(0x0000000000200000)
)

func buildRISCV64Minidump(contextSize int) []byte {
	data := buildAMD64Minidump()
	systemRVA := minidumpStreamRVA(data, streamSystemInfo)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	binary.LittleEndian.PutUint16(data[systemRVA:], testRISCV64MinidumpArchitecture)
	context := make([]byte, contextSize)
	if len(context) >= 8 {
		binary.LittleEndian.PutUint32(context, 0x08000003)
		binary.LittleEndian.PutUint32(context[4:], 1)
	}
	if len(context) >= 16 {
		binary.LittleEndian.PutUint64(context[8:], testRISCV64ModuleBase+0x1010)
	}
	for register := uint32(1); register <= 31; register++ {
		offset := 16 + int(register-1)*8
		if offset+8 <= len(context) {
			binary.LittleEndian.PutUint64(context[offset:], uint64(0x1000+register))
		}
	}
	setRISCV64ContextRegister(context, 1, testRISCV64ModuleBase+0x1050)
	setRISCV64ContextRegister(context, 2, testRISCV64StackBase)
	setRISCV64ContextRegister(context, 8, 0)
	data = append(data, context...)
	binary.LittleEndian.PutUint32(data[threadRVA+44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(data[threadRVA+48:], uint32(len(data)-len(context)))
	return data
}

func setRISCV64ContextRegister(context []byte, register uint32, value uint64) {
	if register == 0 || register > 31 {
		return
	}
	offset := 16 + int(register-1)*8
	if offset+8 <= len(context) {
		binary.LittleEndian.PutUint64(context[offset:], value)
	}
}

func setRISCV64DumpRegister(data []byte, register uint32, value uint64) {
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
	setRISCV64ContextRegister(data[contextRVA:], register, value)
}

func TestParseRISCV64MinidumpContext(t *testing.T) {
	for _, flags := range []uint32{0x08000001, 0x08000003} {
		t.Run(fmt.Sprintf("flags_%08x", flags), func(t *testing.T) {
			data := buildRISCV64Minidump(528)
			threadRVA := minidumpStreamRVA(data, streamThreadList)
			contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
			binary.LittleEndian.PutUint32(data[contextRVA:], flags)
			crash, err := ParseMinidump(data)
			if err != nil {
				t.Fatal(err)
			}
			if crash.Arch != "riscv64" || crash.PointerSize != 8 || crash.RIP != testRISCV64ModuleBase+0x1010 || crash.RSP != testRISCV64StackBase || crash.RBP != 0 || crash.LR != testRISCV64ModuleBase+0x1050 {
				t.Fatalf("RISC-V64 context: %+v", crash)
			}
			if len(crash.registers) != 32 || crash.registers[0] != 0 {
				t.Fatalf("RISC-V64 register map has %d entries and x0=%#x", len(crash.registers), crash.registers[0])
			}
			for register := uint32(1); register <= 31; register++ {
				want := uint64(0x1000 + register)
				switch register {
				case 1:
					want = testRISCV64ModuleBase + 0x1050
				case 2:
					want = testRISCV64StackBase
				case 8:
					want = 0
				}
				if got := crash.registers[register]; got != want {
					t.Fatalf("x%d = %#x; want %#x", register, got, want)
				}
			}
			if len(crash.Frames) != 2 || crash.Frames[1].Instruction != crash.LR {
				t.Fatalf("RISC-V64 x1 fallback: %+v", crash.Frames)
			}
		})
	}
}

func TestParseRISCV64RejectsInvalidContexts(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func() []byte
	}{
		{name: "short context", build: func() []byte { return buildRISCV64Minidump(524) }},
		{name: "long context", build: func() []byte { return buildRISCV64Minidump(536) }},
		{name: "wrong version", build: func() []byte {
			data := buildRISCV64Minidump(528)
			threadRVA := minidumpStreamRVA(data, streamThreadList)
			contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
			binary.LittleEndian.PutUint32(data[contextRVA+4:], 2)
			return data
		}},
		{name: "floating point only", build: func() []byte {
			data := buildRISCV64Minidump(528)
			threadRVA := minidumpStreamRVA(data, streamThreadList)
			contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
			binary.LittleEndian.PutUint32(data[contextRVA:], 0x08000002)
			return data
		}},
		{name: "unknown flags", build: func() []byte {
			data := buildRISCV64Minidump(528)
			threadRVA := minidumpStreamRVA(data, streamThreadList)
			contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
			binary.LittleEndian.PutUint32(data[contextRVA:], 0x08000005)
			return data
		}},
		{name: "wrong architecture", build: func() []byte {
			data := buildRISCV64Minidump(528)
			systemRVA := minidumpStreamRVA(data, streamSystemInfo)
			binary.LittleEndian.PutUint16(data[systemRVA:], cpuArchAMD64)
			return data
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseMinidump(test.build()); err == nil {
				t.Fatal("invalid RISC-V64 minidump was accepted")
			}
		})
	}
}

func TestRISCV64FrameRecordsAndLinkRegisterFallback(t *testing.T) {
	data := buildRISCV64Minidump(528)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	stackRVA := binary.LittleEndian.Uint32(data[threadRVA+40:])
	stack := data[stackRVA : stackRVA+0x100]
	currentFP, callerFP := testRISCV64StackBase+64, testRISCV64StackBase+96
	setRISCV64DumpRegister(data, 2, testRISCV64StackBase+16)
	setRISCV64DumpRegister(data, 8, currentFP)
	setRISCV64DumpRegister(data, 1, testRISCV64ModuleBase+0x1070)
	binary.LittleEndian.PutUint64(stack[48:], callerFP)
	binary.LittleEndian.PutUint64(stack[56:], testRISCV64ModuleBase+0x1050)
	binary.LittleEndian.PutUint64(stack[80:], 0)
	binary.LittleEndian.PutUint64(stack[88:], testRISCV64ModuleBase+0x1090)
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.Frames) != 3 || crash.Frames[1].Instruction != testRISCV64ModuleBase+0x1050 || crash.Frames[2].Instruction != testRISCV64ModuleBase+0x1090 {
		t.Fatalf("RISC-V64 CFA frame records: %+v", crash.Frames)
	}

	data = buildRISCV64Minidump(528)
	threadRVA = minidumpStreamRVA(data, streamThreadList)
	stackRVA = binary.LittleEndian.Uint32(data[threadRVA+40:])
	contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
	binary.LittleEndian.PutUint64(data[stackRVA:], testRISCV64ModuleBase+0x1060)
	setRISCV64DumpRegister(data, 1, testRISCV64ModuleBase+0x1070)
	setRISCV64DumpRegister(data, 8, 0)
	crash, err = ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.Frames) != 2 || crash.Frames[1].Instruction != testRISCV64ModuleBase+0x1070 || crash.Frames[1].Instruction == binary.LittleEndian.Uint64(data[contextRVA+8:]) {
		t.Fatalf("RISC-V64 must use x1 without guessing from SP: %+v", crash.Frames)
	}

	data = buildRISCV64Minidump(528)
	threadRVA = minidumpStreamRVA(data, streamThreadList)
	stackRVA = binary.LittleEndian.Uint32(data[threadRVA+40:])
	binary.LittleEndian.PutUint64(data[stackRVA:], testRISCV64ModuleBase+0x1060)
	setRISCV64DumpRegister(data, 1, 0)
	setRISCV64DumpRegister(data, 8, 0)
	crash, err = ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(crash.Frames) != 1 {
		t.Fatalf("RISC-V64 guessed a caller from SP when x1 was unavailable: %+v", crash.Frames)
	}
}

func TestRISCV64FrameRecordChainStopsAt64Frames(t *testing.T) {
	const (
		stackBase = uint64(0x5000)
		callers   = 70
	)
	stack := make([]byte, (callers+1)*16)
	fp := stackBase + 16
	for i := 0; i < callers; i++ {
		previousFP := fp + 16
		if i == callers-1 {
			previousFP = 0
		}
		offset := fp - stackBase - 16
		binary.LittleEndian.PutUint64(stack[offset:], previousFP)
		binary.LittleEndian.PutUint64(stack[offset+8:], uint64(0x2000+i*4))
		fp += 16
	}
	frames := unwind(Crash{RIP: 0x1000, RBP: stackBase + 16, Arch: "riscv64", PointerSize: 8}, []memoryRange{{start: stackBase, data: stack}})
	if len(frames) != 64 {
		t.Fatalf("RISC-V64 unwind returned %d frames; want 64", len(frames))
	}
}

func TestRISCV64FrameRecordsRejectInvalidLinks(t *testing.T) {
	const (
		stackBase = uint64(0x3000)
		frameFP   = stackBase + 16
	)
	for _, test := range []struct {
		name string
		data []byte
		prev uint64
		ret  uint64
	}{
		{name: "unavailable memory"},
		{name: "return address outside captured memory", data: make([]byte, 8)},
		{name: "nonmonotonic caller frame", data: make([]byte, 16), prev: frameFP - 8, ret: 0x2000},
		{name: "repeated return address", data: make([]byte, 16), ret: 0x1000},
		{name: "frame pointer underflow", data: make([]byte, 16), prev: 0, ret: 0x2000},
	} {
		t.Run(test.name, func(t *testing.T) {
			if len(test.data) >= 8 {
				binary.LittleEndian.PutUint64(test.data, test.prev)
			}
			if len(test.data) >= 16 {
				binary.LittleEndian.PutUint64(test.data[8:], test.ret)
			}
			fp := frameFP
			if test.name == "frame pointer underflow" {
				fp = 8
			}
			var memory []memoryRange
			if len(test.data) > 0 {
				memory = []memoryRange{{start: stackBase, data: test.data}}
			}
			frames := unwind(Crash{RIP: 0x1000, RBP: fp, Arch: "riscv64", PointerSize: 8}, memory)
			if len(frames) != 1 || frames[0].Instruction != 0x1000 {
				t.Fatalf("invalid RISC-V64 frame record yielded callers: %+v", frames)
			}
		})
	}
}

func TestParseELFObjectSymbols(t *testing.T) {
	data, err := os.ReadFile("testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	foundBase := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Filesz > 0 && (!foundBase || program.Vaddr < imageBase) {
			imageBase = program.Vaddr
			foundBase = true
		}
	}
	if !foundBase || imageBase == 0 {
		t.Fatal("ELF fixture lacks a non-zero image base")
	}
	symbols, err := file.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[string]uint64{}
	for _, symbol := range symbols {
		switch symbol.Name {
		case "fixture_function", "main":
			if symbol.Value >= imageBase && symbol.Size > 1 {
				addresses[symbol.Name] = symbol.Value
			}
		}
	}
	if addresses["fixture_function"] == 0 || addresses["main"] == 0 {
		t.Fatalf("ELF fixture lacks expected functions: %v", addresses)
	}
	module, functions := ParseObjectSymbols("fixture", data)
	if module != "fixture" || len(functions) == 0 {
		t.Fatalf("ELF DWARF symbols module=%q functions=%d", module, len(functions))
	}
	for _, test := range []struct {
		name     string
		symbol   string
		function string
		delta    uint64
		line     int
	}{
		{name: "function entry", symbol: "fixture_function", function: "fixture_function", line: 2},
		{name: "first line final instruction", symbol: "fixture_function", function: "fixture_function", delta: 0xa, line: 2},
		{name: "first source transition", symbol: "fixture_function", function: "fixture_function", delta: 0xb, line: 3},
		{name: "second line final instruction", symbol: "fixture_function", function: "fixture_function", delta: 0x10, line: 3},
		{name: "second source transition", symbol: "fixture_function", function: "fixture_function", delta: 0x11, line: 4},
		{name: "main entry", symbol: "main", function: "main", line: 6},
		{name: "next source transition", symbol: "main", function: "main", delta: 8, line: 7},
		{name: "final sequence instruction", symbol: "main", function: "main", delta: 0x12, line: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			crash := Crash{Modules: []Module{{Name: "fixture", Base: 0x70000000, Size: 0x10000000}}}
			address := addresses[test.symbol] - imageBase + test.delta
			crash.Frames = []Frame{{Instruction: 0x70000000 + address, Module: "fixture"}}
			Symbolicate(&crash, SymbolSet{module: functions})
			frame := crash.Frames[0]
			if frame.Function != test.function || !strings.HasSuffix(frame.File, "elf-dwarf-source.c") || frame.Line != test.line {
				t.Fatalf("ELF image-relative symbolication at %#x: want %s:%d, got %+v", addresses[test.symbol]+test.delta, test.function, test.line, frame)
			}
		})
	}
}

func TestSymbolicateByDebugID(t *testing.T) {
	crash := Crash{Modules: []Module{{Name: "C:\\bin\\app.exe", Base: 0x1000, Size: 0x1000, DebugID: "ABCD-1234"}}}
	crash.Frames = []Frame{{Instruction: 0x1100, Module: "C:\\bin\\app.exe"}}
	symbols := SymbolSet{
		moduleKey("C:\\bin\\app.exe"): []SymbolFunction{{Start: 0x200, End: 0x210, Name: "WrongObject"}},
		DebugKey("{ABCD-1234}"):       []SymbolFunction{{Start: 0x100, End: 0x110, Name: "ByDebugID"}},
	}
	Symbolicate(&crash, symbols)
	if crash.Frames[0].Function != "ByDebugID" {
		t.Fatalf("debug-ID fallback: %+v", crash.Frames[0])
	}
}

func TestParseMachODSYMObjectSymbols(t *testing.T) {
	data, err := os.ReadFile("testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var textBase uint64
	for _, load := range file.Loads {
		if segment, ok := load.(*macho.Segment); ok && segment.Name == "__TEXT" {
			textBase = segment.Addr
			break
		}
	}
	table, err := file.DWARF()
	if err != nil {
		t.Fatal(err)
	}
	reader := table.Reader()
	var expectedStart uint64
	for expectedStart == 0 {
		entry, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if entry == nil {
			t.Fatal("Mach-O fixture lacks main")
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		name, _ := entry.Val(dwarf.AttrName).(string)
		if name != "main" {
			continue
		}
		ranges, err := table.Ranges(entry)
		if err == nil && len(ranges) > 0 && ranges[0][0] >= textBase {
			expectedStart = ranges[0][0] - textBase
		}
	}
	module, functions := ParseObjectSymbols("fixture.dSYM", data)
	if module != "fixture.dsym" {
		t.Fatalf("Mach-O module=%q", module)
	}
	var mainFunction *SymbolFunction
	for i := range functions {
		if functions[i].Name == "main" && functions[i].Start == expectedStart {
			mainFunction = &functions[i]
			break
		}
	}
	if mainFunction == nil {
		t.Fatalf("Mach-O main not found at relative %#x in %d functions", expectedStart, len(functions))
	}
	crash := Crash{Modules: []Module{{Name: "fixture.dSYM", Base: 0x70000000, Size: 0x10000000}}}
	crash.Frames = []Frame{{Instruction: 0x70000000 + expectedStart, Module: "fixture.dSYM"}}
	Symbolicate(&crash, SymbolSet{module: functions})
	frame := crash.Frames[0]
	if frame.Function != "main" || !strings.HasSuffix(frame.File, "hello.c") || frame.Line < 1 {
		t.Fatalf("Mach-O dSYM symbolication: %+v", frame)
	}
}

func TestCFIUnwindsWithoutFramePointer(t *testing.T) {
	for _, fixture := range []string{"cfi-amd64", "cfi-amd64-pie"} {
		t.Run(fixture, func(t *testing.T) { testCFIUnwindFixture(t, fixture) })
	}
}

func TestCFISignalFrameUsesUnadjustedReturnAddress(t *testing.T) {
	table, err := parseCFISection(signalFrameCFISection(), 0, true, 8)
	if err != nil || table == nil || len(table.entries) != 3 {
		t.Fatalf("parse signal-frame CFI: table=%+v err=%v", table, err)
	}
	if !table.entries[0].cie.signalFrame || table.entries[1].cie.signalFrame || table.entries[2].cie.signalFrame {
		t.Fatalf("CIE signal-frame markers were not preserved: %+v", table.entries)
	}
	const stackBase = uint64(0x3000)
	stack := make([]byte, 32)
	binary.LittleEndian.PutUint64(stack, 0x2000)
	binary.LittleEndian.PutUint64(stack[8:], 0x3000)
	crash := Crash{
		RIP: 0x1001, RSP: stackBase, Arch: "amd64", PointerSize: 8,
		Modules: []Module{{Name: "signal-frame", Base: 0, Size: 0x3000}},
		stack:   []memoryRange{{start: stackBase, data: stack}},
	}
	frames := UnwindCFI(&crash, UnwindSet{"signal-frame": {table}})
	if len(frames) != 3 || frames[1].Instruction != 0x2000 || frames[2].Instruction != 0x3000 {
		t.Fatalf("signal/ordinary return addresses selected the wrong FDEs: %+v", frames)
	}
}

func signalFrameCFISection() []byte {
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
		_, _ = record.Write([]byte{1, 0x78, 16}) // code alignment, data alignment, return register
		if augmentation[0] == 'z' {
			_, _ = record.Write([]byte{1, 0}) // augmentation length and FDE pointer encoding
		}
		_, _ = record.Write([]byte{0x0c, 7, 8, 0x05, 16, 1}) // CFA = rsp + 8; RA = [CFA - 8]
		return appendRecord(record.Bytes())
	}
	appendFDE := func(cieOffset int, start uint64) {
		var record bytes.Buffer
		idOffset := section.Len() + 4
		_ = binary.Write(&record, binary.LittleEndian, uint32(idOffset-cieOffset))
		_ = binary.Write(&record, binary.LittleEndian, start)
		_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		_ = record.WriteByte(0) // empty FDE augmentation
		appendRecord(record.Bytes())
	}
	signalCIE := appendCIE("zRS")
	appendFDE(signalCIE, 0x1000)
	regularCIE := appendCIE("zR")
	appendFDE(regularCIE, 0x2000)
	appendFDE(regularCIE, 0x2f00)
	return section.Bytes()
}

func TestRISCV64ELFCFIUsesReturnColumnAndRestoredRegisters(t *testing.T) {
	key, table := ParseObjectUnwind("riscv64-fixture", buildRISCV64ELFWithCFI())
	if table == nil {
		t.Fatal("RISC-V64 ELF CFI was not parsed")
	}
	crash, err := ParseMinidump(buildRISCV64Minidump(528))
	if err != nil {
		t.Fatal(err)
	}
	crash.RIP = 0x1010
	crash.RSP = 0x3000
	crash.registers[2] = crash.RSP
	crash.registers[8] = 0x1111
	crash.Modules = []Module{{Name: "riscv64-fixture", Size: 0x3000}}
	stack := make([]byte, 0x1020)
	binary.LittleEndian.PutUint64(stack[0:], 0x4000) // caller x8 at CFA-16
	binary.LittleEndian.PutUint64(stack[8:], 0x2000) // alternate return column at CFA-8
	binary.LittleEndian.PutUint64(stack[0x1008:], 0)
	crash.stack = []memoryRange{{start: 0x3000, data: stack}}
	frames := UnwindCFI(&crash, UnwindSet{key: {table}})
	if len(frames) != 2 || frames[0].Instruction != crash.RIP || frames[1].Instruction != 0x2000 {
		t.Fatalf("RISC-V64 CFI did not use return column64 and restored x8: %+v", frames)
	}

	wrongELF := buildRISCV64ELFWithCFI()
	binary.LittleEndian.PutUint16(wrongELF[18:], uint16(elf.EM_X86_64))
	wrongKey, wrongTable := ParseObjectUnwind("wrong-architecture-fixture", wrongELF)
	if wrongTable == nil {
		t.Fatal("wrong-architecture control lacks CFI")
	}
	wrongCrash := crash
	wrongCrash.Modules = []Module{{Name: "wrong-architecture-fixture", Size: 0x3000}}
	frames = UnwindCFI(&wrongCrash, UnwindSet{wrongKey: {wrongTable}})
	if len(frames) != 0 {
		t.Fatalf("RISC-V64 crash used a mismatched ELF architecture: %+v", frames)
	}

	unsupportedKey, unsupportedTable := ParseObjectUnwind("floating-cfa-fixture", buildRISCV64ELFWithCFI(32))
	if unsupportedTable == nil {
		t.Fatal("floating-CFA control lacks CFI")
	}
	unsupportedCrash := crash
	unsupportedCrash.Modules = []Module{{Name: "floating-cfa-fixture", Size: 0x3000}}
	frames = UnwindCFI(&unsupportedCrash, UnwindSet{unsupportedKey: {unsupportedTable}})
	if len(frames) != 0 {
		t.Fatalf("RISC-V64 CFI guessed a floating-point CFA register: %+v", frames)
	}
}

func buildRISCV64ELFWithCFI(firstCFARegister ...byte) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) int {
		start := section.Len()
		_ = binary.Write(&section, binary.LittleEndian, uint32(len(record)))
		_, _ = section.Write(record)
		return start
	}
	appendCIE := func(cfaRegister byte, saveX8 bool) int {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(0xffffffff))
		_, _ = record.Write([]byte{3, 0, 1, 0x78, 0x40}) // v3, empty augmentation, alignments, return column64
		_, _ = record.Write([]byte{0x0c, cfaRegister, 16})
		_, _ = record.Write([]byte{0x05, 64, 1}) // return column64 saved at CFA-8
		if saveX8 {
			_, _ = record.Write([]byte{0x05, 8, 2}) // x8 saved at CFA-16
		}
		return appendRecord(record.Bytes())
	}
	appendFDE := func(cieOffset int, start uint64) {
		var record bytes.Buffer
		_ = binary.Write(&record, binary.LittleEndian, uint32(cieOffset))
		_ = binary.Write(&record, binary.LittleEndian, start)
		_ = binary.Write(&record, binary.LittleEndian, uint64(0x100))
		appendRecord(record.Bytes())
	}
	firstCFA := byte(2)
	if len(firstCFARegister) > 0 {
		firstCFA = firstCFARegister[0]
	}
	firstCIE := appendCIE(firstCFA, true)
	appendFDE(firstCIE, 0x1000)
	secondCIE := appendCIE(8, false)
	appendFDE(secondCIE, 0x1f00)

	const headerSize, sectionHeaderSize = 64, 64
	sectionNames := []byte("\x00.shstrtab\x00.text\x00.debug_frame\x00")
	sectionNamesOffset := headerSize
	textOffset := (sectionNamesOffset + len(sectionNames) + 7) &^ 7
	textData := make([]byte, 0x300)
	debugFrameOffset := (textOffset + len(textData) + 7) &^ 7
	sectionTableOffset := (debugFrameOffset + section.Len() + 7) &^ 7
	data := make([]byte, sectionTableOffset+4*sectionHeaderSize)
	copy(data[sectionNamesOffset:], sectionNames)
	copy(data[textOffset:], textData)
	copy(data[debugFrameOffset:], section.Bytes())
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.Machine(243)))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint64(data[40:], uint64(sectionTableOffset))
	binary.LittleEndian.PutUint16(data[52:], headerSize)
	binary.LittleEndian.PutUint16(data[58:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[60:], 4)
	binary.LittleEndian.PutUint16(data[62:], 1)
	nameOffset := func(name string) uint32 {
		return uint32(bytes.Index(sectionNames, append([]byte(name), 0)))
	}
	writeSection := func(index int, name string, kind uint32, flags, offset, size uint64, alignment uint64) {
		start := sectionTableOffset + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], nameOffset(name))
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint64(data[start+8:], flags)
		binary.LittleEndian.PutUint64(data[start+24:], offset)
		binary.LittleEndian.PutUint64(data[start+32:], size)
		binary.LittleEndian.PutUint64(data[start+48:], alignment)
	}
	writeSection(1, ".shstrtab", uint32(elf.SHT_STRTAB), 0, uint64(sectionNamesOffset), uint64(len(sectionNames)), 1)
	writeSection(2, ".text", uint32(elf.SHT_PROGBITS), uint64(elf.SHF_ALLOC|elf.SHF_EXECINSTR), uint64(textOffset), uint64(len(textData)), 4)
	writeSection(3, ".debug_frame", uint32(elf.SHT_PROGBITS), 0, uint64(debugFrameOffset), uint64(section.Len()), 4)
	return data
}

func TestCFIExpressionBranchControl(t *testing.T) {
	for _, test := range []struct {
		name       string
		expression []byte
		want       uint64
	}{
		{name: "forward skip", expression: []byte{0x30, 0x2f, 1, 0, 0x31}, want: 0},
		{name: "branch not taken", expression: []byte{0x30, 0x28, 4, 0, 0x31, 0x2f, 1, 0, 0x32}, want: 1},
		{name: "branch taken", expression: []byte{0x31, 0x28, 4, 0, 0x31, 0x2f, 1, 0, 0x32}, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8)
			if !ok || got != test.want {
				t.Fatalf("expression %x = %#x, %v; want %#x", test.expression, got, ok, test.want)
			}
		})
	}

	for _, test := range []struct {
		name       string
		expression []byte
	}{
		{name: "branch into operand", expression: []byte{0x30, 0x2f, 0xff, 0xff, 0x31}},
		{name: "branch into implicit-value block", expression: []byte{0x31, 0x28, 2, 0, 0x9e, 2, 0x11, 0x22, 0x32}},
		{name: "branch out of range", expression: []byte{0x30, 0x2f, 0x7f, 0}},
		{name: "truncated skip operand", expression: []byte{0x30, 0x2f, 0}},
		{name: "truncated branch operand", expression: []byte{0x31, 0x28, 0}},
		{name: "unbounded loop", expression: []byte{0x31, 0x2f, 0xfc, 0xff}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8); ok {
				t.Fatalf("invalid expression %x was accepted", test.expression)
			}
		})
	}
}

func TestCFIExpressionShiftOperations(t *testing.T) {
	for _, test := range []struct {
		name        string
		pointerSize int
		pushValue   []byte
		wantLeft    uint64
		wantLogical uint64
	}{
		{name: "32-bit", pointerSize: 4, pushValue: []byte{0x0c, 3, 0, 0, 0}, wantLeft: 12, wantLogical: 1},
		{name: "64-bit", pointerSize: 8, pushValue: []byte{0x0e, 3, 0, 0, 0, 0, 0, 0, 0}, wantLeft: 12, wantLogical: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			left := append(append([]byte(nil), test.pushValue...), 0x32, 0x24) // value 3, shift by 2
			if got, ok := evaluateCFIExpression(left, nil, 0, nil, test.pointerSize); !ok || got != test.wantLeft {
				t.Fatalf("DW_OP_shl = %#x, %v; want %#x", got, ok, test.wantLeft)
			}
			logical := append(append([]byte(nil), test.pushValue...), 0x31, 0x25) // value 3, shift by 1
			if got, ok := evaluateCFIExpression(logical, nil, 0, nil, test.pointerSize); !ok || got != test.wantLogical {
				t.Fatalf("DW_OP_shr = %#x, %v; want %#x", got, ok, test.wantLogical)
			}
		})
	}
	for _, pointerSize := range []int{4, 8} {
		if got, ok := evaluateCFIExpression([]byte{0x31, 0x30, 0x24}, nil, 0, nil, pointerSize); !ok || got != 1 {
			t.Fatalf("zero shift for pointerSize %d = %#x, %v; want 1", pointerSize, got, ok)
		}
	}

	for _, test := range []struct {
		name        string
		pointerSize int
		pushValue   []byte
	}{
		{name: "32-bit", pointerSize: 4, pushValue: []byte{0x0d, 0xf8, 0xff, 0xff, 0xff}},
		{name: "64-bit", pointerSize: 8, pushValue: []byte{0x0f, 0xf8, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	} {
		t.Run("arithmetic-"+test.name, func(t *testing.T) {
			expression := append(append([]byte(nil), test.pushValue...), 0x31, 0x26) // value -8, shift by 1
			if got, ok := evaluateCFIExpression(expression, nil, 0, nil, test.pointerSize); !ok || got != ^uint64(3) {
				t.Fatalf("DW_OP_shra = %#x, %v; want %#x", got, ok, ^uint64(3))
			}
		})
	}

	for _, test := range []struct {
		name        string
		expression  []byte
		pointerSize int
	}{
		{name: "64-bit count", expression: []byte{0x33, 0x10, 64, 0x24}, pointerSize: 8},
		{name: "32-bit count", expression: []byte{0x33, 0x10, 64, 0x25}, pointerSize: 4},
		{name: "stack underflow", expression: []byte{0x24}, pointerSize: 8},
	} {
		t.Run("invalid-"+test.name, func(t *testing.T) {
			if _, ok := evaluateCFIExpression(test.expression, nil, 0, nil, test.pointerSize); ok {
				t.Fatalf("invalid expression %x was accepted", test.expression)
			}
		})
	}
}

func TestCFIExpressionSignedArithmetic(t *testing.T) {
	for _, test := range []struct {
		name       string
		expression []byte
		want       uint64
	}{
		{name: "absolute value", expression: []byte{0x09, 0xf8, 0x19}, want: 8},
		{name: "negation", expression: []byte{0x09, 0xf8, 0x1f}, want: 8},
		{name: "bitwise complement", expression: []byte{0x30, 0x20}, want: ^uint64(0)},
		{name: "signed division", expression: []byte{0x09, 0xf8, 0x32, 0x1b}, want: ^uint64(3)},
		{name: "equal", expression: []byte{0x09, 0xff, 0x09, 0xff, 0x29}, want: 1},
		{name: "greater-or-equal is signed", expression: []byte{0x09, 0xff, 0x09, 1, 0x2a}, want: 0},
		{name: "greater than", expression: []byte{0x09, 1, 0x09, 0xff, 0x2b}, want: 1},
		{name: "less-or-equal", expression: []byte{0x09, 0xff, 0x09, 1, 0x2c}, want: 1},
		{name: "less than is signed", expression: []byte{0x09, 0xff, 0x09, 1, 0x2d}, want: 1},
		{name: "not equal", expression: []byte{0x09, 0xff, 0x09, 1, 0x2e}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8)
			if !ok || got != test.want {
				t.Fatalf("expression %x = %#x, %v; want %#x", test.expression, got, ok, test.want)
			}
		})
	}

	minInt64 := []byte{0x0f, 0, 0, 0, 0, 0, 0, 0, 0x80}
	for _, test := range []struct {
		name       string
		expression []byte
	}{
		{name: "division by zero", expression: []byte{0x09, 0xf8, 0x30, 0x1b}},
		{name: "division overflow", expression: append(append(append([]byte(nil), minInt64...), 0x09, 0xff), 0x1b)},
		{name: "absolute overflow", expression: append(append([]byte(nil), minInt64...), 0x19)},
		{name: "negation overflow", expression: append(append([]byte(nil), minInt64...), 0x1f)},
		{name: "unary underflow", expression: []byte{0x19}},
		{name: "binary underflow", expression: []byte{0x1b}},
		{name: "comparison underflow", expression: []byte{0x29}},
	} {
		t.Run("invalid-"+test.name, func(t *testing.T) {
			if _, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8); ok {
				t.Fatalf("invalid expression %x was accepted", test.expression)
			}
		})
	}
}

func TestCFIExpressionModulo(t *testing.T) {
	for _, test := range []struct {
		name       string
		expression []byte
		want       uint64
	}{
		{name: "negative numerator", expression: []byte{0x09, 0xf9, 0x09, 3, 0x1d}, want: ^uint64(0)},
		{name: "negative denominator", expression: []byte{0x09, 7, 0x09, 0xfd, 0x1d}, want: 1},
		{name: "exact division", expression: []byte{0x09, 0xfa, 0x09, 3, 0x1d}, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8)
			if !ok || got != test.want {
				t.Fatalf("expression %x = %#x, %v; want %#x", test.expression, got, ok, test.want)
			}
		})
	}

	minInt64 := []byte{0x0f, 0, 0, 0, 0, 0, 0, 0, 0x80}
	for _, test := range []struct {
		name       string
		expression []byte
	}{
		{name: "zero divisor", expression: []byte{0x09, 0xf9, 0x30, 0x1d}},
		{name: "signed overflow", expression: append(append(append([]byte(nil), minInt64...), 0x09, 0xff), 0x1d)},
		{name: "stack underflow", expression: []byte{0x1d}},
	} {
		t.Run("invalid-"+test.name, func(t *testing.T) {
			if _, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8); ok {
				t.Fatalf("invalid expression %x was accepted", test.expression)
			}
		})
	}
}

func TestCFIExpressionStackManipulation(t *testing.T) {
	for _, test := range []struct {
		name       string
		expression []byte
		want       uint64
	}{
		{name: "rotate top three", expression: []byte{0x31, 0x32, 0x33, 0x17, 0x13, 0x13}, want: 3},
		{name: "nop preserves value", expression: []byte{0x37, 0x96}, want: 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8)
			if !ok || got != test.want {
				t.Fatalf("expression %x = %#x, %v; want %#x", test.expression, got, ok, test.want)
			}
		})
	}

	for _, test := range []struct {
		name       string
		expression []byte
	}{
		{name: "empty stack", expression: []byte{0x17}},
		{name: "two values", expression: []byte{0x31, 0x32, 0x17}},
	} {
		t.Run("invalid-"+test.name, func(t *testing.T) {
			if _, ok := evaluateCFIExpression(test.expression, nil, 0, nil, 8); ok {
				t.Fatalf("invalid expression %x was accepted", test.expression)
			}
		})
	}
}

func TestCFILEB128ReadersRejectOverflow(t *testing.T) {
	ulebPrefix := bytes.Repeat([]byte{0xff}, 9)
	ulebMax := append(append([]byte(nil), ulebPrefix...), 0x01)
	slebMax := append(append([]byte(nil), ulebPrefix...), 0x00)
	slebMin := append(bytes.Repeat([]byte{0x80}, 9), 0x7f)
	for _, test := range []struct {
		name  string
		data  []byte
		read  func(*cfiCursor) uint64
		want  uint64
		valid bool
	}{
		{name: "ULEB zero", data: []byte{0}, read: (*cfiCursor).uleb, want: 0, valid: true},
		{name: "ULEB uint64 max", data: ulebMax, read: (*cfiCursor).uleb, want: ^uint64(0), valid: true},
		{name: "ULEB tenth payload overflow", data: append(bytes.Repeat([]byte{0x80}, 9), 0x02), read: (*cfiCursor).uleb},
		{name: "ULEB continuation beyond width", data: append(bytes.Repeat([]byte{0x80}, 9), 0x81), read: (*cfiCursor).uleb},
		{name: "ULEB truncated", data: []byte{0x80}, read: (*cfiCursor).uleb},
		{name: "SLEB int64 max", data: slebMax, read: func(cursor *cfiCursor) uint64 { return uint64(cursor.sleb()) }, want: uint64(^uint64(0) >> 1), valid: true},
		{name: "SLEB int64 min", data: slebMin, read: func(cursor *cfiCursor) uint64 { return uint64(cursor.sleb()) }, want: uint64(1) << 63, valid: true},
		{name: "SLEB positive tenth-byte overflow", data: append(bytes.Repeat([]byte{0x80}, 9), 0x01), read: func(cursor *cfiCursor) uint64 { return uint64(cursor.sleb()) }},
		{name: "SLEB negative tenth-byte overflow", data: append(bytes.Repeat([]byte{0x80}, 9), 0x7e), read: func(cursor *cfiCursor) uint64 { return uint64(cursor.sleb()) }},
		{name: "SLEB continuation beyond width", data: append(bytes.Repeat([]byte{0x80}, 9), 0x80), read: func(cursor *cfiCursor) uint64 { return uint64(cursor.sleb()) }},
		{name: "SLEB truncated", data: []byte{0x80}, read: func(cursor *cfiCursor) uint64 { return uint64(cursor.sleb()) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cursor := cfiCursor{data: test.data}
			got := test.read(&cursor)
			if test.valid {
				if cursor.err != nil || got != test.want || cursor.off != len(test.data) {
					t.Fatalf("decoded %x = %#x, err=%v, offset=%d; want %#x with full consumption", test.data, got, cursor.err, cursor.off, test.want)
				}
			} else if cursor.err == nil {
				t.Fatalf("malformed encoding %x decoded as %#x", test.data, got)
			}
		})
	}
}

func TestCFIExpressionRejectsRegisterIDTruncation(t *testing.T) {
	wideRegister := []byte{0x80, 0x80, 0x80, 0x80, 0x10} // 1<<32, which must not wrap to register zero.
	registers := map[uint32]uint64{0: 0x1000, 5: 0x2000}
	for _, test := range []struct {
		name       string
		expression []byte
		want       uint64
		valid      bool
	}{
		{name: "valid regx", expression: []byte{0x90, 0x05}, want: 0x2000, valid: true},
		{name: "valid bregx", expression: []byte{0x92, 0x05, 0x01}, want: 0x2001, valid: true},
		{name: "regx above uint32", expression: append([]byte{0x90}, wideRegister...)},
		{name: "bregx above uint32", expression: append(append([]byte{0x92}, wideRegister...), 0x00)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := evaluateCFIExpression(test.expression, registers, 0, nil, 8)
			if ok != test.valid || test.valid && got != test.want {
				t.Fatalf("expression %x = %#x, %v; want %#x, %v", test.expression, got, ok, test.want, test.valid)
			}
		})
	}
}

func TestApplyCFIInstructionsRejectsRegisterIDTruncation(t *testing.T) {
	wideRegister := []byte{0x80, 0x80, 0x80, 0x80, 0x10} // 1<<32, which must not wrap to register zero.
	for _, instructions := range [][]byte{
		append([]byte{0x07}, wideRegister...),               // DW_CFA_undefined
		append(append([]byte{0x09}, wideRegister...), 0x01), // DW_CFA_register
		append(append([]byte{0x0c}, wideRegister...), 0x00), // DW_CFA_def_cfa
	} {
		if _, err := applyCFIInstructions(instructions, newCFIState(), cfiCIE{addrSize: 8}, 0, false); err == nil {
			t.Errorf("instructions %x accepted a register ID above uint32", instructions)
		}
	}
}

func TestCFIExpressionDereferenceRespectsSizeOperands(t *testing.T) {
	data := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	ranges := []memoryRange{{start: 0x1000, data: data}}
	for _, pointerSize := range []int{4, 8} {
		t.Run(fmt.Sprintf("pointer-%d", pointerSize), func(t *testing.T) {
			registers := map[uint32]uint64{0: 0x1000}
			wantDereference := uint64(0)
			for i := 0; i < pointerSize; i++ {
				wantDereference |= uint64(data[i]) << (8 * i)
			}
			got, ok := evaluateCFIExpression([]byte{0x90, 0, 0x06}, registers, 0, ranges, pointerSize)
			if !ok || got != wantDereference {
				t.Fatalf("DW_OP_deref = %#x, %v; want %#x", got, ok, wantDereference)
			}
			for size := 1; size <= pointerSize; size++ {
				var want uint64
				for i := 0; i < size; i++ {
					want |= uint64(data[i]) << (8 * i)
				}
				expression := []byte{0x90, 0, 0x94, byte(size)}
				got, ok := evaluateCFIExpression(expression, registers, 0, ranges, pointerSize)
				if !ok || got != want {
					t.Fatalf("DW_OP_deref_size %d = %#x, %v; want %#x", size, got, ok, want)
				}
			}
		})
	}

	for _, test := range []struct {
		name        string
		expression  []byte
		pointerSize int
		address     uint64
	}{
		{name: "missing size operand", expression: []byte{0x90, 0, 0x94}, pointerSize: 8, address: 0x1000},
		{name: "zero size", expression: []byte{0x90, 0, 0x94, 0}, pointerSize: 8, address: 0x1000},
		{name: "larger than pointer", expression: []byte{0x90, 0, 0x94, 9}, pointerSize: 8, address: 0x1000},
		{name: "larger than four-byte pointer", expression: []byte{0x90, 0, 0x94, 5}, pointerSize: 4, address: 0x1000},
		{name: "outside captured memory", expression: []byte{0x90, 0, 0x94, 2}, pointerSize: 8, address: 0x1007},
	} {
		t.Run(test.name, func(t *testing.T) {
			registers := map[uint32]uint64{0: test.address}
			if _, ok := evaluateCFIExpression(test.expression, registers, 0, ranges, test.pointerSize); ok {
				t.Fatalf("invalid dereference expression %x was accepted", test.expression)
			}
		})
	}
}

func TestCFIExpressionAddressUsesTargetWidth(t *testing.T) {
	for _, test := range []struct {
		pointerSize int
		address     uint64
	}{
		{pointerSize: 4, address: 0x12345678},
		{pointerSize: 8, address: 0x0123456789abcdef},
	} {
		expression := []byte{0x03}
		for i := 0; i < test.pointerSize; i++ {
			expression = append(expression, byte(test.address>>(8*i)))
		}
		got, ok := evaluateCFIExpression(expression, nil, 0, nil, test.pointerSize)
		if !ok || got != test.address {
			t.Errorf("DW_OP_addr with pointerSize=%d = %#x, %v; want %#x", test.pointerSize, got, ok, test.address)
		}
	}
	if _, ok := evaluateCFIExpression([]byte{0x03, 0x78, 0x56, 0x34}, nil, 0, nil, 4); ok {
		t.Fatal("truncated four-byte DW_OP_addr was accepted")
	}
}

func TestCFIExpressionImplicitValues(t *testing.T) {
	block := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99}
	for _, pointerSize := range []int{4, 8} {
		t.Run(fmt.Sprintf("pointer-%d", pointerSize), func(t *testing.T) {
			for length := 1; length <= pointerSize; length++ {
				expression := append([]byte{0x9e, byte(length)}, block[:length]...)
				var want uint64
				for index, value := range block[:length] {
					want |= uint64(value) << (8 * index)
				}
				if got, ok := evaluateCFIExpression(expression, nil, 0, nil, pointerSize); !ok || got != want {
					t.Fatalf("DW_OP_implicit_value length=%d = %#x, %v; want %#x", length, got, ok, want)
				}
			}
			if got, ok := evaluateCFIExpression([]byte{0x9e, 1, 0x9f}, nil, 0, nil, pointerSize); !ok || got != 0x9f {
				t.Fatalf("DW_OP_implicit_value treated its final block byte as an opcode: %#x, %v", got, ok)
			}

			invalid := [][]byte{
				{0x9e, 0},
				{0x9e, 0x80},
				{0x9e, 2, 0x11},
			}
			overWidth := append([]byte{0x9e, byte(pointerSize + 1)}, block[:pointerSize+1]...)
			invalid = append(invalid, overWidth)
			for _, expression := range invalid {
				if _, ok := evaluateCFIExpression(expression, nil, 0, nil, pointerSize); ok {
					t.Errorf("invalid implicit-value expression %x was accepted", expression)
				}
			}
		})
	}
}

func TestCFIRegisterExpressionsHonorStackValue(t *testing.T) {
	stack := make([]byte, 16)
	binary.LittleEndian.PutUint64(stack, 0x5678)
	ranges := []memoryRange{{start: 0x1000, data: stack}}
	registers := map[uint32]uint64{0: 0x123456, 1: 0x1000}
	for _, test := range []struct {
		name string
		rule cfiRegisterRule
		want uint64
	}{
		{name: "expression value", rule: cfiRegisterRule{kind: cfiRuleExpression, expr: []byte{0x90, 0, 0x9f}}, want: 0x123456},
		{name: "expression address", rule: cfiRegisterRule{kind: cfiRuleExpression, expr: []byte{0x90, 1}}, want: 0x5678},
		{name: "expression implicit-value address", rule: cfiRegisterRule{kind: cfiRuleExpression, expr: []byte{0x9e, 8, 0, 0x10, 0, 0, 0, 0, 0, 0}}, want: 0x5678},
		{name: "value expression", rule: cfiRegisterRule{kind: cfiRuleValueExpression, expr: []byte{0x90, 0, 0x9f}}, want: 0x123456},
		{name: "value expression implicit scalar", rule: cfiRegisterRule{kind: cfiRuleValueExpression, expr: []byte{0x9e, 4, 0x78, 0x56, 0x34, 0x12}}, want: 0x12345678},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := cfiState{regs: map[uint32]cfiRegisterRule{16: test.rule}}
			got, ok := evaluateRegisterRule(16, state, 0, registers, ranges, 8)
			if !ok || got != test.want {
				t.Fatalf("register expression = %#x, %v; want %#x", got, ok, test.want)
			}
		})
	}

	badRanges := []memoryRange{{start: 0, data: []byte{0x99, 0, 0, 0, 0, 0, 0, 0}}}
	for _, expression := range [][]byte{{0x9f}, {0x90, 0, 0x9f, 0x30}} {
		state := cfiState{regs: map[uint32]cfiRegisterRule{16: {kind: cfiRuleExpression, expr: expression}}}
		if _, ok := evaluateRegisterRule(16, state, 0, registers, badRanges, 8); ok {
			t.Fatalf("empty or nonterminal stack value expression %x was accepted", expression)
		}
	}

	for _, test := range []struct {
		pointerSize int
		address     uint64
		value       uint64
	}{
		{pointerSize: 4, address: 0x9f001000, value: 0x12345678},
		{pointerSize: 8, address: 0x0123456789abcd9f, value: 0x0123456789abcdef},
	} {
		data := make([]byte, test.pointerSize)
		for i := range data {
			data[i] = byte(test.value >> (8 * i))
		}
		expression := []byte{0x03}
		for i := 0; i < test.pointerSize; i++ {
			expression = append(expression, byte(test.address>>(8*i)))
		}
		rule := cfiRegisterRule{kind: cfiRuleExpression, expr: expression}
		state := cfiState{regs: map[uint32]cfiRegisterRule{16: rule}}
		ranges := []memoryRange{{start: test.address, data: data}}
		got, ok := evaluateRegisterRule(16, state, 0, nil, ranges, test.pointerSize)
		if !ok || got != test.value {
			t.Errorf("DW_OP_addr ending in 0x9f with pointerSize=%d = %#x, %v; want %#x", test.pointerSize, got, ok, test.value)
		}
	}
}

func testCFIUnwindFixture(t *testing.T, fixture string) {
	data, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	module, functions := ParseObjectSymbols(fixture, data)
	if module != moduleKey(fixture) {
		t.Fatalf("CFI module=%q", module)
	}
	file, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	symbols, err := file.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[string]uint64{}
	imageBase := elfImageBase(file)
	for _, symbol := range symbols {
		switch symbol.Name {
		case "crash_frame", "middle_frame", "outer_frame":
			if symbol.Value >= imageBase {
				addresses[symbol.Name] = symbol.Value - imageBase
			}
		}
	}
	if len(addresses) != 3 {
		t.Fatalf("CFI fixture functions: %+v", addresses)
	}
	_, table := ParseObjectUnwind(fixture, data)
	if table == nil {
		t.Fatal("CFI fixture has no unwind table")
	}
	const moduleBase = uint64(0x0000000140000000)
	const stackBase = uint64(0x0000000000200000)
	stack := make([]byte, 32)
	binary.LittleEndian.PutUint64(stack[0:], moduleBase+addresses["middle_frame"]+0xc)
	binary.LittleEndian.PutUint64(stack[8:], moduleBase+addresses["outer_frame"]+0xc)
	crash := Crash{
		RIP: moduleBase + addresses["crash_frame"], RSP: stackBase, Arch: "amd64", PointerSize: 8,
		Modules: []Module{{Name: fixture, Base: moduleBase, Size: 0x10000}},
		stack:   []memoryRange{{start: stackBase, data: stack}},
	}
	crash.Frames = UnwindCFI(&crash, UnwindSet{module: []*UnwindTable{table}})
	if len(crash.Frames) != 3 {
		t.Fatalf("CFI frames=%+v", crash.Frames)
	}
	crash.Frames = UnwindCFI(&crash, UnwindSet{module: []*UnwindTable{{}, table}})
	if len(crash.Frames) != 3 {
		t.Fatalf("CFI skipped non-matching table: %+v", crash.Frames)
	}
	Symbolicate(&crash, SymbolSet{module: functions})
	expected := []string{"crash_frame", "middle_frame", "outer_frame"}
	for i, name := range expected {
		if crash.Frames[i].Function != name {
			t.Fatalf("CFI frame %d=%+v want %s", i, crash.Frames[i], name)
		}
	}
}

func TestParseCFIECIEV4AddressAndSegmentSizes(t *testing.T) {
	for _, addressSize := range []byte{4, 8} {
		t.Run(fmt.Sprintf("address_size_%d", addressSize), func(t *testing.T) {
			data := []byte{4, 0, addressSize, 0, 1, 0x78, 16, 0x0c, 7, 8}
			cie, err := parseCFIECIE(data, 8, false)
			if err != nil {
				t.Fatalf("parse version 4 CIE: %v", err)
			}
			if cie.addrSize != int(addressSize) || cie.codeAlign != 1 || cie.dataAlign != -8 || cie.returnReg != 16 {
				t.Fatalf("decoded CIE header: %+v", cie)
			}
			if cie.initial.cfa.register != 7 || cie.initial.cfa.offset != 8 {
				t.Fatalf("decoded CIE instructions: %+v", cie.initial.cfa)
			}
		})
	}

	for name, data := range map[string][]byte{
		"unsupported_address_size": {4, 0, 16, 0, 1, 0x78, 16},
		"unsupported_segment_size": {4, 0, 8, 1, 1, 0x78, 16},
		"truncated_sizes":          {4, 0, 8},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCFIECIE(data, 8, false); err == nil {
				t.Fatal("invalid version 4 CIE was accepted")
			}
		})
	}
}

func TestCFIParserRejectsTruncatedSection(t *testing.T) {
	for _, data := range [][]byte{
		{0x10, 0x00},
		{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	} {
		if table, err := parseCFISection(data, 0, true, 8); err == nil && table != nil && len(table.entries) != 0 {
			t.Fatalf("truncated CFI produced entries: %+v", table.entries)
		}
	}
}

func TestParseDebugArchiveObjects(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, fixture := range map[string]string{
		"Example.dSYM/Contents/Resources/DWARF/Example": "macho-dsym-amd64",
		"linux/libexample.so":                           "elf-dwarf-amd64",
	} {
		data, err := os.ReadFile("testdata/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	objects := ParseDebugObjects("bundle.zip", archive.Bytes())
	if len(objects) != 2 {
		t.Fatalf("archive objects=%d", len(objects))
	}
	found := map[string]bool{}
	for _, object := range objects {
		for _, function := range object.Functions {
			if function.Name == "main" || function.Name == "fixture_function" {
				found[function.Name] = true
			}
		}
	}
	if !found["main"] || !found["fixture_function"] {
		t.Fatalf("archive functions=%+v", found)
	}
	if objects := ParseDebugObjects("broken.zip", []byte{'P', 'K', 0x03, 0x04, 0}); len(objects) != 0 {
		t.Fatalf("truncated archive objects=%+v", objects)
	}
}

func TestParseTARDebugArchivesAndSymbolicate(t *testing.T) {
	elfObject, err := os.ReadFile("testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	machoObject, err := os.ReadFile("testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	breakpad := []byte("MODULE windows x86_64 ABCDEF0123456789 test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 CrashFunction\n1010 20 42 1\n")
	tarFormats := []struct {
		name   string
		format tar.Format
	}{{"ustar", tar.FormatUSTAR}, {"pax", tar.FormatPAX}, {"gnu", tar.FormatGNU}}
	compressions := []struct {
		name string
		gzip bool
	}{{"tar", false}, {"tar.gz", true}}
	for _, archiveFormat := range tarFormats {
		elfName := "linux/libexample.so"
		if archiveFormat.format != tar.FormatUSTAR {
			elfName = strings.Repeat("d", 120) + "/libexample.so"
		}
		archive := buildTestTAR(t, archiveFormat.format, []testTARFile{
			{Name: "symbols/test_app.sym", Data: breakpad},
			{Name: "Example.dSYM/Contents/Resources/DWARF/Example", Data: machoObject},
			{Name: elfName, Data: elfObject},
		})
		for _, compression := range compressions {
			t.Run(archiveFormat.name+"/"+compression.name, func(t *testing.T) {
				data := archive
				if compression.gzip {
					var compressed bytes.Buffer
					writer := gzip.NewWriter(&compressed)
					if _, err := writer.Write(archive); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					data = compressed.Bytes()
				}
				objects := ParseDebugObjects("symbols."+compression.name, data)
				if len(objects) != 3 {
					t.Fatalf("archive objects=%d", len(objects))
				}
				found := map[string]bool{}
				symbols := SymbolSet{}
				for _, object := range objects {
					for _, function := range object.Functions {
						found[function.Name] = true
					}
					symbols[object.Name] = append(symbols[object.Name], object.Functions...)
				}
				if !found["CrashFunction"] || !found["main"] || !found["fixture_function"] {
					t.Fatalf("archive functions=%+v", found)
				}
				crash, err := ParseMinidump(buildAMD64Minidump())
				if err != nil {
					t.Fatal(err)
				}
				Symbolicate(&crash, symbols)
				if frame := crash.Frames[0]; frame.Function != "CrashFunction" || frame.File != "C:\\src\\crash.cc" || frame.Line != 42 {
					t.Fatalf("archive symbolication: %+v", frame)
				}
			})
		}
	}
}

func TestParseTARDebugArchivesRejectPartialSymbols(t *testing.T) {
	symbol := []byte("MODULE windows x86_64 ABCDEF0123456789 test_app.pdb\nFUNC 1010 20 0 CrashFunction\n")
	complete := buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "test_app.sym", Data: symbol}})
	truncated := complete[:512+len(symbol)/2]
	if objects := ParseDebugObjects("broken.tar", truncated); len(objects) != 0 {
		t.Fatalf("truncated archive returned symbols: %+v", objects)
	}
	for _, missingEndBlocks := range []int{1, 2} {
		truncated = complete[:len(complete)-missingEndBlocks*512]
		if objects := ParseDebugObjects("missing-end-blocks.tar", truncated); len(objects) != 0 {
			t.Fatalf("archive missing %d end blocks returned symbols: %+v", missingEndBlocks, objects)
		}
		var compressed bytes.Buffer
		gzipWriter := gzip.NewWriter(&compressed)
		if _, err := gzipWriter.Write(truncated); err != nil {
			t.Fatal(err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if objects := ParseDebugObjects("missing-end-blocks.tar.gz", compressed.Bytes()); len(objects) != 0 {
			t.Fatalf("gzip archive missing %d end blocks returned symbols: %+v", missingEndBlocks, objects)
		}
	}
	withTrailingData := append(append([]byte(nil), complete...), []byte("not tar padding")...)
	if objects := ParseDebugObjects("trailing-data.tar", withTrailingData); len(objects) != 0 {
		t.Fatalf("archive with non-zero trailing data returned symbols: %+v", objects)
	}
	var trailingCompressed bytes.Buffer
	trailingWriter := gzip.NewWriter(&trailingCompressed)
	if _, err := trailingWriter.Write(withTrailingData); err != nil {
		t.Fatal(err)
	}
	if err := trailingWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if objects := ParseDebugObjects("trailing-data.tar.gz", trailingCompressed.Bytes()); len(objects) != 0 {
		t.Fatalf("gzip archive with non-zero trailing data returned symbols: %+v", objects)
	}

	var oversized bytes.Buffer
	writer := tar.NewWriter(&oversized)
	if err := writer.WriteHeader(&tar.Header{Name: "test_app.sym", Mode: 0600, Size: int64(len(symbol)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(symbol); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "too-large.sym", Mode: 0600, Size: maxDebugArchiveBytes + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if objects := ParseDebugObjects("oversized.tar", oversized.Bytes()); len(objects) != 0 {
		t.Fatalf("over-budget archive returned partial symbols: %+v", objects)
	}

	var tooMany bytes.Buffer
	writer = tar.NewWriter(&tooMany)
	if err := writer.WriteHeader(&tar.Header{Name: "test_app.sym", Mode: 0600, Size: int64(len(symbol)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(symbol); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxDebugArchiveEntries; i++ {
		if err := writer.WriteHeader(&tar.Header{Name: fmt.Sprintf("dir-%03d", i), Mode: 0700, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if objects := ParseDebugObjects("too-many.tar", tooMany.Bytes()); len(objects) != 0 {
		t.Fatalf("over-entry-limit archive returned partial symbols: %+v", objects)
	}

	var links bytes.Buffer
	writer = tar.NewWriter(&links)
	if err := writer.WriteHeader(&tar.Header{Name: "test_app.sym", Linkname: "../test_app.sym", Mode: 0600, Typeflag: tar.TypeSymlink}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if objects := ParseDebugObjects("links.tar", links.Bytes()); len(objects) != 0 {
		t.Fatalf("non-regular entry returned symbols: %+v", objects)
	}

	var compressed bytes.Buffer
	writerGzip := gzip.NewWriter(&compressed)
	if _, err := writerGzip.Write(complete); err != nil {
		t.Fatal(err)
	}
	if err := writerGzip.Close(); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), compressed.Bytes()...)
	corrupt[len(corrupt)-8] ^= 0xff
	if objects := ParseDebugObjects("broken.tar.gz", corrupt); len(objects) != 0 {
		t.Fatalf("checksum-invalid archive returned partial symbols: %+v", objects)
	}
}

func TestParseNestedDebugArchivesAndSymbolicate(t *testing.T) {
	symbol := []byte("MODULE windows x86_64 ABCDEF0123456789 test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 CrashFunction\n1010 20 42 1\n")
	nestedTAR := buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "test_app.sym", Data: symbol}})
	nestedGzipTAR := gzipTestArchive(t, nestedTAR)
	nestedZIP := buildTestZIP(t, []testTARFile{{Name: "test_app.sym", Data: symbol}})
	layerTwo := buildTestZIP(t, []testTARFile{{Name: "layer-one.tar", Data: nestedTAR}})
	layerThree := gzipTestArchive(t, buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "layer-two.zip", Data: layerTwo}}))
	fourLayers := buildTestZIP(t, []testTARFile{{Name: "layer-three.tar.gz", Data: layerThree}})
	cases := []struct {
		name string
		data []byte
	}{
		{name: "tar inside zip", data: buildTestZIP(t, []testTARFile{{Name: "nested.tar", Data: nestedTAR}})},
		{name: "zip inside tar", data: buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "nested.zip", Data: nestedZIP}})},
		{name: "gzip tar inside zip", data: buildTestZIP(t, []testTARFile{{Name: "nested.tar.gz", Data: nestedGzipTAR}})},
		{name: "four mixed layers", data: fourLayers},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objects := ParseDebugObjects("outer", tc.data)
			if len(objects) != 1 {
				t.Fatalf("nested symbols=%+v", objects)
			}
			foundFunction := false
			for _, function := range objects[0].Functions {
				foundFunction = foundFunction || function.Name == "CrashFunction"
			}
			if !foundFunction {
				t.Fatalf("nested symbols=%+v", objects)
			}
			crash, err := ParseMinidump(buildAMD64Minidump())
			if err != nil {
				t.Fatal(err)
			}
			Symbolicate(&crash, SymbolSet{objects[0].Name: objects[0].Functions})
			if frame := crash.Frames[0]; frame.Function != "CrashFunction" || frame.File != "C:\\src\\crash.cc" || frame.Line != 42 {
				t.Fatalf("nested archive symbolication=%+v", frame)
			}
		})
	}

	tooDeep := buildTestZIP(t, []testTARFile{
		{Name: "valid.sym", Data: symbol},
		{Name: "layer-four.zip", Data: fourLayers},
	})
	if objects := ParseDebugObjects("too-deep.tar", tooDeep); len(objects) != 0 {
		t.Fatalf("archive deeper than four layers returned partial symbols: %+v", objects)
	}
	malformedNested := buildTestTAR(t, tar.FormatUSTAR, []testTARFile{
		{Name: "valid.sym", Data: symbol},
		{Name: "malformed.zip", Data: []byte{'P', 'K', 0x03, 0x04, 0}},
	})
	if objects := ParseDebugObjects("malformed-nested.tar", malformedNested); len(objects) != 0 {
		t.Fatalf("malformed nested archive returned partial symbols: %+v", objects)
	}

	var inner bytes.Buffer
	innerWriter := tar.NewWriter(&inner)
	if err := innerWriter.WriteHeader(&tar.Header{Name: "test_app.sym", Mode: 0600, Size: int64(len(symbol)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := innerWriter.Write(symbol); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxDebugArchiveEntries; i++ {
		if err := innerWriter.WriteHeader(&tar.Header{Name: fmt.Sprintf("dir-%03d", i), Mode: 0700, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	if err := innerWriter.Close(); err != nil {
		t.Fatal(err)
	}
	outer := buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "inner.tar", Data: inner.Bytes()}})
	if objects := ParseDebugObjects("too-many-nested-entries.tar", outer); len(objects) != 0 {
		t.Fatalf("archive entries counted independently at each layer: %+v", objects)
	}
}

func TestNestedDebugArchiveExpandedByteBudgetIsShared(t *testing.T) {
	const (
		outerPayloadSize = 62 << 20
		innerPayloadSize = 67 << 20
	)
	symbol := []byte("MODULE windows x86_64 ABCDEF0123456789 test_app.pdb\nFUNC 1010 20 0 CrashFunction\n")
	var compressedInner bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedInner)
	innerWriter := tar.NewWriter(gzipWriter)
	if err := innerWriter.WriteHeader(&tar.Header{Name: "padding.bin", Mode: 0600, Size: innerPayloadSize, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	writeTestZeroes(t, innerWriter, innerPayloadSize)
	if err := innerWriter.WriteHeader(&tar.Header{Name: "test_app.sym", Mode: 0600, Size: int64(len(symbol)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := innerWriter.Write(symbol); err != nil {
		t.Fatal(err)
	}
	if err := innerWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	var outer bytes.Buffer
	outerWriter := tar.NewWriter(&outer)
	if err := outerWriter.WriteHeader(&tar.Header{Name: "padding.bin", Mode: 0600, Size: outerPayloadSize, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	writeTestZeroes(t, outerWriter, outerPayloadSize)
	if err := outerWriter.WriteHeader(&tar.Header{Name: "nested.tar.gz", Mode: 0600, Size: int64(compressedInner.Len()), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(outerWriter, &compressedInner); err != nil {
		t.Fatal(err)
	}
	if err := outerWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if len(outer.Bytes()) >= 64<<20 {
		t.Fatalf("fixture exceeds uploaded debug-file limit: %d bytes", outer.Len())
	}
	if objects := ParseDebugObjects("outer.tar", outer.Bytes()); len(objects) != 0 {
		t.Fatalf("nested archives exceeded the shared expanded-byte budget: %+v", objects)
	}
}

type testTARFile struct {
	Name string
	Data []byte
}

func buildTestTAR(t *testing.T, format tar.Format, files []testTARFile) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range files {
		if err := writer.WriteHeader(&tar.Header{Name: file.Name, Mode: 0600, Size: int64(len(file.Data)), Typeflag: tar.TypeReg, Format: format}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(file.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func buildTestZIP(t *testing.T, files []testTARFile) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range files {
		entry, err := writer.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func gzipTestArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func writeTestZeroes(t *testing.T, writer io.Writer, size int64) {
	t.Helper()
	zeroes := make([]byte, 32<<10)
	for size > 0 {
		chunk := zeroes
		if int64(len(chunk)) > size {
			chunk = chunk[:size]
		}
		written, err := writer.Write(chunk)
		if err != nil {
			t.Fatal(err)
		}
		size -= int64(written)
	}
}
