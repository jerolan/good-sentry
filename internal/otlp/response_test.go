package otlp_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"goodsentry/internal/otlp"

	"google.golang.org/protobuf/encoding/protowire"
)

var responseEncodings = []struct {
	name     string
	encoding otlp.Encoding
}{
	{"protobuf", otlp.Protobuf},
	{"JSON", otlp.JSON},
	{"unknown-uses-JSON", otlp.Encoding(255)},
}

func TestSuccessResponsePublicWire(t *testing.T) {
	for _, encoding := range responseEncodings {
		for _, warning := range []bool{false, true} {
			t.Run(encoding.name+"/warning="+strconv.FormatBool(warning), func(t *testing.T) {
				raw := otlp.SuccessResponse(encoding.encoding, warning)
				if len(raw) >= 1024 {
					t.Fatal("success response exceeds the public diagnostic budget")
				}
				if !warning {
					if encoding.encoding == otlp.Protobuf {
						if len(raw) != 0 {
							t.Fatal("full success must encode an empty ExportLogsServiceResponse")
						}
					} else if !bytes.Equal(raw, []byte(`{}`)) {
						t.Fatal("full JSON success must be an empty object")
					}
					return
				}
				var message string
				if encoding.encoding == otlp.Protobuf {
					outer := decodeResponseFields(t, raw)
					if len(outer) != 1 || outer[1].kind != protowire.BytesType {
						t.Fatal("warning must contain the partial_success submessage at field 1")
					}
					partial := decodeResponseFields(t, outer[1].body)
					if len(partial) < 1 || len(partial) > 2 || partial[2].kind != protowire.BytesType {
						t.Fatal("partial success must contain its public diagnostic field")
					}
					if rejected, present := partial[1]; present && (rejected.kind != protowire.VarintType || rejected.integer != 0) {
						t.Fatal("warning must report zero rejected log records")
					}
					message = string(partial[2].body)
				} else {
					outer := decodeResponseJSON(t, raw)
					partial := decodeResponseJSON(t, outer["partialSuccess"])
					if len(outer) != 1 || len(partial) < 1 || len(partial) > 2 {
						t.Fatal("warning must contain only the partialSuccess result")
					}
					if rejected, present := partial["rejectedLogRecords"]; present {
						var count string
						if err := json.Unmarshal(rejected, &count); err != nil || count != "0" {
							t.Fatal("warning must report the exact decimal64 zero rejection count")
						}
					}
					if err := json.Unmarshal(partial["errorMessage"], &message); err != nil {
						t.Fatalf("partial success diagnostic: %v", err)
					}
				}
				for _, word := range []string{"profiles", "dictionary", "unavailable"} {
					if !strings.Contains(strings.ToLower(message), word) {
						t.Fatalf("warning does not explain the unavailable Profiles dictionary: %q", message)
					}
				}
				if !utf8.ValidString(message) {
					t.Fatal("warning is not valid UTF-8")
				}
			})
		}
	}
}

func TestErrorResponsePublicWire(t *testing.T) {
	for _, encoding := range responseEncodings {
		for _, failure := range []struct {
			status int
			code   uint64
			word   string
		}{
			{400, 3, "malformed"},
			{403, 7, "key"},
			{413, 8, "capacity"},
			{415, 12, "encoding"},
			{404, 12, "disabled"},
			{405, 12, "method"},
			{503, 14, "storage"},
			{500, 14, "storage"},
		} {
			t.Run(encoding.name+"/"+strconv.Itoa(failure.status), func(t *testing.T) {
				raw := otlp.ErrorResponse(encoding.encoding, failure.status)
				if len(raw) >= 1024 {
					t.Fatal("error response exceeds the public diagnostic budget")
				}
				var code uint64
				var message string
				if encoding.encoding == otlp.Protobuf {
					fields := decodeResponseFields(t, raw)
					if len(fields) != 2 || fields[1].kind != protowire.VarintType || fields[2].kind != protowire.BytesType {
						t.Fatal("error must be a google.rpc.Status with code and message")
					}
					code, message = fields[1].integer, string(fields[2].body)
				} else {
					fields := decodeResponseJSON(t, raw)
					if len(fields) != 2 {
						t.Fatal("JSON error must contain only the public Status code and message")
					}
					if err := json.Unmarshal(fields["code"], &code); err != nil {
						t.Fatalf("Status.code: %v", err)
					}
					if err := json.Unmarshal(fields["message"], &message); err != nil {
						t.Fatalf("Status.message: %v", err)
					}
				}
				if code != failure.code || !utf8.ValidString(message) || !strings.Contains(strings.ToLower(message), failure.word) {
					t.Fatalf("public failure category differs: code=%d message=%q", code, message)
				}
				if failure.status == 500 && !bytes.Equal(raw, otlp.ErrorResponse(encoding.encoding, 503)) {
					t.Fatal("unknown statuses must use the static storage-unavailable result")
				}
			})
		}
	}
}

type responseField struct {
	kind    protowire.Type
	integer uint64
	body    []byte
}

// Decode field numbers from the public schemas, independently of the encoder.
func decodeResponseFields(t *testing.T, raw []byte) map[protowire.Number]responseField {
	t.Helper()
	fields := make(map[protowire.Number]responseField)
	for len(raw) != 0 {
		number, kind, n := protowire.ConsumeTag(raw)
		if n < 0 {
			t.Fatalf("invalid public response tag: %v", protowire.ParseError(n))
		}
		raw = raw[n:]
		field := responseField{kind: kind}
		switch kind {
		case protowire.VarintType:
			field.integer, n = protowire.ConsumeVarint(raw)
		case protowire.BytesType:
			field.body, n = protowire.ConsumeBytes(raw)
		default:
			t.Fatalf("unexpected response wire type %d", kind)
		}
		if n < 0 {
			t.Fatalf("invalid public response value: %v", protowire.ParseError(n))
		}
		if _, duplicate := fields[number]; duplicate {
			t.Fatalf("duplicate public response field %d", number)
		}
		fields[number], raw = field, raw[n:]
	}
	return fields
}

func decodeResponseJSON(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		t.Fatalf("invalid public response object: %v", err)
	}
	return fields
}
