package otlp

import (
	"bytes"
	"encoding/json"
	"math"
	"time"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

type jsonContext struct {
	message     proto.Message
	schema      string
	measure     jsonMeasure
	schemaBytes int64
}

func measureContext(message proto.Message, schema string, limits Limits) (jsonContext, error) {
	limits.MaxBytes *= 2
	measured, err := measureNativeJSON(message, limits)
	if err != nil {
		return jsonContext{}, err
	}
	schemaBytes, err := jsonStringSize(schema, true)
	if err != nil {
		return jsonContext{}, err
	}
	return jsonContext{message, schema, measured, schemaBytes}, nil
}

func admitSelected(resource, scope jsonContext, record *logspb.LogRecord, quotedProjectionBytes int64, limits Limits) error {
	representation := limits
	representation.MaxBytes *= 2
	measured, err := measureNativeJSON(record, representation)
	if err != nil {
		return err
	}
	total := int64(0)
	for _, size := range []int64{resource.measure.bytes, resource.schemaBytes, scope.measure.bytes, scope.schemaBytes, measured.bytes} {
		if size > representation.MaxBytes-total {
			return ErrTooLarge
		}
		total += size
	}
	if quotedProjectionBytes > representation.MaxBytes-total {
		return ErrTooLarge
	}

	// The enclosing native DTO/data add two object levels. The fixed wrapper
	// contributes17 values besides the three measured message values.
	nodes := 17
	for _, value := range []jsonMeasure{resource.measure, scope.measure, measured} {
		if value.depth+2 > maxDepth || value.nodes > limits.MaxNodes-nodes {
			return ErrTooLarge
		}
		nodes += value.nodes
	}
	return nil
}

// Select reconstructs one admitted record, independently of list/time filters.
// A lower current limit can reject a historical batch rather than truncate it.
func Select(payload []byte, index int, receivedAt time.Time, limits Limits) (*Selection, error) {
	if index < 0 || limits.MaxBytes > math.MaxInt64/2 {
		return nil, ErrMalformed
	}
	data, err := Decode(bytes.NewReader(payload), Protobuf, "", limits)
	if err != nil {
		return nil, err
	}
	receivedAt, err = receiverTime(receivedAt)
	if err != nil {
		return nil, err
	}
	ordinal := 0
	for _, resource := range data.ResourceLogs {
		for _, scope := range resource.ScopeLogs {
			for _, record := range scope.LogRecords {
				if ordinal != index {
					ordinal++
					continue
				}
				resourceJSON, err := measureContext(resource.GetResource(), resource.SchemaUrl, limits)
				if err != nil {
					return nil, err
				}
				scopeJSON, err := measureContext(scope.GetScope(), scope.SchemaUrl, limits)
				if err != nil {
					return nil, err
				}
				planned, err := planRecord(record, attributeStrings(resource.GetResource().GetAttributes()), receivedAt, limits, limits.MaxBytes)
				if err != nil {
					return nil, err
				}
				if err := admitSelected(resourceJSON, scopeJSON, record, planned.quotedBytes, limits); err != nil {
					return nil, err
				}
				resourceRaw, err := marshalContext(resourceJSON.message)
				if err != nil {
					return nil, err
				}
				scopeRaw, err := marshalContext(scopeJSON.message)
				if err != nil {
					return nil, err
				}
				recordRaw, err := marshalJSON(record)
				if err != nil {
					return nil, err
				}
				raw, err := json.Marshal(struct {
					Resource          json.RawMessage `json:"resource"`
					ResourceSchemaURL string          `json:"resourceSchemaUrl"`
					Scope             json.RawMessage `json:"scope"`
					ScopeSchemaURL    string          `json:"scopeSchemaUrl"`
					LogRecord         json.RawMessage `json:"logRecord"`
				}{resourceRaw, resource.SchemaUrl, scopeRaw, scope.SchemaUrl, recordRaw})
				if err != nil {
					return nil, ErrMalformed
				}
				return &Selection{raw, planned.record.TimestampSource, receivedAt, planned.record.TraceFlags}, nil
			}
		}
	}
	return nil, ErrMalformed
}

func marshalContext(message proto.Message) ([]byte, error) {
	if message == nil || !message.ProtoReflect().IsValid() {
		return []byte("null"), nil
	}
	return marshalJSON(message)
}
