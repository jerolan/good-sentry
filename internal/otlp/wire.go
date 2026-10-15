// Package otlp implements the native OTLP/HTTP log boundary.
package otlp

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

var (
	ErrMalformed = errors.New("malformed OTLP logs")
	ErrTooLarge  = errors.New("OTLP admission limit exceeded")
	ErrEncoding  = errors.New("unsupported OTLP content encoding")
)

type Encoding uint8

const (
	Protobuf Encoding = iota
	JSON
	maxDepth = 64
)

// Limits apply before allocating the official decoded message.
type Limits struct {
	MaxBytes   int64
	MaxRecords int
	MaxNodes   int
}

// Decode returns known-schema log data, preserving record order and values.
// It does not authorize, persist, deduplicate or acknowledge a request.
func Decode(body io.Reader, encoding Encoding, contentEncoding string, limits Limits) (*logspb.LogsData, error) {
	if body == nil || limits.MaxBytes < 1 || limits.MaxBytes == math.MaxInt64 || limits.MaxRecords < 1 || limits.MaxNodes < 1 {
		return nil, ErrMalformed
	}
	compression := strings.ToLower(strings.TrimSpace(contentEncoding))
	if compression != "" && compression != "identity" && compression != "gzip" {
		return nil, ErrEncoding
	}
	if encoding != Protobuf && encoding != JSON {
		return nil, ErrEncoding
	}
	raw, err := readBounded(body, limits.MaxBytes)
	if err != nil {
		return nil, err
	}
	if compression == "gzip" {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, ErrMalformed
		}
		raw, err = readBounded(reader, limits.MaxBytes)
		closeErr := reader.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, ErrMalformed
		}
	}
	budget := admission{limits: limits}
	var data *logspb.LogsData
	if encoding == JSON {
		if !utf8.Valid(raw) {
			return nil, ErrMalformed
		}
		if err := budget.json(raw); err != nil {
			return nil, err
		}
		data, err = decodeJSON(raw)
	} else {
		data = new(logspb.LogsData)
		if _, err := budget.wire(raw, data.ProtoReflect().Descriptor(), 1, 0); err != nil {
			return nil, err
		}
		err = (proto.UnmarshalOptions{DiscardUnknown: true, RecursionLimit: maxDepth}).Unmarshal(raw, data)
	}
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return nil, ErrTooLarge
		}
		return nil, ErrMalformed
	}
	if int64(proto.Size(data)) > limits.MaxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if int64(len(raw)) > maximum {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, ErrMalformed
	}
	return raw, nil
}
