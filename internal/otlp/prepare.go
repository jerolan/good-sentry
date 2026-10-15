package otlp

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Batch is the fully admitted input to durable OTLP ingestion. Records retain
// occurrence order; Payload contains each shared resource/scope only once.
type Batch struct {
	Payload         []byte
	ReceivedAt      time.Time
	Records         []Record
	ProfilesWarning bool
}

// Record contains searchable projections; canonical typed data stays in Payload.
type Record struct {
	Timestamp                                          time.Time
	Level, Body, Service, Environment, TraceID, SpanID string
	TimestampSource                                    string
	TraceFlags                                         uint32
}

// Selection contains only one record and its own resource/scope contexts.
type Selection struct {
	Data            json.RawMessage `json:"data"`
	TimestampSource string          `json:"timestampSource"`
	ReceivedAt      time.Time       `json:"receivedAt"`
	TraceFlags      uint32          `json:"traceFlags"`
}

// Prepare validates every projection and selected JSON representation before the
// caller begins a transaction. No record is discarded or deduplicated.
func Prepare(body io.Reader, encoding Encoding, contentEncoding string, limits Limits, receivedAt time.Time) (*Batch, error) {
	data, err := Decode(body, encoding, contentEncoding, limits)
	if err != nil {
		return nil, err
	}
	receivedAt, err = receiverTime(receivedAt)
	if err != nil {
		return nil, err
	}
	if limits.MaxBytes > math.MaxInt64/2 {
		return nil, ErrMalformed
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(data)
	if err != nil {
		return nil, ErrMalformed
	}
	// JSON and canonical protobuf have distinct preallocation node counts.
	// Admit the stored representation now so Select can reopen every receipt
	// under the same capacities.
	canonicalBudget := admission{limits: limits}
	if _, err := canonicalBudget.wire(payload, data.ProtoReflect().Descriptor(), 1, 0); err != nil {
		return nil, err
	}
	warning, err := profileIndices(data.ProtoReflect(), &admission{limits: limits}, 1)
	if err != nil {
		return nil, err
	}
	batch := &Batch{Payload: payload, ReceivedAt: receivedAt, Records: []Record{}, ProfilesWarning: warning}
	var projectionBytes int64
	for _, resource := range data.ResourceLogs {
		resourceValues := attributeStrings(resource.GetResource().GetAttributes())
		var resourceJSON *jsonContext
		for _, scope := range resource.ScopeLogs {
			if len(scope.LogRecords) == 0 {
				continue
			}
			if resourceJSON == nil {
				context, e := measureContext(resource.GetResource(), resource.SchemaUrl, limits)
				if e != nil {
					return nil, e
				}
				resourceJSON = &context
			}
			scopeJSON, e := measureContext(scope.GetScope(), scope.SchemaUrl, limits)
			if e != nil {
				return nil, e
			}
			for _, record := range scope.LogRecords {
				planned, e := planRecord(record, resourceValues, receivedAt, limits, limits.MaxBytes-projectionBytes)
				if e != nil {
					return nil, e
				}
				if e := admitSelected(*resourceJSON, scopeJSON, record, planned.quotedBytes, limits); e != nil {
					return nil, e
				}
				projected, e := planned.materialize()
				if e != nil {
					return nil, e
				}
				projectionBytes += planned.bytes
				batch.Records = append(batch.Records, projected)
			}
		}
	}
	return batch, nil
}

type stringProjection struct {
	value   string
	present bool
}
type searchAttributes struct{ service, environmentName, environment, sentryEnvironment, recordEnvironment stringProjection }

func attributeStrings(attributes []*commonpb.KeyValue) searchAttributes {
	var result searchAttributes
	for _, attribute := range attributes {
		if attribute.GetKeyStrindex() != 0 {
			continue
		}
		value, ok := attribute.GetValue().GetValue().(*commonpb.AnyValue_StringValue)
		if !ok {
			continue
		}
		projected := stringProjection{value.StringValue, true}
		switch attribute.GetKey() {
		case "service.name":
			result.service = projected
		case "deployment.environment.name":
			result.environmentName = projected
		case "deployment.environment":
			result.environment = projected
		case "environment":
			result.recordEnvironment = projected
		case "sentry.environment":
			result.sentryEnvironment = projected
		}
	}
	return result
}

func firstString(values ...stringProjection) string {
	for _, value := range values {
		if value.present {
			return value.value
		}
	}
	return ""
}

// A projection plan retains references to admitted input values. Its exact
// sizes are checked together with the selected detail before materialization.
type projectionPlan struct {
	record             Record
	jsonBody           *commonpb.AnyValue
	bytes, quotedBytes int64
}

func planRecord(record *logspb.LogRecord, resource searchAttributes, receivedAt time.Time, limits Limits, remaining int64) (projectionPlan, error) {
	attributes := attributeStrings(record.Attributes)
	result := Record{Level: severity(record.SeverityNumber), TimestampSource: "received", Timestamp: receivedAt, TraceFlags: record.Flags & 0xff}
	nanos := record.TimeUnixNano
	if nanos != 0 {
		result.TimestampSource = "event"
	} else if record.ObservedTimeUnixNano != 0 {
		nanos = record.ObservedTimeUnixNano
		result.TimestampSource = "observed"
	}
	if nanos != 0 {
		result.Timestamp = time.Unix(int64(nanos/1_000_000_000), int64(nanos%1_000_000_000)).UTC().Truncate(time.Microsecond)
	}
	if validID(record.TraceId, 16) {
		result.TraceID = hex.EncodeToString(record.TraceId)
		if validID(record.SpanId, 8) {
			result.SpanID = hex.EncodeToString(record.SpanId)
		}
	}
	result.Service = firstString(resource.service, attributes.service)
	result.Environment = firstString(resource.environmentName, resource.environment, attributes.sentryEnvironment, attributes.recordEnvironment)
	plan := projectionPlan{record: result}
	if record.Body != nil {
		switch value := record.Body.Value.(type) {
		case nil, *commonpb.AnyValue_StringValueStrindex:
		case *commonpb.AnyValue_StringValue:
			plan.record.Body = value.StringValue
		default:
			bodyLimits := limits
			bodyLimits.MaxBytes = remaining
			measured, quoted, err := measureJSONProjection(record.Body, bodyLimits, limits.MaxBytes*2)
			if err != nil {
				return projectionPlan{}, err
			}
			plan.jsonBody = record.Body
			plan.bytes = measured.bytes
			plan.quotedBytes = quoted
		}
	}
	values := []string{plan.record.Service, plan.record.Environment, plan.record.Level, plan.record.TraceID, plan.record.SpanID}
	if plan.jsonBody == nil {
		values = append(values, plan.record.Body)
	}
	for _, raw := range values {
		size := int64(len(raw))
		zeros := int64(strings.Count(raw, "\x00"))
		if size > remaining-plan.bytes || zeros > (remaining-plan.bytes-size)/5 {
			return projectionPlan{}, ErrTooLarge
		}
		plan.bytes += size + zeros*5
		quoted, err := jsonStringSize(raw, true)
		if err != nil {
			return projectionPlan{}, err
		}
		// The visible NUL projection is six literal characters: its backslash
		// needs one more byte than the original control escape in native JSON.
		quoted += zeros
		if quoted > limits.MaxBytes*2-plan.quotedBytes {
			return projectionPlan{}, ErrTooLarge
		}
		plan.quotedBytes += quoted
	}
	return plan, nil
}

func (plan projectionPlan) materialize() (Record, error) {
	result := plan.record
	if plan.jsonBody != nil {
		raw, err := marshalJSON(plan.jsonBody)
		if err != nil {
			return Record{}, err
		}
		result.Body = string(raw)
	} else {
		result.Body = strings.ReplaceAll(result.Body, "\x00", `\u0000`)
	}
	result.Service = strings.ReplaceAll(result.Service, "\x00", `\u0000`)
	result.Environment = strings.ReplaceAll(result.Environment, "\x00", `\u0000`)
	return result, nil
}

func severity(number logspb.SeverityNumber) string {
	if number < 1 || number > 24 {
		return "unknown"
	}
	return [...]string{"trace", "debug", "info", "warn", "error", "fatal"}[(int(number)-1)/4]
}

func validID(value []byte, size int) bool {
	if len(value) != size {
		return false
	}
	for _, part := range value {
		if part != 0 {
			return true
		}
	}
	return false
}

func receiverTime(value time.Time) (time.Time, error) {
	value = value.UTC().Truncate(time.Microsecond)
	if _, err := value.MarshalJSON(); err != nil {
		return time.Time{}, ErrMalformed
	}
	return value, nil
}

// The decoded tree has already passed admission. This walk still shares the
// explicit depth/node safety invariant, including empty contexts and oneofs.
func profileIndices(message protoreflect.Message, budget *admission, depth int) (bool, error) {
	if err := budget.node(depth); err != nil {
		return false, err
	}
	var found bool
	var failure error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind || field.IsList() {
			if failure = budget.node(depth); failure != nil {
				return false
			}
		}
		switch field.FullName() {
		case "opentelemetry.proto.common.v1.AnyValue.string_value_strindex":
			found = true
			return false
		case "opentelemetry.proto.common.v1.KeyValue.key_strindex":
			if value.Int() != 0 {
				found = true
				return false
			}
		}
		if field.Kind() != protoreflect.MessageKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				found, failure = profileIndices(list.Get(i).Message(), budget, depth+1)
				if found || failure != nil {
					return false
				}
			}
		} else {
			found, failure = profileIndices(value.Message(), budget, depth+1)
		}
		return !found && failure == nil
	})
	return found, failure
}
