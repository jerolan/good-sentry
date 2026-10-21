package native

import (
	"encoding/binary"
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
