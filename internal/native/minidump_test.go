package native

import (
	"bytes"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
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
	crash.Frames = UnwindCFI(&crash, UnwindSet{module: table})
	if len(crash.Frames) != 3 {
		t.Fatalf("CFI frames=%+v", crash.Frames)
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
