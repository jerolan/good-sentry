package notify

import (
	"bytes"
	"context"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestAccountMailMIMEAndConsoleDelivery(t *testing.T) {
	var console bytes.Buffer
	opts := senderOptions()
	opts.Console = &console
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	messageFixture := AccountMail{ID: "reset-123", Name: "Owner", Recipient: "owner@example.com", ResetURL: "https://errors.example/base/?resetToken=" + strings.Repeat("a", 64), ExpiresAt: time.Now().Add(30 * time.Minute)}
	outcome, err := sender.DeliverAccountMail(context.Background(), messageFixture)
	if err != nil || outcome.Disposition != Delivered || outcome.Reason != "accepted" {
		t.Fatalf("delivery=%+v err=%v", outcome, err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(console.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.Get("To") != messageFixture.Recipient || message.Header.Get("Message-ID") != "<reset-123@errors.example>" || !strings.Contains(message.Header.Get("Subject"), "Reset your GoodSentry password") {
		t.Fatalf("headers=%+v", message.Header)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Hello Owner") || !strings.Contains(string(body), messageFixture.ResetURL) || !strings.Contains(string(body), "can be used once") || !strings.Contains(string(body), messageFixture.ExpiresAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("body=%s", body)
	}
}

func TestAccountMailRejectsUnsafeInput(t *testing.T) {
	for _, mutate := range []func(*AccountMail){
		func(m *AccountMail) { m.ID = "bad id" },
		func(m *AccountMail) { m.Recipient = "Owner <owner@example.com>" },
		func(m *AccountMail) { m.Recipient = "owner@example.com\r\nBcc: victim@example.com" },
		func(m *AccountMail) { m.ResetURL = "/reset" },
		func(m *AccountMail) { m.ResetURL = "https://user:secret@example.com/reset" },
		func(m *AccountMail) { m.ExpiresAt = time.Time{} },
		func(m *AccountMail) { m.Name = string([]byte{0xff}) },
	} {
		var console bytes.Buffer
		opts := senderOptions()
		opts.Console = &console
		sender, err := NewSender(opts)
		if err != nil {
			t.Fatal(err)
		}
		messageFixture := AccountMail{ID: "reset-123", Name: "Owner", Recipient: "owner@example.com", ResetURL: "https://errors.example/reset?token=valid", ExpiresAt: time.Now().Add(time.Hour)}
		mutate(&messageFixture)
		outcome, err := sender.DeliverAccountMail(context.Background(), messageFixture)
		sender.Close()
		if err != nil || outcome.Disposition != Permanent || outcome.Reason != "invalid_payload" || console.Len() != 0 {
			t.Fatalf("unsafe message=%+v err=%v console=%q", outcome, err, console.String())
		}
	}
}
