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

type InvitationMail struct {
	ID, Name, Recipient, Organization, Inviter, Role, InviteURL string
	ExpiresAt                                                   time.Time
}

func (s *Sender) DeliverInvitationMail(ctx context.Context, mail InvitationMail) (outcome Outcome, err error) {
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
	if !safeIdentity(mail.ID) || !bareEmail(mail.Recipient) || !validText(mail.Name) || !validText(mail.Organization) || !validText(mail.Inviter) || utf8.RuneCountInString(mail.Name) > 120 || utf8.RuneCountInString(mail.Organization) > 200 || utf8.RuneCountInString(mail.Inviter) > 120 || (mail.Role != "owner" && mail.Role != "admin" && mail.Role != "member") || len(mail.InviteURL) > 4096 || !validHTTPAddress(mail.InviteURL, false) || mail.ExpiresAt.IsZero() {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}, nil
	}
	message, err := s.invitationMessage(mail)
	if err != nil {
		return Outcome{Disposition: Permanent, Reason: "invalid_payload"}, nil
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	return s.deliverMailMessage(deliveryCtx, mail.Recipient, message), nil
}

func safeAccountLine(value string) string {
	return strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
			return ' '
		}
		return ch
	}, value)
}

func (s *Sender) invitationMessage(mail InvitationMail) ([]byte, error) {
	body := "Hello " + safeAccountLine(mail.Name) + ",\r\n\r\n" + safeAccountLine(mail.Inviter) + " invited you to join " + safeAccountLine(mail.Organization) + " in GoodSentry as " + mail.Role + ".\r\n\r\n" +
		"Open this link to accept the invitation:\r\n" + mail.InviteURL + "\r\n\r\n" +
		"This invitation expires at " + mail.ExpiresAt.UTC().Format(time.RFC3339) + " and can be used once.\r\n" +
		"If you were not expecting this invitation, ignore this message.\r\n"
	var output bytes.Buffer
	subject := "Join " + safeAccountLine(mail.Organization) + " in GoodSentry"
	output.WriteString("From: " + s.options.FromEmail + "\r\nTo: " + mail.Recipient + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
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
