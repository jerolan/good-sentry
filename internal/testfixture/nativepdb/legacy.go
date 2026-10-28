package nativepdb

import (
	"bytes"
	"encoding/binary"
)

// SmallMSF2 builds an owned VC98 PDB with NewDBI and two ST procedures.
// The container follows microsoft/pdb-rs at 2081a0dd6f1150cf965dec10855e75cfecfde176,
// msf/src/open.rs, and microsoft/microsoft-pdb at
// 805655a28bd8198004be2ac27e6e0290121a5e89, PDB/msf/msf.cpp and include/cvinfo.h.
func SmallMSF2(pageSize int, age uint32, streamZeroAlias bool) Fixture {
	return smallMSF2(pageSize, age, streamZeroAlias, nil, nil).Fixture
}

type C11Fixture struct {
	Fixture
	C11Start       int
	C11Size        int
	SecondC11Start int
}

func WithC11SourceLines() C11Fixture {
	lines := c11SourceLines(0x10, 0x3f, []c11SourceFile{
		{name: `C:\src\first.cc`, start: 0x10, end: 0x1f, offsets: []uint32{0x10, 0x10, 0x10, 0x14, 0x18, 0x1c}, lines: []uint16{0, 42, 43, 65535, 0, 44}},
		{name: `C:\src\second.cc`, start: 0x28, end: 0x3f, offsets: []uint32{0x28, 0x30}, lines: []uint16{52, 53}},
	})
	return smallMSF2(1024, 1, false, lines, nil)
}

func WithMultipleC11SourceLineModules() C11Fixture {
	first := c11SourceLines(0x10, 0x5f, []c11SourceFile{
		{name: `C:\src\crash.cc`, start: 0x10, end: 0x5f, offsets: []uint32{0x10, 0x40}, lines: []uint16{42, 99}},
	})
	second := c11SourceLines(0x10, 0x5f, []c11SourceFile{
		{name: `C:\src\crash.cc`, start: 0x10, end: 0x5f, offsets: []uint32{0x10, 0x20, 0x40}, lines: []uint16{77, 78, 52}},
	})
	return smallMSF2(1024, 1, false, first, second)
}

func smallMSF2(pageSize int, age uint32, streamZeroAlias bool, c11, secondC11 []byte) C11Fixture {
	fpmPages := 0
	switch pageSize {
	case 1024:
		fpmPages = 8
	case 2048:
		fpmPages = 4
	case 4096:
		fpmPages = 1
	default:
		panic("unsupported Small MSF fixture page size")
	}
	info := make([]byte, 12)
	binary.LittleEndian.PutUint32(info, 19970604) // VC98
	binary.LittleEndian.PutUint32(info[4:], 0x65ae7563)
	binary.LittleEndian.PutUint32(info[8:], age)

	first := stProcedure("CrashFunction", 0x10, 0x20, 0x100b)
	second := stProcedure("OtherFunction", 0x80, 0x10, 0x100a)
	module := append([]byte{2, 0, 0, 0}, first...)
	module = append(module, second...)
	symbolBytes := len(module)
	module = append(module, c11...)
	moduleInfo := moduleInfoRecord(4, symbolBytes, 0, "fixture.obj", "fixture.obj")
	binary.LittleEndian.PutUint32(moduleInfo[40:], uint32(len(c11)))
	var secondModule []byte
	secondSymbolBytes := 0
	if secondC11 != nil {
		secondModule = append([]byte{2, 0, 0, 0}, stProcedure("OtherModuleFunction", 0x40, 0x20, 0x100b)...)
		secondSymbolBytes = len(secondModule)
		secondModule = append(secondModule, secondC11...)
		record := moduleInfoRecord(6, secondSymbolBytes, 0, "fixture-extra.obj", "fixture-extra.obj")
		binary.LittleEndian.PutUint32(record[40:], uint32(len(secondC11)))
		moduleInfo = append(moduleInfo, record...)
	}
	dbi := make([]byte, 64)
	binary.LittleEndian.PutUint32(dbi, ^uint32(0))
	binary.LittleEndian.PutUint32(dbi[4:], 19970606) // NewDBI V60
	binary.LittleEndian.PutUint32(dbi[8:], age)
	binary.LittleEndian.PutUint16(dbi[12:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[16:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[20:], ^uint16(0))
	binary.LittleEndian.PutUint32(dbi[24:], uint32(len(moduleInfo)))
	binary.LittleEndian.PutUint32(dbi[48:], 22)
	binary.LittleEndian.PutUint32(dbi[52:], 25)
	dbi = append(dbi, moduleInfo...)
	dbi = append(dbi, bytes.Repeat([]byte{0xe1}, 25)...)
	optional := bytes.Repeat([]byte{0xff}, 22)
	binary.LittleEndian.PutUint16(optional[10:], 5)
	dbi = append(dbi, optional...)

	section := make([]byte, 40)
	copy(section, ".text")
	binary.LittleEndian.PutUint32(section[8:], 0x100)
	binary.LittleEndian.PutUint32(section[12:], 0x1000)
	binary.LittleEndian.PutUint32(section[16:], 0x100)
	binary.LittleEndian.PutUint32(section[36:], 0x60000020)
	streams := [][]byte{nil, info, nil, dbi, module, section}
	if secondModule != nil {
		streams = append(streams, secondModule)
	}
	if streamZeroAlias {
		streams[0] = info
	}
	directoryPage := 1 + 2*fpmPages
	pages := make([]int, len(streams))
	nextPage := directoryPage + 1
	for i := 1; i < len(streams); i++ {
		if streams[i] != nil {
			if len(streams[i]) > pageSize {
				panic("Small MSF fixture stream no longer fits one page")
			}
			pages[i] = nextPage
			nextPage++
		}
	}
	if streamZeroAlias {
		pages[0] = pages[1]
	}
	directory := make([]byte, 4+8*len(streams))
	binary.LittleEndian.PutUint16(directory, uint16(len(streams)))
	for i, stream := range streams {
		size := ^uint32(0)
		if stream != nil {
			size = uint32(len(stream))
		}
		binary.LittleEndian.PutUint32(directory[4+i*8:], size)
		binary.LittleEndian.PutUint32(directory[8+i*8:], 0xe1e1e1e1) // obsolete pointer
	}
	for i, stream := range streams {
		if stream != nil {
			directory = binary.LittleEndian.AppendUint16(directory, uint16(pages[i]))
		}
	}
	data := make([]byte, nextPage*pageSize)
	copy(data[:44], "Microsoft C/C++ program database 2.00\r\n\x1aJG\x00\x00")
	binary.LittleEndian.PutUint32(data[44:], uint32(pageSize))
	binary.LittleEndian.PutUint16(data[48:], uint16(1+fpmPages))
	binary.LittleEndian.PutUint16(data[50:], uint16(nextPage))
	binary.LittleEndian.PutUint32(data[52:], uint32(len(directory)))
	binary.LittleEndian.PutUint32(data[56:], 0xe1e1e1e1) // obsolete directory pointer
	binary.LittleEndian.PutUint16(data[60:], uint16(directoryPage))
	copy(data[directoryPage*pageSize:], directory)
	for i := 1; i < len(streams); i++ {
		if streams[i] != nil {
			copy(data[pages[i]*pageSize:], streams[i])
		}
	}
	fixture := C11Fixture{
		Fixture: Fixture{
			Data: data, InfoBlock: pages[1], DBIBlock: pages[3], ModuleBlock: pages[4],
			FirstProcedureLength: len(first),
		},
		C11Start: pages[4]*pageSize + symbolBytes, C11Size: len(c11),
	}
	if secondModule != nil {
		fixture.SecondC11Start = pages[6]*pageSize + secondSymbolBytes
	}
	return fixture
}

type c11SourceFile struct {
	name       string
	start, end uint32
	offsets    []uint32
	lines      []uint16
}

// c11SourceLines uses the pinned cvlines.h structures: referenced offsets are
// relative to this extent, and every SE end is inclusive. File records are
// deliberately written in reverse order, after their referenced line tables.
func c11SourceLines(rootStart, rootEnd uint32, files []c11SourceFile) []byte {
	rootSize := 4 + 4*len(files) + 8 + 2
	data := make([]byte, (rootSize+3)&^3)
	binary.LittleEndian.PutUint16(data, uint16(len(files)))
	binary.LittleEndian.PutUint16(data[2:], 1)
	ranges := 4 + 4*len(files)
	binary.LittleEndian.PutUint32(data[ranges:], rootStart)
	binary.LittleEndian.PutUint32(data[ranges+4:], rootEnd)
	binary.LittleEndian.PutUint16(data[ranges+8:], 1)
	for i := rootSize; i < len(data); i++ {
		data[i] = 0xa5
	}
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		if len(file.offsets) != len(file.lines) || len(file.name) > 255 {
			panic("invalid owned C11 fixture")
		}
		lineOffset := len(data)
		data = binary.LittleEndian.AppendUint16(data, 1)
		data = binary.LittleEndian.AppendUint16(data, uint16(len(file.lines)))
		for _, offset := range file.offsets {
			data = binary.LittleEndian.AppendUint32(data, offset)
		}
		for _, line := range file.lines {
			data = binary.LittleEndian.AppendUint16(data, line)
		}
		for len(data)%4 != 0 {
			data = append(data, 0xa5)
		}
		binary.LittleEndian.PutUint32(data[4+i*4:], uint32(len(data)))
		data = binary.LittleEndian.AppendUint16(data, 1)
		data = binary.LittleEndian.AppendUint16(data, 0xa5a5)
		data = binary.LittleEndian.AppendUint32(data, uint32(lineOffset))
		data = binary.LittleEndian.AppendUint32(data, file.start)
		data = binary.LittleEndian.AppendUint32(data, file.end)
		data = append(data, byte(len(file.name)))
		data = append(data, file.name...)
		for len(data)%4 != 0 {
			data = append(data, 0xa5)
		}
	}
	return data
}

func stProcedure(name string, offset, size uint32, kind uint16) []byte {
	record := procedure(name, offset, size, kind)
	// Replace the NUL-terminated spelling with the ST length-prefixed spelling.
	return append(record[:4+35], append([]byte{byte(len(name))}, name...)...)
}

// WithStreamZeroAlias gives the obsolete MSF7 directory snapshot stream the
// same page as current Info; active streams retain their original bytes.
func WithStreamZeroAlias() Fixture {
	fixture := Minimal()
	const pageSize = 512
	directorySize := int(binary.LittleEndian.Uint32(fixture.Data[44:]))
	directory := append([]byte(nil), fixture.Data[3*pageSize:3*pageSize+directorySize]...)
	const firstPageOffset = 4 + 6*4
	directory = append(directory[:firstPageOffset], append(make([]byte, 4), directory[firstPageOffset:]...)...)
	binary.LittleEndian.PutUint32(directory[4:], 28)
	binary.LittleEndian.PutUint32(directory[firstPageOffset:], uint32(fixture.InfoBlock))
	binary.LittleEndian.PutUint32(fixture.Data[44:], uint32(len(directory)))
	copy(fixture.Data[3*pageSize:], directory)
	return fixture
}
