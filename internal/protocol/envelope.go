// Package protocol decodes the public Sentry SDK transport formats. It does not
// decide whether a public ingestion key is authorized for a stored project.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

var (
	ErrMalformed = errors.New("malformed Sentry payload")
	ErrTooLarge  = errors.New("Sentry payload exceeds limit")
	ErrEncoding  = errors.New("unsupported content encoding")
	ErrAuth      = errors.New("invalid Sentry authentication")
)

// Limits bound the HTTP body, its decoded representation and its items. A
// nonpositive field selects the package default; callers may set each field.
type Limits struct {
	MaxCompressed   int64
	MaxDecompressed int64
	MaxItems        int
	MaxItemBytes    int64
}

type Envelope struct {
	Header map[string]json.RawMessage
	Items  []Item
}

type Item struct {
	Header  map[string]json.RawMessage
	Type    string
	Payload []byte
}

func (l Limits) defaults() Limits {
	if l.MaxCompressed <= 0 {
		l.MaxCompressed = 20 << 20
	}
	if l.MaxDecompressed <= 0 {
		l.MaxDecompressed = 100 << 20
	}
	if l.MaxItems <= 0 {
		l.MaxItems = 1000
	}
	if l.MaxItemBytes <= 0 {
		l.MaxItemBytes = 20 << 20
	}
	return l
}

// ParseEnvelope applies byte-counted framing without interpreting item payloads.
// Returned payloads reference data; callers must not mutate it while using them.
func ParseEnvelope(data []byte, limits Limits) (Envelope, error) {
	limits = limits.defaults()
	if int64(len(data)) > limits.MaxDecompressed {
		return Envelope{}, ErrTooLarge
	}
	headerLine, rest, _ := nextLine(data)
	header, err := parseHeader(headerLine)
	if err != nil {
		return Envelope{}, err
	}
	env := Envelope{Header: header}
	for len(rest) > 0 {
		if len(env.Items) >= limits.MaxItems {
			return Envelope{}, ErrTooLarge
		}
		line, payload, terminated := nextLine(rest)
		if !terminated {
			return Envelope{}, fmt.Errorf("%w: item header lacks newline", ErrMalformed)
		}
		itemHeader, err := parseHeader(line)
		if err != nil {
			return Envelope{}, err
		}
		var itemType string
		if err := json.Unmarshal(itemHeader["type"], &itemType); err != nil || itemType == "" {
			return Envelope{}, fmt.Errorf("%w: item type must be a nonempty string", ErrMalformed)
		}
		var body []byte
		if rawLength, exists := itemHeader["length"]; exists {
			n, err := strconv.ParseInt(string(rawLength), 10, 64)
			if err != nil || n < 0 {
				return Envelope{}, fmt.Errorf("%w: invalid item length", ErrMalformed)
			}
			if n > limits.MaxItemBytes {
				return Envelope{}, ErrTooLarge
			}
			if n > int64(len(payload)) {
				return Envelope{}, fmt.Errorf("%w: truncated item", ErrMalformed)
			}
			body, rest = payload[:int(n)], payload[int(n):]
			if len(rest) > 0 {
				if rest[0] != '\n' {
					return Envelope{}, fmt.Errorf("%w: bytes after item payload", ErrMalformed)
				}
				rest = rest[1:]
			}
		} else {
			body, rest, _ = nextLine(payload)
			if int64(len(body)) > limits.MaxItemBytes {
				return Envelope{}, ErrTooLarge
			}
		}
		env.Items = append(env.Items, Item{Header: itemHeader, Type: itemType, Payload: body})
	}
	return env, nil
}

func nextLine(data []byte) (line, rest []byte, terminated bool) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return data[:i], data[i+1:], true
	}
	return data, nil, false
}

func parseHeader(line []byte) (map[string]json.RawMessage, error) {
	if len(line) == 0 || !utf8.Valid(line) || !bytes.Equal(line, bytes.TrimSpace(line)) {
		return nil, fmt.Errorf("%w: invalid header encoding", ErrMalformed)
	}
	// Decode keys separately so duplicate identities cannot silently overwrite
	// one another as they would when unmarshalling straight into a map.
	dec := json.NewDecoder(bytes.NewReader(line))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("%w: header must be an object", ErrMalformed)
	}
	header := make(map[string]json.RawMessage)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: invalid header key", ErrMalformed)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("%w: invalid header key", ErrMalformed)
		}
		if _, exists := header[key]; exists {
			return nil, fmt.Errorf("%w: duplicate header key", ErrMalformed)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("%w: invalid header value", ErrMalformed)
		}
		header[key] = value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') || dec.InputOffset() != int64(len(line)) {
		return nil, fmt.Errorf("%w: invalid header object", ErrMalformed)
	}
	return header, nil
}
