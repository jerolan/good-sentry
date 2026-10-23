package spanarchive

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestParquetRoundTripPreservesSpanRows(t *testing.T) {
	rows := []Span{
		{ID: 42, ProjectID: 9, TraceID: "trace-2", SpanID: "span-2", ParentSpanID: "parent", Name: "child", Status: "error", IsSegment: false,
			StartTimestamp: 1_700_000_000_123_456, EndTimestamp: 1_700_000_000_123_789, Duration: 0.333, Op: "db", Environment: "qa", Release: "app@2",
			Attributes: []byte(`{"precision":"\u00e9"}`), Data: []byte{'{', '"', 'x', '"', ':', 0, 0xff, '}'}, CreatedAt: 1_700_000_100_654_321},
		{ID: 41, ProjectID: 9, TraceID: "trace-1", SpanID: "span-1", Name: "root", Status: "ok", IsSegment: true,
			StartTimestamp: 1_699_999_999_000_001, EndTimestamp: 1_700_000_000_000_001, Duration: 1000, Attributes: []byte(`{}`), Data: []byte(`{"name":"root"}`), CreatedAt: 1_700_000_100_000_001},
	}

	encoded, err := Encode(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 8 || !bytes.Equal(encoded[:4], []byte("PAR1")) || !bytes.Equal(encoded[len(encoded)-4:], []byte("PAR1")) {
		t.Fatalf("encoded data is not a Parquet file: %q", encoded[:min(4, len(encoded))])
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	want := []Span{rows[1], rows[0]}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded spans differ from the original rows:\n got %#v\nwant %#v", decoded, want)
	}
	encodedAgain, err := Encode(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, encodedAgain) {
		t.Fatal("encoding the same cohort produced different object bytes")
	}
}

func TestParquetRejectsInvalidOrOversizedCohorts(t *testing.T) {
	valid := Span{ID: 1, ProjectID: 2, Attributes: []byte(`{}`), Data: []byte(`{}`)}
	for _, tc := range []struct {
		name string
		rows []Span
	}{
		{name: "empty"},
		{name: "over row limit", rows: makeRows(MaxRows + 1)},
		{name: "duplicate identity", rows: []Span{valid, valid}},
		{name: "mixed projects", rows: []Span{{ID: 1, ProjectID: 2}, {ID: 2, ProjectID: 3}}},
		{name: "non-positive identity", rows: []Span{{ID: 1, ProjectID: 0}}},
		{name: "over size", rows: []Span{{ID: 1, ProjectID: 2, Data: make([]byte, MaxObjectBytes+1)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Encode(tc.rows); err == nil {
				t.Fatal("invalid cohort was encoded")
			} else if tc.name == "over size" && !errors.Is(err, ErrObjectTooLarge) {
				t.Fatalf("oversized cohort error=%v", err)
			}
		})
	}
	if _, err := Decode(make([]byte, MaxObjectBytes+1)); err == nil {
		t.Fatal("oversized Parquet object was accepted")
	}
	if _, err := Decode([]byte("not a parquet file")); err == nil {
		t.Fatal("corrupt Parquet input was accepted")
	}
	var overfull bytes.Buffer
	writer := parquet.NewGenericWriter[Span](&overfull)
	if _, err := writer.Write(makeRows(MaxRows + 1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(overfull.Bytes()); err == nil {
		t.Fatal("Parquet object exceeding the row limit was accepted")
	}
}

func makeRows(count int) []Span {
	rows := make([]Span, count)
	for i := range rows {
		rows[i] = Span{ID: int64(i + 1), ProjectID: 1, Attributes: []byte(`{}`), Data: []byte(`{}`)}
	}
	return rows
}
