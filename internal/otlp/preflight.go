package otlp

import (
	"bytes"
	"encoding/json"
	"io"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type admission struct {
	limits         Limits
	nodes, records int
}

func (b *admission) node(depth int) error {
	if depth > maxDepth || b.nodes >= b.limits.MaxNodes {
		return ErrTooLarge
	}
	b.nodes++
	return nil
}

func (b *admission) record() error {
	if b.records >= b.limits.MaxRecords {
		return ErrTooLarge
	}
	b.records++
	return nil
}

// Walk only declared embedded message fields. Unknown bytes are opaque; an
// unknown wire group still consumes depth and node budget before being skipped.
func (b *admission) wire(raw []byte, descriptor protoreflect.MessageDescriptor, depth int, group protowire.Number) (int, error) {
	if err := b.node(depth); err != nil {
		return 0, err
	}
	offset := 0
	for offset < len(raw) {
		number, kind, n := protowire.ConsumeTag(raw[offset:])
		if n < 0 || !number.IsValid() {
			return 0, ErrMalformed
		}
		offset += n
		if err := b.node(depth); err != nil {
			return 0, err
		}
		if kind == protowire.EndGroupType {
			if group == 0 || number != group {
				return 0, ErrMalformed
			}
			return offset, nil
		}
		var field protoreflect.FieldDescriptor
		if descriptor != nil {
			field = descriptor.Fields().ByNumber(protoreflect.FieldNumber(number))
		}
		switch kind {
		case protowire.VarintType:
			_, n = protowire.ConsumeVarint(raw[offset:])
		case protowire.Fixed32Type:
			_, n = protowire.ConsumeFixed32(raw[offset:])
		case protowire.Fixed64Type:
			_, n = protowire.ConsumeFixed64(raw[offset:])
		case protowire.BytesType:
			value, consumed := protowire.ConsumeBytes(raw[offset:])
			n = consumed
			if n >= 0 && field != nil && field.Kind() == protoreflect.MessageKind {
				if field.Message().FullName() == "opentelemetry.proto.logs.v1.LogRecord" {
					if err := b.record(); err != nil {
						return 0, err
					}
				}
				if _, err := b.wire(value, field.Message(), depth+1, 0); err != nil {
					return 0, err
				}
			}
		case protowire.StartGroupType:
			consumed, err := b.wire(raw[offset:], nil, depth+1, number)
			if err != nil {
				return 0, err
			}
			n = consumed
		default:
			return 0, ErrMalformed
		}
		if n < 0 {
			return 0, ErrMalformed
		}
		offset += n
	}
	if group != 0 {
		return 0, ErrMalformed
	}
	return offset, nil
}

func (b *admission) json(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	// The envelope itself must be an object. Its field values are scanned
	// without materializing maps, slices or the official protobuf messages.
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return ErrMalformed
	}
	descriptor := (&logspb.LogsData{}).ProtoReflect().Descriptor()
	if err := b.jsonValue(decoder, descriptor, false, false, 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrMalformed
	}
	return nil
}

func (b *admission) jsonValue(decoder *json.Decoder, descriptor protoreflect.MessageDescriptor, repeated, record bool, depth int) error {
	if err := b.node(depth); err != nil {
		return err
	}
	if record && !repeated {
		if err := b.record(); err != nil {
			return err
		}
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrMalformed
	}
	switch token {
	case json.Delim('['):
		if !repeated {
			descriptor, record = nil, false
		}
		for decoder.More() {
			if err := b.jsonValue(decoder, descriptor, false, record, depth+1); err != nil {
				return err
			}
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim(']') {
			return ErrMalformed
		}
	case json.Delim('{'):
		if repeated {
			descriptor = nil
		}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return ErrMalformed
			}
			var child protoreflect.MessageDescriptor
			var list, isRecord bool
			if descriptor != nil {
				if field := descriptor.Fields().ByJSONName(name); field != nil && field.JSONName() == name {
					child, list = field.Message(), field.IsList()
					isRecord = child != nil && child.FullName() == "opentelemetry.proto.logs.v1.LogRecord"
				}
			}
			if err := b.jsonValue(decoder, child, list, isRecord, depth+1); err != nil {
				return err
			}
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
			return ErrMalformed
		}
	}
	return nil
}
