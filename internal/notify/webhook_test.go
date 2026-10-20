package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type slackWireMessage struct {
	Text        string `json:"text"`
	Mrkdwn      *bool  `json:"mrkdwn"`
	Parse       string `json:"parse"`
	UnfurlLinks *bool  `json:"unfurl_links"`
	UnfurlMedia *bool  `json:"unfurl_media"`
	Blocks      []struct {
		Type string `json:"type"`
		Text struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Emoji    *bool  `json:"emoji"`
			Verbatim *bool  `json:"verbatim"`
		} `json:"text"`
	} `json:"blocks"`
}

func slackPayload(kind string) Payload {
	p := delivery(Recipient{Type: "slack"}).Payload.Public(senderOptions().Domain)
	p.Kind, p.Count, p.WindowMinutes, p.Environment = kind, 7, 5, "production"
	p.OccurredAt = time.Date(2026, 10, 14, 2, 0, 0, 0, time.FixedZone("fixture", 2*60*60))
	switch kind {
	case "error_frequency":
		p.Issue = &Resource{ID: "issue-1", Name: "Wrong turn"}
	case "uptime_down":
		p.Monitor = &Monitor{Resource: Resource{ID: "monitor-1", Name: "Database"}, PreviousStatus: "up", Status: "down"}
		p.URL = "https://errors.example/base/organizations/org/monitors/monitor-1"
	case "uptime_recovered":
		p.Monitor = &Monitor{Resource: Resource{ID: "monitor-1", Name: "Database"}, PreviousStatus: "down", Status: "up"}
		p.URL = "https://errors.example/base/organizations/org/monitors/monitor-1"
	}
	return p
}

func decodeSlackMessage(t *testing.T, p Payload) slackWireMessage {
	t.Helper()
	messageBody, err := webhookRequestFor("slack", p)
	if err != nil {
		t.Fatal(err)
	}
	body := messageBody.Body
	var message slackWireMessage
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link_names", "channel", "username"} {
		if _, present := fields[name]; present {
			t.Errorf("Slack message overrides forbidden field %s", name)
		}
	}
	if message.Mrkdwn == nil || *message.Mrkdwn || message.Parse != "none" || message.UnfurlLinks == nil || *message.UnfurlLinks || message.UnfurlMedia == nil || *message.UnfurlMedia {
		t.Errorf("Slack parsing/unfurl controls missing: %s", body)
	}
	if len(message.Blocks) != 2 {
		t.Fatalf("expected summary and resource sections, got %s", body)
	}
	if message.Blocks[0].Type != "section" || message.Blocks[0].Text.Type != "plain_text" || message.Blocks[0].Text.Emoji == nil || *message.Blocks[0].Text.Emoji || message.Blocks[1].Type != "section" || message.Blocks[1].Text.Type != "mrkdwn" || message.Blocks[1].Text.Verbatim == nil || !*message.Blocks[1].Text.Verbatim {
		t.Errorf("Slack text-object safety controls missing: %s", body)
	}
	for _, block := range message.Blocks {
		if !utf8.ValidString(block.Text.Text) || utf8.RuneCountInString(block.Text.Text) < 1 || utf8.RuneCountInString(block.Text.Text) > 3000 {
			t.Error("Slack section violates Unicode text-object bounds")
		}
	}
	return message
}

func TestSlackWebhookMessages(t *testing.T) {
	for _, tc := range []struct{ kind, detail string }{
		{"error_frequency", "Issue: Wrong turn\n7 events in 5 minutes"},
		{"uptime_down", "Monitor: Database\nStatus: up -> down"},
		{"uptime_recovered", "Monitor: Database\nStatus: down -> up"},
		{"test", "Test notification"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			p := slackPayload(tc.kind)
			message := decodeSlackMessage(t, p)
			want := "GoodSentry: Fallos 🙂\nProject: App\n" + tc.detail + "\nEnvironment: production\nOccurred: 2026-10-14T00:00:00Z\nDelivery: delivery-123"
			if message.Blocks[0].Text.Text != want {
				t.Errorf("summary=%q, want %q", message.Blocks[0].Text.Text, want)
			}
			fallback := strings.ReplaceAll(want, ">", "&gt;") + "\n" + p.URL
			if message.Text != fallback || message.Blocks[1].Text.Text != "<"+p.URL+"|View in GoodSentry>" {
				t.Errorf("accessible fallback or prefixed resource link lost: %#v", message)
			}
		})
	}
	t.Run("frequency without optional issue or environment", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Issue, p.Environment = nil, ""
		message := decodeSlackMessage(t, p)
		want := "GoodSentry: Fallos 🙂\nProject: App\n7 events in 5 minutes\nOccurred: 2026-10-14T00:00:00Z\nDelivery: delivery-123"
		if message.Blocks[0].Text.Text != want || message.Text != want+"\n"+p.URL {
			t.Errorf("optional context changed summary: %#v", message)
		}
	})
}

func TestSlackWebhookEscapingAndBounds(t *testing.T) {
	t.Run("telemetry stays plain and resource URL is encoded", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Rule.Name = "<!channel> & <@U123> | *rule*"
		p.Project.Name = "<!here> & _App_"
		p.Issue.Name = "<@U456> `issue`"
		p.Environment = "<!everyone> >prod<"
		p.URL = "https://errors.example/base/organizations/org/issues/issue<1>|?a=1&b=<x>"
		message := decodeSlackMessage(t, p)
		want := "GoodSentry: <!channel> & <@U123> | *rule*\nProject: <!here> & _App_\nIssue: <@U456> `issue`\n7 events in 5 minutes\nEnvironment: <!everyone> >prod<\nOccurred: 2026-10-14T00:00:00Z\nDelivery: delivery-123"
		fallback := "GoodSentry: &lt;!channel&gt; &amp; &lt;@U123&gt; | *rule*\nProject: &lt;!here&gt; &amp; _App_\nIssue: &lt;@U456&gt; `issue`\n7 events in 5 minutes\nEnvironment: &lt;!everyone&gt; &gt;prod&lt;\nOccurred: 2026-10-14T00:00:00Z\nDelivery: delivery-123\nhttps://errors.example/base/organizations/org/issues/issue&lt;1&gt;|?a=1&amp;b=&lt;x&gt;"
		link := "<https://errors.example/base/organizations/org/issues/issue%3C1%3E%7C?a=1&amp;b=%3Cx%3E|View in GoodSentry>"
		if message.Blocks[0].Text.Text != want || message.Text != fallback || message.Blocks[1].Text.Text != link {
			t.Errorf("unsafe Slack interpolation: %#v", message)
		}
	})
	t.Run("summary truncates by Unicode characters and keeps the resource", func(t *testing.T) {
		p := slackPayload("error_frequency")
		p.Issue.Name = strings.Repeat("🙂", 3200)
		message := decodeSlackMessage(t, p)
		prefix := "GoodSentry: Fallos 🙂\nProject: App\nIssue: "
		want := prefix + strings.Repeat("🙂", 2999-utf8.RuneCountInString(prefix)) + "…"
		if message.Blocks[0].Text.Text != want || message.Text != want+"\n"+p.URL || message.Blocks[1].Text.Text != "<"+p.URL+"|View in GoodSentry>" {
			t.Error("summary truncation lost Unicode boundary, ellipsis or accessible resource")
		}
	})
	t.Run("resource text at the protocol limit", func(t *testing.T) {
		p := slackPayload("test")
		p.URL = "https://errors.example/base/" + strings.Repeat("x", 3000-len("<https://errors.example/base/|View in GoodSentry>"))
		message := decodeSlackMessage(t, p)
		if utf8.RuneCountInString(message.Blocks[1].Text.Text) != 3000 || message.Blocks[1].Text.Text != "<"+p.URL+"|View in GoodSentry>" {
			t.Error("valid maximum-length public resource link was changed")
		}
	})
	t.Run("admitted public prefix is URL escaped", func(t *testing.T) {
		p := slackPayload("test")
		p.URL = "https://errors.example/base path/日本語?issue=1&org=x"
		message := decodeSlackMessage(t, p)
		want := "<https://errors.example/base%20path/%E6%97%A5%E6%9C%AC%E8%AA%9E?issue=1&amp;org=x|View in GoodSentry>"
		if message.Blocks[1].Text.Text != want || !strings.HasSuffix(message.Text, "\nhttps://errors.example/base path/日本語?issue=1&amp;org=x") {
			t.Error("public prefix lost its escaped link or accessible URL")
		}
	})
}

func TestSlackWebhookInvalidPayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Payload)
	}{
		{"unsupported kind", func(p *Payload) { p.Kind = "uptime" }},
		{"missing kind", func(p *Payload) { p.Kind = "" }},
		{"missing frequency count", func(p *Payload) { p.Count = 0 }},
		{"missing frequency window", func(p *Payload) { p.WindowMinutes = 0 }},
		{"missing down monitor", func(p *Payload) { p.Kind, p.Monitor = "uptime_down", nil }},
		{"missing recovery monitor", func(p *Payload) { p.Kind, p.Monitor = "uptime_recovered", nil }},
		{"relative public link", func(p *Payload) { p.URL = "/organizations/org/issues/issue-1" }},
		{"public link credentials", func(p *Payload) { p.URL = "https://user:fixture-value@errors.example/base/issue-1" }},
		{"public link fragment", func(p *Payload) { p.URL += "#" }},
		{"overlong public link", func(p *Payload) { p.URL = "https://errors.example/" + strings.Repeat("x", 3000) }},
		{"escaping expands public link beyond bound", func(p *Payload) { p.URL = "https://errors.example/?x=" + strings.Repeat("&", 700) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := slackPayload("error_frequency")
			tc.edit(&p)
			if _, err := webhookRequestFor("slack", p); err == nil {
				t.Fatal("misleading or invalid Slack message accepted")
			}
		})
	}
}

func TestGenericWebhookBodyPreserved(t *testing.T) {
	p := delivery(Recipient{Type: "webhook"}).Payload.Public(senderOptions().Domain)
	p.Count, p.WindowMinutes, p.Environment = 7, 5, "production"
	p.Issue = &Resource{ID: "issue-1", Name: "Wrong turn"}
	message, err := webhookRequestFor("webhook", p)
	if err != nil {
		t.Fatal(err)
	}
	body := message.Body
	var got, want map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	const fixture = `{"version":1,"id":"delivery-123","kind":"error_frequency","occurredAt":"2026-10-14T00:00:00Z","rule":{"id":"","name":"Fallos 🙂"},"organization":"","project":{"id":"project-1","name":"App","slug":"app"},"issue":{"id":"issue-1","name":"Wrong turn"},"count":7,"windowMinutes":5,"environment":"production","url":"https://errors.example/base/organizations/org/issues/issue-1"}`
	if err := json.Unmarshal([]byte(fixture), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generic webhook JSON contract changed: %s", body)
	}
}

type webhookReader struct {
	reader io.Reader
	err    error
	reads  int
}

func (r *webhookReader) Read(p []byte) (int, error) {
	r.reads++
	if r.err != nil {
		return 0, r.err
	}
	return r.reader.Read(p)
}

func TestSlackWebhookAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		status             int
		readError          error
		want               Disposition
	}{
		{name: "ok", status: 200, body: "ok", want: Delivered, reason: "accepted"},
		{name: "surrounding whitespace", status: 200, body: " \t\r\nok \n", want: Delivered, reason: "accepted"},
		{name: "exact body bound", status: 200, body: strings.Repeat(" ", 4094) + "ok", want: Delivered, reason: "accepted"},
		{name: "created", status: 201, body: "ok", want: Permanent, reason: "slack_response"},
		{name: "no content", status: 204, want: Permanent, reason: "slack_response"},
		{name: "other success status", status: 299, body: "ok", want: Permanent, reason: "slack_response"},
		{name: "unexpected body", status: 200, body: "receiver fixture detail", want: Permanent, reason: "slack_response"},
		{name: "empty body", status: 200, want: Permanent, reason: "slack_response"},
		{name: "oversized body", status: 200, body: strings.Repeat(" ", 4095) + "ok", want: Permanent, reason: "slack_response"},
		{name: "body read failure", status: 200, readError: errors.New("receiver fixture detail"), want: Retry, reason: "http_error"},
		{name: "body deadline", status: 200, readError: context.DeadlineExceeded, want: Retry, reason: "timeout"},
		{name: "redirect", status: 302, body: "ok", want: Permanent, reason: "http_status"},
		{name: "server unavailable", status: 503, body: "ok", want: Retry, reason: "http_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &webhookReader{reader: strings.NewReader(tc.body), err: tc.readError}
			response := &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(reader)}
			got := webhookOutcome(context.Background(), "slack", response)
			if got.Disposition != tc.want || got.Reason != tc.reason || got.RetryAfter != 0 {
				t.Errorf("Slack acknowledgment=%+v, want disposition=%s reason=%s", got, tc.want, tc.reason)
			}
			if tc.readError == nil && (got.StatusCode == nil || *got.StatusCode != tc.status) {
				t.Errorf("response status was lost: %+v", got)
			}
		})
	}
}

func TestSlackWebhookRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		want   time.Duration
	}{
		{name: "absent"},
		{name: "zero", values: []string{"0"}},
		{name: "seconds", values: []string{"15"}, want: 15 * time.Second},
		{name: "long provider floor", values: []string{"3600"}, want: time.Hour},
		{name: "whitespace", values: []string{" \t45 \t"}, want: 45 * time.Second},
		{name: "leading zeroes", values: []string{"0003"}, want: 3 * time.Second},
		{name: "duration maximum seconds", values: []string{"9223372036"}, want: 9223372036 * time.Second},
		{name: "empty", values: []string{""}},
		{name: "negative", values: []string{"-1"}},
		{name: "signed zero", values: []string{"-0"}},
		{name: "positive sign", values: []string{"+5"}},
		{name: "fraction", values: []string{"1.5"}},
		{name: "inner whitespace", values: []string{"1 5"}},
		{name: "non-ASCII digits", values: []string{"１５"}},
		{name: "HTTP date", values: []string{"Wed, 21 Oct 2026 07:28:00 GMT"}},
		{name: "comma combined", values: []string{"1, 2"}},
		{name: "repeated", values: []string{"1", "2"}},
		{name: "repeated equal", values: []string{"1", "1"}},
		{name: "duration overflow", values: []string{"9223372037"}},
		{name: "integer overflow", values: []string{"999999999999999999999999999999"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("receiver fixture detail"))}
			for _, value := range tc.values {
				response.Header.Add("Retry-After", value)
			}
			got := webhookOutcome(context.Background(), "slack", response)
			if got.Disposition != Retry || got.Reason != "http_status" || got.StatusCode == nil || *got.StatusCode != 429 || got.RetryAfter != tc.want {
				t.Errorf("Slack rate limit=%+v, want RetryAfter=%s", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		kind   string
		status int
	}{{"webhook", 429}, {"slack", 503}, {"slack", 200}} {
		response := &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader("ok"))}
		if got := webhookOutcome(context.Background(), tc.kind, response); got.RetryAfter != 0 {
			t.Errorf("provider retry hint leaked to %s/%d: %+v", tc.kind, tc.status, got)
		}
	}
}

func TestGenericWebhookAcknowledgmentIgnoresBody(t *testing.T) {
	for _, status := range []int{200, 204} {
		reader := &webhookReader{reader: strings.NewReader("arbitrary receiver body"), err: errors.New("generic receiver need not complete its body")}
		response := &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(reader)}
		got := webhookOutcome(context.Background(), "webhook", response)
		if got.Disposition != Delivered || got.Reason != "accepted" || got.StatusCode == nil || *got.StatusCode != status || got.RetryAfter != 0 || reader.reads != 0 {
			t.Errorf("generic %d acknowledgment changed: %+v reads=%d", status, got, reader.reads)
		}
	}
}
