package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func providerMessageFor(t *testing.T, kind string, payload Payload) providerMessage {
	t.Helper()
	message, err := webhookRequestFor(kind, payload)
	if err != nil {
		t.Fatalf("%s encoder: %v", kind, err)
	}
	return message
}

func decodeProviderObject(t *testing.T, message providerMessage) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(message.Body, &object); err != nil {
		t.Fatalf("provider JSON: %v: %s", err, message.Body)
	}
	return object
}

func TestProviderMessages(t *testing.T) {
	payload := slackPayload("error_frequency")
	wantSummary := "GoodSentry: Fallos 🙂\nProject: App\nIssue: Wrong turn\n7 events in 5 minutes\nEnvironment: production\nOccurred: 2026-10-14T00:00:00Z\nDelivery: delivery-123"

	t.Run("discord", func(t *testing.T) {
		message := providerMessageFor(t, "discord", payload)
		if message.ContentType != "application/json" {
			t.Fatalf("Discord content type: %s", message.ContentType)
		}
		object := decodeProviderObject(t, message)
		if object["content"] != wantSummary+"\n"+payload.URL || object["flags"] != float64(4) {
			t.Fatalf("Discord content: %#v", object)
		}
		mentions, ok := object["allowed_mentions"].(map[string]any)
		if !ok || len(mentions["parse"].([]any)) != 0 || len(mentions["users"].([]any)) != 0 || len(mentions["roles"].([]any)) != 0 || mentions["replied_user"] != false {
			t.Fatalf("Discord mention guard: %#v", object["allowed_mentions"])
		}
		for _, forbidden := range []string{"channel", "username", "avatar_url"} {
			if _, present := object[forbidden]; present {
				t.Fatalf("Discord override present: %s", forbidden)
			}
		}
	})

	t.Run("teams", func(t *testing.T) {
		object := decodeProviderObject(t, providerMessageFor(t, "teams", payload))
		if object["@type"] != "MessageCard" || object["@context"] != "https://schema.org/extensions" || object["summary"] != "GoodSentry: Fallos 🙂" {
			t.Fatalf("Teams card identity: %#v", object)
		}
		sections := object["sections"].([]any)
		section := sections[0].(map[string]any)
		if section["markdown"] != false || section["text"] != wantSummary {
			t.Fatalf("Teams plain section: %#v", section)
		}
		actions := object["potentialAction"].([]any)
		action := actions[0].(map[string]any)
		target := action["targets"].([]any)[0].(map[string]any)
		if action["@type"] != "OpenUri" || action["name"] != "View in GoodSentry" || target["os"] != "default" || target["uri"] != payload.URL {
			t.Fatalf("Teams resource action: %#v", action)
		}
	})

	t.Run("googlechat", func(t *testing.T) {
		object := decodeProviderObject(t, providerMessageFor(t, "googlechat", payload))
		if object["text"] != wantSummary+"\n"+payload.URL {
			t.Fatalf("Google Chat text: %#v", object)
		}
	})

	t.Run("feishu", func(t *testing.T) {
		object := decodeProviderObject(t, providerMessageFor(t, "feishu", payload))
		content := object["content"].(map[string]any)
		if object["msg_type"] != "text" || content["text"] != wantSummary+"\n"+payload.URL {
			t.Fatalf("Feishu text: %#v", object)
		}
	})

	t.Run("ntfy", func(t *testing.T) {
		message := providerMessageFor(t, "ntfy", payload)
		if message.ContentType != "text/plain; charset=UTF-8" || string(message.Body) != wantSummary+"\n"+payload.URL || message.Headers["Title"] != "GoodSentry: Fallos 🙂" || message.Headers["Click"] != payload.URL {
			t.Fatalf("ntfy request: %+v", message)
		}
	})

	t.Run("zulip", func(t *testing.T) {
		object := decodeProviderObject(t, providerMessageFor(t, "zulip", payload))
		if object["text"] != wantSummary+"\n"+payload.URL {
			t.Fatalf("Zulip text: %#v", object)
		}
	})
}

func TestMattermostWebhookMessageEscapesTelemetryAndKeepsResource(t *testing.T) {
	payload := slackPayload("error_frequency")
	payload.Rule.Name = "*Fallos* @channel"
	payload.URL = "https://errors.example/base/organizations/org/issues/issue-1?next=a%28b%29"
	message := providerMessageFor(t, "mattermost", payload)
	if message.ContentType != "application/json" {
		t.Fatalf("Mattermost content type: %q", message.ContentType)
	}
	object := decodeProviderObject(t, message)
	text, ok := object["text"].(string)
	if !ok || !strings.HasPrefix(text, "GoodSentry\\: \\*Fallos\\* @\u200bchannel\nProject\\: App") {
		t.Fatalf("Mattermost Markdown or mention defanging: %#v", object)
	}
	if strings.Contains(text, "@channel") || !strings.Contains(text, "[View in GoodSentry](https://errors.example/base/organizations/org/issues/issue-1?next=a%28b%29)") {
		t.Fatalf("Mattermost mention or generated resource link: %q", text)
	}
	if got := utf8.RuneCountInString(text); got > 4000 {
		t.Fatalf("Mattermost text exceeds 4000 runes: %d", got)
	}
}

func TestMattermostWebhookMessageContextsAndBounds(t *testing.T) {
	for _, tc := range []struct{ kind, context string }{
		{"error_frequency", "7 events in 5 minutes"},
		{"uptime_down", "Status\\: up \\-\\> down"},
		{"uptime_recovered", "Status\\: down \\-\\> up"},
		{"test", "Test notification"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			payload := slackPayload(tc.kind)
			message := providerMessageFor(t, "mattermost", payload)
			text := decodeProviderObject(t, message)["text"].(string)
			if !strings.Contains(text, tc.context) || !strings.Contains(text, "[View in GoodSentry](") {
				t.Fatalf("Mattermost %s context or link missing: %q", tc.kind, text)
			}
		})
	}
	t.Run("truncates summary and preserves generated link", func(t *testing.T) {
		payload := slackPayload("error_frequency")
		payload.Issue.Name = strings.Repeat("*", 5000)
		text := decodeProviderObject(t, providerMessageFor(t, "mattermost", payload))["text"].(string)
		if utf8.RuneCountInString(text) != 4000 || !strings.Contains(text, "…\n[View in GoodSentry](") {
			t.Fatalf("Mattermost truncation lost bound, ellipsis or link: runes=%d", utf8.RuneCountInString(text))
		}
	})
	t.Run("rejects a resource link that cannot fit", func(t *testing.T) {
		payload := slackPayload("test")
		payload.URL = "https://errors.example/" + strings.Repeat("x", 4100)
		if _, err := webhookRequestFor("mattermost", payload); err == nil {
			t.Fatal("overlong Mattermost resource URL was accepted")
		}
	})
}

func TestProviderMessageContexts(t *testing.T) {
	for _, tc := range []struct{ kind, detail string }{
		{"error_frequency", "7 events in 5 minutes"},
		{"uptime_down", "Status: up -> down"},
		{"uptime_recovered", "Status: down -> up"},
		{"test", "Test notification"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			payload := slackPayload(tc.kind)
			for _, provider := range []string{"discord", "teams", "googlechat", "feishu", "ntfy", "zulip"} {
				message := providerMessageFor(t, provider, payload)
				body := string(message.Body)
				resource := payload.URL
				detail := tc.detail
				if provider == "googlechat" {
					detail = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(detail)
				} else if provider == "feishu" {
					detail = strings.NewReplacer("<", "＜", ">", "＞").Replace(detail)
				}
				if provider == "ntfy" {
					body = string(message.Body)
				} else {
					object := decodeProviderObject(t, message)
					switch provider {
					case "discord":
						body = object["content"].(string)
					case "teams":
						body = object["sections"].([]any)[0].(map[string]any)["text"].(string)
						resource = object["potentialAction"].([]any)[0].(map[string]any)["targets"].([]any)[0].(map[string]any)["uri"].(string)
					case "feishu":
						body = object["content"].(map[string]any)["text"].(string)
					default:
						body = object["text"].(string)
					}
				}
				if !strings.Contains(body, detail) || provider == "teams" && resource != payload.URL || provider != "teams" && !strings.Contains(body, payload.URL) {
					t.Fatalf("%s %s lost context or resource: %s", provider, tc.kind, body)
				}
			}
		})
	}
}

func TestProviderEscapingAndBounds(t *testing.T) {
	t.Run("discord suppresses mention-like telemetry", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Rule.Name = "@everyone <@123>"
		object := decodeProviderObject(t, providerMessageFor(t, "discord", p))
		if !strings.Contains(object["content"].(string), "@everyone") {
			t.Fatal("literal telemetry was lost")
		}
		mentions := object["allowed_mentions"].(map[string]any)
		if len(mentions["parse"].([]any)) != 0 {
			t.Fatal("Discord mentions were enabled")
		}
	})

	t.Run("google chat escapes HTML-significant telemetry", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Rule.Name = "<@all> & alert"
		p.URL = "https://errors.example/base path/日本?issue=1&org=x"
		object := decodeProviderObject(t, providerMessageFor(t, "googlechat", p))
		text := object["text"].(string)
		if !strings.Contains(text, "GoodSentry: &lt;@all&gt; &amp; alert") || !strings.HasSuffix(text, "\nhttps://errors.example/base%20path/%E6%97%A5%E6%9C%AC?issue=1&amp;org=x") {
			t.Fatalf("Google Chat escaping or URL canonicalization: %s", text)
		}
	})

	t.Run("feishu defangs mention directives", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Rule.Name = `<at user_id="all">everyone</at>`
		object := decodeProviderObject(t, providerMessageFor(t, "feishu", p))
		text := object["content"].(map[string]any)["text"].(string)
		if strings.Contains(text, "<at") || !strings.Contains(text, `＜at user_id="all"＞`) {
			t.Fatalf("Feishu mention directive was not defanged: %s", text)
		}
	})

	t.Run("zulip defangs mentions", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Rule.Name = "@all @**on-call**"
		object := decodeProviderObject(t, providerMessageFor(t, "zulip", p))
		text := object["text"].(string)
		if strings.Contains(text, "@all") || strings.Contains(text, "@**") || !strings.Contains(text, "@\u200ball") {
			t.Fatalf("Zulip mention was not defanged: %q", text)
		}
	})

	t.Run("unicode truncation preserves resource", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Issue.Name = strings.Repeat("🙂", 5000)
		for _, provider := range []string{"discord", "teams", "googlechat", "feishu", "ntfy", "zulip"} {
			message := providerMessageFor(t, provider, p)
			switch provider {
			case "ntfy":
				if len(message.Body) > ntfyBodyLimit || !utf8.Valid(message.Body) || !strings.HasSuffix(string(message.Body), "\n"+p.URL) {
					t.Fatalf("ntfy bound: %d bytes", len(message.Body))
				}
			case "teams":
				object := decodeProviderObject(t, message)
				section := object["sections"].([]any)[0].(map[string]any)["text"].(string)
				if utf8.RuneCountInString(section) > teamsTextLimit || !strings.HasSuffix(section, "…") || utf8.RuneCountInString(object["summary"].(string)) > teamsSummaryLimit {
					t.Fatalf("Teams bound: %#v", object)
				}
			default:
				object := decodeProviderObject(t, message)
				var text string
				switch provider {
				case "discord":
					text = object["content"].(string)
				case "feishu":
					text = object["content"].(map[string]any)["text"].(string)
				default:
					text = object["text"].(string)
				}
				limits := map[string]int{"discord": discordContentLimit, "googlechat": googleChatTextLimit, "feishu": feishuTextLimit, "zulip": zulipTextLimit}
				if !utf8.ValidString(text) || utf8.RuneCountInString(text) > limits[provider] || !strings.HasSuffix(text, "\n"+p.URL) || !strings.Contains(text, "…") {
					t.Fatalf("%s bound: %d runes valid=%v suffix=%v ellipsis=%v tail=%q", provider, utf8.RuneCountInString(text), utf8.ValidString(text), strings.HasSuffix(text, "\n"+p.URL), strings.Contains(text, "…"), text[len(text)-120:])
				}
			}
		}
	})
}

func TestProviderInvalidPayload(t *testing.T) {
	for _, provider := range []string{"discord", "teams", "googlechat", "feishu", "ntfy", "zulip"} {
		for _, tc := range []struct {
			name string
			edit func(*Payload)
		}{
			{"unsupported kind", func(p *Payload) { p.Kind = "uptime" }},
			{"relative resource", func(p *Payload) { p.URL = "/issues/one" }},
			{"resource fragment", func(p *Payload) { p.URL += "#" }},
			{"resource credentials", func(p *Payload) { p.URL = "https://user:fixture@errors.example/issue" }},
			{"frequency count", func(p *Payload) { p.Count = 0 }},
			{"frequency window", func(p *Payload) { p.WindowMinutes = 0 }},
			{"missing monitor", func(p *Payload) { p.Kind, p.Monitor = "uptime_down", nil }},
			{"overlong resource", func(p *Payload) { p.URL = "https://errors.example/" + strings.Repeat("x", 5000) }},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				p := slackPayload("error_frequency")
				tc.edit(&p)
				if _, err := webhookRequestFor(provider, p); err == nil {
					t.Fatal("invalid provider payload accepted")
				}
			})
		}
	}
}

func providerAckResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestFeishuAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		status, want       int
		disposition        Disposition
	}{
		{name: "current success", status: 200, body: `{"code":0,"msg":"success","data":{}}`, want: 200, disposition: Delivered, reason: "accepted"},
		{name: "legacy success", status: 200, body: `{"StatusCode":0,"StatusMessage":"success"}`, want: 200, disposition: Delivered, reason: "accepted"},
		{name: "nonzero code", status: 200, body: `{"code":19001,"msg":"invalid"}`, want: 200, disposition: Permanent, reason: "feishu_response"},
		{name: "conflicting codes", status: 200, body: `{"code":0,"StatusCode":1}`, want: 200, disposition: Permanent, reason: "feishu_response"},
		{name: "missing code", status: 200, body: `{"msg":"success"}`, want: 200, disposition: Permanent, reason: "feishu_response"},
		{name: "malformed", status: 200, body: `{`, want: 200, disposition: Permanent, reason: "feishu_response"},
		{name: "created", status: 201, body: `{"code":0}`, want: 201, disposition: Permanent, reason: "feishu_response"},
		{name: "throttled", status: 429, body: `{"code":0}`, want: 429, disposition: Retry, reason: "http_status"},
		{name: "unavailable", status: 503, body: `{"code":0}`, want: 503, disposition: Retry, reason: "http_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome := webhookOutcome(context.Background(), "feishu", providerAckResponse(tc.status, tc.body))
			if outcome.Disposition != tc.disposition || outcome.Reason != tc.reason || outcome.StatusCode == nil || *outcome.StatusCode != tc.want {
				t.Fatalf("Feishu outcome: %+v", outcome)
			}
		})
	}
	outcome := webhookOutcome(context.Background(), "feishu", providerAckResponse(200, strings.Repeat(" ", providerAckBodyLimit+1)))
	if outcome.Disposition != Permanent || outcome.Reason != "feishu_response" {
		t.Fatalf("Feishu oversized acknowledgment: %+v", outcome)
	}
}

func TestZulipAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		status, want       int
		disposition        Disposition
	}{
		{name: "success", status: 200, body: `{"result":"success","msg":""}`, want: 200, disposition: Delivered, reason: "accepted"},
		{name: "error result", status: 200, body: `{"result":"error","msg":"invalid"}`, want: 200, disposition: Permanent, reason: "zulip_response"},
		{name: "missing result", status: 200, body: `{}`, want: 200, disposition: Permanent, reason: "zulip_response"},
		{name: "malformed", status: 200, body: `[`, want: 200, disposition: Permanent, reason: "zulip_response"},
		{name: "created", status: 201, body: `{"result":"success"}`, want: 201, disposition: Permanent, reason: "zulip_response"},
		{name: "throttled", status: 429, body: `{}`, want: 429, disposition: Retry, reason: "http_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome := webhookOutcome(context.Background(), "zulip", providerAckResponse(tc.status, tc.body))
			if outcome.Disposition != tc.disposition || outcome.Reason != tc.reason || outcome.StatusCode == nil || *outcome.StatusCode != tc.want {
				t.Fatalf("Zulip outcome: %+v", outcome)
			}
		})
	}
	outcome := webhookOutcome(context.Background(), "zulip", providerAckResponse(200, strings.Repeat(" ", providerAckBodyLimit+1)))
	if outcome.Disposition != Permanent || outcome.Reason != "zulip_response" {
		t.Fatalf("Zulip oversized acknowledgment: %+v", outcome)
	}
}

func TestMattermostAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		status, want       int
		disposition        Disposition
	}{
		{name: "success", status: 200, body: "ok", want: 200, disposition: Delivered, reason: "accepted"},
		{name: "surrounding whitespace", status: 200, body: " \nok\t ", want: 200, disposition: Delivered, reason: "accepted"},
		{name: "wrong acknowledgment", status: 200, body: "OK", want: 200, disposition: Permanent, reason: "mattermost_response"},
		{name: "created", status: 201, body: "ok", want: 201, disposition: Permanent, reason: "mattermost_response"},
		{name: "no content", status: 204, body: "", want: 204, disposition: Permanent, reason: "mattermost_response"},
		{name: "throttled", status: 429, body: "ok", want: 429, disposition: Retry, reason: "http_status"},
		{name: "unavailable", status: 503, body: "ok", want: 503, disposition: Retry, reason: "http_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome := webhookOutcome(context.Background(), "mattermost", providerAckResponse(tc.status, tc.body))
			if outcome.Disposition != tc.disposition || outcome.Reason != tc.reason || outcome.StatusCode == nil || *outcome.StatusCode != tc.want || outcome.RetryAfter != 0 {
				t.Fatalf("Mattermost outcome: %+v", outcome)
			}
		})
	}
	outcome := webhookOutcome(context.Background(), "mattermost", providerAckResponse(200, strings.Repeat("x", 4097)))
	if outcome.Disposition != Permanent || outcome.Reason != "mattermost_response" {
		t.Fatalf("Mattermost oversized acknowledgment: %+v", outcome)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	reader := &webhookReader{reader: strings.NewReader("ok"), err: context.DeadlineExceeded}
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(reader)}
	if outcome := webhookOutcome(ctx, "mattermost", response); outcome.Disposition != Retry || outcome.Reason != "timeout" {
		t.Fatalf("Mattermost acknowledgment deadline classification: %+v", outcome)
	}
}

func TestBodyIndependentProviderAcknowledgments(t *testing.T) {
	for _, provider := range []string{"discord", "teams", "googlechat"} {
		for _, status := range []int{200, 202, 204, 299} {
			response := providerAckResponse(status, "provider fixture body that need not be read")
			if got := webhookOutcome(context.Background(), provider, response); got.Disposition != Delivered || got.Reason != "accepted" || got.StatusCode == nil || *got.StatusCode != status {
				t.Fatalf("%s %d: %+v", provider, status, got)
			}
		}
	}
}

func TestProviderAcknowledgmentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	reader := &webhookReader{reader: strings.NewReader(`{"code":0}`), err: context.DeadlineExceeded}
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(reader)}
	if got := webhookOutcome(ctx, "feishu", response); got.Disposition != Retry || got.Reason != "timeout" {
		t.Fatalf("Feishu deadline classification: %+v", got)
	}
}
