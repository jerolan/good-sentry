package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"goodsentry/internal/nettarget"
)

type Options struct {
	Domain, EmailURL, FromEmail string
	Timeout                     time.Duration
	AllowPrivate                bool
	Console                     io.Writer
}

// Sender is shared by delivery workers; each SMTP delivery owns its socket.
type Sender struct {
	options   Options
	mail      mailConfig
	client    *http.Client
	transport *http.Transport
	consoleMu sync.Mutex
}

func NewSender(options Options) (*Sender, error) {
	config, err := parseMail(options.EmailURL, options.FromEmail)
	if err != nil {
		return nil, err
	}
	if !validHTTPAddress(options.Domain, true) {
		return nil, errors.New("public domain must be an absolute HTTP or HTTPS URL without credentials, query or fragment")
	}
	if options.Timeout <= 0 || options.Timeout > time.Minute {
		return nil, errors.New("delivery timeout must be positive and at most 60 seconds")
	}
	if config.scheme == "consolemail" && options.Console == nil {
		return nil, errors.New("consolemail requires a console writer")
	}
	options.Domain = strings.TrimRight(options.Domain, "/")
	dialer := nettarget.Dialer{AllowPrivate: options.AllowPrivate}
	transport := &http.Transport{DialContext: dialer.DialContext, MaxResponseHeaderBytes: 64 << 10, MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	return &Sender{options: options, mail: config, transport: transport, client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (s *Sender) Close() { s.transport.CloseIdleConnections() }

// Deliver exposes only stable reasons. Caller cancellation is returned to the
// worker; the configured delivery deadline is a retryable outcome.
func (s *Sender) Deliver(ctx context.Context, delivery Delivery) (outcome Outcome, err error) {
	started := time.Now()
	defer func() {
		outcome.ElapsedMS = time.Since(started).Milliseconds()
		if ctx.Err() != nil && outcome.Disposition != Delivered {
			outcome, err = Outcome{}, ctx.Err()
		}
	}()
	if ctx.Err() != nil {
		return Outcome{}, ctx.Err()
	}
	if err := validateRecipient(delivery.Recipient, false); err != nil {
		return Outcome{Disposition: Permanent, Reason: "invalid_destination"}, nil
	}
	if !safeIdentity(delivery.ID) || !safeIdentity(delivery.Payload.Kind) {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}, nil
	}
	payload := delivery.Payload.Public(s.options.Domain)
	deliveryCtx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	if delivery.Recipient.Type == "webhook" {
		return s.webhook(deliveryCtx, delivery, payload), nil
	}
	message, messageErr := s.message(delivery, payload)
	if messageErr != nil {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}, nil
	}
	if s.mail.scheme == "consolemail" {
		s.consoleMu.Lock()
		defer s.consoleMu.Unlock()
		if deliveryCtx.Err() != nil {
			return failure(deliveryCtx.Err(), deliveryCtx, "console_error"), nil
		}
		n, writeErr := s.options.Console.Write(message)
		if writeErr != nil || n != len(message) {
			return Outcome{Disposition: Retry, Reason: "console_error"}, nil
		}
		return Outcome{Disposition: Delivered, Reason: "accepted"}, nil
	}
	return s.smtp(deliveryCtx, delivery.Recipient.Destination, message), nil
}

func safeIdentity(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.') {
			return false
		}
	}
	return true
}

func (s *Sender) webhook(ctx context.Context, delivery Delivery, payload Payload) Outcome {
	body, err := json.Marshal(payload)
	if err != nil {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.Recipient.Destination, bytes.NewReader(body))
	if err != nil {
		return Outcome{Disposition: Permanent, Reason: "invalid_destination"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GoodSentry-Delivery-ID", delivery.ID)
	request.Header.Set("X-GoodSentry-Event", payload.Kind)
	response, err := s.client.Do(request)
	if err != nil {
		return failure(err, ctx, "http_error")
	}
	// Receiver content has no delivery semantics. Closing without reading bounds
	// memory and prevents an unending response body from delaying acceptance.
	response.Body.Close()
	outcome := Outcome{Disposition: Permanent, Reason: "http_status", StatusCode: &response.StatusCode}
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		outcome.Disposition, outcome.Reason = Delivered, "accepted"
	case response.StatusCode == 408 || response.StatusCode == 425 || response.StatusCode == 429 || response.StatusCode >= 500 && response.StatusCode < 600:
		outcome.Disposition = Retry
	}
	return outcome
}

func (s *Sender) message(delivery Delivery, payload Payload) ([]byte, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	domain, _ := url.Parse(s.options.Domain)
	messageHost := domain.Hostname()
	if strings.Contains(messageHost, ":") {
		messageHost = "[" + messageHost + "]"
	}
	subject := strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) {
			return ' '
		}
		return ch
	}, "GoodSentry: "+delivery.RuleName)
	var output bytes.Buffer
	output.WriteString("From: " + s.options.FromEmail + "\r\nTo: " + delivery.Recipient.Destination + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	output.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + delivery.ID + "@" + messageHost + ">\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	writer := quotedprintable.NewWriter(&output)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	output.WriteString("\r\n")
	return output.Bytes(), nil
}

func (s *Sender) smtp(ctx context.Context, recipient string, message []byte) Outcome {
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.mail.host, s.mail.port))
	if err != nil {
		return failure(err, ctx, "connection_error")
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return failure(err, ctx, "connection_error")
		}
	}
	rawConnection := connection
	stop := context.AfterFunc(ctx, func() { rawConnection.Close() })
	defer stop()
	tlsConfig := &tls.Config{ServerName: s.mail.host, MinVersion: tls.VersionTLS12}
	if s.mail.scheme == "smtps" {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return failure(err, ctx, "tls_error")
		}
		connection = tlsConnection
	}
	client, err := smtp.NewClient(connection, s.mail.host)
	if err != nil {
		return failure(err, ctx, "smtp_error")
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return failure(err, ctx, "smtp_error")
	}
	_, secure := client.TLSConnectionState()
	if !secure {
		if offered, _ := client.Extension("STARTTLS"); offered {
			if err := client.StartTLS(tlsConfig); err != nil {
				return failure(err, ctx, "tls_error")
			}
			secure = true
		} else if s.mail.scheme == "smtp+starttls" || s.mail.credentials {
			return Outcome{Disposition: Permanent, Reason: "tls_required"}
		}
	}
	if s.mail.credentials {
		if !secure {
			return Outcome{Disposition: Permanent, Reason: "tls_required"}
		}
		if err := client.Auth(smtp.PlainAuth("", s.mail.username, s.mail.password, s.mail.host)); err != nil {
			return failure(err, ctx, "smtp_error")
		}
	}
	if err := client.Mail(s.options.FromEmail); err != nil {
		return failure(err, ctx, "smtp_error")
	}
	if err := client.Rcpt(recipient); err != nil {
		return failure(err, ctx, "smtp_error")
	}
	writer, err := client.Data()
	if err != nil {
		return failure(err, ctx, "smtp_error")
	}
	if _, err := writer.Write(message); err != nil {
		return failure(err, ctx, "smtp_error")
	}
	if err := writer.Close(); err != nil {
		return failure(err, ctx, "smtp_error")
	}
	// DATA acceptance owns success. A subsequent QUIT failure cannot undo it.
	client.Quit()
	status := 250
	return Outcome{Disposition: Delivered, Reason: "accepted", StatusCode: &status}
}

func failure(err error, ctx context.Context, fallback string) Outcome {
	outcome := Outcome{Disposition: Retry, Reason: fallback}
	var networkErr net.Error
	var certificateErr *tls.CertificateVerificationError
	var authorityErr x509.UnknownAuthorityError
	var smtpErr *textproto.Error
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		outcome.Reason = "timeout"
	case errors.Is(err, nettarget.ErrBlocked):
		outcome.Disposition, outcome.Reason = Permanent, "destination_blocked"
	case errors.Is(err, nettarget.ErrDNS):
		outcome.Reason = "dns_error"
	case errors.As(err, &certificateErr), errors.As(err, &authorityErr):
		outcome.Disposition, outcome.Reason = Permanent, "certificate_error"
	case errors.As(err, &smtpErr):
		outcome.StatusCode = &smtpErr.Code
		outcome.Reason = "smtp_status"
		if smtpErr.Code >= 500 && smtpErr.Code < 600 {
			outcome.Disposition = Permanent
		}
	case errors.As(err, &networkErr):
		if networkErr.Timeout() {
			outcome.Reason = "timeout"
		} else {
			outcome.Reason = "connection_error"
		}
	}
	return outcome
}
