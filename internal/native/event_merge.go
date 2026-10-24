package native

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// MergeMinidumpEvent enriches a Sentry Native SDK event with data recovered
// from its typed Minidump attachment while preserving the client's event.
func MergeMinidumpEvent(rawEvent json.RawMessage, crash Crash, now time.Time) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawEvent, &fields); err != nil || fields == nil {
		return nil, errors.New("native event must be a JSON object")
	}
	var platform string
	if err := json.Unmarshal(fields["platform"], &platform); err != nil || platform != "native" {
		return nil, errors.New("minidump attachment requires a native event")
	}
	generated, _, err := BuildEvent(crash, nil, now)
	if err != nil {
		return nil, err
	}
	var generatedFields map[string]json.RawMessage
	if err := json.Unmarshal(generated, &generatedFields); err != nil {
		return nil, err
	}
	for _, key := range []string{"level", "message", "timestamp", "type"} {
		if isMissingJSON(fields[key]) {
			fields[key] = generatedFields[key]
		}
	}
	fields["exception"], err = mergeNativeException(fields["exception"], generatedFields["exception"])
	if err != nil {
		return nil, err
	}
	fields["debug_meta"], err = mergeNativeDebugMeta(fields["debug_meta"], generatedFields["debug_meta"])
	if err != nil {
		return nil, err
	}
	merged, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return merged, nil
}

func mergeNativeException(existing, generated json.RawMessage) (json.RawMessage, error) {
	var generatedObject map[string]json.RawMessage
	if err := json.Unmarshal(generated, &generatedObject); err != nil || generatedObject == nil {
		return nil, errors.New("generated native exception is invalid")
	}
	var generatedValues []json.RawMessage
	if err := json.Unmarshal(generatedObject["values"], &generatedValues); err != nil || len(generatedValues) == 0 {
		return nil, errors.New("generated native exception has no values")
	}
	if isMissingJSON(existing) {
		return generated, nil
	}
	var collection map[string]json.RawMessage
	if err := json.Unmarshal(existing, &collection); err == nil && collection != nil {
		valuesRaw := collection["values"]
		if isMissingJSON(valuesRaw) {
			collection["values"] = generatedObject["values"]
			return json.Marshal(collection)
		}
		var values []json.RawMessage
		if err := json.Unmarshal(valuesRaw, &values); err != nil {
			return nil, errors.New("native event exception values are invalid")
		}
		if len(values) == 0 {
			collection["values"] = generatedObject["values"]
			return json.Marshal(collection)
		}
		first, err := mergeNativeExceptionValue(values[0], generatedValues[0])
		if err != nil {
			return nil, err
		}
		values[0] = first
		collection["values"], err = json.Marshal(values)
		if err != nil {
			return nil, err
		}
		return json.Marshal(collection)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(existing, &values); err != nil {
		return nil, errors.New("native event exception is invalid")
	}
	if len(values) == 0 {
		return generated, nil
	}
	first, err := mergeNativeExceptionValue(values[0], generatedValues[0])
	if err != nil {
		return nil, err
	}
	values[0] = first
	return json.Marshal(values)
}

func mergeNativeExceptionValue(existing, generated json.RawMessage) (json.RawMessage, error) {
	var current map[string]json.RawMessage
	if err := json.Unmarshal(existing, &current); err != nil {
		return nil, errors.New("native event exception value is invalid")
	}
	var recovered map[string]json.RawMessage
	if err := json.Unmarshal(generated, &recovered); err != nil || recovered == nil {
		return nil, errors.New("generated native exception value is invalid")
	}
	if current == nil {
		current = make(map[string]json.RawMessage)
	}
	for _, key := range []string{"type", "value", "mechanism"} {
		if isMissingJSON(current[key]) {
			current[key] = recovered[key]
		}
	}
	current["stacktrace"] = recovered["stacktrace"]
	return json.Marshal(current)
}

func mergeNativeDebugMeta(existing, generated json.RawMessage) (json.RawMessage, error) {
	var recovered map[string]json.RawMessage
	if err := json.Unmarshal(generated, &recovered); err != nil || recovered == nil {
		return nil, errors.New("generated native debug metadata is invalid")
	}
	var current map[string]json.RawMessage
	if !isMissingJSON(existing) {
		if err := json.Unmarshal(existing, &current); err != nil {
			current = nil
		}
	}
	if current == nil {
		current = make(map[string]json.RawMessage)
	}
	var currentImages []json.RawMessage
	if raw := current["images"]; !isMissingJSON(raw) {
		if err := json.Unmarshal(raw, &currentImages); err != nil {
			currentImages = nil
		}
	}
	var recoveredImages []json.RawMessage
	if err := json.Unmarshal(recovered["images"], &recoveredImages); err != nil {
		return nil, errors.New("generated native debug images are invalid")
	}
	merged := make([]json.RawMessage, 0, len(currentImages)+len(recoveredImages))
	positions := make(map[string]int, len(currentImages)+len(recoveredImages))
	for _, image := range currentImages {
		key, identified := nativeDebugImageKey(image)
		if identified {
			if _, duplicate := positions[key]; duplicate {
				continue
			}
			positions[key] = len(merged)
		}
		merged = append(merged, image)
	}
	for _, image := range recoveredImages {
		key, identified := nativeDebugImageKey(image)
		if identified {
			if position, exists := positions[key]; exists {
				merged[position] = image
				continue
			}
			positions[key] = len(merged)
		}
		merged = append(merged, image)
	}
	images, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	current["images"] = images
	return json.Marshal(current)
}

func nativeDebugImageKey(raw json.RawMessage) (string, bool) {
	var image struct {
		DebugID   string `json:"debug_id"`
		ImageAddr string `json:"image_addr"`
	}
	if err := json.Unmarshal(raw, &image); err != nil || image.ImageAddr == "" {
		return "", false
	}
	return image.DebugID + "\x00" + image.ImageAddr, true
}

func isMissingJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}
