package spanarchive

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/parquet-go/parquet-go"
)

const (
	MaxRows        = 250
	MaxObjectBytes = 64 << 20
)

var ErrObjectTooLarge = errors.New("span archive object exceeds 64 MiB")

// Span is the lossless row projection stored in cold Parquet objects. Times use
// PostgreSQL's microsecond precision explicitly instead of relying on a Parquet
// timestamp conversion that could change units between readers.
type Span struct {
	ID             int64   `parquet:"id"`
	ProjectID      int64   `parquet:"project_id"`
	TraceID        string  `parquet:"trace_id"`
	SpanID         string  `parquet:"span_id"`
	ParentSpanID   string  `parquet:"parent_span_id"`
	Name           string  `parquet:"name"`
	Status         string  `parquet:"status"`
	IsSegment      bool    `parquet:"is_segment"`
	StartTimestamp int64   `parquet:"start_timestamp_us"`
	EndTimestamp   int64   `parquet:"end_timestamp_us"`
	Duration       float64 `parquet:"duration_ms"`
	Op             string  `parquet:"op"`
	Environment    string  `parquet:"environment"`
	Release        string  `parquet:"release"`
	Attributes     []byte  `parquet:"attributes_json"`
	Data           []byte  `parquet:"data_json"`
	CreatedAt      int64   `parquet:"created_at_us"`
}

type boundedBuffer struct {
	bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxObjectBytes-b.Len() {
		return 0, ErrObjectTooLarge
	}
	return b.Buffer.Write(p)
}

// Encode sorts by immutable database ID so retries with the same cohort yield
// the same Parquet row order and deterministic object bytes.
func Encode(rows []Span) ([]byte, error) {
	if len(rows) == 0 || len(rows) > MaxRows {
		return nil, fmt.Errorf("span archive row count must be between 1 and %d", MaxRows)
	}
	ordered := append([]Span(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	inputBytes := 0
	for i, row := range ordered {
		if row.ID <= 0 || row.ProjectID <= 0 {
			return nil, errors.New("span archive rows require positive row and project IDs")
		}
		if i > 0 && ordered[i-1].ProjectID != row.ProjectID {
			return nil, errors.New("span archive rows must belong to one project")
		}
		if i > 0 && ordered[i-1].ID == row.ID {
			return nil, errors.New("span archive rows contain a duplicate ID")
		}
		inputBytes += len(row.TraceID) + len(row.SpanID) + len(row.ParentSpanID) + len(row.Name) +
			len(row.Status) + len(row.Op) + len(row.Environment) + len(row.Release) + len(row.Attributes) + len(row.Data)
		if inputBytes > MaxObjectBytes {
			return nil, ErrObjectTooLarge
		}
	}
	var output boundedBuffer
	writer := parquet.NewGenericWriter[Span](&output)
	if _, err := writer.Write(ordered); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("encode span archive Parquet rows: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish span archive Parquet object: %w", err)
	}
	return append([]byte(nil), output.Bytes()...), nil
}

// Decode rejects oversized and overfull objects before allocating rows. It
// reads in bounded chunks and preserves JSON payloads as their original bytes.
func Decode(data []byte) ([]Span, error) {
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return nil, errors.New("span archive object size must be between 1 byte and 64 MiB")
	}
	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open span archive Parquet object: %w", err)
	}
	rowCount := file.NumRows()
	if rowCount < 1 || rowCount > MaxRows {
		return nil, fmt.Errorf("span archive row count must be between 1 and %d", MaxRows)
	}
	return decodeRows(data, int(rowCount))
}

func decodeRows(data []byte, rowCount int) (rows []Span, err error) {
	// parquet-go's generic reader constructor panics when the file schema cannot
	// be represented by T. Archive files can be truncated or externally damaged,
	// so turn that malformed-object panic into a normal read failure.
	defer func() {
		if recovered := recover(); recovered != nil {
			rows = nil
			err = fmt.Errorf("decode span archive Parquet object: %v", recovered)
		}
	}()
	reader := parquet.NewGenericReader[Span](bytes.NewReader(data))
	defer reader.Close()
	rows = make([]Span, rowCount)
	read, err := reader.Read(rows)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode span archive Parquet rows: %w", err)
	}
	if read != len(rows) {
		return nil, errors.New("span archive Parquet row count does not match its footer")
	}
	projectID := rows[0].ProjectID
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row.ID <= 0 || row.ProjectID <= 0 {
			return nil, errors.New("span archive contains an invalid row identity")
		}
		if row.ProjectID != projectID {
			return nil, errors.New("span archive rows must belong to one project")
		}
		if _, exists := seen[row.ID]; exists {
			return nil, errors.New("span archive contains a duplicate row ID")
		}
		seen[row.ID] = struct{}{}
	}
	return rows, nil
}
