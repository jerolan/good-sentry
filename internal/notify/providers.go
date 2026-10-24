package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	discordContentLimit  = 2000
	teamsSummaryLimit    = 150
	teamsTextLimit       = 3000
	teamsResourceLimit   = 2000
	googleChatTextLimit  = 4096
	feishuTextLimit      = 3000
	ntfyBodyLimit        = 4096
	zulipTextLimit       = 3000
	providerAckBodyLimit = 64 << 10
)

func providerTextWithURL(summary, resource string, limit int) (string, error) {
	urlRunes := utf8.RuneCountInString(resource)
	if urlRunes < 1 || urlRunes+2 > limit {
		return "", errors.New("resource URL exceeds provider text bound")
	}
	summary = truncateRunes(summary, limit-urlRunes-1)
	text := summary + "\n" + resource
	if !validText(text) || utf8.RuneCountInString(text) > limit {
		return "", errors.New("provider text exceeds its bound")
	}
	return text, nil
}

func discordMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Discord alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	content, err := providerTextWithURL(summary, resource.String(), discordContentLimit)
	if err != nil {
		return providerMessage{}, invalid
	}
	return jsonProvider(json.Marshal(struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Parse       []string `json:"parse"`
			Users       []string `json:"users"`
			Roles       []string `json:"roles"`
			RepliedUser bool     `json:"replied_user"`
		} `json:"allowed_mentions"`
		Flags int `json:"flags"`
	}{Content: content, AllowedMentions: struct {
		Parse       []string `json:"parse"`
		Users       []string `json:"users"`
		Roles       []string `json:"roles"`
		RepliedUser bool     `json:"replied_user"`
	}{Parse: []string{}, Users: []string{}, Roles: []string{}}, Flags: 4}))
}

func teamsMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Teams alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	if utf8.RuneCountInString(resource.String()) > teamsResourceLimit {
		return providerMessage{}, invalid
	}
	sectionText, err := providerTextWithURL(summary, resource.String(), teamsTextLimit)
	if err != nil {
		return providerMessage{}, invalid
	}
	cardSummary := strings.SplitN(sectionText, "\n", 2)[0]
	cardSummary = truncateRunes(cardSummary, teamsSummaryLimit)
	card := struct {
		Type       string `json:"@type"`
		Context    string `json:"@context"`
		Summary    string `json:"summary"`
		ThemeColor string `json:"themeColor"`
		Sections   []struct {
			Markdown bool   `json:"markdown"`
			Text     string `json:"text"`
		} `json:"sections"`
		PotentialAction []struct {
			Type    string `json:"@type"`
			Name    string `json:"name"`
			Targets []struct {
				OS  string `json:"os"`
				URI string `json:"uri"`
			} `json:"targets"`
		} `json:"potentialAction"`
	}{
		Type:       "MessageCard",
		Context:    "https://schema.org/extensions",
		Summary:    cardSummary,
		ThemeColor: "2F855A",
	}
	card.Sections = append(card.Sections, struct {
		Markdown bool   `json:"markdown"`
		Text     string `json:"text"`
	}{Text: sectionText})
	card.PotentialAction = append(card.PotentialAction, struct {
		Type    string `json:"@type"`
		Name    string `json:"name"`
		Targets []struct {
			OS  string `json:"os"`
			URI string `json:"uri"`
		} `json:"targets"`
	}{Type: "OpenUri", Name: "View in GoodSentry", Targets: []struct {
		OS  string `json:"os"`
		URI string `json:"uri"`
	}{{OS: "default", URI: resource.String()}}})
	return jsonProvider(json.Marshal(card))
}

func googleChatMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Google Chat alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	escapedURL := escapeHTML(resource.String())
	urlRunes := utf8.RuneCountInString(escapedURL)
	if urlRunes+2 > googleChatTextLimit {
		return providerMessage{}, invalid
	}
	escapedSummary, _ := escapeHTMLWithin(summary, googleChatTextLimit-urlRunes-1)
	text := escapedSummary + "\n" + escapedURL
	if !validText(text) || utf8.RuneCountInString(text) > googleChatTextLimit {
		return providerMessage{}, invalid
	}
	return jsonProvider(json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text}))
}

func feishuMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Feishu alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	text, err := providerTextWithURL(defangAngles(summary), resource.String(), feishuTextLimit)
	if err != nil {
		return providerMessage{}, invalid
	}
	return jsonProvider(json.Marshal(struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}{MsgType: "text", Content: struct {
		Text string `json:"text"`
	}{Text: text}}))
}

func ntfyMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid ntfy alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	url := resource.String()
	if len(url)+2 > ntfyBodyLimit {
		return providerMessage{}, invalid
	}
	summary = truncateUTF8Bytes(summary, ntfyBodyLimit-len(url)-1)
	body := []byte(summary + "\n" + url)
	if !validText(string(body)) || len(body) > ntfyBodyLimit {
		return providerMessage{}, invalid
	}
	title := truncateRunes("GoodSentry: "+payload.Rule.Name, 200)
	return providerMessage{Body: body, ContentType: "text/plain; charset=UTF-8", Headers: map[string]string{
		"Title": title,
		"Click": url,
	}}, nil
}

func zulipMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Zulip alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	text, err := providerTextWithURL(defangMentions(summary), resource.String(), zulipTextLimit)
	if err != nil {
		return providerMessage{}, invalid
	}
	return jsonProvider(json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text}))
}

func feishuOutcome(ctx context.Context, response *http.Response) Outcome {
	outcome := Outcome{Disposition: Permanent, Reason: "feishu_response", StatusCode: &response.StatusCode}
	if response.StatusCode != http.StatusOK {
		return outcome
	}
	body, err := readLimitedBody(response.Body, providerAckBodyLimit)
	if errors.Is(err, errResponseTooLarge) {
		return outcome
	}
	if err != nil {
		failed := failure(err, ctx, "http_error")
		failed.StatusCode = &response.StatusCode
		return failed
	}
	var acknowledgment struct {
		Code          *int   `json:"code"`
		StatusCode    *int   `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if json.Unmarshal(body, &acknowledgment) != nil || acknowledgment.Code == nil && acknowledgment.StatusCode == nil {
		return outcome
	}
	if acknowledgment.Code != nil && *acknowledgment.Code != 0 || acknowledgment.StatusCode != nil && *acknowledgment.StatusCode != 0 {
		return outcome
	}
	outcome.Disposition, outcome.Reason = Delivered, "accepted"
	return outcome
}

func zulipOutcome(ctx context.Context, response *http.Response) Outcome {
	outcome := Outcome{Disposition: Permanent, Reason: "zulip_response", StatusCode: &response.StatusCode}
	if response.StatusCode != http.StatusOK {
		return outcome
	}
	body, err := readLimitedBody(response.Body, providerAckBodyLimit)
	if errors.Is(err, errResponseTooLarge) {
		return outcome
	}
	if err != nil {
		failed := failure(err, ctx, "http_error")
		failed.StatusCode = &response.StatusCode
		return failed
	}
	var acknowledgment struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(body, &acknowledgment) != nil || acknowledgment.Result != "success" {
		return outcome
	}
	outcome.Disposition, outcome.Reason = Delivered, "accepted"
	return outcome
}
