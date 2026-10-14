// Package artifact remaps JavaScript frames using uploaded source maps only.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-sourcemap/sourcemap"
)

type SourceMap struct {
	Name    string
	DebugID string
	Data    []byte
}

type parsedMap struct {
	SourceMap
	consumer *sourcemap.Consumer
}

// Symbolicate retains original stacks in raw_stacktrace and modifies only
// frames for which an uploaded map contains a mapping. It never fetches sources.
func Symbolicate(raw json.RawMessage, maps []SourceMap) (json.RawMessage, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil || event == nil {
		return nil, errors.New("event must be a JSON object")
	}
	parsed := make([]parsedMap, 0, len(maps))
	for _, m := range maps {
		if err := validateMap(m.Data); err != nil {
			return nil, fmt.Errorf("source map %q: %w", m.Name, err)
		}
		consumer, err := sourcemap.Parse(m.Name, m.Data)
		if err != nil {
			return nil, fmt.Errorf("parse source map %q: %w", m.Name, err)
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
		applyContext(frame, m.consumer.SourceContent(source), originalLine)
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

// The consumer accepts some invalid VLQ strings without reporting an error.
// Check their syntax here; leave mapping semantics to the source-map consumer.
func validateMap(raw json.RawMessage) error {
	var header struct {
		Version  int     `json:"version"`
		Mappings *string `json:"mappings"`
		Sections []struct {
			Map json.RawMessage `json:"map"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(raw, &header); err != nil || header.Version != 3 {
		return errors.New("expected a version 3 source-map object")
	}
	if header.Mappings == nil {
		if header.Sections == nil {
			return errors.New("source map requires mappings or sections")
		}
		for _, section := range header.Sections {
			if err := validateMap(section.Map); err != nil {
				return err
			}
		}
		return nil
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	fields, continuation, afterComma := 0, false, false
	validSegment := func() bool { return !continuation && (fields == 1 || fields == 4 || fields == 5) }
	for _, c := range *header.Mappings {
		if c == ',' || c == ';' {
			if !validSegment() && !(c == ';' && fields == 0 && !continuation && !afterComma) {
				return errors.New("source-map segment must contain 1, 4 or 5 complete VLQ fields")
			}
			fields, continuation, afterComma = 0, false, c == ','
			continue
		}
		value := strings.IndexRune(alphabet, c)
		if value < 0 {
			return errors.New("source-map mappings contain invalid base64-VLQ characters")
		}
		continuation = value&32 != 0
		if !continuation {
			fields++
		}
		afterComma = false
	}
	if !validSegment() && !(fields == 0 && !continuation && !afterComma) {
		return errors.New("source-map segment must contain 1, 4 or 5 complete VLQ fields")
	}
	return nil
}
