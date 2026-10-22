package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

const mattermostTextLimit = 4000

func mattermostMessage(payload Payload) (providerMessage, error) {
	invalid := errors.New("invalid Mattermost alert payload")
	summary, err := alertSummary(payload)
	if err != nil {
		return providerMessage{}, invalid
	}
	resource, err := publicResource(payload.URL)
	if err != nil {
		return providerMessage{}, invalid
	}
	linkURL := strings.NewReplacer("\\", "%5C", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "|", "%7C").Replace(resource.String())
	link := "[View in GoodSentry](" + linkURL + ")"
	linkRunes := utf8.RuneCountInString(link)
	if linkRunes+2 > mattermostTextLimit {
		return providerMessage{}, invalid
	}
	text := mattermostEscapeSummary(summary, mattermostTextLimit-linkRunes-1) + "\n" + link
	if !validText(text) || utf8.RuneCountInString(text) > mattermostTextLimit {
		return providerMessage{}, invalid
	}
	return jsonProvider(json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text}))
}

func mattermostEscapeSummary(summary string, limit int) string {
	const punctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	pieces := make([]string, 0, utf8.RuneCountInString(summary))
	total := 0
	for _, character := range summary {
		piece := string(character)
		if character == '@' {
			piece = "@\u200b"
		} else if strings.ContainsRune(punctuation, character) {
			piece = "\\" + piece
		}
		pieces = append(pieces, piece)
		total += utf8.RuneCountInString(piece)
	}
	if total <= limit {
		return strings.Join(pieces, "")
	}
	if limit <= 0 {
		return ""
	}
	var output strings.Builder
	used := 0
	for _, piece := range pieces {
		pieceRunes := utf8.RuneCountInString(piece)
		if used+pieceRunes > limit-1 {
			break
		}
		output.WriteString(piece)
		used += pieceRunes
	}
	output.WriteRune('…')
	return output.String()
}

func mattermostOutcome(ctx context.Context, response *http.Response) Outcome {
	outcome := Outcome{Disposition: Permanent, Reason: "mattermost_response", StatusCode: &response.StatusCode}
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
