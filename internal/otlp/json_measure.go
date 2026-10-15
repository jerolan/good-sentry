package otlp

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type jsonMeasure struct {
	bytes        int64
	nodes, depth int
}

type jsonMeasureCounter struct {
	jsonMeasure
	limits      Limits
	escapeHTML  bool
	quoted      int64
	quotedLimit int64
}

func measureNativeJSON(m proto.Message, limits Limits) (jsonMeasure, error) {
	counter := jsonMeasureCounter{limits: limits, escapeHTML: true}
	if err := counter.target(m); err != nil {
		return jsonMeasure{}, err
	}
	return counter.jsonMeasure, nil
}

func measureJSONProjection(m proto.Message, limits Limits, quotedLimit int64) (jsonMeasure, int64, error) {
	if quotedLimit <= 0 {
		return jsonMeasure{}, 0, ErrTooLarge
	}
	counter := jsonMeasureCounter{limits: limits, quotedLimit: quotedLimit}
	if err := counter.addQuoted(2); err != nil { // Quotes around the whole JSON.
		return jsonMeasure{}, 0, err
	}
	if err := counter.target(m); err != nil {
		return jsonMeasure{}, 0, err
	}
	return counter.jsonMeasure, counter.quoted, nil
}

func (c *jsonMeasureCounter) target(m proto.Message) error {
	if m == nil || !m.ProtoReflect().IsValid() {
		if err := c.node(1); err != nil {
			return err
		}
		return c.add(4) // The caller emits literal null for absent data.
	}
	return c.message(m.ProtoReflect(), 1)
}

func (c *jsonMeasureCounter) node(depth int) error {
	if depth > maxDepth || c.nodes >= c.limits.MaxNodes {
		return ErrTooLarge
	}
	c.nodes++
	if depth > c.depth {
		c.depth = depth
	}
	return nil
}

func (c *jsonMeasureCounter) add(size int64) error {
	if size < 0 || c.bytes > c.limits.MaxBytes || size > c.limits.MaxBytes-c.bytes {
		return ErrTooLarge
	}
	if err := c.addQuoted(size); err != nil {
		return err
	}
	c.bytes += size
	return nil
}

func (c *jsonMeasureCounter) addQuoted(size int64) error {
	if c.quotedLimit == 0 {
		return nil
	}
	if size < 0 || c.quoted > c.quotedLimit || size > c.quotedLimit-c.quoted {
		return ErrTooLarge
	}
	c.quoted += size
	return nil
}

func (c *jsonMeasureCounter) quotedToken(size int64) error {
	if err := c.add(size); err != nil {
		return err
	}
	return c.addQuoted(2) // Escape the token's two quotes.
}

func (c *jsonMeasureCounter) message(message protoreflect.Message, depth int) error {
	if err := c.node(depth); err != nil {
		return err
	}
	descriptor := message.Descriptor()
	// These types have special ProtoJSON shapes rather than ordinary fields.
	if strings.HasPrefix(string(descriptor.FullName()), "google.protobuf.") || descriptor.IsMapEntry() || descriptor.Syntax() != protoreflect.Proto3 {
		return ErrMalformed
	}
	if err := c.add(2); err != nil {
		return err
	}
	// A selected nil message inside a protobuf is an empty object. Only an
	// absent top-level detail component uses the literal-null convention.
	if !message.IsValid() {
		return nil
	}
	first := true
	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.IsMap() || field.Kind() == protoreflect.GroupKind {
			return ErrMalformed
		}
		if !message.Has(field) {
			continue
		}
		if !first {
			if err := c.add(1); err != nil {
				return err
			}
		}
		first = false
		name := field.JSONName()
		if !plainJSONFieldName(name) {
			return ErrMalformed
		}
		if err := c.add(int64(len(name)) + 3); err != nil { // Quotes and colon.
			return err
		}
		if err := c.addQuoted(2); err != nil {
			return err
		}
		value := message.Get(field)
		if !field.IsList() {
			if err := c.value(field, value, depth+1); err != nil {
				return err
			}
			continue
		}
		if err := c.node(depth + 1); err != nil {
			return err
		}
		if err := c.add(2); err != nil {
			return err
		}
		list := value.List()
		for j := 0; j < list.Len(); j++ {
			if j > 0 {
				if err := c.add(1); err != nil {
					return err
				}
			}
			if err := c.value(field, list.Get(j), depth+2); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *jsonMeasureCounter) value(field protoreflect.FieldDescriptor, value protoreflect.Value, depth int) error {
	if field.Kind() == protoreflect.MessageKind {
		return c.message(value.Message(), depth)
	}
	if err := c.node(depth); err != nil {
		return err
	}
	var number [20]byte
	switch field.Kind() {
	case protoreflect.BoolKind:
		if value.Bool() {
			return c.add(4)
		}
		return c.add(5)
	case protoreflect.EnumKind:
		if field.Enum().FullName() == "google.protobuf.NullValue" {
			return ErrMalformed
		}
		return c.add(int64(len(strconv.AppendInt(number[:0], int64(value.Enum()), 10))))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return c.add(int64(len(strconv.AppendInt(number[:0], value.Int(), 10))))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return c.add(int64(len(strconv.AppendUint(number[:0], value.Uint(), 10))))
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return c.quotedToken(int64(len(strconv.AppendInt(number[:0], value.Int(), 10))) + 2)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return c.quotedToken(int64(len(strconv.AppendUint(number[:0], value.Uint(), 10))) + 2)
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		n := value.Float()
		switch {
		case math.IsNaN(n):
			return c.quotedToken(5)
		case math.IsInf(n, 1):
			return c.quotedToken(10)
		case math.IsInf(n, -1):
			return c.quotedToken(11)
		}
		// A finite number has a small bounded token; no message or large value
		// is serialized to obtain its size.
		var raw []byte
		var err error
		if field.Kind() == protoreflect.FloatKind {
			raw, err = json.Marshal(float32(n))
		} else {
			raw, err = json.Marshal(n)
		}
		if err != nil {
			return ErrMalformed
		}
		return c.add(int64(len(raw)))
	case protoreflect.StringKind:
		var quotedLimit int64
		if c.quotedLimit != 0 {
			quotedLimit = c.quotedLimit - c.quoted
			if quotedLimit < 4 {
				return ErrTooLarge
			}
		}
		size, quoted, err := countJSONStringPair(value.String(), c.escapeHTML, c.limits.MaxBytes-c.bytes, quotedLimit)
		if err != nil {
			return err
		}
		if err := c.add(size); err != nil {
			return err
		}
		if c.quotedLimit != 0 {
			return c.addQuoted(quoted - size)
		}
		return nil
	case protoreflect.BytesKind:
		if err := c.quotedToken(2); err != nil {
			return err
		}
		size := int64(len(value.Bytes()))
		switch field.FullName() {
		case "opentelemetry.proto.logs.v1.LogRecord.trace_id", "opentelemetry.proto.logs.v1.LogRecord.span_id":
			if size > (c.limits.MaxBytes-c.bytes)/2 {
				return ErrTooLarge
			}
			return c.add(size * 2)
		default:
			groups := size / 3
			if groups > (c.limits.MaxBytes-c.bytes)/4 {
				return ErrTooLarge
			}
			if err := c.add(groups * 4); err != nil {
				return err
			}
			if size%3 != 0 {
				return c.add(4)
			}
			return nil
		}
	default:
		return ErrMalformed
	}
}

func plainJSONFieldName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if b != '_' && (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') {
			return false
		}
	}
	return true
}

func jsonStringSize(value string, escapeHTML bool) (int64, error) {
	return countJSONString(value, escapeHTML, math.MaxInt64)
}

func countJSONString(value string, escapeHTML bool, limit int64) (int64, error) {
	size, _, err := countJSONStringPair(value, escapeHTML, limit, 0)
	return size, err
}

// A zero quotedLimit disables the secondary count. Otherwise quoted counts
// this string token after encoding the complete compact JSON as a string.
func countJSONStringPair(value string, escapeHTML bool, limit, quotedLimit int64) (int64, int64, error) {
	if limit < 2 {
		return 0, 0, ErrTooLarge
	}
	size := int64(2) // Quotes.
	var quoted int64
	if quotedLimit != 0 {
		if quotedLimit < 4 {
			return 0, 0, ErrTooLarge
		}
		quoted = 4 // Each token quote becomes an escaped quote.
	}
	for i := 0; i < len(value); {
		r, width := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && width == 1 {
			return 0, 0, ErrMalformed
		}
		n, q := int64(width), int64(width)
		switch {
		case r == '"' || r == '\\':
			n, q = 2, 4
		case r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
			n, q = 2, 3
		case r < 0x20:
			n, q = 6, 7
		case r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
			q = 6
			if escapeHTML {
				n, q = 6, 7
			}
		}
		if n > limit-size {
			return 0, 0, ErrTooLarge
		}
		if quotedLimit != 0 {
			if q > quotedLimit-quoted {
				return 0, 0, ErrTooLarge
			}
			quoted += q
		}
		size += n
		i += width
	}
	return size, quoted, nil
}
