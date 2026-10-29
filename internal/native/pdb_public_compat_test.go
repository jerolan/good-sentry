package native

import (
	"encoding/binary"
	"goodsentry/internal/testfixture/nativepdb"
	"testing"
)

func TestModernPublicDirectSelectorAndOpaquePadding(t *testing.T) {
	f := nativepdb.WithPublicOnlySymbols()
	for _, tc := range []struct {
		name              string
		selector, padding bool
		first, second     uint64
	}{
		{"direct selector", true, false, 0x1010, 0x1030},
		{"opaque padding", false, true, 0x1030, 0x1050},
		{"both", true, true, 0x1010, 0x1030},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), f.Data...)
			if tc.selector {
				entry := data[f.SectionMapStart+4:]
				binary.LittleEndian.PutUint16(entry, binary.LittleEndian.Uint16(entry)|0x100)
				binary.LittleEndian.PutUint32(entry[12:], 0)
			}
			if tc.padding {
				data[f.PublicSymbolsBlock*512+26] = 0x55
			}
			objects := ParseDebugObjects("symbols.pdb", data)
			if len(objects) != 1 {
				t.Fatal("valid public symbols rejected")
			}
			var first, second bool
			for _, symbol := range objects[0].Functions {
				first = first || symbol.Name == "PublicFirst" && symbol.Start == tc.first
				second = second || symbol.Name == "PublicSecond" && symbol.Start == tc.second
			}
			if !first || !second {
				t.Fatalf("public names and addresses missing: first=%v second=%v", first, second)
			}
		})
	}
}
