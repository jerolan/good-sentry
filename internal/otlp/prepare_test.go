package otlp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"goodsentry/internal/otlp"
	"google.golang.org/protobuf/proto"
)

var received = time.Date(2026, 10, 15, 22, 3, 4, 123456789, time.FixedZone("test", -6*3600))

func TestPrepareSearchAndSelectedDetail(t *testing.T) {
	raw := `{"resourceLogs":[{"schemaUrl":"resource://one","resource":{"attributes":[{"key":"service.name","value":{"stringValue":"old"}},{"key":"service.name","value":{"intValue":"7"}},{"key":"service.name","value":{"stringValue":"svc\u0000<>&\u2028"}},{"key":"deployment.environment.name","value":{"stringValue":""}},{"key":"deployment.environment","value":{"stringValue":"ignored"}}]},"scopeLogs":[{"schemaUrl":"scope://one","scope":{"name":"instrument","version":"1","attributes":[{"key":"service.name","value":{"stringValue":"scope-only"}}]},"logRecords":[{"timeUnixNano":"18446744073709551615","observedTimeUnixNano":"1","severityNumber":25,"severityText":"ERROR","body":{"stringValue":"hello\u0000<script>"},"attributes":[{"key":"environment","value":{"stringValue":"not-used"}}],"traceId":"00112233445566778899AABBCCDDEEFF","spanId":"0102030405060708","flags":259},{"observedTimeUnixNano":"1000000001","severityNumber":17,"body":{"bytesValue":"AP8="},"traceId":"00000000000000000000000000000000","spanId":"0102030405060708"}]}]},{"scopeLogs":[{"logRecords":[{"attributes":[{"key":"service.name","value":{"stringValue":"record-service"}},{"key":"sentry.environment","value":{"stringValue":"record-env"}}]}]}]}]}`
	limits := otlp.Limits{MaxBytes: 16384, MaxNodes: 10000, MaxRecords: 20}
	b, err := otlp.Prepare(strings.NewReader(raw), otlp.JSON, "", limits, received)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != 3 || b.ProfilesWarning || !b.ReceivedAt.Equal(received.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("batch metadata: records=%d warning=%v received=%v", len(b.Records), b.ProfilesWarning, b.ReceivedAt)
	}
	first := b.Records[0]
	if first.Level != "unknown" || first.Body != `hello\u0000<script>` || first.Service != "svc\\u0000<>&\u2028" || first.Environment != "" || first.TraceID != "00112233445566778899aabbccddeeff" || first.SpanID != "0102030405060708" || first.TraceFlags != 3 || first.TimestampSource != "event" || !first.Timestamp.Equal(time.Date(2554, 7, 21, 23, 34, 33, 709551000, time.UTC)) {
		t.Fatalf("first projection: %+v", first)
	}
	second := b.Records[1]
	if second.Level != "error" || second.Body != `{"bytesValue":"AP8="}` || second.TraceID != "" || second.SpanID != "" || second.TimestampSource != "observed" || !second.Timestamp.Equal(time.Unix(1, 0).UTC()) {
		t.Fatalf("second projection: %+v", second)
	}
	third := b.Records[2]
	if third.Service != "record-service" || third.Environment != "record-env" || third.Body != "" || third.Level != "unknown" || third.TimestampSource != "received" || !third.Timestamp.Equal(b.ReceivedAt) {
		t.Fatalf("third projection: %+v", third)
	}
	var stored logspb.LogsData
	if err := proto.Unmarshal(b.Payload, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ResourceLogs[0].ScopeLogs[0].LogRecords[0].TimeUnixNano != math.MaxUint64 || stored.ResourceLogs[0].Resource.Attributes[2].Value.GetStringValue() != "svc\x00<>&\u2028" {
		t.Fatal("canonical payload lost exact values")
	}
	selected, err := otlp.Select(b.Payload, 0, b.ReceivedAt, limits)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Resource          json.RawMessage `json:"resource"`
		ResourceSchemaURL string          `json:"resourceSchemaUrl"`
		Scope             json.RawMessage `json:"scope"`
		ScopeSchemaURL    string          `json:"scopeSchemaUrl"`
		LogRecord         struct {
			Time     string `json:"timeUnixNano"`
			Severity int32  `json:"severityNumber"`
			Body     struct {
				String string `json:"stringValue"`
			} `json:"body"`
			Trace string `json:"traceId"`
		} `json:"logRecord"`
	}
	if err := json.Unmarshal(selected.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.ResourceSchemaURL != "resource://one" || d.ScopeSchemaURL != "scope://one" || d.LogRecord.Time != "18446744073709551615" || d.LogRecord.Severity != 25 || d.LogRecord.Body.String != "hello\x00<script>" || d.LogRecord.Trace != "00112233445566778899aabbccddeeff" || selected.TimestampSource != "event" || selected.TraceFlags != 3 {
		t.Fatal("selected context or exact values lost")
	}
	if bytes.Contains(selected.Data, []byte(`AP8=`)) || bytes.Contains(selected.Data, []byte(`record-service`)) {
		t.Fatal("selected detail leaked another record")
	}
	emptyContext, err := otlp.Select(b.Payload, 2, b.ReceivedAt, limits)
	if err != nil {
		t.Fatal(err)
	}
	var missing map[string]json.RawMessage
	if err := json.Unmarshal(emptyContext.Data, &missing); err != nil {
		t.Fatal(err)
	}
	if string(missing["resource"]) != "null" || string(missing["scope"]) != "null" {
		t.Fatal("missing contexts must be explicit null")
	}
	for _, index := range []int{-1, 3} {
		if _, err := otlp.Select(b.Payload, index, b.ReceivedAt, limits); !errors.Is(err, otlp.ErrMalformed) {
			t.Fatalf("invalid ordinal%d: %v", index, err)
		}
	}
}

func TestPrepareSeverityCorrelationAndProfiles(t *testing.T) {
	severities := []struct {
		number int32
		level  string
	}{{-1, "unknown"}, {0, "unknown"}, {1, "trace"}, {4, "trace"}, {5, "debug"}, {8, "debug"}, {9, "info"}, {12, "info"}, {13, "warn"}, {16, "warn"}, {17, "error"}, {20, "error"}, {21, "fatal"}, {24, "fatal"}, {25, "unknown"}}
	records := make([]*logspb.LogRecord, 0, len(severities))
	for _, s := range severities {
		records = append(records, &logspb.LogRecord{SeverityNumber: logspb.SeverityNumber(s.number)})
	}
	records[0].TraceId = bytes.Repeat([]byte{1}, 16)
	records[0].SpanId = []byte{1}
	records[1].TraceId = []byte{1}
	records[1].SpanId = bytes.Repeat([]byte{2}, 8)
	data := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}}}}}
	raw, err := proto.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	limits := otlp.Limits{MaxBytes: 8192, MaxNodes: 10000, MaxRecords: 100}
	b, err := otlp.Prepare(bytes.NewReader(raw), otlp.Protobuf, "", limits, received)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range severities {
		if b.Records[i].Level != s.level {
			t.Errorf("severity%d = %q", s.number, b.Records[i].Level)
		}
	}
	if b.Records[0].TraceID != strings.Repeat("01", 16) || b.Records[0].SpanID != "" || b.Records[1].TraceID != "" || b.Records[1].SpanID != "" {
		t.Fatal("invalid IDs must not contribute correlation")
	}
	cases := []struct {
		name, raw     string
		warning       bool
		body, service string
	}{
		{"implicit-key-zero", `{"attributes":[{"key":"service.name","keyStrindex":0,"value":{"stringValue":"ok"}}]}`, false, "", "ok"},
		{"unresolved-key", `{"attributes":[{"key":"service.name","keyStrindex":7,"value":{"stringValue":"unavailable"}}]}`, true, "", ""},
		{"present-string-zero", `{"body":{"stringValueStrindex":0}}`, true, "", ""},
		{"nested-index", `{"body":{"arrayValue":{"values":[{"kvlistValue":{"values":[{"keyStrindex":2,"value":{"intValue":"7"}}]}}]}}}`, true, `{"arrayValue":{"values":[{"kvlistValue":{"values":[{"value":{"intValue":"7"},"keyStrindex":2}]}}]}}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[` + c.raw + `]}]}]}`
			b, e := otlp.Prepare(strings.NewReader(raw), otlp.JSON, "", limits, received)
			if e != nil {
				t.Fatal(e)
			}
			bodyMatches := b.Records[0].Body == c.body
			if c.name == "nested-index" {
				var got, want any
				var compact bytes.Buffer
				if err := json.Unmarshal([]byte(b.Records[0].Body), &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(c.body), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Compact(&compact, []byte(b.Records[0].Body)); err != nil {
					t.Fatal(err)
				}
				bodyMatches = reflect.DeepEqual(got, want) && compact.String() == b.Records[0].Body
			}
			if b.ProfilesWarning != c.warning || !bodyMatches || b.Records[0].Service != c.service {
				t.Fatalf("warning=%v body=%q service=%q", b.ProfilesWarning, b.Records[0].Body, b.Records[0].Service)
			}
		})
	}
}

func TestPrepareExpandedAdmission(t *testing.T) {
	limits := otlp.Limits{MaxBytes: 2048, MaxNodes: 10000, MaxRecords: 100}
	makeBatch := func(text string, n int) *logspb.LogsData {
		records := make([]*logspb.LogRecord, n)
		for i := range records {
			records[i] = &logspb.LogRecord{}
		}
		return &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: text}}}}}, ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}}}}}
	}
	for _, tc := range []struct {
		name string
		data *logspb.LogsData
		want error
	}{
		{"ascii-fits-exact", makeBatch(strings.Repeat("a", 1500), 1), nil},
		{"nul-expanded-detail", makeBatch(strings.Repeat("\x00", 1500), 1), otlp.ErrTooLarge},
		{"html-expanded-detail", makeBatch(strings.Repeat("<", 1500), 1), otlp.ErrTooLarge},
		{"repeated-search-projection", makeBatch(strings.Repeat("a", 600), 4), otlp.ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, e := proto.Marshal(tc.data)
			if e != nil {
				t.Fatal(e)
			}
			if int64(len(raw)) > limits.MaxBytes {
				t.Fatal("fixture exceeds wire budget instead of derived budget")
			}
			b, e := otlp.Prepare(bytes.NewReader(raw), otlp.Protobuf, "", limits, received)
			if !errors.Is(e, tc.want) {
				t.Fatalf("Prepare=%v want%v", e, tc.want)
			}
			if e == nil {
				selection, e := otlp.Select(b.Payload, 0, b.ReceivedAt, limits)
				if e != nil {
					t.Fatal(e)
				}
				native, e := json.Marshal(struct {
					Body, Service, Environment string
					Detail                     *otlp.Selection
				}{b.Records[0].Body, b.Records[0].Service, b.Records[0].Environment, selection})
				if e != nil {
					t.Fatal(e)
				}
				if int64(len(native)) > 2*limits.MaxBytes+4096 {
					t.Fatal("accepted representation exceeds detail budget")
				}
			}
		})
	}
	value := &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "deep"}}
	for range 20 {
		value = &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: []*commonpb.AnyValue{value}}}}
	}
	deep := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{Body: value}}}}}}}
	raw, e := proto.Marshal(deep)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := otlp.Decode(bytes.NewReader(raw), otlp.Protobuf, "", limits); e != nil {
		t.Fatalf("fixture must fit binary depth: %v", e)
	}
	if _, e := otlp.Prepare(bytes.NewReader(raw), otlp.Protobuf, "", limits, received); !errors.Is(e, otlp.ErrTooLarge) {
		t.Fatalf("selected JSON expansion depth: %v", e)
	}
	if _, e := otlp.Prepare(bytes.NewReader(nil), otlp.Protobuf, "", limits, received); e != nil {
		t.Fatalf("valid empty batch: %v", e)
	}
}

// JSON counts one object value for each empty record, while the persisted
// protobuf reader counts both its field and embedded message. Admission must
// also protect reopening the canonical representation with unchanged limits.
func TestPrepareRejectsUnreadableCanonicalBudgets(t *testing.T) {
	limits := otlp.Limits{MaxBytes: 4096, MaxNodes: 100, MaxRecords: 100}
	raw := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[` + strings.Repeat(`{},`, 49) + `{}]}]}]}`
	if _, err := otlp.Decode(strings.NewReader(raw), otlp.JSON, "", limits); err != nil {
		t.Fatalf("fixture exceeds original JSON budget: %v", err)
	}
	if _, err := otlp.Prepare(strings.NewReader(raw), otlp.JSON, "", limits, received); !errors.Is(err, otlp.ErrTooLarge) {
		t.Fatalf("canonical reopen budget must be admitted before persistence: %v", err)
	}
}

func TestSelectTypedValuesAndExactRepresentationCapacity(t *testing.T) {
	content := strings.Repeat("<>&\u2028\u2029", 100) + "\x00\\\"\b\f\n\r\t"
	values := []struct {
		key   string
		value *commonpb.AnyValue
		json  string
	}{
		{"minimum", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: math.MinInt64}}, `{"intValue":"-9223372036854775808"}`},
		{"maximum", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: math.MaxInt64}}, `{"intValue":"9223372036854775807"}`},
		{"false", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: false}}, `{"boolValue":false}`},
		{"bytes", &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: []byte{0, 255}}}, `{"bytesValue":"AP8="}`},
		{"empty-bytes", &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{}}, `{"bytesValue":""}`},
		{"empty-string", &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{}}, `{"stringValue":""}`},
		{"empty-array", &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{}}}, `{"arrayValue":{}}`},
		{"empty-kvlist", &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{}}}, `{"kvlistValue":{}}`},
		{"empty-any", &commonpb.AnyValue{}, `{}`},
		{"nan", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: math.NaN()}}, `{"doubleValue":"NaN"}`},
		{"positive-inf", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: math.Inf(1)}}, `{"doubleValue":"Infinity"}`},
		{"negative-inf", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: math.Inf(-1)}}, `{"doubleValue":"-Infinity"}`},
		{"negative-zero", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: math.Copysign(0, -1)}}, `{"doubleValue":-0}`},
		{"profile-zero", &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValueStrindex{}}, `{"stringValueStrindex":0}`},
	}
	attributes := make([]*commonpb.KeyValue, 0, len(values))
	for _, v := range values {
		attributes = append(attributes, &commonpb.KeyValue{Key: v.key, Value: v.value})
	}
	record := &logspb.LogRecord{Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: []*commonpb.AnyValue{{Value: &commonpb.AnyValue_StringValue{StringValue: content}}}}}}, Attributes: attributes, SeverityNumber: logspb.SeverityNumber(math.MinInt32), SeverityText: "separate", TraceId: []byte{1}, SpanId: []byte{2}, Flags: math.MaxUint32, DroppedAttributesCount: math.MaxUint32, EventName: "event\x00name"}
	data := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{record}}}}}}
	payload, err := proto.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	limits := otlp.Limits{MaxBytes: 65536, MaxNodes: 10000, MaxRecords: 10}
	batch, err := otlp.Prepare(bytes.NewReader(payload), otlp.Protobuf, "", limits, received)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := otlp.Select(batch.Payload, 0, batch.ReceivedAt, limits)
	if err != nil {
		t.Fatal(err)
	}
	var context struct {
		Resource          json.RawMessage `json:"resource"`
		ResourceSchemaURL string          `json:"resourceSchemaUrl"`
		Scope             json.RawMessage `json:"scope"`
		ScopeSchemaURL    string          `json:"scopeSchemaUrl"`
		Record            json.RawMessage `json:"logRecord"`
	}
	if err := json.Unmarshal(selected.Data, &context); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Attributes []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"attributes"`
		Body struct {
			Array struct {
				Values []struct {
					String string `json:"stringValue"`
				} `json:"values"`
			} `json:"arrayValue"`
		} `json:"body"`
		Flags    uint32 `json:"flags"`
		Dropped  uint32 `json:"droppedAttributesCount"`
		Severity int32  `json:"severityNumber"`
		Trace    string `json:"traceId"`
		Span     string `json:"spanId"`
		Event    string `json:"eventName"`
	}
	if err := json.Unmarshal(context.Record, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Attributes) != len(values) || len(got.Body.Array.Values) != 1 || got.Body.Array.Values[0].String != content || got.Flags != math.MaxUint32 || got.Dropped != math.MaxUint32 || got.Severity != math.MinInt32 || got.Trace != "01" || got.Span != "02" || got.Event != "event\x00name" || selected.TraceFlags != 255 || !batch.ProfilesWarning {
		t.Fatal("selected known fields were normalized, lost or resolved")
	}
	for i, want := range values {
		var actual, expected any
		if err := json.Unmarshal(got.Attributes[i].Value, &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(want.json), &expected); err != nil {
			t.Fatal(err)
		}
		if got.Attributes[i].Key != want.key || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("typed attribute %s: %s", want.key, got.Attributes[i].Value)
		}
		if want.key == "negative-zero" && !math.Signbit(actual.(map[string]any)["doubleValue"].(float64)) {
			t.Fatal("negative-zero sign lost")
		}
	}
	// encoding/json is the independent public response serializer. Derive the
	// exact dynamic budget from its actual token lengths, not private counters.
	dynamic := int64(0)
	for _, component := range []any{context.Resource, context.ResourceSchemaURL, context.Scope, context.ScopeSchemaURL, context.Record, batch.Records[0].Body, batch.Records[0].Service, batch.Records[0].Environment, batch.Records[0].Level, batch.Records[0].TraceID, batch.Records[0].SpanID} {
		raw, e := json.Marshal(component)
		if e != nil {
			t.Fatal(e)
		}
		dynamic += int64(len(raw))
	}
	minimum := (dynamic + 1) / 2
	projection := batch.Records[0]
	projectionBytes := len(projection.Body) + len(projection.Service) + len(projection.Environment) + len(projection.Level) + len(projection.TraceID) + len(projection.SpanID)
	if minimum <= int64(len(payload)) || minimum <= int64(projectionBytes) {
		t.Fatal("fixture must bind expanded selected JSON rather than wire/projection capacity")
	}
	limits.MaxBytes = minimum
	if _, err := otlp.Prepare(bytes.NewReader(payload), otlp.Protobuf, "", limits, received); err != nil {
		t.Fatalf("exact fitting representation rejected: %v", err)
	}
	limits.MaxBytes--
	if _, err := otlp.Prepare(bytes.NewReader(payload), otlp.Protobuf, "", limits, received); !errors.Is(err, otlp.ErrTooLarge) {
		t.Fatalf("one-byte-too-small capacity: %v", err)
	}
}
