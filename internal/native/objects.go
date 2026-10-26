package native

import (
	"bytes"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"io"
	"sort"
	"strings"
)

const (
	maxDWARFLineRows     = 1 << 18
	maxDWARFLineSpans    = 1 << 18
	maxObjectNoteBytes   = 1 << 20
	elfNoteGNU           = 3
	machoLoadCommandUUID = 0x1b
)

type dwarfLineSpan struct {
	start uint64
	end   uint64
	file  string
	line  int
}

// ParseObjectSymbols recognizes the standard ELF and Mach-O containers used by
// native debug files. Unsupported objects return no symbols rather than error,
// allowing a project to retain PDB and other formats without breaking the
// Minidump request.
func ParseObjectSymbols(name string, data []byte) (string, []SymbolFunction) {
	key := moduleKey(name)
	var functions []SymbolFunction
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		file, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return key, nil
		}
		base := elfImageBase(file)
		if table, err := file.DWARF(); err == nil {
			functions = append(functions, dwarfFunctions(table)...)
		}
		for _, readSymbols := range []func() ([]elf.Symbol, error){file.Symbols, file.DynamicSymbols} {
			list, err := readSymbols()
			if err != nil {
				continue
			}
			for _, symbol := range list {
				if symbol.Value != 0 && symbol.Size != 0 && !strings.Contains(symbol.Name, ".cold") {
					functions = append(functions, SymbolFunction{Start: symbol.Value, End: symbol.Value + symbol.Size, Name: symbol.Name, Line: -1})
				}
			}
		}
		functions = objectFunctions(functions, base)
	} else if len(data) >= 4 && isMachOMagic(data[:4]) {
		file, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			return key, nil
		}
		base := machoImageBase(file)
		if table, err := file.DWARF(); err == nil {
			functions = append(functions, dwarfFunctions(table)...)
		}
		if file.Symtab != nil {
			var tableFunctions []SymbolFunction
			for _, symbol := range file.Symtab.Syms {
				if symbol.Value != 0 {
					tableFunctions = append(tableFunctions, SymbolFunction{Start: symbol.Value, Name: symbol.Name, Line: -1})
				}
			}
			sort.SliceStable(tableFunctions, func(i, j int) bool { return tableFunctions[i].Start < tableFunctions[j].Start })
			nextStart := uint64(0)
			for i := len(tableFunctions) - 1; i >= 0; i-- {
				if i+1 < len(tableFunctions) && tableFunctions[i+1].Start > tableFunctions[i].Start {
					nextStart = tableFunctions[i+1].Start
				}
				tableFunctions[i].End = tableFunctions[i].Start + 1
				if nextStart > tableFunctions[i].Start {
					tableFunctions[i].End = nextStart
				}
			}
			functions = append(functions, tableFunctions...)
		}
		functions = objectFunctions(functions, base)
	}
	if len(functions) == 0 {
		return key, nil
	}
	sort.SliceStable(functions, func(i, j int) bool {
		if functions[i].Start != functions[j].Start {
			return functions[i].Start < functions[j].Start
		}
		return functions[i].End > functions[j].End
	})
	return key, functions
}

// ObjectDebugID returns the identity embedded in an ELF build-id note or a
// Mach-O LC_UUID command. Other formats carry identity in their own parser.
func ObjectDebugID(data []byte) string {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		file, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return ""
		}
		return elfBuildID(file)
	}
	if len(data) >= 4 && isMachOMagic(data[:4]) {
		file, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			return ""
		}
		return machoUUID(file)
	}
	return ""
}

func elfBuildID(file *elf.File) string {
	named := file.Section(".note.gnu.build-id")
	if named != nil {
		if id := elfSectionBuildID(named, file.ByteOrder); id != "" {
			return id
		}
	}
	for _, section := range file.Sections {
		if section == named || section.Type != elf.SHT_NOTE {
			continue
		}
		if id := elfSectionBuildID(section, file.ByteOrder); id != "" {
			return id
		}
	}
	return ""
}

func elfSectionBuildID(section *elf.Section, order binary.ByteOrder) string {
	data, err := section.Data()
	if err != nil || len(data) == 0 || len(data) > maxObjectNoteBytes {
		return ""
	}
	return parseELFBuildIDNote(data, order)
}

func parseELFBuildIDNote(data []byte, order binary.ByteOrder) string {
	for len(data) >= 12 {
		namesz := uint64(order.Uint32(data[:4]))
		descsz := uint64(order.Uint32(data[4:8]))
		kind := order.Uint32(data[8:12])
		nameEnd := 12 + namesz
		descStart := (nameEnd + 3) &^ 3
		descEnd := descStart + descsz
		recordEnd := (descEnd + 3) &^ 3
		if recordEnd > uint64(len(data)) {
			return ""
		}
		name := bytes.TrimRight(data[12:nameEnd], "\x00")
		desc := data[descStart:descEnd]
		if kind == elfNoteGNU && bytes.Equal(name, []byte("GNU")) && len(desc) != 0 {
			return hex.EncodeToString(desc)
		}
		data = data[recordEnd:]
	}
	return ""
}

func machoUUID(file *macho.File) string {
	for _, load := range file.Loads {
		raw := load.Raw()
		if len(raw) >= 24 && file.ByteOrder.Uint32(raw[:4]) == machoLoadCommandUUID {
			return hex.EncodeToString(raw[8:24])
		}
	}
	return ""
}

func elfImageBase(file *elf.File) uint64 {
	var base uint64
	found := false
	for _, program := range file.Progs {
		if program.Type != elf.PT_LOAD || program.Filesz == 0 {
			continue
		}
		if !found || program.Vaddr < base {
			base, found = program.Vaddr, true
		}
	}
	return base
}

func machoImageBase(file *macho.File) uint64 {
	for _, load := range file.Loads {
		if segment, ok := load.(*macho.Segment); ok && segment.Name == "__TEXT" {
			return segment.Addr
		}
	}
	var base uint64
	found := false
	for _, load := range file.Loads {
		segment, ok := load.(*macho.Segment)
		if !ok || segment.Name == "__PAGEZERO" || segment.Memsz == 0 {
			continue
		}
		if !found || segment.Addr < base {
			base, found = segment.Addr, true
		}
	}
	return base
}

// Minidump module addresses are relative to the image load address, while ELF
// and Mach-O symbol tables normally store preferred virtual addresses.
func objectFunctions(functions []SymbolFunction, base uint64) []SymbolFunction {
	result := functions[:0]
	for _, function := range functions {
		if function.Start < base || function.End <= base {
			continue
		}
		function.Start -= base
		function.End -= base
		result = append(result, function)
	}
	return result
}

func isMachOMagic(value []byte) bool {
	if len(value) < 4 {
		return false
	}
	for _, magic := range []uint32{binary.LittleEndian.Uint32(value), binary.BigEndian.Uint32(value)} {
		switch magic {
		case macho.Magic32, macho.Magic64, macho.MagicFat:
			return true
		}
	}
	return false
}

func dwarfFunctions(table *dwarf.Data) []SymbolFunction {
	reader := table.Reader()
	var functions []SymbolFunction
	var lineSpans []dwarfLineSpan
	var sourceSpanCount int
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}
		if entry.Tag == dwarf.TagCompileUnit {
			lineReader, err := table.LineReader(entry)
			if err != nil {
				lineSpans = nil
			} else {
				lineSpans = dwarfLineSpans(lineReader)
			}
			continue
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		name, _ := entry.Val(dwarf.AttrName).(string)
		if name == "" {
			name, _ = entry.Val(dwarf.AttrLinkageName).(string)
		}
		ranges, err := table.Ranges(entry)
		if err != nil || len(ranges) == 0 {
			continue
		}
		for _, r := range ranges {
			if r[0] == 0 || r[1] <= r[0] {
				continue
			}
			functions = append(functions, SymbolFunction{Start: r[0], End: r[1], Name: name, Line: -1})
			if sourceSpanCount >= maxDWARFLineSpans {
				continue
			}
			firstSpan := sort.Search(len(lineSpans), func(i int) bool { return lineSpans[i].end > r[0] })
			for i := firstSpan; i < len(lineSpans) && lineSpans[i].start < r[1]; i++ {
				if sourceSpanCount >= maxDWARFLineSpans {
					break
				}
				span := lineSpans[i]
				start, end := max(r[0], span.start), min(r[1], span.end)
				if start < end {
					functions = append(functions, SymbolFunction{Start: start, End: end, Name: name, File: span.file, Line: span.line})
					sourceSpanCount++
				}
			}
		}
	}
	return functions
}

func dwarfLineSpans(reader *dwarf.LineReader) []dwarfLineSpan {
	var spans []dwarfLineSpan
	var previous dwarf.LineEntry
	hasPrevious := false
	var lastAddress uint64
	hasAddress := false
	rows := 0
	for {
		var entry dwarf.LineEntry
		err := reader.Next(&entry)
		if err == io.EOF {
			if hasPrevious {
				return nil
			}
			return spans
		}
		if err != nil {
			return nil
		}
		rows++
		if rows > maxDWARFLineRows || (hasAddress && entry.Address < lastAddress) {
			return nil
		}
		lastAddress, hasAddress = entry.Address, true
		if hasPrevious && entry.Address > previous.Address && previous.File != nil && previous.Line > 0 {
			spans = append(spans, dwarfLineSpan{start: previous.Address, end: entry.Address, file: previous.File.Name, line: previous.Line})
		}
		if entry.EndSequence {
			hasPrevious = false
			continue
		}
		previous, hasPrevious = entry, true
	}
}
