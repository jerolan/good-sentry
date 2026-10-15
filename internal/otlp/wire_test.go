package otlp_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"math"
	"strings"
	"testing"

	"goodsentry/internal/otlp"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

var limits = otlp.Limits{MaxBytes: 1 << 20, MaxRecords: 1000, MaxNodes: 100000}

func wireFixture(t *testing.T) (*logspb.LogsData, []byte, []byte) {
	t.Helper()
	str := func(value string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
	}
	expected := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{
		Resource:  &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: str("api\x00worker")}}, DroppedAttributesCount: 2},
		SchemaUrl: "https://example.invalid/resource", ScopeLogs: []*logspb.ScopeLogs{{
			Scope:     &commonpb.InstrumentationScope{Name: "library", Version: "1", Attributes: []*commonpb.KeyValue{{Key: "scope", Value: str("separate")}}},
			SchemaUrl: "https://example.invalid/scope", LogRecords: []*logspb.LogRecord{{
				TimeUnixNano: math.MaxUint64, ObservedTimeUnixNano: 1791981001123456789,
				SeverityNumber: 25, SeverityText: "custom", EventName: "event.kind", Flags: 0xff000001, DroppedAttributesCount: 3,
				TraceId: []byte{0, 17, 34, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255}, SpanId: []byte{0, 17, 34, 51, 68, 85, 102, 119},
				Attributes: []*commonpb.KeyValue{{Key: "int", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: math.MaxInt64}}}, {Key: "int", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: math.MinInt64}}}},
				Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: []*commonpb.AnyValue{
					{}, str("<script>\x00text"), {Value: &commonpb.AnyValue_BytesValue{BytesValue: []byte{0, 255}}},
					{Value: &commonpb.AnyValue_BoolValue{BoolValue: false}}, {Value: &commonpb.AnyValue_DoubleValue{DoubleValue: math.Inf(1)}},
				}}}},
			}},
		}},
	}}}
	binary, err := proto.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	json := []byte(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api\u0000worker"}}],"droppedAttributesCount":2},"schemaUrl":"https://example.invalid/resource","scopeLogs":[{"scope":{"name":"library","version":"1","attributes":[{"key":"scope","value":{"stringValue":"separate"}}]},"schemaUrl":"https://example.invalid/scope","logRecords":[{"timeUnixNano":18446744073709551615,"observedTimeUnixNano":"1791981001123456789","severityNumber":25,"severityText":"custom","eventName":"event.kind","flags":4278190081,"droppedAttributesCount":3,"traceId":"00112233445566778899AABBCCDDEEFF","spanId":"0011223344556677","attributes":[{"key":"int","value":{"intValue":"9223372036854775807"}},{"key":"int","value":{"intValue":-9223372036854775808}}],"body":{"arrayValue":{"values":[{},{"stringValue":"<script>\u0000text"},{"bytesValue":"AP8="},{"boolValue":false},{"doubleValue":"Infinity"}]}},"futureRecord":{"value":42}}]}],"futureResource":[{}]}],"futureEnvelope":null}`)
	return expected, binary, json
}

func compressed(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestDecodeLogsContract(t *testing.T) {
	expected, binary, json := wireFixture(t)
	for _, tc := range []struct {
		name        string
		body        []byte
		encoding    otlp.Encoding
		compression string
	}{
		{"protobuf", binary, otlp.Protobuf, ""}, {"JSON", json, otlp.JSON, "identity"},
		{"protobuf-gzip", compressed(t, binary), otlp.Protobuf, "gzip"}, {"JSON-gzip", compressed(t, json), otlp.JSON, "gzip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := otlp.Decode(bytes.NewReader(tc.body), tc.encoding, tc.compression, limits)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(got, expected) {
				t.Fatalf("decoded data differs from the public value contract: %v", got)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"resource_logs":[{}]}`, `{"resourceLogs":null}`, `{"future": {"resourceLogs":[{}]}}`} {
		got, err := otlp.Decode(strings.NewReader(raw), otlp.JSON, "", limits)
		if err != nil || len(got.GetResourceLogs()) != 0 {
			t.Fatalf("empty/unknown request %s: %v %v", raw, got, err)
		}
	}
	got, err := otlp.Decode(bytes.NewReader(nil), otlp.Protobuf, "", limits)
	if err != nil || len(got.GetResourceLogs()) != 0 {
		t.Fatalf("empty binary: %v %v", got, err)
	}
}

func logJSON(record string) string {
	return `{"resourceLogs":[{"scopeLogs":[{"logRecords":[` + record + `]}]}]}`
}

func TestDecodeAnyValuePresenceAndTypes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      func(*commonpb.AnyValue) bool
	}{
		{"absent", `{}`, func(v *commonpb.AnyValue) bool { return v == nil }},
		{"null absent", `{"body":null}`, func(v *commonpb.AnyValue) bool { return v == nil }},
		{"present empty", `{"body":{}}`, func(v *commonpb.AnyValue) bool { return v != nil && v.Value == nil }},
		{"null oneof", `{"body":{"stringValue":null}}`, func(v *commonpb.AnyValue) bool { return v != nil && v.Value == nil }},
		{"empty string", `{"body":{"stringValue":""}}`, func(v *commonpb.AnyValue) bool {
			_, ok := v.Value.(*commonpb.AnyValue_StringValue)
			return ok && v.GetStringValue() == ""
		}},
		{"true", `{"body":{"boolValue":true}}`, func(v *commonpb.AnyValue) bool { return v.GetBoolValue() }},
		{"zero integer", `{"body":{"intValue":"0"}}`, func(v *commonpb.AnyValue) bool {
			_, ok := v.Value.(*commonpb.AnyValue_IntValue)
			return ok && v.GetIntValue() == 0
		}},
		{"NaN", `{"body":{"doubleValue":"NaN"}}`, func(v *commonpb.AnyValue) bool { return math.IsNaN(v.GetDoubleValue()) }},
		{"negative infinity", `{"body":{"doubleValue":"-Infinity"}}`, func(v *commonpb.AnyValue) bool { return math.IsInf(v.GetDoubleValue(), -1) }},
		{"URL-safe bytes", `{"body":{"bytesValue":"_w"}}`, func(v *commonpb.AnyValue) bool { return bytes.Equal(v.GetBytesValue(), []byte{255}) }},
		{"empty bytes", `{"body":{"bytesValue":""}}`, func(v *commonpb.AnyValue) bool {
			_, ok := v.Value.(*commonpb.AnyValue_BytesValue)
			return ok && len(v.GetBytesValue()) == 0
		}},
		{"duplicate nested keys", `{"body":{"kvlistValue":{"values":[{"key":"same","value":{"arrayValue":{}}},{"key":"same","value":{}}]}}}`, func(v *commonpb.AnyValue) bool {
			values := v.GetKvlistValue().GetValues()
			return len(values) == 2 && values[0].Key == "same" && values[1].Key == "same" && values[0].Value.GetArrayValue() != nil && len(values[0].Value.GetArrayValue().Values) == 0 && values[1].Value != nil && values[1].Value.Value == nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := otlp.Decode(strings.NewReader(logJSON(tc.body)), otlp.JSON, "", limits)
			if err != nil {
				t.Fatal(err)
			}
			body := got.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Body
			if !tc.valid(body) {
				t.Fatalf("AnyValue contract violated: %v", body)
			}
		})
	}
}

func TestDecodeMalformedAndEncodingBoundaries(t *testing.T) {
	for _, raw := range []string{
		``, `null`, `[]`, `{} {}`, "{}\n{}", string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}),
		`{"resourceLogs":[null]}`, logJSON(`{"body":{"arrayValue":{"values":[null]}}}`),
		logJSON(`{"severityNumber":"SEVERITY_NUMBER_WARN"}`), logJSON(`{"severityNumber":"13"}`),
		logJSON(`{"severityNumber":2147483648}`), logJSON(`{"timeUnixNano":18446744073709551616}`),
		logJSON(`{"timeUnixNano":-1}`), logJSON(`{"timeUnixNano":1.5}`),
		logJSON(`{"traceId":"not hex"}`), logJSON(`{"body":{"bytesValue":"not base64!"}}`),
		logJSON(`{"body":{"stringValue":"one","intValue":"2"}}`), logJSON(`{"severityNumber":9,"severityNumber":13}`),
	} {
		if _, err := otlp.Decode(strings.NewReader(raw), otlp.JSON, "", limits); !errors.Is(err, otlp.ErrMalformed) {
			t.Errorf("%s: want malformed, got %v", raw, err)
		}
	}
	for _, raw := range [][]byte{{0}, {0x0a, 0x80}, {0x0c}, {0x0a, 0x03, 0x12}} {
		if _, err := otlp.Decode(bytes.NewReader(raw), otlp.Protobuf, "", limits); !errors.Is(err, otlp.ErrMalformed) {
			t.Errorf("bad protobuf %x: %v", raw, err)
		}
	}
	broken := compressed(t, []byte(`{}`))
	broken[len(broken)-8] ^= 1
	for _, raw := range [][]byte{broken, broken[:len(broken)-6], {1, 2, 3}} {
		if _, err := otlp.Decode(bytes.NewReader(raw), otlp.JSON, "gzip", limits); !errors.Is(err, otlp.ErrMalformed) {
			t.Errorf("bad gzip: %v", err)
		}
	}
	for _, encoding := range []string{"br", "deflate", "gzip, gzip"} {
		if _, err := otlp.Decode(strings.NewReader(`{}`), otlp.JSON, encoding, limits); !errors.Is(err, otlp.ErrEncoding) {
			t.Errorf("unsupported %s: %v", encoding, err)
		}
	}
}

func embedded(field protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, field, protowire.BytesType), value)
}

func TestDecodeAdmissionBudgets(t *testing.T) {
	// A later malformed suffix distinguishes early admission from first fully
	// decoding an excessive tree and checking its size afterwards.
	var records []byte
	for range 9 {
		records = append(records, embedded(2, nil)...)
	}
	records = append(records, 0xff)
	binary := embedded(1, embedded(2, records))
	json := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[` + strings.Repeat(`{},`, 9) + `!]}]}]}`
	l := limits
	l.MaxRecords = 8
	for _, tc := range []struct {
		data     []byte
		encoding otlp.Encoding
	}{{binary, otlp.Protobuf}, {[]byte(json), otlp.JSON}} {
		if _, err := otlp.Decode(bytes.NewReader(tc.data), tc.encoding, "", l); !errors.Is(err, otlp.ErrTooLarge) {
			t.Errorf("early record budget: %v", err)
		}
	}
	l = limits
	l.MaxNodes = 20
	var unknown []byte
	for range 30 {
		unknown = protowire.AppendVarint(protowire.AppendTag(unknown, 99, protowire.VarintType), 1)
	}
	unknown = append(unknown, 0xff)
	for _, tc := range []struct {
		data     []byte
		encoding otlp.Encoding
	}{{unknown, otlp.Protobuf}, {[]byte(`{"future":[` + strings.Repeat(`{},`, 30) + `!]}`), otlp.JSON}} {
		if _, err := otlp.Decode(bytes.NewReader(tc.data), tc.encoding, "", l); !errors.Is(err, otlp.ErrTooLarge) {
			t.Errorf("early node budget: %v", err)
		}
	}
	l = limits
	l.MaxBytes = 64
	for _, tc := range []struct {
		data        []byte
		compression string
	}{{bytes.Repeat([]byte(" "), 65), ""}, {compressed(t, []byte(`{"future":"`+strings.Repeat("a", 1000)+`"}`)), "gzip"}} {
		if _, err := otlp.Decode(bytes.NewReader(tc.data), otlp.JSON, tc.compression, l); !errors.Is(err, otlp.ErrTooLarge) {
			t.Errorf("body budget: %v", err)
		}
	}
	deep := `{"future":` + strings.Repeat(`[`, 70) + `0` + strings.Repeat(`]`, 70) + `}`
	if _, err := otlp.Decode(strings.NewReader(deep), otlp.JSON, "", limits); !errors.Is(err, otlp.ErrTooLarge) {
		t.Errorf("JSON depth: %v", err)
	}
	var groups []byte
	for range 70 {
		groups = protowire.AppendTag(groups, 99, protowire.StartGroupType)
	}
	for range 70 {
		groups = protowire.AppendTag(groups, 99, protowire.EndGroupType)
	}
	if _, err := otlp.Decode(bytes.NewReader(groups), otlp.Protobuf, "", limits); !errors.Is(err, otlp.ErrTooLarge) {
		t.Errorf("unknown group depth: %v", err)
	}
	// A short unsigned varint in an int32 field canonicalizes to a ten-byte
	// negative int32. Canonical persisted size must also fit admission.
	negative := protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), math.MaxUint32)
	canonicalInflation := embedded(1, embedded(2, embedded(2, negative)))
	l = limits
	l.MaxBytes = int64(len(canonicalInflation))
	if _, err := otlp.Decode(bytes.NewReader(canonicalInflation), otlp.Protobuf, "", l); !errors.Is(err, otlp.ErrTooLarge) {
		t.Errorf("canonical byte budget: %v", err)
	}
}

func TestDecodeForwardFieldsAndBinaryMerge(t *testing.T) {
	_, binary, _ := wireFixture(t)
	unknown := embedded(123, []byte{0xff, 0})
	input := append(append(append([]byte{}, binary...), binary...), unknown...)
	got, err := otlp.Decode(bytes.NewReader(input), otlp.Protobuf, "", limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetResourceLogs()) != 2 || len(got.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("merge/unknown semantics: %v", got)
	}
	raw := logJSON(`{"severity_number":13,"body":{"futureValue":[1,2]},"traceId":"0000","spanId":"0000"}`)
	got, err = otlp.Decode(strings.NewReader(raw), otlp.JSON, "", limits)
	if err != nil {
		t.Fatal(err)
	}
	r := got.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	if r.SeverityNumber != 0 || r.Body == nil || r.Body.Value != nil || !bytes.Equal(r.TraceId, []byte{0, 0}) {
		t.Fatalf("unknown-name/empty/invalid-length preservation: %v", r)
	}
}
