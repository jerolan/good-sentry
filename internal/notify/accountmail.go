package notify

import (
	"bytes"
	"context"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// AccountMail is a purpose-specific account message. It deliberately excludes
// alert payloads and arbitrary caller-supplied subject/body fields.
type AccountMail struct {
	ID, Name, Recipient, ResetURL string
	ExpiresAt                     time.Time
}

func (s *Sender) DeliverAccountMail(ctx context.Context, mail AccountMail) (outcome Outcome, err error) {
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
	if !safeIdentity(mail.ID) || !bareEmail(mail.Recipient) || len(mail.ResetURL) > 4096 || !validHTTPAddress(mail.ResetURL, false) || !validText(mail.Name) || utf8.RuneCountInString(mail.Name) > 120 || mail.ExpiresAt.IsZero() {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}, nil
	}
	message, err := s.accountMessage(mail)
	if err != nil {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}, nil
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	return s.deliverMailMessage(deliveryCtx, mail.Recipient, message), nil
}

func (s *Sender) accountMessage(mail AccountMail) ([]byte, error) {
	name := strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
			return ' '
		}
		return ch
	}, mail.Name)
	body := "Hello " + name + ",\r\n\r\nA password reset was requested for your GoodSentry account.\r\n\r\n" +
		"Open this link to choose a new password:\r\n" + mail.ResetURL + "\r\n\r\n" +
		"This link expires at " + mail.ExpiresAt.UTC().Format(time.RFC3339) + " and can be used once.\r\n" +
		"If you did not request this change, ignore this message.\r\n"
	var output bytes.Buffer
	output.WriteString("From: " + s.options.FromEmail + "\r\nTo: " + mail.Recipient + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", "Reset your GoodSentry password") + "\r\n")
	output.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + mail.ID + "@" + s.messageHost() + ">\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	writer := quotedprintable.NewWriter(&output)
	if _, err := writer.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	output.WriteString("\r\n")
	return output.Bytes(), nil
}
