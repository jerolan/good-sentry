package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func senderOptions() Options {
	return Options{Domain: "https://errors.example/base", EmailURL: "consolemail://", FromEmail: "alerts@example.com", Timeout: time.Second, Console: &bytes.Buffer{}}
}

func delivery(recipient Recipient) Delivery {
	return Delivery{ID: "delivery-123", RuleName: "Fallos 🙂", Recipient: recipient, Payload: Payload{Version: 1, ID: "delivery-123", Kind: "error_frequency", Rule: Resource{Name: "Fallos 🙂"}, OccurredAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Project: Project{Resource: Resource{ID: "project-1", Name: "App"}, Slug: "app"}, URL: "/organizations/org/issues/issue-1"}}
}

func TestSenderOptions(t *testing.T) {
	for _, edit := range []func(*Options){func(o *Options) { o.Domain = "/relative" }, func(o *Options) { o.Domain = "https://user:secret@example.com" }, func(o *Options) { o.Domain += "?query" }, func(o *Options) { o.Timeout = 0 }, func(o *Options) { o.Timeout = 61 * time.Second }, func(o *Options) { o.Console = nil }} {
		opts := senderOptions()
		edit(&opts)
		if sender, err := NewSender(opts); err == nil {
			sender.Close()
			t.Fatal("invalid options accepted")
		}
	}
}

func TestWebhookDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-GoodSentry-Delivery-ID") != "delivery-123" || r.Header.Get("X-GoodSentry-Event") != "error_frequency" {
			t.Errorf("unexpected request identity: %s %#v", r.Method, r.Header)
		}
		var p Payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.ID != "delivery-123" || p.URL != "https://errors.example/base/organizations/org/issues/issue-1" {
			t.Errorf("webhook payload: %#v %v", p, err)
		}
		status, _ := strconv.Atoi(r.URL.Query().Get("status"))
		w.WriteHeader(status)
	}))
	defer server.Close()
	opts := senderOptions()
	opts.AllowPrivate = true
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	for _, tc := range []struct {
		status int
		want   Disposition
	}{{200, Delivered}, {204, Delivered}, {299, Delivered}, {408, Retry}, {425, Retry}, {429, Retry}, {500, Retry}, {503, Retry}, {600, Permanent}, {301, Permanent}, {400, Permanent}, {401, Permanent}, {404, Permanent}} {
		outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "webhook", Destination: server.URL + "?status=" + strconv.Itoa(tc.status)}))
		if err != nil || outcome.Disposition != tc.want || outcome.StatusCode == nil || *outcome.StatusCode != tc.status || outcome.ElapsedMS < 0 {
			t.Errorf("HTTP %d: %#v %v", tc.status, outcome, err)
		}
	}
}

func TestSlackWebhookDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var received atomic.Int64
	headersFlushed := make(chan struct{})
	incompleteReleased := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-GoodSentry-Delivery-ID") != "delivery-123" || r.Header.Get("X-GoodSentry-Event") != "error_frequency" {
			t.Errorf("unexpected Slack request identity: %s %#v", r.Method, r.Header)
		}
		var message struct {
			Text   string            `json:"text"`
			Blocks []json.RawMessage `json:"blocks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil || len(message.Blocks) != 2 || !strings.Contains(message.Text, "https://errors.example/base/organizations/org/issues/issue-1") {
			t.Errorf("Slack POST lost blocks or accessible public resource: %#v %v", message, err)
		}
		switch r.URL.Path {
		case "/rate-limit":
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
		case "/oversized":
			io.WriteString(w, strings.Repeat(" ", 4095)+"ok")
		case "/incomplete":
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(headersFlushed)
			select {
			case <-r.Context().Done():
			case <-release:
			}
			close(incompleteReleased)
		default:
			io.WriteString(w, "ok")
		}
	}))
	defer server.Close()
	defer close(release)
	opts := senderOptions()
	opts.AllowPrivate = true
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	for _, tc := range []struct {
		path, reason string
		want         Disposition
		status       int
		retryAfter   time.Duration
	}{
		{path: "/accepted", want: Delivered, reason: "accepted", status: 200},
		{path: "/rate-limit", want: Retry, reason: "http_status", status: 429, retryAfter: time.Hour},
		{path: "/oversized", want: Permanent, reason: "slack_response", status: 200},
		{path: "/incomplete", want: Retry, reason: "timeout"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			d := delivery(Recipient{Type: "slack", Destination: server.URL + tc.path})
			d.Payload.Count, d.Payload.WindowMinutes = 7, 5
			outcome, err := sender.Deliver(ctx, d)
			if err != nil || outcome.Disposition != tc.want || outcome.Reason != tc.reason || outcome.RetryAfter != tc.retryAfter || outcome.ElapsedMS < 0 {
				t.Errorf("Slack POST outcome: %+v %v", outcome, err)
			}
			if tc.status != 0 && (outcome.StatusCode == nil || *outcome.StatusCode != tc.status) {
				t.Errorf("Slack HTTP status lost: %+v", outcome)
			}
			if tc.path == "/incomplete" {
				select {
				case <-headersFlushed:
				default:
					t.Error("deadline did not reach the incomplete acknowledgment body")
				}
				select {
				case <-incompleteReleased:
				case <-time.After(time.Second):
					t.Error("delivery deadline did not release the incomplete response handler")
				}
			}
		})
	}
	if received.Load() != 4 {
		t.Errorf("Slack delivery did not reach the real HTTP receiver four times: %d", received.Load())
	}
}

func TestProviderWebhookDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider := strings.TrimPrefix(r.URL.Path, "/")
		if r.Method != http.MethodPost || r.Header.Get("X-GoodSentry-Delivery-ID") != "delivery-123" || r.Header.Get("X-GoodSentry-Event") != "error_frequency" {
			t.Errorf("%s lost GoodSentry request identity: %s %#v", provider, r.Method, r.Header)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("%s body: %v", provider, err)
			return
		}
		if provider == "ntfy" {
			if r.Header.Get("Content-Type") != "text/plain; charset=UTF-8" || r.Header.Get("Title") != "GoodSentry: Fallos 🙂" || r.Header.Get("Click") != "https://errors.example/base/organizations/org/issues/issue-1" || !strings.Contains(string(body), r.Header.Get("Click")) {
				t.Errorf("ntfy request bytes/headers: %s %#v", body, r.Header)
			}
		} else {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("%s content type: %s", provider, r.Header.Get("Content-Type"))
			}
			var object map[string]any
			if err := json.Unmarshal(body, &object); err != nil {
				t.Errorf("%s JSON body: %v", provider, err)
			}
			if key := map[string]string{"slack": "blocks", "discord": "content", "teams": "sections", "googlechat": "text", "feishu": "msg_type", "zulip": "text"}[provider]; key != "" {
				if _, present := object[key]; !present {
					t.Errorf("%s body lacks %s: %s", provider, key, body)
				}
			}
		}
		switch provider {
		case "discord":
			w.WriteHeader(http.StatusNoContent)
		case "feishu":
			io.WriteString(w, `{"code":0,"msg":"success"}`)
		case "zulip":
			io.WriteString(w, `{"result":"success","msg":""}`)
		case "slack":
			io.WriteString(w, "ok")
		default:
			io.WriteString(w, `{"ok":true}`)
		}
	}))
	defer server.Close()
	opts := senderOptions()
	opts.AllowPrivate = true
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	for _, provider := range []string{"slack", "discord", "teams", "googlechat", "feishu", "ntfy", "zulip"} {
		t.Run(provider, func(t *testing.T) {
			d := delivery(Recipient{Type: provider, Destination: server.URL + "/" + provider})
			d.Payload.Count, d.Payload.WindowMinutes = 7, 5
			outcome, err := sender.Deliver(context.Background(), d)
			if err != nil || outcome.Disposition != Delivered || outcome.Reason != "accepted" || outcome.StatusCode == nil {
				t.Fatalf("%s delivery: %+v %v", provider, outcome, err)
			}
		})
	}
}

func TestWebhookNetworkPolicy(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", 302)
		}
	}))
	defer server.Close()
	opts := senderOptions()
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "webhook", Destination: server.URL}))
	sender.Close()
	if err != nil || outcome.Disposition != Permanent || outcome.Reason != "destination_blocked" || received.Load() != 0 {
		t.Fatalf("private target: %#v %v received=%d", outcome, err, received.Load())
	}
	opts.AllowPrivate = true
	sender, err = NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	outcome, err = sender.Deliver(context.Background(), delivery(Recipient{Type: "webhook", Destination: server.URL + "/redirect"}))
	if err != nil || outcome.Disposition != Permanent || received.Load() != 1 || outcome.StatusCode == nil || *outcome.StatusCode != 302 {
		t.Fatalf("redirect followed: %#v %v received=%d", outcome, err, received.Load())
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsServer.Close()
	outcome, err = sender.Deliver(context.Background(), delivery(Recipient{Type: "webhook", Destination: tlsServer.URL}))
	if err != nil || outcome.Disposition != Permanent || outcome.Reason != "certificate_error" {
		t.Fatalf("untrusted webhook TLS: %#v %v", outcome, err)
	}
}

func TestWebhookResponseBoundsAndCancellation(t *testing.T) {
	started := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/headers":
			w.Header().Set("X-Huge", strings.Repeat("x", 128<<10))
		case "/body":
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			started <- struct{}{}
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	opts := senderOptions()
	opts.AllowPrivate = true
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	for _, tc := range []struct {
		path string
		want Disposition
	}{{"/headers", Retry}, {"/body", Delivered}, {"/timeout", Retry}} {
		outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "webhook", Destination: server.URL + tc.path}))
		if err != nil || outcome.Disposition != tc.want || (tc.path == "/timeout" && outcome.Reason != "timeout") {
			t.Errorf("%s: %#v %v", tc.path, outcome, err)
		}
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := sender.Deliver(ctx, delivery(Recipient{Type: "webhook", Destination: server.URL}))
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation: %v", err)
	}
}

func TestConsoleMailMIMEAndConcurrentWrites(t *testing.T) {
	var output bytes.Buffer
	opts := senderOptions()
	opts.Console = &output
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	d := delivery(Recipient{Type: "email", Destination: "admin@example.com"})
	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := sender.Deliver(context.Background(), d)
			if err != nil || outcome.Disposition != Delivered {
				t.Errorf("console: %#v %v", outcome, err)
			}
		}()
	}
	wg.Wait()
	if n := strings.Count(output.String(), "Message-ID: <delivery-123@"); n != workers {
		t.Fatalf("console messages interleaved/lost: %d", n)
	}
	verifyMail(t, output.Bytes(), "admin@example.com")
	d.Recipient.Destination = "admin@example.com\r\nBcc: victim@example.com"
	outcome, err := sender.Deliver(context.Background(), d)
	if err != nil || outcome.Disposition != Permanent {
		t.Fatalf("header injection: %#v %v", outcome, err)
	}
}

func TestSMTPDeliveryAndResponses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rcpt        int
		quitFailure bool
		want        Disposition
	}{{"accepted", 250, false, Delivered}, {"accepted quit failure", 250, true, Delivered}, {"temporary", 450, false, Retry}, {"permanent", 550, false, Permanent}} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := smtpServer(t, smtpBehavior{rcptStatus: tc.rcpt, quitFailure: tc.quitFailure})
			opts := senderOptions()
			opts.EmailURL = "smtp://" + fixture.address
			sender, err := NewSender(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "email", Destination: "admin@example.com"}))
			if err != nil || outcome.Disposition != tc.want {
				t.Fatalf("SMTP outcome: %#v %v", outcome, err)
			}
			if tc.want == Delivered {
				verifyMail(t, <-fixture.messages, "admin@example.com")
				commands := strings.Join(fixture.commands(), "\n")
				if !strings.Contains(commands, "MAIL FROM:<alerts@example.com>") || !strings.Contains(commands, "RCPT TO:<admin@example.com>") {
					t.Fatalf("SMTP envelope: %s", commands)
				}
			}
		})
	}
}

func TestSMTPRequiredTLSAndCredentialProtection(t *testing.T) {
	for _, tc := range []struct{ scheme, credentials string }{{"smtp+starttls", ""}, {"smtp", "operator:very-secret@"}} {
		fixture := smtpServer(t, smtpBehavior{rcptStatus: 250})
		opts := senderOptions()
		opts.EmailURL = tc.scheme + "://" + tc.credentials + fixture.address
		sender, err := NewSender(opts)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "email", Destination: "admin@example.com"}))
		sender.Close()
		if err != nil || outcome.Disposition != Permanent || outcome.Reason != "tls_required" {
			t.Fatalf("TLS requirement: %#v %v", outcome, err)
		}
		commands := strings.Join(fixture.commands(), "\n")
		if strings.Contains(commands, "AUTH") || strings.Contains(commands, "MAIL FROM") || strings.Contains(outcome.Reason, "very-secret") {
			t.Fatalf("credentials/mail sent without TLS: %s %#v", commands, outcome)
		}
	}
	tlsHTTP := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsHTTP.Close()
	for _, implicit := range []bool{false, true} {
		fixture := smtpServer(t, smtpBehavior{rcptStatus: 250, tlsConfig: tlsHTTP.TLS, implicitTLS: implicit})
		opts := senderOptions()
		opts.EmailURL = "smtp+starttls://" + fixture.address
		if implicit {
			opts.EmailURL = "smtps://" + fixture.address
		}
		sender, err := NewSender(opts)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "email", Destination: "admin@example.com"}))
		sender.Close()
		if err != nil || outcome.Disposition != Permanent || outcome.Reason != "certificate_error" {
			t.Fatalf("untrusted SMTP TLS implicit=%v: %#v %v", implicit, outcome, err)
		}
	}
}

func TestSMTPNamesCannotInjectHeaders(t *testing.T) {
	fixture := smtpServer(t, smtpBehavior{rcptStatus: 250})
	opts := senderOptions()
	opts.EmailURL = "smtp://" + fixture.address
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	d := delivery(Recipient{Type: "email", Destination: "admin@example.com"})
	d.RuleName = "Fallos 🙂\r\nBcc: victim@example.com\r\nTo: forged@example.com"
	d.Payload.Rule.Name = d.RuleName
	d.Payload.Issue = &Resource{ID: "issue-1", Name: "issue\r\nBcc: victim@example.com"}
	d.Payload.Monitor = &Monitor{Resource: Resource{ID: "monitor-1", Name: "monitor\r\nFrom: forged@example.com"}}
	outcome, err := sender.Deliver(context.Background(), d)
	if err != nil || outcome.Disposition != Delivered {
		t.Fatalf("SMTP delivery: %#v %v", outcome, err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(<-fixture.messages))
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.Get("Bcc") != "" || len(message.Header["To"]) != 1 || len(message.Header["From"]) != 1 || len(message.Header["Subject"]) != 1 || message.Header.Get("To") != "admin@example.com" {
		t.Fatalf("forged MIME headers: %#v", message.Header)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil || strings.ContainsAny(subject, "\r\n") || !strings.Contains(subject, "Fallos 🙂") {
		t.Fatalf("unsafe subject: %q %v", subject, err)
	}
}

func TestSMTPCancellationAndTimeout(t *testing.T) {
	fixture := smtpServer(t, smtpBehavior{stall: true})
	opts := senderOptions()
	opts.EmailURL = "smtp://" + fixture.address
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "email", Destination: "admin@example.com"}))
	if err != nil || outcome.Disposition != Retry || outcome.Reason != "timeout" {
		t.Fatalf("SMTP timeout: %#v %v", outcome, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := sender.Deliver(ctx, delivery(Recipient{Type: "email", Destination: "admin@example.com"}))
		done <- err
	}()
	<-fixture.connections
	<-fixture.connections
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("SMTP cancellation: %v", err)
	}
}

func verifyMail(t *testing.T, data []byte, to string) {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil || !strings.Contains(subject, "Fallos 🙂") {
		t.Fatalf("subject: %q %v", subject, err)
	}
	if message.Header.Get("From") != "alerts@example.com" || message.Header.Get("To") != to || message.Header.Get("Message-ID") != "<delivery-123@errors.example>" || message.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("MIME headers: %#v", message.Header)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil || !bytes.Contains(body, []byte("https://errors.example/base/organizations/org/issues/issue-1")) {
		t.Fatalf("MIME body: %q %v", body, err)
	}
}

type smtpBehavior struct {
	rcptStatus                      int
	quitFailure, stall, implicitTLS bool
	tlsConfig                       *tls.Config
	authUsername, authPassword      string
}
type smtpFixture struct {
	address     string
	messages    chan []byte
	connections chan struct{}
	mu          sync.Mutex
	received    []string
}

func (f *smtpFixture) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.received...)
}

func smtpServer(t *testing.T, behavior smtpBehavior) *smtpFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &smtpFixture{address: listener.Addr().String(), messages: make(chan []byte, 8), connections: make(chan struct{}, 8)}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.connections <- struct{}{}
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				if behavior.stall {
					io.Copy(io.Discard, conn)
					return
				}
				if behavior.implicitTLS {
					tlsConn := tls.Server(conn, behavior.tlsConfig)
					if tlsConn.Handshake() != nil {
						return
					}
					conn = tlsConn
				}
				text := textproto.NewConn(conn)
				if text.PrintfLine("220 localhost SMTP fixture") != nil {
					return
				}
				secure := behavior.implicitTLS
				authenticated := false
				for {
					line, err := text.ReadLine()
					if err != nil {
						return
					}
					f.mu.Lock()
					f.received = append(f.received, line)
					f.mu.Unlock()
					command := strings.SplitN(line, " ", 2)[0]
					switch command {
					case "EHLO":
						if behavior.tlsConfig != nil && !secure {
							text.PrintfLine("250-localhost\r\n250 STARTTLS")
						} else if behavior.authUsername != "" {
							text.PrintfLine("250-localhost\r\n250 AUTH PLAIN")
						} else {
							text.PrintfLine("250 localhost")
						}
					case "AUTH":
						if !secure {
							text.PrintfLine("530 TLS required")
							continue
						}
						parts := strings.SplitN(line, " ", 3)
						if len(parts) != 3 || parts[1] != "PLAIN" {
							text.PrintfLine("504 unsupported authentication")
							continue
						}
						credentials, err := base64.StdEncoding.DecodeString(parts[2])
						if err != nil || string(credentials) != "\x00"+behavior.authUsername+"\x00"+behavior.authPassword {
							text.PrintfLine("535 invalid fixture credentials")
							continue
						}
						authenticated = true
						text.PrintfLine("235 authenticated")
					case "MAIL":
						if behavior.authUsername != "" && !authenticated {
							text.PrintfLine("530 authentication required")
							continue
						}
						text.PrintfLine("250 OK")
					case "HELO":
						text.PrintfLine("250 OK")
					case "RCPT":
						text.PrintfLine("%d recipient result", behavior.rcptStatus)
					case "DATA":
						text.PrintfLine("354 send data")
						body, err := text.ReadDotBytes()
						if err != nil {
							return
						}
						f.messages <- body
						text.PrintfLine("250 accepted")
					case "STARTTLS":
						text.PrintfLine("220 begin TLS")
						tlsConn := tls.Server(conn, behavior.tlsConfig)
						if tlsConn.Handshake() != nil {
							return
						}
						conn = tlsConn
						secure = true
						text = textproto.NewConn(conn)
					case "QUIT":
						if behavior.quitFailure {
							text.PrintfLine("421 goodbye failed")
						} else {
							text.PrintfLine("221 goodbye")
						}
						return
					default:
						text.PrintfLine("500 unsupported command")
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	return f
}
