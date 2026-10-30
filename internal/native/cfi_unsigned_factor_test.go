package native

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestCFIUnsignedFactoredOffsetLimits(t *testing.T) {
	cases := []struct {
		name    string
		factor  uint64
		align   int64
		negate  bool
		want    int64
		wantErr bool
	}{
		{"negative minimum", uint64(1) << 63, -1, false, math.MinInt64, false},
		{"positive overflow", uint64(1) << 63, 1, false, 0, true},
		{"minimum alignment", 1, math.MinInt64, false, math.MinInt64, false},
		{"minimum alignment overflow", 2, math.MinInt64, false, 0, true},
		{"minimum alignment negated overflow", 1, math.MinInt64, true, 0, true},
		{"zero alignment", math.MaxUint64, 0, false, 0, false},
		{"zero alignment negated", math.MaxUint64, 0, true, 0, false},
		{"positive alignment", 3, 7, false, 21, false},
		{"GNU negates positive", 3, 7, true, -21, false},
		{"GNU negates negative", 3, -7, true, 21, false},
		{"GNU negative minimum", uint64(1) << 63, 1, true, math.MinInt64, false},
		{"GNU positive overflow", uint64(1) << 63, -1, true, 0, true},
		{"positive maximum", math.MaxInt64, 1, false, math.MaxInt64, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var encoded [10]byte
			n := binary.PutUvarint(encoded[:], tc.factor)
			cursor := cfiCursor{data: encoded[:n]}
			got, err := readCFIUnsignedFactoredOffset(&cursor, tc.align, tc.negate)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("offset = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCFIUnsignedFactoredOffsetRejectsTruncatedULEB(t *testing.T) {
	cursor := cfiCursor{data: []byte{0x80}}
	if _, err := readCFIUnsignedFactoredOffset(&cursor, -1, false); err == nil {
		t.Fatal("truncated ULEB accepted")
	}
}
