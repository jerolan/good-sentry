package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const slackTextLimit = 3000

type slackTextObject struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Emoji    *bool  `json:"emoji,omitempty"`
	Verbatim bool   `json:"verbatim,omitempty"`
}

type slackSection struct {
	Type string          `json:"type"`
	Text slackTextObject `json:"text"`
}

type providerMessage struct {
	Body        []byte
	ContentType string
	Headers     map[string]string
}

func jsonProvider(body []byte, err error) (providerMessage, error) {
	return providerMessage{Body: body, ContentType: "application/json"}, err
}

func webhookRequestFor(kind string, payload Payload) (providerMessage, error) {
	switch kind {
	case "webhook":
		return jsonProvider(json.Marshal(payload))
	case "slack":
		return slackMessage(payload)
	case "discord":
		return discordMessage(payload)
	case "teams":
		return teamsMessage(payload)
	case "googlechat":
		return googleChatMessage(payload)
	case "feishu":
		return feishuMessage(payload)
	case "ntfy":
		return ntfyMessage(payload)
	case "zulip":
		return zulipMessage(payload)
	case "mattermost":
		return mattermostMessage(payload)
	default:
		return providerMessage{}, errors.New("unsupported webhook provider")
	}
}

func slackMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Slack alert payload")
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	if text := []rune(summary); len(text) > slackTextLimit {
		summary = string(text[:slackTextLimit-1]) + "…"
	}
	// Telemetry stays in plain_text. Only this generated resource URL enters
	// Slack's manual link syntax; it cannot introduce another label or mention.
	linkURL := strings.NewReplacer("<", "%3C", ">", "%3E", "|", "%7C", "&", "&amp;").Replace(resource.String())
	link := "<" + linkURL + "|View in GoodSentry>"
	if utf8.RuneCountInString(link) > slackTextLimit {
		return providerMessage{}, invalid
	}
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	emoji := false
	return jsonProvider(json.Marshal(struct {
		Text        string          `json:"text"`
		Mrkdwn      bool            `json:"mrkdwn"`
		Parse       string          `json:"parse"`
		UnfurlLinks bool            `json:"unfurl_links"`
		UnfurlMedia bool            `json:"unfurl_media"`
		Blocks      [2]slackSection `json:"blocks"`
	}{Text: escape.Replace(summary + "\n" + payload.URL), Parse: "none", Blocks: [2]slackSection{
		{Type: "section", Text: slackTextObject{Type: "plain_text", Text: summary, Emoji: &emoji}},
		{Type: "section", Text: slackTextObject{Type: "mrkdwn", Text: link, Verbatim: true}},
	}}))
}

func webhookOutcome(ctx context.Context, kind string, response *http.Response) Outcome {
	defer response.Body.Close()
	outcome := Outcome{Disposition: Permanent, Reason: "http_status", StatusCode: &response.StatusCode}
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		switch kind {
		case "slack":
			return slackOutcome(ctx, response)
		case "feishu":
			return feishuOutcome(ctx, response)
		case "zulip":
			return zulipOutcome(ctx, response)
		case "mattermost":
			return mattermostOutcome(ctx, response)
		}
		outcome.Disposition, outcome.Reason = Delivered, "accepted"
	case response.StatusCode == 408 || response.StatusCode == 425 || response.StatusCode == 429 || response.StatusCode >= 500 && response.StatusCode < 600:
		outcome.Disposition = Retry
		if kind == "slack" && response.StatusCode == http.StatusTooManyRequests {
			outcome.RetryAfter = slackRetryAfter(response.Header)
		}
	}
	return outcome
}

func slackOutcome(ctx context.Context, response *http.Response) Outcome {
	outcome := Outcome{Disposition: Permanent, Reason: "slack_response", StatusCode: &response.StatusCode}
	if response.StatusCode != http.StatusOK {
		return outcome
	}
	body, err := readLimitedBody(response.Body, 4096)
	if errors.Is(err, errResponseTooLarge) {
		return outcome
	}
	if err != nil {
		failed := failure(err, ctx, "http_error")
		failed.StatusCode = &response.StatusCode
		return failed
	}
	if strings.TrimSpace(string(body)) != "ok" {
		return outcome
	}
	outcome.Disposition, outcome.Reason = Delivered, "accepted"
	return outcome
}

func slackRetryAfter(header http.Header) time.Duration {
	values := header.Values("Retry-After")
	if len(values) != 1 {
		return 0
	}
	value := strings.TrimSpace(values[0])
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil || seconds > uint64(time.Duration(1<<63-1)/time.Second) {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
