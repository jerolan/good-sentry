package notify

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var errResponseTooLarge = errors.New("provider response exceeds its bound")

// alertSummary is shared by every provider so one compact payload produces the
// same human context regardless of transport-specific presentation.
func alertSummary(payload Payload) (string, error) {
	summary := fmt.Sprintf("GoodSentry: %s\nProject: %s", payload.Rule.Name, payload.Project.Name)
	switch payload.Kind {
	case "error_frequency":
		if payload.Count < 1 || payload.WindowMinutes < 1 {
			return "", errors.New("invalid frequency context")
		}
		if payload.Issue != nil {
			summary += "\nIssue: " + payload.Issue.Name
		}
		summary += fmt.Sprintf("\n%d events in %d minutes", payload.Count, payload.WindowMinutes)
	case "uptime_down", "uptime_recovered":
		if payload.Monitor == nil {
			return "", errors.New("missing uptime context")
		}
		summary += fmt.Sprintf("\nMonitor: %s\nStatus: %s -> %s", payload.Monitor.Name, payload.Monitor.PreviousStatus, payload.Monitor.Status)
	case "test":
		summary += "\nTest notification"
	default:
		return "", errors.New("unsupported alert kind")
	}
	if payload.Environment != "" {
		summary += "\nEnvironment: " + payload.Environment
	}
	summary += "\nOccurred: " + payload.OccurredAt.UTC().Format(time.RFC3339) + "\nDelivery: " + payload.ID
	if !validText(summary) {
		return "", errors.New("invalid alert text")
	}
	return summary, nil
}

func publicResource(raw string) (*url.URL, error) {
	resource, err := url.Parse(raw)
	if err != nil || !validHTTPAddress(raw, false) {
		return nil, errors.New("invalid public resource URL")
	}
	return resource, nil
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func escapeHTML(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

// escapeHTMLWithin preserves complete entities and reserves one rune for an
// ellipsis whenever user text must be shortened.
func escapeHTMLWithin(value string, limit int) (string, bool) {
	if limit <= 0 {
		return "", value != ""
	}
	output := make([]rune, 0, limit)
	for _, character := range value {
		escaped := string(character)
		switch character {
		case '&':
			escaped = "&amp;"
		case '<':
			escaped = "&lt;"
		case '>':
			escaped = "&gt;"
		}
		escapedRunes := []rune(escaped)
		if len(output)+len(escapedRunes) > limit {
			if len(output) == limit {
				output = output[:limit-1]
			}
			return string(append(output, '…')), true
		}
		output = append(output, escapedRunes...)
	}
	return string(output), false
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	if limit < len("…") {
		return ""
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "…"
}

func defangAngles(value string) string {
	return strings.NewReplacer("<", "＜", ">", "＞").Replace(value)
}

func defangMentions(value string) string {
	return strings.ReplaceAll(value, "@", "@\u200b")
}

func readLimitedBody(reader io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("invalid response bound")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errResponseTooLarge
	}
	return body, nil
}
