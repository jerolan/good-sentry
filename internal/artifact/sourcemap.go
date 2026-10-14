// Package artifact remaps JavaScript frames using uploaded source maps only.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const maxSourceMapMappings = 1_000_000

type SourceMap struct {
	Name        string
	DebugID     string
	Data        []byte
	DebugIDOnly bool // Only the matching debug image may select this artifact.
}

type parsedMap struct {
	SourceMap
	consumer *sourceMapConsumer
}

// ValidateSourceMap checks the upload before its bytes become durable artifacts.
func ValidateSourceMap(name string, data []byte) error {
	_, err := parseSourceMap(name, data)
	return err
}

// Symbolicate retains original stacks in raw_stacktrace and modifies only
// frames for which an uploaded map contains a mapping. It never fetches sources.
func Symbolicate(raw json.RawMessage, maps []SourceMap) (json.RawMessage, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil || event == nil {
		return nil, errors.New("event must be a JSON object")
	}
	parsed := make([]parsedMap, 0, len(maps))
	mappingCount := 0
	for _, m := range maps {
		consumer, err := parseSourceMap(m.Name, m.Data)
		if err != nil {
			return nil, fmt.Errorf("parse source map %q: %w", m.Name, err)
		}
		mappingCount += consumer.MappingCount()
		if mappingCount > maxSourceMapMappings {
			return nil, fmt.Errorf("%w: selected source maps exceed the mapping limit", ErrSourceMapLimit)
		}
		parsed = append(parsed, parsedMap{SourceMap: m, consumer: consumer})
	}
	images := debugImages(event["debug_meta"])
	changed := symbolicateStack(event, parsed, images)
	var exceptions map[string]json.RawMessage
	wrapped := json.Unmarshal(event["exception"], &exceptions) == nil && exceptions != nil
	rawValues := event["exception"]
	if wrapped {
		rawValues = exceptions["values"]
	}
	var values []json.RawMessage
	if json.Unmarshal(rawValues, &values) == nil {
		changedExceptions := false
		for i, raw := range values {
			var value map[string]json.RawMessage
			if json.Unmarshal(raw, &value) == nil && value != nil && symbolicateStack(value, parsed, images) {
				values[i], _ = json.Marshal(value)
				changedExceptions = true
			}
		}
		if changedExceptions {
			if wrapped {
				exceptions["values"], _ = json.Marshal(values)
				event["exception"], _ = json.Marshal(exceptions)
			} else {
				event["exception"], _ = json.Marshal(values)
			}
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(event)
}

type debugImage struct {
	Type     string `json:"type"`
	CodeFile string `json:"code_file"`
	DebugID  string `json:"debug_id"`
}

func debugImages(raw json.RawMessage) []debugImage {
	var meta struct {
		Images []debugImage `json:"images"`
	}
	_ = json.Unmarshal(raw, &meta)
	return meta.Images
}

func symbolicateStack(owner map[string]json.RawMessage, maps []parsedMap, images []debugImage) bool {
	stackRaw := owner["stacktrace"]
	if raw := owner["raw_stacktrace"]; len(raw) > 0 {
		var existing map[string]json.RawMessage
		var frames []json.RawMessage
		if json.Unmarshal(raw, &existing) == nil && existing != nil && json.Unmarshal(existing["frames"], &frames) == nil && len(frames) > 0 {
			stackRaw = raw
		}
	}
	var stack map[string]json.RawMessage
	if json.Unmarshal(stackRaw, &stack) != nil || stack == nil {
		return false
	}
	var frames []json.RawMessage
	if json.Unmarshal(stack["frames"], &frames) != nil {
		return false
	}
	changed := false
	for i, raw := range frames {
		var frame map[string]json.RawMessage
		if json.Unmarshal(raw, &frame) != nil || frame == nil {
			continue
		}
		var absPath string
		var line, column int
		if json.Unmarshal(frame["abs_path"], &absPath) != nil || absPath == "" || json.Unmarshal(frame["lineno"], &line) != nil || line < 1 {
			continue
		}
		if rawColumn, ok := frame["colno"]; ok && json.Unmarshal(rawColumn, &column) != nil {
			continue
		}
		if column < 0 {
			continue
		}
		m := matchingMap(absPath, maps, images)
		if m == nil {
			continue
		}
		// The Sentry wire format uses one-based line AND column numbers.
		// Source maps use zero-based columns. A missing/zero column tolerates
		// SDKs that cannot determine a column and searches the first column.
		if column > 0 {
			column--
		}
		source, name, originalLine, originalColumn, ok := m.consumer.Source(line, column)
		if !ok || source == "" || originalLine < 1 || originalColumn < 0 {
			continue
		}
		frame["filename"], _ = json.Marshal(source)
		frame["lineno"], _ = json.Marshal(originalLine)
		frame["colno"], _ = json.Marshal(originalColumn + 1)
		if name != "" {
			frame["function"], _ = json.Marshal(name)
		}
		applyContext(frame, m.consumer.SourceContent(line, column), originalLine)
		frames[i], _ = json.Marshal(frame)
		changed = true
	}
	if !changed {
		return false
	}
	if _, exists := owner["raw_stacktrace"]; !exists {
		owner["raw_stacktrace"] = stackRaw
	}
	// Existing annotations may be absent from raw_stacktrace. Keep them while
	// replacing its frames with mappings of the original raw locations.
	var existing map[string]json.RawMessage
	if json.Unmarshal(owner["stacktrace"], &existing) == nil {
		for key, value := range existing {
			if key != "frames" {
				stack[key] = value
			}
		}
	}
	stack["frames"], _ = json.Marshal(frames)
	owner["stacktrace"], _ = json.Marshal(stack)
	return true
}

func matchingMap(absPath string, maps []parsedMap, images []debugImage) *parsedMap {
	for _, image := range images {
		if image.Type != "sourcemap" || image.CodeFile != absPath || image.DebugID == "" {
			continue
		}
		var found *parsedMap
		for i := range maps {
			if normalizeDebugID(maps[i].DebugID) == normalizeDebugID(image.DebugID) {
				if found != nil {
					return nil
				}
				found = &maps[i]
			}
		}
		// An explicit build identity cannot safely fall back to another map
		// with the same filename when that build's artifact is unavailable.
		return found
	}
	var found *parsedMap
	frameName := cleanURL(absPath)
	for i := range maps {
		if maps[i].DebugIDOnly {
			continue
		}
		name := strings.TrimSuffix(cleanURL(maps[i].Name), ".map")
		matches := name == frameName
		if strings.HasPrefix(name, "~/") {
			if frameURL, err := url.Parse(frameName); err == nil {
				matches = strings.TrimPrefix(name, "~") == frameURL.Path
			}
		}
		if matches {
			if found != nil {
				return nil
			}
			found = &maps[i]
		}
	}
	return found
}

func normalizeDebugID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))
}

func cleanURL(value string) string {
	if u, err := url.Parse(value); err == nil {
		u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
		return u.String()
	}
	return value
}

func applyContext(frame map[string]json.RawMessage, source string, line int) {
	if source == "" {
		return
	}
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if line < 1 || line > len(lines) {
		return
	}
	index := line - 1
	start, end := max(0, index-5), min(len(lines), index+6)
	frame["context_line"], _ = json.Marshal(lines[index])
	frame["pre_context"], _ = json.Marshal(lines[start:index])
	frame["post_context"], _ = json.Marshal(lines[index+1 : end])
}

func validateMap(raw json.RawMessage) error {
	_, err := parseSourceMap("", raw)
	return err
}
