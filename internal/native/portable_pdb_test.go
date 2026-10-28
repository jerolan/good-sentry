package native

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func TestPortablePDBCompilerSequencePoints(t *testing.T) {
	data, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	pdb, ok := ParsePortablePDB(data)
	if !ok {
		t.Fatal("compiler-produced Portable PDB was rejected")
	}
	if pdb.DebugID != "c6f075cc44074e01aeee5d0399a7d7d1b08a78f8" {
		t.Fatalf("Portable PDB identity %q does not match PE GUID and stamp", pdb.DebugID)
	}
	for _, test := range []struct {
		method  uint32
		offsets []uint32
		lines   []int
		files   []string
		hidden  []bool
	}{
		{1, []uint32{0, 1, 5, 9, 15}, []int{5, 100, 0, 200, 13}, []string{"/Program.cs", "/alpha.cs", "/alpha.cs", "/beta.cs", "/Program.cs"}, []bool{false, false, true, false, false}},
		{2, []uint32{0}, []int{14}, []string{"/Program.cs"}, []bool{false}},
	} {
		points := pdb.Methods[test.method]
		if len(points) != len(test.offsets) {
			t.Fatalf("method %d: got %d points, want %d", test.method, len(points), len(test.offsets))
		}
		for i, point := range points {
			if point.Offset != test.offsets[i] || point.Line != test.lines[i] || point.Hidden != test.hidden[i] || !strings.HasSuffix(point.Document, test.files[i]) {
				t.Errorf("method %d point %d = %+v", test.method, i, point)
			}
		}
	}
}

func TestPortablePDBRejectsMalformedMetadata(t *testing.T) {
	original, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(f func([]byte)) []byte { data := append([]byte(nil), original...); f(data); return data }
	oversized := make([]byte, maxPDBBytes+1)
	copy(oversized, original)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"truncated metadata root", original[:20]},
		{"truncated file", original[:len(original)-1]},
		{"bad signature", mutate(func(d []byte) { d[0] ^= 1 })},
		{"unterminated version", mutate(func(d []byte) {
			for i := 0x10; i < 0x1c; i++ {
				d[i] = 'x'
			}
		})},
		{"PDB stream beyond file", mutate(func(d []byte) { binary.LittleEndian.PutUint32(d[0x20:], 0xfffffff0) })},
		{"PDB stream truncated", mutate(func(d []byte) { binary.LittleEndian.PutUint32(d[0x24:], 8) })},
		{"table stream beyond file", mutate(func(d []byte) { binary.LittleEndian.PutUint32(d[0x30:], 0xfffffff0) })},
		{"overlapping streams", mutate(func(d []byte) { binary.LittleEndian.PutUint32(d[0x30:], 0x7c) })},
		{"document row budget", mutate(func(d []byte) { binary.LittleEndian.PutUint32(d[0xdc:], 1_000_001) })},
		{"method count mismatch", mutate(func(d []byte) { binary.LittleEndian.PutUint32(d[0xe0:], 3) })},
		{"invalid document name blob", mutate(func(d []byte) { binary.LittleEndian.PutUint16(d[0xf4:], 0xffff) })},
		{"duplicate document name", mutate(func(d []byte) { copy(d[0xfc:0xfe], d[0xf4:0xf6]) })},
		{"invalid document index", mutate(func(d []byte) { binary.LittleEndian.PutUint16(d[0x124:], 0xffff) })},
		{"invalid sequence blob", mutate(func(d []byte) { binary.LittleEndian.PutUint16(d[0x126:], 0xffff) })},
		{"invalid compressed integer", mutate(func(d []byte) { d[0x2a15] = 0xff })},
		{"oversized input", oversized},
	} {
		t.Run(test.name, func(t *testing.T) {
			if parsed, ok := ParsePortablePDB(test.data); ok || parsed != nil {
				t.Fatal("malformed Portable PDB returned mappings")
			}
		})
	}
}

func TestPortablePDBCannotFallBackToBreakpadText(t *testing.T) {
	data, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nFUNC 1000 20 0 UnexpectedNativeSymbol\n")...)
	if objects := ParseDebugObjects("portable.pdb", data); len(objects) != 0 {
		t.Fatalf("Portable PDB emitted %d native symbol objects", len(objects))
	}
}

func TestPortablePDBLookupMatchesSentryCache(t *testing.T) {
	data, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	pdb, ok := ParsePortablePDB(data)
	if !ok {
		t.Fatal("fixture rejected")
	}
	for _, test := range []struct {
		method, offset uint32
		path           string
		line           int
		want           bool
	}{
		{1, 0, "/Program.cs", 5, true}, {1, 1, "/alpha.cs", 100, true},
		{1, 5, "/alpha.cs", 100, true}, {1, 8, "/alpha.cs", 100, true},
		{1, 9, "/beta.cs", 200, true}, {1, 15, "/Program.cs", 13, true},
		{1, ^uint32(0), "/Program.cs", 13, true}, {2, 0, "/Program.cs", 14, true},
		{0, 0, "", 0, false}, {3, 0, "", 0, false},
	} {
		point, found := pdb.Lookup(test.method, test.offset)
		if found != test.want || found && (point.Line != test.line || !strings.HasSuffix(point.Document, test.path)) {
			t.Errorf("lookup method=%d offset=%d: point=%+v found=%t", test.method, test.offset, point, found)
		}
	}
	late := append([]byte(nil), data...)
	late[0x2a17] = 3 // First sequence point's initial IL offset.
	latePDB, ok := ParsePortablePDB(late)
	if !ok {
		t.Fatal("late-start fixture rejected")
	}
	if _, found := latePDB.Lookup(1, 2); found {
		t.Fatal("offset before first visible point resolved")
	}
	if point, found := latePDB.Lookup(1, 3); !found || point.Line != 5 {
		t.Fatalf("first late point=%+v found=%t", point, found)
	}
}
