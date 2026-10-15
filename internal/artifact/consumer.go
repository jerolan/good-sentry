package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
)

var ErrSourceMapLimit = errors.New("source-map processing limit exceeded")

// Positions remain zero-based internally and leave room for the one-based
// numbers emitted in Sentry frames. ECMA-426 VLQ payloads include the sign bit.
const maxMapPosition = math.MaxInt32 - 1

type sourceMapping struct {
	line, column, originalLine, originalColumn int32
	source, name                               int32
}

type mapSource struct {
	url, content string
	known        bool
}

type sourceMapConsumer struct {
	mappings []sourceMapping
	sources  []mapSource
	names    []string
}

func parseSourceMap(name string, data []byte) (*sourceMapConsumer, error) {
	c := &sourceMapConsumer{}
	if err := c.decode(name, data, 0, 0, 0); err != nil {
		return nil, err
	}
	sort.SliceStable(c.mappings, func(i, j int) bool {
		a, b := c.mappings[i], c.mappings[j]
		return mapPositionLess(a.line, a.column, b.line, b.column)
	})
	return c, nil
}

func (c *sourceMapConsumer) MappingCount() int { return len(c.mappings) }

func (c *sourceMapConsumer) mappingAt(line, column int) (sourceMapping, bool) {
	if line < 1 || column < 0 || line-1 > maxMapPosition || column > maxMapPosition {
		return sourceMapping{}, false
	}
	l, col := int32(line-1), int32(column)
	i := sort.Search(len(c.mappings), func(i int) bool {
		m := c.mappings[i]
		return mapPositionLess(l, col, m.line, m.column)
	}) - 1
	if i < 0 || c.mappings[i].line != l || c.mappings[i].source < 0 {
		return sourceMapping{}, false
	}
	m := c.mappings[i]
	return m, c.sources[m.source].known
}

func (c *sourceMapConsumer) Source(line, column int) (source, name string, originalLine, originalColumn int, ok bool) {
	m, ok := c.mappingAt(line, column)
	if !ok {
		return "", "", 0, 0, false
	}
	s := c.sources[m.source]
	if m.name >= 0 {
		name = c.names[m.name]
	}
	return s.url, name, int(m.originalLine) + 1, int(m.originalColumn), true
}

func (c *sourceMapConsumer) SourceContent(line, column int) string {
	m, ok := c.mappingAt(line, column)
	if !ok {
		return ""
	}
	return c.sources[m.source].content
}

func (c *sourceMapConsumer) decode(name string, raw json.RawMessage, depth int, offsetLine, offsetColumn int32) error {
	if depth > 32 {
		return fmt.Errorf("%w: nesting exceeds 32", ErrSourceMapLimit)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return errors.New("source map must be an object")
	}
	var version float64
	if json.Unmarshal(object["version"], &version) != nil || version != 3 {
		return errors.New("source-map version must be 3")
	}
	if sections, exists := object["sections"]; exists {
		return c.decodeSections(name, sections, depth, offsetLine, offsetColumn)
	}
	var mappings *string
	if json.Unmarshal(object["mappings"], &mappings) != nil || mappings == nil {
		return errors.New("source-map mappings must be a string")
	}
	var sourceValues []json.RawMessage
	if json.Unmarshal(object["sources"], &sourceValues) != nil || sourceValues == nil {
		return errors.New("source-map sources must be an array")
	}
	var root string
	if value, exists := object["sourceRoot"]; exists && string(value) != "null" {
		if json.Unmarshal(value, &root) != nil {
			return errors.New("sourceRoot must be a string")
		}
	}
	var contentValues []json.RawMessage
	if value, exists := object["sourcesContent"]; exists && string(value) != "null" {
		if json.Unmarshal(value, &contentValues) != nil || contentValues == nil {
			return errors.New("sourcesContent must be an array")
		}
	}
	contents := make([]*string, len(contentValues))
	for i, value := range contentValues {
		if json.Unmarshal(value, &contents[i]) != nil {
			return errors.New("sourcesContent entries must be strings or null")
		}
	}
	sourceStart := len(c.sources)
	for i, raw := range sourceValues {
		var source *string
		if json.Unmarshal(raw, &source) != nil {
			return errors.New("sources entries must be strings or null")
		}
		s := mapSource{}
		if source != nil {
			resolved, err := resolveMapSource(name, root, *source)
			if err != nil {
				return err
			}
			s = mapSource{url: resolved, known: true}
			if i < len(contents) && contents[i] != nil {
				s.content = *contents[i]
			}
		}
		c.sources = append(c.sources, s)
	}
	var nameValues []json.RawMessage
	if value, exists := object["names"]; exists {
		if json.Unmarshal(value, &nameValues) != nil || nameValues == nil {
			return errors.New("source-map names must be an array")
		}
	}
	nameStart := len(c.names)
	for _, raw := range nameValues {
		var name *string
		if json.Unmarshal(raw, &name) != nil || name == nil {
			return errors.New("source-map names entries must be strings")
		}
		c.names = append(c.names, *name)
	}
	if len(c.sources) > maxMapPosition || len(c.names) > maxMapPosition {
		return errors.New("source-map index space exceeds supported range")
	}
	return c.decodeMappings(*mappings, sourceStart, len(sourceValues), nameStart, len(nameValues), offsetLine, offsetColumn)
}

func (c *sourceMapConsumer) decodeSections(name string, raw json.RawMessage, depth int, parentLine, parentColumn int32) error {
	var sections []json.RawMessage
	if json.Unmarshal(raw, &sections) != nil || sections == nil {
		return errors.New("source-map sections must be an array")
	}
	var previousLine, previousColumn int32
	previous := false
	for _, raw := range sections {
		var section struct {
			Offset struct {
				Line   json.RawMessage `json:"line"`
				Column json.RawMessage `json:"column"`
			} `json:"offset"`
			Map json.RawMessage `json:"map"`
			URL json.RawMessage `json:"url"`
		}
		if json.Unmarshal(raw, &section) != nil || len(section.URL) > 0 {
			return errors.New("source-map sections require an inline map")
		}
		line, err := mapOffset(section.Offset.Line)
		if err != nil {
			return fmt.Errorf("section line: %w", err)
		}
		column, err := mapOffset(section.Offset.Column)
		if err != nil {
			return fmt.Errorf("section column: %w", err)
		}
		globalLine, globalColumn, err := offsetMapPosition(line, column, parentLine, parentColumn)
		if err != nil {
			return err
		}
		if previous && !mapPositionLess(previousLine, previousColumn, globalLine, globalColumn) {
			return errors.New("source-map sections are unordered or overlapping")
		}
		start := len(c.mappings)
		// A section begins a new generated-code region. Its initial gap must
		// not inherit a previous section's same-line source mapping.
		if err := c.appendMapping(sourceMapping{line: globalLine, column: globalColumn, source: -1, name: -1}); err != nil {
			return err
		}
		if err := c.decode(name, section.Map, depth+1, globalLine, globalColumn); err != nil {
			return err
		}
		previousLine, previousColumn, previous = globalLine, globalColumn, true
		for _, m := range c.mappings[start:] {
			if mapPositionLess(previousLine, previousColumn, m.line, m.column) {
				previousLine, previousColumn = m.line, m.column
			}
		}
	}
	return nil
}

func (c *sourceMapConsumer) decodeMappings(raw string, sourceStart, sourceCount, nameStart, nameCount int, offsetLine, offsetColumn int32) error {
	var generatedLine, generatedColumn, source, originalLine, originalColumn, name int64
	for at := 0; at < len(raw); {
		if raw[at] == ';' {
			generatedLine++
			generatedColumn = 0
			at++
			if generatedLine > maxMapPosition {
				return errors.New("generated source-map line exceeds supported range")
			}
			continue
		}
		var fields [5]int64
		count := 0
		for at < len(raw) && raw[at] != ',' && raw[at] != ';' {
			if count == len(fields) {
				return errors.New("source-map segment has more than five fields")
			}
			value, err := decodeMapVLQ(raw, &at)
			if err != nil {
				return err
			}
			fields[count], count = value, count+1
		}
		if count != 1 && count != 4 && count != 5 {
			return errors.New("source-map segment requires one, four or five fields")
		}
		generatedColumn += fields[0]
		if generatedColumn < 0 || generatedColumn > maxMapPosition {
			return errors.New("generated source-map column is outside supported range")
		}
		line, column, err := offsetMapPosition(int32(generatedLine), int32(generatedColumn), offsetLine, offsetColumn)
		if err != nil {
			return err
		}
		m := sourceMapping{line: line, column: column, source: -1, name: -1}
		if count > 1 {
			source += fields[1]
			originalLine += fields[2]
			originalColumn += fields[3]
			if source < 0 || source >= int64(sourceCount) || originalLine < 0 || originalLine > maxMapPosition || originalColumn < 0 || originalColumn > maxMapPosition {
				return errors.New("source-map source index or original position is outside supported range")
			}
			m.source, m.originalLine, m.originalColumn = int32(sourceStart)+int32(source), int32(originalLine), int32(originalColumn)
			if count == 5 {
				name += fields[4]
				if name < 0 || name >= int64(nameCount) {
					return errors.New("source-map name index is outside names")
				}
				m.name = int32(nameStart) + int32(name)
			}
		}
		if err := c.appendMapping(m); err != nil {
			return err
		}
		if at < len(raw) && raw[at] == ',' {
			at++
			if at == len(raw) || raw[at] == ',' || raw[at] == ';' {
				return errors.New("empty source-map segment after comma")
			}
		}
	}
	return nil
}

func (c *sourceMapConsumer) appendMapping(m sourceMapping) error {
	if len(c.mappings) >= maxSourceMapMappings {
		return fmt.Errorf("%w: mappings exceed %d", ErrSourceMapLimit, maxSourceMapMappings)
	}
	c.mappings = append(c.mappings, m)
	return nil
}

func decodeMapVLQ(raw string, at *int) (int64, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var value uint64
	var shift uint
	for *at < len(raw) {
		digit := strings.IndexByte(alphabet, raw[*at])
		if digit < 0 {
			return 0, errors.New("invalid base64-VLQ character")
		}
		*at += 1
		payload := uint64(digit & 31)
		if (shift >= 32 && payload != 0) || (shift < 32 && payload<<shift > math.MaxUint32-value) {
			return 0, errors.New("base64-VLQ payload exceeds 32 bits")
		}
		if shift < 32 {
			value |= payload << shift
		}
		if digit&32 == 0 {
			magnitude := int64(value >> 1)
			if value&1 != 0 {
				if magnitude == 0 {
					return math.MinInt32, nil
				}
				return -magnitude, nil
			}
			return magnitude, nil
		}
		if shift < 32 {
			shift += 5
		}
	}
	return 0, errors.New("unterminated base64-VLQ field")
}

func mapOffset(raw json.RawMessage) (int32, error) {
	var value float64
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &value) != nil || value < 0 || value > maxMapPosition || math.Trunc(value) != value {
		return 0, errors.New("offset requires a nonnegative integer in the supported position range")
	}
	return int32(value), nil
}

func offsetMapPosition(line, column, offsetLine, offsetColumn int32) (int32, int32, error) {
	l, col := int64(line)+int64(offsetLine), int64(column)
	if line == 0 {
		col += int64(offsetColumn)
	}
	if l > maxMapPosition || col > maxMapPosition {
		return 0, 0, errors.New("indexed source-map position exceeds supported range")
	}
	return int32(l), int32(col), nil
}

func mapPositionLess(aLine, aColumn, bLine, bColumn int32) bool {
	return aLine < bLine || aLine == bLine && aColumn < bColumn
}

func resolveMapSource(name, root, source string) (string, error) {
	if root != "" {
		source = strings.TrimSuffix(root, "/") + "/" + source
	}
	reference, err := url.Parse(source)
	if err != nil {
		return "", fmt.Errorf("invalid source URL: %w", err)
	}
	if reference.IsAbs() {
		return reference.String(), nil
	}
	base, err := url.Parse(name)
	if err == nil && base.IsAbs() {
		return base.ResolveReference(reference).String(), nil
	}
	// Artifact aliases such as ~/assets/app.js.map are not absolute URLs.
	// Keep their original source paths rather than inventing a network origin.
	return source, nil
}
