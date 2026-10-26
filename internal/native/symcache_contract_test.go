package native

import (
	"encoding/binary"
	"testing"

	"goodsentry/internal/testfixture/nativesymcache"
)

func TestSymCacheRecordCountBounds(t *testing.T) {
	for _, field := range []int{40, 44, 48} {
		fixture := nativesymcache.Build(9, 0)
		data := append([]byte(nil), fixture.Data...)
		binary.LittleEndian.PutUint32(data[8+field:], maxSymCacheEntries+1)
		if objects := ParseDebugObjects("over-budget.symcache", data); len(objects) != 0 {
			t.Fatalf("header field %d above the entry budget returned objects: %+v", field, objects)
		}
	}
}

func TestSymCacheOversizedFile(t *testing.T) {
	if _, _, ok := parseSymCache(make([]byte, maxSymCacheBytes+1)); ok {
		t.Fatal("a SymCache above 64MiB was accepted")
	}
}
