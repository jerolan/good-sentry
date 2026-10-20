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

func TestInvitationMailMIMEAndConsoleDelivery(t *testing.T) {
	var console bytes.Buffer
	opts := senderOptions()
	opts.Console = &console
	sender, err := NewSender(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	fixture := InvitationMail{ID: "invite-123", Name: "Taylor", Recipient: "taylor@example.com", Organization: "Northstar", Inviter: "Owner", Role: "member", InviteURL: "https://errors.example/base/?inviteToken=" + strings.Repeat("a", 64), ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	outcome, err := sender.DeliverInvitationMail(context.Background(), fixture)
	if err != nil || outcome.Disposition != Delivered {
		t.Fatalf("delivery=%+v err=%v", outcome, err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(console.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.Get("To") != fixture.Recipient || message.Header.Get("Message-ID") != "<invite-123@errors.example>" || !strings.Contains(message.Header.Get("Subject"), "Northstar") {
		t.Fatalf("headers=%+v", message.Header)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Owner invited you to join Northstar") || !strings.Contains(string(body), fixture.InviteURL) || !strings.Contains(string(body), fixture.ExpiresAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("body=%s", body)
	}
}

func TestInvitationMailRejectsUnsafeInput(t *testing.T) {
	for _, edit := range []func(*InvitationMail){
		func(m *InvitationMail) { m.Role = "superuser" },
		func(m *InvitationMail) { m.Recipient = "Name <a@example.com>" },
		func(m *InvitationMail) { m.InviteURL = "/invite" },
		func(m *InvitationMail) { m.ExpiresAt = time.Time{} },
	} {
		var console bytes.Buffer
		opts := senderOptions()
		opts.Console = &console
		sender, err := NewSender(opts)
		if err != nil {
			t.Fatal(err)
		}
		fixture := InvitationMail{ID: "invite-123", Name: "Taylor", Recipient: "taylor@example.com", Organization: "Northstar", Inviter: "Owner", Role: "member", InviteURL: "https://errors.example/invite?token=valid", ExpiresAt: time.Now().Add(time.Hour)}
		edit(&fixture)
		outcome, err := sender.DeliverInvitationMail(context.Background(), fixture)
		sender.Close()
		if err != nil || outcome.Disposition != Permanent || console.Len() != 0 {
			t.Fatalf("outcome=%+v err=%v console=%q", outcome, err, console.String())
		}
	}
}
