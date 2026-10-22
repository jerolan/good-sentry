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
	"os"
	"strings"
	"testing"
)

func buildAMD64Minidump() []byte {
	const (
		moduleBase = uint64(0x0000000140000000)
		stackBase  = uint64(0x0000000000200000)
	)
	context := make([]byte, 1232)
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

func buildX86Minidump() []byte {
	const moduleBase = uint32(0x00400000)
	const stackBase = uint32(0x00200000)
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[180:], stackBase)
	binary.LittleEndian.PutUint32(context[184:], moduleBase+0x1010)
	binary.LittleEndian.PutUint32(context[196:], stackBase)
	stack := make([]byte, 0x100)
	binary.LittleEndian.PutUint32(stack[4:], moduleBase+0x1050)
	system := make([]byte, 2)
	binary.LittleEndian.PutUint16(system, 3)
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

func buildARM32Minidump(contextSize int) []byte {
	data := buildX86Minidump()
	systemRVA := minidumpStreamRVA(data, streamSystemInfo)
	threadRVA := minidumpStreamRVA(data, streamThreadList)
	binary.LittleEndian.PutUint16(data[systemRVA:], cpuArchARM)
	context := make([]byte, contextSize)
	if contextSize >= 68 {
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
	var functionAddress uint64
	for _, symbol := range symbols {
		if symbol.Name == "fixture_function" && symbol.Value >= imageBase && symbol.Size > 1 {
			functionAddress = symbol.Value
			break
		}
	}
	if functionAddress == 0 {
		t.Fatal("ELF fixture lacks fixture_function")
	}
	module, functions := ParseObjectSymbols("fixture", data)
	if module != "fixture" || len(functions) == 0 {
		t.Fatalf("ELF DWARF symbols module=%q functions=%d", module, len(functions))
	}
	crash := Crash{Modules: []Module{{Name: "fixture", Base: 0x70000000, Size: 0x10000000}}}
	crash.Frames = []Frame{{Instruction: 0x70000000 + functionAddress - imageBase, Module: "fixture"}}
	Symbolicate(&crash, SymbolSet{module: functions})
	frame := crash.Frames[0]
	if frame.Function != "fixture_function" || !strings.HasSuffix(frame.File, "elf-dwarf-source.c") || frame.Line < 1 {
		t.Fatalf("ELF image-relative symbolication: raw=%#x base=%#x frame=%+v", functionAddress, imageBase, crash.Frames[0])
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
		{name: "value expression", rule: cfiRegisterRule{kind: cfiRuleValueExpression, expr: []byte{0x90, 0, 0x9f}}, want: 0x123456},
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
	addresses := map[string]uint64{}
	for _, function := range functions {
		switch function.Name {
		case "crash_frame", "middle_frame", "outer_frame":
			addresses[function.Name] = function.Start
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

	nested := buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "test_app.sym", Data: symbol}})
	outer := buildTestTAR(t, tar.FormatUSTAR, []testTARFile{{Name: "nested.tar", Data: nested}})
	if objects := ParseDebugObjects("outer.tar", outer); len(objects) != 0 {
		t.Fatalf("nested archive returned symbols: %+v", objects)
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
