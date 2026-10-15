package otlp

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func decodeJSON(raw []byte) (*logspb.LogsData, error) {
	data := new(logspb.LogsData)
	adapted, err := rewriteOTLPJSON(raw, data.ProtoReflect().Descriptor(), true)
	if err != nil {
		return nil, err
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true, RecursionLimit: maxDepth}).Unmarshal(adapted, data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return data, nil
}

func marshalJSON(m proto.Message) ([]byte, error) {
	options := protojson.MarshalOptions{UseEnumNumbers: true}
	if m == nil {
		return options.Marshal(m)
	}
	if err := checkMarshalJSONDepth(m.ProtoReflect(), 1); err != nil {
		return nil, err
	}
	raw, err := options.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return rewriteOTLPJSON(raw, m.ProtoReflect().Descriptor(), false)
}

// Rewrite only schema fields. Raw scalar tokens keep decimal integers and
// nonfinite spellings intact; attribute names are ordinary string values.
func rewriteOTLPJSON(raw []byte, descriptor protoreflect.MessageDescriptor, decode bool) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, ErrMalformed
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var output bytes.Buffer
	if err := rewriteJSONMessage(decoder, &output, descriptor, 1, false, decode); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrMalformed
	}
	return output.Bytes(), nil
}

func rewriteJSONMessage(decoder *json.Decoder, output *bytes.Buffer, descriptor protoreflect.MessageDescriptor, depth int, nullable, decode bool) error {
	if depth > maxDepth {
		return ErrTooLarge
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrMalformed
	}
	if token == nil && nullable {
		output.WriteString("null")
		return nil
	}
	if token != json.Delim('{') {
		return ErrMalformed
	}
	output.WriteByte('{')
	seen := make(map[protoreflect.FieldNumber]bool)
	first := true
	for decoder.More() {
		token, err := decoder.Token()
		name, valid := token.(string)
		if err != nil || !valid {
			return ErrMalformed
		}
		field := descriptor.Fields().ByJSONName(name)
		if field == nil {
			if err := skipOTLPJSONValue(decoder, depth+1); err != nil {
				return err
			}
			continue
		}
		if seen[field.Number()] {
			return ErrMalformed
		}
		seen[field.Number()] = true
		if !first {
			output.WriteByte(',')
		}
		first = false
		writeJSONString(output, name)
		output.WriteByte(':')
		if field.IsList() || field.IsMap() {
			err = rewriteJSONCollection(decoder, output, field, depth+1, decode)
		} else {
			err = rewriteJSONValue(decoder, output, field, depth+1, true, decode)
		}
		if err != nil {
			return err
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return ErrMalformed
	}
	output.WriteByte('}')
	return nil
}

func rewriteJSONCollection(decoder *json.Decoder, output *bytes.Buffer, field protoreflect.FieldDescriptor, depth int, decode bool) error {
	if depth > maxDepth {
		return ErrTooLarge
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrMalformed
	}
	if token == nil {
		output.WriteString("null")
		return nil
	}
	open, close := json.Delim('['), json.Delim(']')
	valueField := field
	if field.IsMap() {
		open, close = '{', '}'
		valueField = field.MapValue()
	}
	if token != open {
		return ErrMalformed
	}
	output.WriteByte(byte(open))
	first := true
	for decoder.More() {
		if !first {
			output.WriteByte(',')
		}
		first = false
		if field.IsMap() {
			token, err := decoder.Token()
			name, valid := token.(string)
			if err != nil || !valid {
				return ErrMalformed
			}
			writeJSONString(output, name)
			output.WriteByte(':')
		}
		if err := rewriteJSONValue(decoder, output, valueField, depth+1, field.IsMap(), decode); err != nil {
			return err
		}
	}
	if token, err := decoder.Token(); err != nil || token != close {
		return ErrMalformed
	}
	output.WriteByte(byte(close))
	return nil
}

func rewriteJSONValue(decoder *json.Decoder, output *bytes.Buffer, field protoreflect.FieldDescriptor, depth int, nullable, decode bool) error {
	if depth > maxDepth {
		return ErrTooLarge
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		return rewriteJSONMessage(decoder, output, field.Message(), depth, nullable, decode)
	}
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return ErrMalformed
	}
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		if !nullable {
			return ErrMalformed
		}
		output.Write(raw)
		return nil
	}
	if field.Kind() == protoreflect.EnumKind && (len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9'))) {
		return ErrMalformed
	}
	switch field.FullName() {
	case "opentelemetry.proto.logs.v1.LogRecord.trace_id", "opentelemetry.proto.logs.v1.LogRecord.span_id":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return ErrMalformed
		}
		if decode {
			data, err := hex.DecodeString(value)
			if err != nil {
				return ErrMalformed
			}
			value = base64.StdEncoding.EncodeToString(data)
		} else {
			data, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				return ErrMalformed
			}
			value = hex.EncodeToString(data)
		}
		writeJSONString(output, value)
	default:
		output.Write(raw)
	}
	return nil
}

func writeJSONString(output *bytes.Buffer, value string) {
	raw, _ := json.Marshal(value)
	output.Write(raw)
}

func skipOTLPJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxDepth {
		return ErrTooLarge
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrMalformed
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return ErrMalformed
	}
	for decoder.More() {
		if delimiter == '{' {
			if token, err := decoder.Token(); err != nil {
				return ErrMalformed
			} else if _, valid := token.(string); !valid {
				return ErrMalformed
			}
		}
		if err := skipOTLPJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	close := json.Delim(']')
	if delimiter == '{' {
		close = '}'
	}
	if token, err := decoder.Token(); err != nil || token != close {
		return ErrMalformed
	}
	return nil
}

// Count the JSON representation before protojson's unbounded marshal walk.
// Field names are not values; each field/element adds one level.
func checkMarshalJSONDepth(message protoreflect.Message, depth int) error {
	if depth > maxDepth {
		return ErrTooLarge
	}
	if !message.IsValid() {
		return nil
	}
	var result error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if depth+1 > maxDepth {
			result = ErrTooLarge
			return false
		}
		switch {
		case field.IsList():
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if result = checkMarshalJSONValueDepth(field, list.Get(i), depth+2); result != nil {
					return false
				}
			}
		case field.IsMap():
			value.Map().Range(func(_ protoreflect.MapKey, entry protoreflect.Value) bool {
				result = checkMarshalJSONValueDepth(field.MapValue(), entry, depth+2)
				return result == nil
			})
		default:
			result = checkMarshalJSONValueDepth(field, value, depth+1)
		}
		return result == nil
	})
	return result
}

func checkMarshalJSONValueDepth(field protoreflect.FieldDescriptor, value protoreflect.Value, depth int) error {
	if depth > maxDepth {
		return ErrTooLarge
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		return checkMarshalJSONDepth(value.Message(), depth)
	}
	return nil
}
