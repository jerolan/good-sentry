package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxSecurityReports bounds the work performed by a browser's Reporting API
// batch independently of the transport's byte limit.
const MaxSecurityReports = 100

// SecurityReportOptions are server-selected metadata, never values copied from
// arbitrary event fields in a browser report.
type SecurityReportOptions struct {
	Release, Environment, UserAgent string
}

// NormalizeSecurityReports implements the public CSP report-uri and Reporting
// API wire formats. Every report is validated before the caller writes any of
// them. Browser reports have no stable event identity; each delivery receives a
// new event ID, including repeated deliveries of the same violation.
//
// Contracts: https://www.w3.org/TR/CSP3/#reporting and
// https://www.w3.org/TR/reporting-1/#report-upload-http-request.
func NormalizeSecurityReports(raw []byte, now time.Time, options SecurityReportOptions) ([]Event, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("security report must contain valid UTF-8")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("security report is empty")
	}
	var reports []json.RawMessage
	modern := raw[0] == '['
	if modern {
		if json.Unmarshal(raw, &reports) != nil || len(reports) == 0 || len(reports) > MaxSecurityReports {
			return nil, fmt.Errorf("security batch must contain 1 to %d reports", MaxSecurityReports)
		}
	} else {
		reports = []json.RawMessage{raw}
	}
	events := make([]Event, 0, len(reports))
	for _, report := range reports {
		ev, err := normalizeSecurityReport(report, modern, now, options)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func normalizeSecurityReport(raw json.RawMessage, modern bool, now time.Time, options SecurityReportOptions) (Event, error) {
	outer, err := securityObject(raw)
	if err != nil {
		return Event{}, err
	}
	bodyRaw := outer["csp-report"]
	userAgent := options.UserAgent
	timestamp := now.UTC()
	var fallbackURL string
	if modern {
		kind, err := securityString(outer, "type", 100)
		if err != nil || kind != "csp-violation" {
			return Event{}, errors.New("only csp-violation security reports are supported")
		}
		bodyRaw = outer["body"]
		fallbackURL, err = securityString(outer, "url", 8192)
		if err != nil {
			return Event{}, err
		}
		if agent, err := securityString(outer, "user_agent", 4096); err != nil {
			return Event{}, err
		} else if agent != "" {
			userAgent = agent
		}
		if age, exists := outer["age"]; exists {
			milliseconds, err := strconv.ParseFloat(string(age), 64)
			whole, fractional := math.Modf(milliseconds)
			if err != nil || math.IsNaN(milliseconds) || milliseconds < 0 || whole > float64(math.MaxInt64/int64(time.Millisecond)) {
				return Event{}, errors.New("security report age must be finite nonnegative milliseconds within the duration range")
			}
			// Keep integral millisecond arithmetic exact and truncate only the
			// fractional nanoseconds. Check the sum before converting its sign.
			elapsed := time.Duration(whole) * time.Millisecond
			fraction := time.Duration(fractional * float64(time.Millisecond))
			if fraction > time.Duration(math.MaxInt64)-elapsed {
				return Event{}, errors.New("security report age exceeds the duration range")
			}
			timestamp = timestamp.Add(-(elapsed + fraction))
			if timestamp.Year() < 1 {
				return Event{}, errors.New("security report age is outside supported dates")
			}
		}
	}
	body, err := securityObject(bodyRaw)
	if err != nil {
		return Event{}, errors.New("security report requires a CSP body object")
	}
	csp := make(map[string]any)
	// Map only the documented browser fields. Preserve the entire original
	// report separately, including future browser extensions.
	for _, field := range []struct{ legacy, modern, target string }{
		{"document-uri", "documentURL", "document_uri"},
		{"blocked-uri", "blockedURL", "blocked_uri"},
		{"effective-directive", "effectiveDirective", "effective_directive"},
		{"violated-directive", "violatedDirective", "violated_directive"},
		{"original-policy", "originalPolicy", "original_policy"},
		{"referrer", "referrer", "referrer"},
		{"source-file", "sourceFile", "source_file"},
		{"script-sample", "sample", "script_sample"},
		{"disposition", "disposition", "disposition"},
	} {
		name := field.legacy
		if modern {
			name = field.modern
			if bytes.Equal(bytes.TrimSpace(body[name]), []byte("null")) {
				switch name {
				case "referrer", "blockedURL", "sourceFile", "sample":
					csp[field.target] = nil
					continue
				}
			}
		}
		value, err := securityString(body, name, 65536)
		if err != nil {
			return Event{}, err
		}
		if _, exists := body[name]; exists {
			csp[field.target] = value
		}
	}
	for _, field := range []struct{ legacy, modern, target string }{
		{"line-number", "lineNumber", "line_number"},
		{"column-number", "columnNumber", "column_number"},
		{"status-code", "statusCode", "status_code"},
	} {
		name := field.legacy
		if modern {
			name = field.modern
		}
		if raw, exists := body[name]; exists {
			if modern && name != "statusCode" && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				csp[field.target] = nil
				continue
			}
			number, err := strconv.ParseUint(string(raw), 10, 32)
			if err != nil {
				return Event{}, fmt.Errorf("CSP %s must be a nonnegative integer", name)
			}
			csp[field.target] = number
		}
	}
	document, _ := csp["document_uri"].(string)
	if document == "" {
		document = fallbackURL
		csp["document_uri"] = document
	}
	directive, _ := csp["effective_directive"].(string)
	violated, _ := csp["violated_directive"].(string)
	if directive == "" {
		directive = strings.TrimSpace(strings.SplitN(violated, " ", 2)[0])
		csp["effective_directive"] = directive
	}
	if strings.TrimSpace(document) == "" || strings.TrimSpace(directive) == "" {
		return Event{}, errors.New("CSP document URL and effective directive are required")
	}
	if violated == "" {
		csp["violated_directive"] = directive
	}
	blocked, _ := csp["blocked_uri"].(string)
	groupBlocked := blocked
	tags := map[string]string{"effective-directive": directive}
	if blocked != "" {
		tags["blocked-uri"] = blocked
		if parsed, err := url.Parse(blocked); err == nil && parsed.Hostname() != "" {
			tags["blocked-host"] = parsed.Hostname()
			parsed.RawQuery, parsed.Fragment, parsed.RawFragment = "", "", ""
			groupBlocked = parsed.String()
		}
	}
	title := "Content Security Policy violation: " + directive
	if blocked != "" {
		title += " blocked " + blocked
	}
	request := map[string]any{"url": document}
	if userAgent != "" {
		request["headers"] = [][2]string{{"User-Agent", userAgent}}
	}
	payload, err := json.Marshal(map[string]any{
		"type": "csp", "platform": "javascript", "level": "warning", "message": title,
		"timestamp": timestamp.Format(time.RFC3339Nano), "release": options.Release, "environment": options.Environment,
		"fingerprint": []string{"goodsentry-csp-v1", directive, groupBlocked},
		"csp":         csp, "request": request, "tags": tags,
		"extra": map[string]any{"security_report": raw},
	})
	if err != nil {
		return Event{}, err
	}
	return Normalize(payload, "", now)
}

func securityString(fields map[string]json.RawMessage, key string, maximum int) (string, error) {
	raw, exists := fields[key]
	if !exists {
		return "", nil
	}
	var value string
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || utf8.RuneCountInString(value) > maximum || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("CSP %s must be a string of at most %d characters without NUL", key, maximum)
	}
	return value, nil
}

func securityObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("security report must be a JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid security report field")
		}
		key, ok := token.(string)
		if _, exists := fields[key]; !ok || exists {
			return nil, errors.New("duplicate security report field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("invalid security report value")
		}
		fields[key] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, errors.New("invalid security report object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("bytes after security report object")
	}
	return fields, nil
}
