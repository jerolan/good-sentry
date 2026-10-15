package eventview

import (
	"encoding/json"
	"mime"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type orderedPair [2]*string

func requestView(raw json.RawMessage) json.RawMessage {
	fields := object(raw)
	if fields == nil {
		return nil
	}
	nullableStrings(fields, "apiTarget", "fragment", "method", "protocol", "url")
	nullableObjects(fields, "env")
	if value, present := fields["bodySize"]; present && !isNull(value) {
		if len(value) == 0 || (value[0] != '-' && (value[0] < '0' || value[0] > '9')) || strings.ContainsAny(string(value), ".eE") {
			delete(fields, "bodySize")
		}
	}
	var headers []orderedPair
	if value, present := fields["headers"]; present {
		var valid bool
		if headers, valid = requestPairs(value, false); valid {
			fields["headers"], _ = json.Marshal(headers)
		} else {
			delete(fields, "headers")
		}
	}
	query, explicit := fields["query"]
	if !explicit {
		query, explicit = fields["query_string"]
	}
	if explicit {
		if pairs, valid := requestPairs(query, true); valid {
			fields["query"], _ = json.Marshal(pairs)
		} else {
			delete(fields, "query")
		}
	} else if address := stringPointer(fields["url"]); address != nil {
		if parsed, err := url.Parse(*address); err == nil && (parsed.RawQuery != "" || parsed.ForceQuery) {
			fields["query"], _ = json.Marshal(encodedQuery(parsed.RawQuery))
		}
	}
	if value, present := fields["cookies"]; present && !isNull(value) && stringPointer(value) == nil {
		if cookies := object(value); cookies != nil {
			for key, value := range cookies {
				if stringPointer(value) == nil && !isNull(value) {
					delete(cookies, key)
				}
			}
			fields["cookies"], _ = json.Marshal(cookies)
		} else if pairs, valid := requestPairs(value, false); valid {
			fields["cookies"], _ = json.Marshal(pairs)
		} else {
			delete(fields, "cookies")
		}
	}
	contentType := stringPointer(fields["inferredContentType"])
	if contentType == nil || mediaType(*contentType) == "" {
		contentType = nil
		for _, header := range headers {
			if header[0] != nil && header[1] != nil && strings.EqualFold(*header[0], "content-type") {
				if typ := mediaType(*header[1]); typ != "" {
					contentType = &typ
					break
				}
			}
		}
	}
	fields["inferredContentType"], _ = json.Marshal(contentType)
	encoded, _ := json.Marshal(fields)
	return encoded
}

func mediaType(value string) string {
	typ, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.Contains(typ, "/") {
		return ""
	}
	return typ
}

func requestPairs(raw json.RawMessage, query bool) ([]orderedPair, bool) {
	if isNull(raw) {
		return nil, true
	}
	pairs := []orderedPair{}
	if fields := object(raw); fields != nil {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if value := stringPointer(fields[key]); value != nil || isNull(fields[key]) {
				pairs = append(pairs, orderedPair{&key, value})
			}
		}
		return pairs, true
	}
	if query {
		if encoded := stringPointer(raw); encoded != nil {
			return encodedQuery(*encoded), true
		}
	}
	items := array(raw)
	if items == nil {
		return nil, false
	}
	for _, item := range items {
		pair := array(item)
		if len(pair) != 2 {
			continue
		}
		key, value := stringPointer(pair[0]), stringPointer(pair[1])
		if (key != nil || isNull(pair[0])) && (value != nil || isNull(pair[1])) {
			pairs = append(pairs, orderedPair{key, value})
		}
	}
	return pairs, true
}

func encodedQuery(encoded string) []orderedPair {
	pairs := []orderedPair{}
	for _, part := range strings.Split(encoded, "&") {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		key, keyErr := url.QueryUnescape(key)
		value, valueErr := url.QueryUnescape(value)
		if keyErr == nil && valueErr == nil {
			pairs = append(pairs, orderedPair{&key, &value})
		}
	}
	return pairs
}

func breadcrumbView(raw json.RawMessage) json.RawMessage {
	encoded, values := collection(raw)
	if len(encoded) == 0 {
		return nil
	}
	for _, value := range values {
		nullableStrings(value, "type", "category", "message", "event_id", "level")
		nullableObjects(value, "data")
		if level := stringPointer(value["level"]); level != nil {
			switch *level {
			case "fatal", "error", "warning", "info", "debug":
			default:
				delete(value, "level")
			}
		}
		if rawTimestamp, present := value["timestamp"]; present {
			value["timestamp"] = json.RawMessage("null")
			if at, valid := breadcrumbTimestamp(rawTimestamp); valid {
				value["timestamp"], _ = json.Marshal(at.UTC().Format(time.RFC3339Nano))
			}
		}
	}
	encoded, _ = json.Marshal(map[string]any{"values": values})
	return encoded
}

func nullableStrings(fields map[string]json.RawMessage, keys ...string) {
	for _, key := range keys {
		if raw, present := fields[key]; present && stringPointer(raw) == nil && !isNull(raw) {
			delete(fields, key)
		}
	}
}

func nullableObjects(fields map[string]json.RawMessage, keys ...string) {
	for _, key := range keys {
		if raw, present := fields[key]; present && object(raw) == nil && !isNull(raw) {
			delete(fields, key)
		}
	}
}

func isNull(raw json.RawMessage) bool { return strings.TrimSpace(string(raw)) == "null" }

func breadcrumbTimestamp(raw json.RawMessage) (time.Time, bool) {
	if text := stringPointer(raw); text != nil {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
			if at, err := time.Parse(layout, *text); err == nil {
				at = at.UTC()
				return at, at.Year() >= 1 && at.Year() <= 9999
			}
		}
		return time.Time{}, false
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || (text[0] != '-' && (text[0] < '0' || text[0] > '9')) {
		return time.Time{}, false
	}
	negative := text[0] == '-'
	text = strings.TrimPrefix(text, "-")
	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(text), "e")
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return time.Unix(0, 0).UTC(), true
	}
	var exponent int64
	if hasExponent {
		var err error
		exponent, err = strconv.ParseInt(exponentText, 10, 64)
		if err != nil {
			if strings.HasPrefix(exponentText, "-") {
				return time.Unix(0, 0).UTC(), true
			}
			return time.Time{}, false
		}
	}
	// These bounds precede integer arithmetic and allocation. Huge positive
	// exponents exceed supported dates; huge negative ones round below a nanosecond.
	if exponent > int64(len(fraction))+12 {
		return time.Time{}, false
	}
	if exponent < -int64(len(digits))-10 {
		return time.Unix(0, 0).UTC(), true
	}
	point := len(digits) + int(exponent) - len(fraction)
	if point > 12 {
		return time.Time{}, false
	}
	if point < -9 {
		return time.Unix(0, 0).UTC(), true
	}
	var seconds, nanos int64
	for i := 0; i < max(0, point); i++ {
		seconds *= 10
		if i < len(digits) {
			seconds += int64(digits[i] - '0')
		}
	}
	for i := range 9 {
		nanos *= 10
		if position := point + i; position >= 0 && position < len(digits) {
			nanos += int64(digits[position] - '0')
		}
	}
	if position := point + 9; position >= 0 && position < len(digits) && digits[position] >= '5' {
		nanos++
	}
	if negative {
		seconds, nanos = -seconds, -nanos
	}
	at := time.Unix(seconds, nanos).UTC()
	return at, at.Year() >= 1 && at.Year() <= 9999
}
