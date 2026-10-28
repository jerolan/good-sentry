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
	return smallMSF2(pageSize, age, streamZeroAlias, nil, nil, false, nil, nil).Fixture
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
	return smallMSF2(1024, 1, false, lines, nil, false, nil, nil)
}

func WithMultipleC11SourceLineModules() C11Fixture {
	first, second := multipleC11SourceLinePayloads()
	return smallMSF2(1024, 1, false, first, second, false, nil, nil)
}

// WithV50SourceLines needs companion PE sections and uses the 48-byte MODI50
// prefix and a direct logical section map.
func WithV50SourceLines(dbiAge uint32) C11Fixture {
	first, second := multipleC11SourceLinePayloads()
	fixture := smallMSF2(1024, 1, false, first, second, true, nil, nil)
	binary.LittleEndian.PutUint32(fixture.Data[fixture.DBIBlock*1024+8:], dbiAge)
	return fixture
}

func multipleC11SourceLinePayloads() ([]byte, []byte) {
	first := c11SourceLines(0x10, 0x5f, []c11SourceFile{
		{name: `C:\src\crash.cc`, start: 0x10, end: 0x5f, offsets: []uint32{0x10, 0x40}, lines: []uint16{42, 99}},
	})
	second := c11SourceLines(0x10, 0x5f, []c11SourceFile{
		{name: `C:\src\crash.cc`, start: 0x10, end: 0x5f, offsets: []uint32{0x10, 0x20, 0x40}, lines: []uint16{77, 78, 52}},
	})
	return first, second
}

func WithLegacyThunk() Fixture {
	return legacyThunk(0, nil)
}

func WithLegacyThunkVariant() Fixture {
	return legacyThunk(3, []byte{0xa1, 0xb2, 0xc3, 0xd4})
}

func WithLegacyPublicSymbols() C11Fixture {
	lines := c11SourceLines(0x10, 0x3f, []c11SourceFile{{
		name: `C:\src\first.cc`, start: 0x10, end: 0x1f,
		offsets: []uint32{0x10, 0x14}, lines: []uint16{42, 43},
	}})
	var global []byte
	appendRecord := func(kind uint16, body []byte) {
		record := make([]byte, 4+len(body))
		binary.LittleEndian.PutUint16(record[2:], kind)
		copy(record[4:], body)
		for len(record)%4 != 0 {
			record = append(record, 0)
		}
		binary.LittleEndian.PutUint16(record, uint16(len(record)-2))
		global = append(global, record...)
	}
	public := func(kind uint16, name string, offset uint32) {
		body := make([]byte, 11)
		if kind == 0x0203 {
			body = make([]byte, 9)
			binary.LittleEndian.PutUint32(body, offset)
			binary.LittleEndian.PutUint16(body[4:], 1)
			binary.LittleEndian.PutUint16(body[6:], 0x1001)
			body[8] = byte(len(name))
		} else {
			binary.LittleEndian.PutUint32(body[4:], offset)
			binary.LittleEndian.PutUint16(body[8:], 1)
			body[10] = byte(len(name))
		}
		appendRecord(kind, append(body, []byte(name)...))
	}
	public(0x1009, "PublicOverlap", 0x14)
	public(0x1009, "PublicFirst", 0x60)
	appendRecord(0x0400, make([]byte, 12))
	global = append(global, 3, 'r', 'e', 'f')
	public(0x1009, "PublicAlias", 0x60)
	public(0x0203, "PublicSecond", 0x70)
	return smallMSF2(1024, 1, false, lines, nil, false, nil, global)
}

func legacyThunk(ordinal byte, variant []byte) Fixture {
	name := []byte("LegacyThunk")
	record := make([]byte, 26+len(name)+len(variant))
	binary.LittleEndian.PutUint16(record, uint16(len(record)-2))
	binary.LittleEndian.PutUint16(record[2:], 0x0206)
	binary.LittleEndian.PutUint32(record[16:], 0x60)
	binary.LittleEndian.PutUint16(record[20:], 1)
	binary.LittleEndian.PutUint16(record[22:], 0x10)
	record[24] = ordinal
	record[25] = byte(len(name))
	copy(record[26:], name)
	copy(record[26+len(name):], variant)
	return smallMSF2(1024, 1, false, nil, nil, false, record, nil).Fixture
}

func smallMSF2(pageSize int, age uint32, streamZeroAlias bool, c11, secondC11 []byte, v50 bool, extra, global []byte) C11Fixture {
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
	module = append(module, extra...)
	symbolBytes := len(module)
	module = append(module, c11...)
	moduleInfo := moduleInfoRecord(4, symbolBytes, 0, "fixture.obj", "fixture.obj")
	binary.LittleEndian.PutUint32(moduleInfo[40:], uint32(len(c11)))
	if v50 {
		moduleInfo = moduleInfo50(4, symbolBytes, len(c11), "fixture.obj")
	}
	var secondModule []byte
	secondSymbolBytes := 0
	if secondC11 != nil {
		secondModule = append([]byte{2, 0, 0, 0}, stProcedure("OtherModuleFunction", 0x40, 0x20, 0x100b)...)
		secondSymbolBytes = len(secondModule)
		secondModule = append(secondModule, secondC11...)
		record := moduleInfoRecord(6, secondSymbolBytes, 0, "fixture-extra.obj", "fixture-extra.obj")
		binary.LittleEndian.PutUint32(record[40:], uint32(len(secondC11)))
		if v50 {
			record = moduleInfo50(6, secondSymbolBytes, len(secondC11), "fixture-extra.obj")
		}
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
	var sectionMap []byte
	if v50 || global != nil {
		if v50 {
			binary.LittleEndian.PutUint32(dbi[4:], 19960307)
			binary.LittleEndian.PutUint32(dbi[48:], 0)
		}
		sectionMap = make([]byte, 24)
		binary.LittleEndian.PutUint16(sectionMap, 1)
		binary.LittleEndian.PutUint16(sectionMap[2:], 1)
		binary.LittleEndian.PutUint16(sectionMap[4:], 1|4|8|0x100)
		binary.LittleEndian.PutUint16(sectionMap[4+6:], 1)
		binary.LittleEndian.PutUint32(sectionMap[4+16:], 0x100)
		binary.LittleEndian.PutUint32(dbi[32:], uint32(len(sectionMap)))
	}
	dbi = append(dbi, moduleInfo...)
	dbi = append(dbi, sectionMap...)
	dbi = append(dbi, bytes.Repeat([]byte{0xe1}, 25)...)
	optional := bytes.Repeat([]byte{0xff}, 22)
	binary.LittleEndian.PutUint16(optional[10:], 5)
	if !v50 {
		dbi = append(dbi, optional...)
	}

	section := make([]byte, 40)
	copy(section, ".text")
	binary.LittleEndian.PutUint32(section[8:], 0x100)
	binary.LittleEndian.PutUint32(section[12:], 0x1000)
	binary.LittleEndian.PutUint32(section[16:], 0x100)
	binary.LittleEndian.PutUint32(section[36:], 0x60000020)
	streams := [][]byte{nil, info, nil, dbi, module, section}
	if v50 {
		streams[5] = nil
	}
	if secondModule != nil {
		streams = append(streams, secondModule)
	}
	globalIndex := 0
	if global != nil {
		globalIndex = len(streams)
		binary.LittleEndian.PutUint16(dbi[20:], uint16(globalIndex))
		streams = append(streams, global)
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
	if global != nil {
		fixture.PublicSymbolsBlock = pages[globalIndex]
		fixture.SectionMapStart = pages[3]*pageSize + 64 + len(moduleInfo)
	}
	if secondModule != nil {
		fixture.SecondC11Start = pages[6]*pageSize + secondSymbolBytes
	}
	return fixture
}

func moduleInfo50(stream uint16, symbols, c11 int, name string) []byte {
	record := make([]byte, 48)
	binary.LittleEndian.PutUint16(record[26:], stream)
	binary.LittleEndian.PutUint32(record[28:], uint32(symbols))
	binary.LittleEndian.PutUint32(record[32:], uint32(c11))
	record = append(record, name...)
	record = append(record, 0)
	record = append(record, name...)
	record = append(record, 0)
	for len(record)%4 != 0 {
		record = append(record, 0)
	}
	return record
}

type PEFixture struct {
	Data          []byte
	SectionHeader int
	DebugEntry    int
	CodeView      int
}

// V50CompanionPE is a data-only PE32 with a file-pointer-only NB10 record.
func V50CompanionPE(timestamp, age uint32) PEFixture {
	data := make([]byte, 0x600)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[0x3c:], 0x80)
	copy(data[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(data[0x84:], 0x14c)
	binary.LittleEndian.PutUint16(data[0x86:], 1)
	binary.LittleEndian.PutUint16(data[0x94:], 224)
	binary.LittleEndian.PutUint16(data[0x96:], 0x2102)
	const optional = 0x98
	binary.LittleEndian.PutUint16(data[optional:], 0x10b)
	binary.LittleEndian.PutUint32(data[optional+16:], 0x1010)
	binary.LittleEndian.PutUint32(data[optional+20:], 0x1000)
	binary.LittleEndian.PutUint32(data[optional+28:], 0x400000)
	binary.LittleEndian.PutUint32(data[optional+32:], 0x1000)
	binary.LittleEndian.PutUint32(data[optional+36:], 0x200)
	binary.LittleEndian.PutUint32(data[optional+56:], 0x2000)
	binary.LittleEndian.PutUint32(data[optional+60:], 0x200)
	binary.LittleEndian.PutUint16(data[optional+68:], 3)
	binary.LittleEndian.PutUint32(data[optional+92:], 16)
	binary.LittleEndian.PutUint32(data[optional+96+6*8:], 0x1200)
	binary.LittleEndian.PutUint32(data[optional+96+6*8+4:], 28)
	const section = 0x178
	copy(data[section:], ".text")
	binary.LittleEndian.PutUint32(data[section+8:], 0x400)
	binary.LittleEndian.PutUint32(data[section+12:], 0x1000)
	binary.LittleEndian.PutUint32(data[section+16:], 0x400)
	binary.LittleEndian.PutUint32(data[section+20:], 0x200)
	binary.LittleEndian.PutUint32(data[section+36:], 0x60000020)
	const debug = 0x400
	const codeView = 0x480
	path := []byte("different-name.pdb\x00")
	binary.LittleEndian.PutUint32(data[debug+12:], 2)
	binary.LittleEndian.PutUint32(data[debug+16:], uint32(16+len(path)))
	binary.LittleEndian.PutUint32(data[debug+24:], codeView)
	copy(data[codeView:], "NB10")
	binary.LittleEndian.PutUint32(data[codeView+8:], timestamp)
	binary.LittleEndian.PutUint32(data[codeView+12:], age)
	copy(data[codeView+16:], path)
	return PEFixture{Data: data, SectionHeader: section, DebugEntry: debug, CodeView: codeView}
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

// OldDBIC7 builds an old DBI header and two 16-bit-type C7 procedures. The
// matching PE sections are supplied separately, as with the V50 fixture.
func OldDBIC7() Fixture {
	f := smallMSF2(1024, 1, false, nil, nil, true, nil, nil).Fixture
	data := f.Data
	binary.LittleEndian.PutUint32(data[f.InfoBlock*1024:], 19950623)
	first := procedure16("CrashFunction", 0x10, 0x20, 0x205)
	second := procedure16("OtherFunction", 0x80, 0x10, 0x204)
	module := append([]byte{1, 0, 0, 0}, first...)
	module = append(module, second...)
	copy(data[f.ModuleBlock*1024:], module)
	oldDBI := data[f.DBIBlock*1024:]
	moduleSize := int(binary.LittleEndian.Uint32(oldDBI[24:]))
	mapSize := int(binary.LittleEndian.Uint32(oldDBI[32:]))
	moduleInfo := append([]byte(nil), oldDBI[64:64+moduleSize]...)
	binary.LittleEndian.PutUint32(moduleInfo[28:], uint32(len(module)))
	mapData := append([]byte(nil), oldDBI[64+moduleSize:64+moduleSize+mapSize]...)
	header := make([]byte, 24)
	binary.LittleEndian.PutUint16(header, 0xffff)
	binary.LittleEndian.PutUint16(header[4:], 0xffff)
	binary.LittleEndian.PutUint32(header[8:], uint32(moduleSize))
	binary.LittleEndian.PutUint32(header[16:], uint32(mapSize))
	dbi := append(header, moduleInfo...)
	dbi = append(dbi, mapData...)
	copy(oldDBI, dbi)
	directory := int(binary.LittleEndian.Uint16(data[60:])) * 1024
	binary.LittleEndian.PutUint32(data[directory+4+3*8:], uint32(len(dbi)))
	binary.LittleEndian.PutUint32(data[directory+4+4*8:], uint32(len(module)))
	f.FirstProcedureLength = len(first)
	return f
}

func OldDBIC7WithThunk() Fixture {
	return oldDBIC7WithThunk(WithLegacyThunk())
}

func OldDBIC7WithThunkVariant() Fixture {
	return oldDBIC7WithThunk(WithLegacyThunkVariant())
}

func OldDBIC7WithPublicSymbols() Fixture {
	f := OldDBIC7()
	source := WithLegacyPublicSymbols()
	sourceDirectory := int(binary.LittleEndian.Uint16(source.Data[60:])) * 1024
	sourceStream := binary.LittleEndian.Uint16(source.Data[source.DBIBlock*1024+20:])
	size := binary.LittleEndian.Uint32(source.Data[sourceDirectory+4+int(sourceStream)*8:])
	global := source.Data[source.PublicSymbolsBlock*1024 : source.PublicSymbolsBlock*1024+int(size)]
	page := len(f.Data) / 1024
	f.Data = append(f.Data, make([]byte, 1024)...)
	copy(f.Data[page*1024:], global)
	binary.LittleEndian.PutUint16(f.Data[50:], uint16(page+1))
	directory := int(binary.LittleEndian.Uint16(f.Data[60:])) * 1024
	oldSize := int(binary.LittleEndian.Uint32(f.Data[52:]))
	old := append([]byte(nil), f.Data[directory:directory+oldSize]...)
	count := int(binary.LittleEndian.Uint16(old))
	entryEnd := 4 + count*8
	next := append([]byte(nil), old[:entryEnd]...)
	next = append(next, make([]byte, 8)...)
	binary.LittleEndian.PutUint32(next[entryEnd:], size)
	next = append(next, old[entryEnd:]...)
	next = binary.LittleEndian.AppendUint16(next, uint16(page))
	binary.LittleEndian.PutUint16(next, uint16(count+1))
	binary.LittleEndian.PutUint32(f.Data[52:], uint32(len(next)))
	copy(f.Data[directory:], next)
	binary.LittleEndian.PutUint16(f.Data[f.DBIBlock*1024+4:], uint16(count))
	f.PublicSymbolsBlock = page
	return f
}

func oldDBIC7WithThunk(sourceFixture Fixture) Fixture {
	f := OldDBIC7()
	source := sourceFixture.Data
	name := bytes.Index(source, []byte("LegacyThunk"))
	start := name - 26
	record := source[start : start+int(binary.LittleEndian.Uint16(source[start:]))+2]
	dbi := f.DBIBlock * 1024
	symbolBytes := binary.LittleEndian.Uint32(f.Data[dbi+24+28:])
	copy(f.Data[f.ModuleBlock*1024+int(symbolBytes):], record)
	symbolBytes += uint32(len(record))
	binary.LittleEndian.PutUint32(f.Data[dbi+24+28:], symbolBytes)
	directory := int(binary.LittleEndian.Uint16(f.Data[60:])) * 1024
	binary.LittleEndian.PutUint32(f.Data[directory+4+4*8:], symbolBytes)
	return f
}

func procedure16(name string, offset, size uint32, kind uint16) []byte {
	// PROCSYM32_16t: type index follows segment, unlike PROCSYM32.
	record := make([]byte, 4+33)
	binary.LittleEndian.PutUint16(record[2:], kind)
	binary.LittleEndian.PutUint32(record[4+12:], size)
	binary.LittleEndian.PutUint32(record[4+24:], offset)
	binary.LittleEndian.PutUint16(record[4+28:], 1)
	binary.LittleEndian.PutUint16(record[4+30:], 0x1001)
	record = append(record, byte(len(name)))
	record = append(record, name...)
	for len(record)%4 != 0 {
		record = append(record, 0)
	}
	binary.LittleEndian.PutUint16(record, uint16(len(record)-2))
	return record
}

// OldDBIC7SourceLines adds an sstSrcModule ST line table to the C7 fixture.
func OldDBIC7SourceLines() C11Fixture {
	f := OldDBIC7()
	lines := c11SourceLines(0x10, 0x2f, []c11SourceFile{{
		name: `C:\src\old.cc`, start: 0x10, end: 0x2f,
		offsets: []uint32{0x10, 0x10, 0x18, 0x1c}, lines: []uint16{42, 43, 0, 44},
	}})
	data := f.Data
	dbi := f.DBIBlock * 1024
	symbolBytes := binary.LittleEndian.Uint32(data[dbi+24+28:])
	binary.LittleEndian.PutUint32(data[dbi+24+32:], uint32(len(lines)))
	start := f.ModuleBlock*1024 + int(symbolBytes)
	copy(data[start:], lines)
	directory := int(binary.LittleEndian.Uint16(data[60:])) * 1024
	binary.LittleEndian.PutUint32(data[directory+4+4*8:], symbolBytes+uint32(len(lines)))
	return C11Fixture{Fixture: f, C11Start: start, C11Size: len(lines)}
}
