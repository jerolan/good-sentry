package native

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// budgetPDB constructs standalone metadata for aggregate-limit tests. Each
// document has a distinct name while sharing its long component in the heap.
func budgetPDB(identity uint32, documents, points int, longNames bool) []byte {
	compressed := func(n int) []byte {
		switch {
		case n < 0x80:
			return []byte{byte(n)}
		case n < 0x4000:
			return []byte{byte(n>>8) | 0x80, byte(n)}
		default:
			return []byte{byte(n>>24) | 0xc0, byte(n >> 16), byte(n >> 8), byte(n)}
		}
	}
	heap := []byte{0}
	blob := func(data []byte) int {
		index := len(heap)
		heap = append(heap, compressed(len(data))...)
		heap = append(heap, data...)
		return index
	}
	prefix := "source"
	if longNames {
		prefix = string(bytes.Repeat([]byte{'a'}, 4080))
	}
	common := blob([]byte(prefix))
	names := make([]int, documents)
	for i := range names {
		suffix := blob([]byte(fmt.Sprintf("%05d", i)))
		name := append([]byte{'/'}, compressed(common)...)
		name = append(name, compressed(suffix)...)
		names[i] = blob(name)
	}
	// The method's initial document is row one. Additional documents are
	// selected by document records between their visible sequence points.
	sequence := []byte{0, 0, 0, 1, 1, 1}
	for i := 1; i < points; i++ {
		if i < documents {
			sequence = append(sequence, 0)
			sequence = append(sequence, compressed(i+1)...)
		}
		sequence = append(sequence, 1, 0, 1, 0, 0)
	}
	sequenceIndex := blob(sequence)
	pdb := make([]byte, 36)
	binary.LittleEndian.PutUint32(pdb, identity)
	binary.LittleEndian.PutUint32(pdb[16:], 0x12345678)
	binary.LittleEndian.PutUint64(pdb[24:], 1<<6)
	binary.LittleEndian.PutUint32(pdb[32:], 1)
	tables := make([]byte, 32+documents*12+6)
	tables[4], tables[6], tables[7] = 2, 4, 1
	binary.LittleEndian.PutUint64(tables[8:], 1<<48|1<<49)
	binary.LittleEndian.PutUint32(tables[24:], uint32(documents))
	binary.LittleEndian.PutUint32(tables[28:], 1)
	for i, index := range names {
		binary.LittleEndian.PutUint32(tables[32+i*12:], uint32(index))
	}
	method := 32 + documents*12
	binary.LittleEndian.PutUint16(tables[method:], 1)
	binary.LittleEndian.PutUint32(tables[method+2:], uint32(sequenceIndex))
	streams := []struct {
		name string
		data []byte
	}{{"#Pdb", pdb}, {"#~", tables}, {"#Blob", heap}}
	header := make([]byte, 32)
	copy(header, []byte{'B', 'S', 'J', 'B'})
	binary.LittleEndian.PutUint16(header[4:], 1)
	binary.LittleEndian.PutUint16(header[6:], 1)
	binary.LittleEndian.PutUint32(header[12:], 12)
	copy(header[16:], []byte("PDB v1.0\x00"))
	binary.LittleEndian.PutUint16(header[30:], uint16(len(streams)))
	size := len(header)
	for _, stream := range streams {
		size += 8 + (len(stream.name)+1+3)&^3
	}
	for _, stream := range streams {
		entry := make([]byte, 8+(len(stream.name)+1+3)&^3)
		binary.LittleEndian.PutUint32(entry, uint32(size))
		binary.LittleEndian.PutUint32(entry[4:], uint32(len(stream.data)))
		copy(entry[8:], stream.name)
		header = append(header, entry...)
		size += len(stream.data)
	}
	for _, stream := range streams {
		header = append(header, stream.data...)
	}
	return header
}

func TestPortableEventAggregateBudgets(t *testing.T) {
	for _, tc := range []struct {
		name              string
		documents, points int
		longNames         bool
	}{
		{"sequence points", 1, 500001, false},
		{"decoded document names", 8300, 8300, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := budgetPDB(1, tc.documents, tc.points, tc.longNames)
			second := budgetPDB(2, tc.documents, tc.points, tc.longNames)
			id1 := "00000001-0000-0000-0000-000000000000-12345678"
			id2 := "00000002-0000-0000-0000-000000000000-12345678"
			raw := []byte(fmt.Sprintf(`{"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"%s"},{"type":"pe_dotnet","debug_id":"%s"}]},"stacktrace":{"frames":[{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x0"}]}}`, id1, id2))
			file1 := DebugSymbolFile{Name: "first.pdb", Content: first}
			file2 := DebugSymbolFile{Name: "second.pdb", Content: second}
			if _, changed, err := SymbolicatePortableEvent(raw, []DebugSymbolFile{file1}); err != nil || !changed {
				t.Fatalf("first valid file: changed=%v err=%v", changed, err)
			}
			secondFrame := bytes.Replace(raw, []byte("rel:0"), []byte("rel:1"), 1)
			if _, changed, err := SymbolicatePortableEvent(secondFrame, []DebugSymbolFile{file2}); err != nil || !changed {
				t.Fatalf("second valid file: changed=%v err=%v", changed, err)
			}
			got, changed, err := SymbolicatePortableEvent(raw, []DebugSymbolFile{file1, file2})
			if err != nil || changed || !bytes.Equal(got, raw) {
				t.Fatalf("over budget: changed=%v err=%v", changed, err)
			}
		})
	}
}

func TestPortableEventAggregateBudgetsAllowBoundary(t *testing.T) {
	for _, tc := range []struct {
		name              string
		documents, points int
		longNames         bool
	}{
		{"one million points", 1, 500000, false},
		{"names just under 64 MiB", 8212, 8212, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := DebugSymbolFile{Name: "first.pdb", Content: budgetPDB(1, tc.documents, tc.points, tc.longNames)}
			second := DebugSymbolFile{Name: "second.pdb", Content: budgetPDB(2, tc.documents, tc.points, tc.longNames)}
			raw := []byte(`{"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"00000001-0000-0000-0000-000000000000-12345678"},{"type":"pe_dotnet","debug_id":"00000002-0000-0000-0000-000000000000-12345678"}]},"stacktrace":{"frames":[{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x0"},{"addr_mode":"rel:1","function_id":"0x1","instruction_addr":"0x0"}]}}`)
			got, changed, err := SymbolicatePortableEvent(raw, []DebugSymbolFile{first, second})
			if err != nil || !changed || bytes.Count(got, []byte(`"filename"`)) != 2 {
				t.Fatalf("within budget: changed=%v err=%v", changed, err)
			}
		})
	}
}
