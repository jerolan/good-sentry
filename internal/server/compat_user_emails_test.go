//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

func newUserEmailServer(t *testing.T, f *resolutionHTTPFixture) (*httptest.Server, *http.Client, string, *bytes.Buffer) {
	t.Helper()
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	console := &bytes.Buffer{}
	sender, err := notify.NewSender(notify.Options{Domain: cfg.Domain, EmailURL: "consolemail://", FromEmail: "accounts@example.test", Timeout: time.Second, Console: console})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sender.Close)
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewWithAccountMail(cfg, f.db, sender, box))
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "POST", "/api/v1/login", `{"email":"owner@resolution.test","password":"resolution-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("email fixture login: %+v %v", session, err)
	}
	return server, client, session.CSRFToken, console
}

func emailBody(t *testing.T, console *bytes.Buffer) string {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(console.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestCompatibleUserEmailsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	server, client, csrf, console := newUserEmailServer(t, f)
	emailsPath := "/api/0/users/me/emails/"
	csrfHeader := http.Header{"X-CSRF-Token": {csrf}}
	var emails []store.CompatibleUserEmail
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "GET", emailsPath, "", nil, 200), &emails); err != nil || len(emails) != 1 || !emails[0].IsPrimary || !emails[0].IsVerified {
		t.Fatalf("initial emails: %+v %v", emails, err)
	}
	var created store.CompatibleUserEmail
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "POST", emailsPath, `{"email":"secondary@resolution.test"}`, csrfHeader, 201), &created); err != nil || created.Email != "secondary@resolution.test" || created.IsPrimary || created.IsVerified {
		t.Fatalf("create secondary email: %+v %v", created, err)
	}
	for _, body := range []string{`{}`, `{"email":"bad"}`, `{"email":"secondary@resolution.test","unknown":true}`} {
		mailCall(t, f.ctx, client, server, "POST", emailsPath, body, csrfHeader, 400)
	}
	body := emailBody(t, console)
	marker := "token="
	index := strings.Index(body, marker)
	if index < 0 || len(body) < index+len(marker)+64 {
		t.Fatalf("verification token absent from email: %q", body)
	}
	token := body[index+len(marker) : index+len(marker)+64]
	noRedirect := *server.Client()
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	mailCall(t, f.ctx, &noRedirect, server, "GET", "/account/emails/confirm?token="+token, "", nil, http.StatusSeeOther)
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "GET", emailsPath, "", nil, 200), &emails); err != nil || len(emails) != 2 || emails[1].Email != "secondary@resolution.test" || !emails[1].IsVerified {
		t.Fatalf("verified email list: %+v %v", emails, err)
	}
	mailCall(t, f.ctx, client, server, "POST", emailsPath, `{"email":"secondary@resolution.test"}`, csrfHeader, 400)
	login, _ := cookiejar.New(nil)
	secondaryClient := &http.Client{Jar: login}
	mailCall(t, f.ctx, secondaryClient, server, "POST", "/api/v1/login", `{"email":"secondary@resolution.test","password":"resolution-http-password"}`, nil, 200)
	var primary store.CompatibleUserEmail
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "PUT", emailsPath, `{"email":"secondary@resolution.test"}`, csrfHeader, 200), &primary); err != nil || !primary.IsPrimary || primary.Email != "secondary@resolution.test" {
		t.Fatalf("primary update: %+v %v", primary, err)
	}
	mailCall(t, f.ctx, client, server, "DELETE", emailsPath, `{"email":"secondary@resolution.test"}`, csrfHeader, 400)
	mailCall(t, f.ctx, client, server, "DELETE", emailsPath, `{"email":"owner@resolution.test"}`, csrfHeader, 204)
	oldLogin, _ := cookiejar.New(nil)
	mailCall(t, f.ctx, &http.Client{Jar: oldLogin}, server, "POST", "/api/v1/login", `{"email":"owner@resolution.test","password":"resolution-http-password"}`, nil, 401)
}
