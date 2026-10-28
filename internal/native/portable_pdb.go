package native

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxPortableRows = 1_000_000

// PortableSequencePoint identifies a source location within a managed method.
type PortableSequencePoint struct {
	Offset   uint32
	Document string
	Line     int
	Hidden   bool
}

// PortablePDB contains managed method sequence points, not native addresses.
// Treat Methods and its slices as read-only after parsing.
type PortablePDB struct {
	DebugID string
	Methods map[uint32][]PortableSequencePoint
	visible map[uint32][]uint32
}

// Lookup follows Sentry's Portable PDB cache: hidden points do not stop the
// previous visible point, and points never cross method boundaries.
func (p *PortablePDB) Lookup(method, offset uint32) (PortableSequencePoint, bool) {
	if p == nil {
		return PortableSequencePoint{}, false
	}
	points, visible := p.Methods[method], p.visible[method]
	if len(visible) > 0 && int(visible[len(visible)-1]) >= len(points) {
		return PortableSequencePoint{}, false
	}
	i := sort.Search(len(visible), func(i int) bool { return points[visible[i]].Offset > offset })
	if i == 0 {
		return PortableSequencePoint{}, false
	}
	return points[visible[i-1]], true
}

type portableCursor struct {
	data []byte
	pos  int
}

func (c *portableCursor) take(n int) ([]byte, bool) {
	if n < 0 || n > len(c.data)-c.pos {
		return nil, false
	}
	out := c.data[c.pos : c.pos+n]
	c.pos += n
	return out, true
}
func (c *portableCursor) u32() (uint32, bool) {
	b, ok := c.take(4)
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b), true
}
func (c *portableCursor) index(size int) (uint32, bool) {
	b, ok := c.take(size)
	if !ok {
		return 0, false
	}
	if size == 2 {
		return uint32(binary.LittleEndian.Uint16(b)), true
	}
	return binary.LittleEndian.Uint32(b), true
}

// compressed reads ECMA-335 compressed integers and returns their encoded width.
func (c *portableCursor) compressed() (uint32, int, bool) {
	first, ok := c.take(1)
	if !ok {
		return 0, 0, false
	}
	b := first[0]
	switch {
	case b&0x80 == 0:
		return uint32(b), 1, true
	case b&0xc0 == 0x80:
		tail, ok := c.take(1)
		if !ok {
			return 0, 0, false
		}
		return uint32(b&0x3f)<<8 | uint32(tail[0]), 2, true
	case b&0xe0 == 0xc0:
		tail, ok := c.take(3)
		if !ok {
			return 0, 0, false
		}
		return uint32(b&0x1f)<<24 | uint32(tail[0])<<16 | uint32(tail[1])<<8 | uint32(tail[2]), 4, true
	default:
		return 0, 0, false
	}
}
func (c *portableCursor) unsigned() (uint32, bool) { n, _, ok := c.compressed(); return n, ok }
func (c *portableCursor) signed() (int32, bool) {
	n, width, ok := c.compressed()
	if !ok {
		return 0, false
	}
	value := int32(n >> 1)
	if n&1 != 0 {
		switch width {
		case 1:
			value |= ^int32(0x3f)
		case 2:
			value |= ^int32(0x1fff)
		case 4:
			value |= ^int32(0x0fffffff)
		}
	}
	return value, true
}

type portableExtent struct{ start, end uint64 }

func portableStreams(data []byte) (map[string][]byte, bool) {
	if len(data) < 20 || len(data) > maxPDBBytes || !bytes.Equal(data[:4], []byte("BSJB")) {
		return nil, false
	}
	versionSize := uint64(binary.LittleEndian.Uint32(data[12:]))
	pos := uint64(16) + versionSize
	if versionSize == 0 || pos > uint64(len(data)) || pos%4 != 0 || pos+4 > uint64(len(data)) || bytes.IndexByte(data[16:pos], 0) < 0 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint16(data[pos+2:]))
	pos += 4
	if count == 0 || count > 64 {
		return nil, false
	}
	type stream struct {
		name   string
		extent portableExtent
	}
	entries := make([]stream, 0, count)
	names := map[string]bool{}
	for i := 0; i < count; i++ {
		if pos+8 > uint64(len(data)) {
			return nil, false
		}
		start := uint64(binary.LittleEndian.Uint32(data[pos:]))
		size := uint64(binary.LittleEndian.Uint32(data[pos+4:]))
		pos += 8
		limit := min(uint64(len(data)), pos+32)
		endName := bytes.IndexByte(data[pos:limit], 0)
		if endName < 0 {
			return nil, false
		}
		name := string(data[pos : pos+uint64(endName)])
		if name == "" || names[name] || !utf8.ValidString(name) {
			return nil, false
		}
		names[name] = true
		pos = (pos + uint64(endName) + 1 + 3) &^ 3
		if pos > uint64(len(data)) || start > uint64(len(data)) || size > uint64(len(data))-start {
			return nil, false
		}
		entries = append(entries, stream{name, portableExtent{start, start + size}})
	}
	streams := make(map[string][]byte, count)
	for i, entry := range entries {
		if entry.extent.start < pos {
			return nil, false
		}
		for _, previous := range entries[:i] {
			if entry.extent.start < entry.extent.end && previous.extent.start < previous.extent.end && entry.extent.start < previous.extent.end && previous.extent.start < entry.extent.end {
				return nil, false
			}
		}
		streams[entry.name] = data[entry.extent.start:entry.extent.end]
	}
	return streams, true
}

func portableBlob(heap []byte, index uint32) ([]byte, bool) {
	if index == 0 {
		return []byte{}, true
	}
	if uint64(index) >= uint64(len(heap)) {
		return nil, false
	}
	cursor := portableCursor{data: heap, pos: int(index)}
	length, ok := cursor.unsigned()
	if !ok || uint64(length) > uint64(len(heap)-cursor.pos) {
		return nil, false
	}
	return cursor.take(int(length))
}

func portableDocumentName(heap []byte, index uint32, work *int) (string, bool) {
	if index == 0 {
		return "", false
	}
	blob, ok := portableBlob(heap, index)
	if !ok || len(blob) == 0 {
		return "", false
	}
	if len(blob) > maxPDBBytes-*work {
		return "", false
	}
	*work += len(blob)
	sepRune, sepSize := utf8.DecodeRune(blob)
	if sepRune == utf8.RuneError && sepSize == 1 {
		return "", false
	}
	sep := string(blob[:sepSize])
	if sepRune == 0 {
		sep = ""
	}
	cursor := portableCursor{data: blob, pos: sepSize}
	var name strings.Builder
	parts := 0
	for cursor.pos < len(blob) {
		if parts >= maxPortableRows {
			return "", false
		}
		idx, ok := cursor.unsigned()
		if !ok {
			return "", false
		}
		part, ok := portableBlob(heap, idx)
		if !ok || !utf8.Valid(part) || bytes.IndexByte(part, 0) >= 0 {
			return "", false
		}
		extra := len(part)
		if parts > 0 {
			extra += len(sep)
		}
		if extra > maxPDBProcedureName-name.Len() || extra > maxPDBBytes-*work {
			return "", false
		}
		*work += extra
		if parts > 0 {
			name.WriteString(sep)
		}
		name.Write(part)
		parts++
	}
	if parts == 0 {
		return "", false
	}
	return name.String(), true
}

// ParsePortablePDB decodes source points from a standalone Portable PDB. The
// method row and IL offset remain separate; neither is a native address.
func ParsePortablePDB(data []byte) (*PortablePDB, bool) {
	streams, ok := portableStreams(data)
	if !ok {
		return nil, false
	}
	pdb, hasPDB := streams["#Pdb"]
	tables, hasTables := streams["#~"]
	heap, hasBlob := streams["#Blob"]
	if !hasPDB || !hasTables || !hasBlob || len(pdb) < 32 || len(tables) < 24 {
		return nil, false
	}
	id := fmt.Sprintf("%08x%04x%04x%x%x", binary.LittleEndian.Uint32(pdb), binary.LittleEndian.Uint16(pdb[4:]), binary.LittleEndian.Uint16(pdb[6:]), pdb[8:16], binary.LittleEndian.Uint32(pdb[16:]))
	external := binary.LittleEndian.Uint64(pdb[24:])
	if external>>48 != 0 || uint64(32+bits.OnesCount64(external)*4) > uint64(len(pdb)) {
		return nil, false
	}
	var rows [64]uint32
	cursor := portableCursor{data: pdb, pos: 32}
	for i := 0; i < 48; i++ {
		if external&(uint64(1)<<i) != 0 {
			n, ok := cursor.u32()
			if !ok || n > maxPortableRows {
				return nil, false
			}
			rows[i] = n
		}
	}
	if cursor.pos != len(pdb) {
		return nil, false
	}
	mask := binary.LittleEndian.Uint64(tables[8:])
	const debugMask uint64 = uint64(0xff) << 48
	if mask & ^debugMask != 0 {
		return nil, false
	}
	tc := portableCursor{data: tables, pos: 24}
	total := uint64(0)
	for i := 48; i < 56; i++ {
		if mask&(uint64(1)<<i) != 0 {
			n, ok := tc.u32()
			if !ok {
				return nil, false
			}
			total += uint64(n)
			if total > maxPortableRows {
				return nil, false
			}
			rows[i] = n
		}
	}
	if rows[49] != 0 && rows[49] != rows[6] {
		return nil, false
	}
	heapFlags := tables[6]
	if heapFlags & ^byte(7) != 0 {
		return nil, false
	}
	width := func(flag byte) int {
		if heapFlags&flag != 0 {
			return 4
		}
		return 2
	}
	stringWidth, guidWidth, blobWidth := width(1), width(2), width(4)
	indexWidth := func(table int) int {
		if rows[table] >= 1<<16 {
			return 4
		}
		return 2
	}
	codedWidth := 2
	for _, table := range []int{6, 4, 1, 2, 8, 9, 10, 0, 14, 23, 20, 17, 26, 27, 32, 35, 38, 39, 40, 42, 44, 43, 48, 50, 51, 52, 53} {
		if rows[table] >= 1<<11 {
			codedWidth = 4
			break
		}
	}
	sizes := [8]int{
		2*blobWidth + 2*guidWidth,
		indexWidth(48) + blobWidth,
		indexWidth(6) + indexWidth(53) + indexWidth(51) + indexWidth(52) + 8,
		4 + stringWidth,
		stringWidth + blobWidth,
		indexWidth(53) + blobWidth,
		2 * indexWidth(6),
		codedWidth + guidWidth + blobWidth,
	}
	starts := [8]int{}
	for i := 0; i < 8; i++ {
		starts[i] = tc.pos
		length := uint64(rows[48+i]) * uint64(sizes[i])
		if length > uint64(len(tables)-tc.pos) {
			return nil, false
		}
		tc.pos += int(length)
	}
	// Producers may append a reserved zero word and alignment padding.
	if len(tables)-tc.pos > 7 {
		return nil, false
	}
	for _, b := range tables[tc.pos:] {
		if b != 0 {
			return nil, false
		}
	}
	guidHeap := streams["#GUID"]
	if len(guidHeap)%16 != 0 {
		return nil, false
	}
	documents := make([]string, rows[48]+1)
	dc := portableCursor{data: tables, pos: starts[0]}
	nameWork := 0
	seenDocuments := make(map[string]bool)
	for i := uint32(1); i <= rows[48]; i++ {
		name, ok := dc.index(blobWidth)
		if !ok {
			return nil, false
		}
		hashAlgorithm, ok := dc.index(guidWidth)
		if !ok {
			return nil, false
		}
		hash, ok := dc.index(blobWidth)
		if !ok {
			return nil, false
		}
		language, ok := dc.index(guidWidth)
		if !ok {
			return nil, false
		}
		if uint64(hashAlgorithm)*16 > uint64(len(guidHeap)) || uint64(language)*16 > uint64(len(guidHeap)) {
			return nil, false
		}
		if _, ok = portableBlob(heap, hash); !ok {
			return nil, false
		}
		documents[i], ok = portableDocumentName(heap, name, &nameWork)
		if !ok || seenDocuments[documents[i]] {
			return nil, false
		}
		seenDocuments[documents[i]] = true
	}
	result := &PortablePDB{DebugID: id, Methods: make(map[uint32][]PortableSequencePoint), visible: make(map[uint32][]uint32)}
	mc := portableCursor{data: tables, pos: starts[1]}
	points := 0
	for i := uint32(1); i <= rows[49]; i++ {
		document, ok := mc.index(indexWidth(48))
		if !ok || document > rows[48] {
			return nil, false
		}
		blobIndex, ok := mc.index(blobWidth)
		if !ok {
			return nil, false
		}
		if blobIndex == 0 {
			continue
		}
		blob, ok := portableBlob(heap, blobIndex)
		if !ok {
			return nil, false
		}
		decoded, ok := portableSequencePoints(blob, document, documents, &points, rows[17])
		if !ok {
			return nil, false
		}
		if len(decoded) > 0 {
			result.Methods[i] = decoded
			for index, point := range decoded {
				if !point.Hidden {
					result.visible[i] = append(result.visible[i], uint32(index))
				}
			}
		}
	}
	return result, true
}

func portableSequencePoints(blob []byte, document uint32, documents []string, budget *int, standaloneSignatures uint32) ([]PortableSequencePoint, bool) {
	cursor := portableCursor{data: blob}
	localSignature, ok := cursor.unsigned()
	if !ok || localSignature > standaloneSignatures {
		return nil, false
	}
	if document == 0 {
		document, ok = cursor.unsigned()
		if !ok || document == 0 || int(document) >= len(documents) {
			return nil, false
		}
	}
	var result []PortableSequencePoint
	var offset uint64
	var previousLine, previousColumn int64
	hasPoint, hasVisible := false, false
	for cursor.pos < len(blob) {
		if *budget >= maxPortableRows {
			return nil, false
		}
		*budget++
		delta, ok := cursor.unsigned()
		if !ok {
			return nil, false
		}
		if hasPoint && delta == 0 {
			document, ok = cursor.unsigned()
			if !ok || document == 0 || int(document) >= len(documents) {
				return nil, false
			}
			continue
		}
		offset += uint64(delta)
		if offset >= 0x20000000 {
			return nil, false
		}
		deltaLines, ok := cursor.unsigned()
		if !ok {
			return nil, false
		}
		var deltaColumns int64
		if deltaLines == 0 {
			x, ok := cursor.unsigned()
			if !ok {
				return nil, false
			}
			deltaColumns = int64(x)
		} else {
			x, ok := cursor.signed()
			if !ok {
				return nil, false
			}
			deltaColumns = int64(x)
		}
		if deltaLines == 0 && deltaColumns == 0 {
			result = append(result, PortableSequencePoint{Offset: uint32(offset), Document: documents[document], Hidden: true})
			hasPoint = true
			continue
		}
		var line, column int64
		if !hasVisible {
			l, ok := cursor.unsigned()
			if !ok {
				return nil, false
			}
			c, ok := cursor.unsigned()
			if !ok {
				return nil, false
			}
			line, column = int64(l), int64(c)
		} else {
			l, ok := cursor.signed()
			if !ok {
				return nil, false
			}
			c, ok := cursor.signed()
			if !ok {
				return nil, false
			}
			line, column = previousLine+int64(l), previousColumn+int64(c)
		}
		endLine, endColumn := line+int64(deltaLines), column+deltaColumns
		if line < 0 || line >= 0x20000000 || line == 0xfeefee || endLine < line || endLine >= 0x20000000 || endLine == 0xfeefee || column < 0 || column >= 0x10000 || endColumn < 0 || endColumn >= 0x10000 || deltaLines == 0 && deltaColumns <= 0 {
			return nil, false
		}
		result = append(result, PortableSequencePoint{Offset: uint32(offset), Document: documents[document], Line: int(line)})
		previousLine, previousColumn = line, column
		hasPoint, hasVisible = true, true
	}
	return result, true
}
