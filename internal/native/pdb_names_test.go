package native

import (
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativepdb"
)

func TestPDBGlobalEmptyHashEntryKeepsSourceLines(t *testing.T) {
	fixture := nativepdb.WithGlobalEmptyHashEntry()
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
		want   bool
	}{
		{"empty hash string", func([]byte) {}, true},
		{"empty source filename", func(data []byte) {
			base := fixture.NamesBlock * 512
			empty := binary.LittleEndian.Uint32(data[base+8:]) - 1
			binary.LittleEndian.PutUint32(data[fixture.C13Start+8+48+8:], empty)
		}, false},
		{"invalid hash offset", func(data []byte) {
			base := fixture.NamesBlock * 512
			size := binary.LittleEndian.Uint32(data[base+8:])
			binary.LittleEndian.PutUint32(data[base+12+int(size)+4:], size)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), fixture.Data...)
			tc.mutate(data)
			objects := ParseDebugObjects("fixture.pdb", data)
			if !tc.want {
				if len(objects) != 0 {
					t.Fatal("invalid filename or hash emitted symbols")
				}
				return
			}
			if len(objects) != 1 {
				t.Fatal("empty hash string rejected valid PDB")
			}
			found := false
			for _, f := range objects[0].Functions {
				if f.Name == "CrashFunction" && f.File == `C:\src\crash.cc` && f.Line == 42 {
					found = true
				}
			}
			if !found {
				t.Fatal("source line missing")
			}
		})
	}
}
