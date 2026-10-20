//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

type invitationMailSink struct {
	mu       sync.Mutex
	messages [][]byte
}

func (s *invitationMailSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, bytes.Clone(p))
	return len(p), nil
}

func (s *invitationMailSink) get(index int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.messages) {
		return nil
	}
	return bytes.Clone(s.messages[index])
}

func TestOrganizationInvitationHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	adminConn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer adminConn.Close(context.Background())
	schema := fmt.Sprintf("goodsentry_invite_http_%d", time.Now().UnixNano())
	if _, err := adminConn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer adminConn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	var mailSink invitationMailSink
	cfg := config.Config{Domain: "http://example.test/good", Prefix: "/good", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}
	sender, err := notify.NewSender(notify.Options{Domain: cfg.Domain, EmailURL: "consolemail://", FromEmail: "accounts@example.test", Timeout: time.Second, Console: &mailSink})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	stopInvitations := jobs.StartInvitations(ctx, db, sender, box, jobs.InvitationOptions{Workers: 1, MaxAttempts: 3, Timeout: time.Second, Domain: cfg.Domain})
	defer stopInvitations()
	srv := httptest.NewServer(NewWithAccountMail(cfg, db, sender, box))
	defer srv.Close()
	request := func(client *http.Client, method, path, body, csrf string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		data := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &data)
		}
		return res.StatusCode, data
	}
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar}
	code, setup := request(owner, "POST", "/api/v1/setup", `{"email":"owner@invite.test","password":"invite-password","name":"Owner","organization":"Invite Org"}`, "")
	if code != 201 {
		t.Fatalf("setup=%d %+v", code, setup)
	}
	org := setup["organizations"].([]any)[0].(map[string]any)["slug"].(string)
	csrf := setup["csrfToken"].(string)
	invites := "/api/v1/organizations/" + org + "/invitations"
	if code, _ := request(owner, "POST", invites, `{"email":"new@invite.test","role":"member"}`, ""); code != 403 {
		t.Fatalf("no CSRF invite=%d", code)
	}
	code, invited := request(owner, "POST", invites, `{"email":"new@invite.test","role":"member"}`, csrf)
	if code != 201 {
		t.Fatalf("invite=%d %+v", code, invited)
	}
	if code, _ := request(owner, "POST", invites, `{"email":"new@invite.test","role":"admin"}`, csrf); code != 409 {
		t.Fatalf("duplicate invite=%d", code)
	}
	deadline := time.Now().Add(10 * time.Second)
	inspectInvitation := func(token string, existing bool) map[string]any {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		var lastCode int
		var last map[string]any
		for time.Now().Before(deadline) {
			lastCode, last = request(http.DefaultClient, "POST", "/api/v1/invitations/inspect", fmt.Sprintf(`{"token":%q}`, token), "")
			if lastCode == 200 && last["existingAccount"] == existing {
				return last
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("inspect=%d %+v", lastCode, last)
		return nil
	}
	for mailSink.get(0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	message, err := mail.ReadMessage(bytes.NewReader(mailSink.get(0)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(message.Body))
	match := regexp.MustCompile(`inviteToken=([0-9a-f]{64})`).FindStringSubmatch(string(body))
	if len(match) != 2 {
		t.Fatalf("invite body=%s", body)
	}
	token := match[1]
	inspectInvitation(token, false)
	newJar, _ := cookiejar.New(nil)
	newcomer := &http.Client{Jar: newJar}
	if code, me := request(newcomer, "POST", "/api/v1/invitations/accept", fmt.Sprintf(`{"token":%q,"name":"New Member","password":"new-invite-password"}`, token), ""); code != 200 || me["user"].(map[string]any)["email"] != "new@invite.test" {
		t.Fatalf("accept new=%d %+v", code, me)
	}
	if code, _ := request(newcomer, "GET", "/api/v1/me", "", ""); code != 200 {
		t.Fatalf("new session=%d", code)
	}
	if code, _ := request(http.DefaultClient, "POST", "/api/v1/invitations/accept", fmt.Sprintf(`{"token":%q,"name":"Replay","password":"replay-password"}`, token), ""); code != 400 {
		t.Fatalf("replay=%d", code)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("existing-password"), bcrypt.MinCost)
	if _, err := fixture.Exec(ctx, `INSERT INTO users(email,password_hash,name) VALUES('existing@invite.test',$1,'Existing')`, hash); err != nil {
		t.Fatal(err)
	}
	if code, _ := request(owner, "POST", invites, `{"email":"existing@invite.test","role":"admin"}`, csrf); code != 201 {
		t.Fatalf("existing invite=%d", code)
	}
	for mailSink.get(1) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	message, err = mail.ReadMessage(bytes.NewReader(mailSink.get(1)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(quotedprintable.NewReader(message.Body))
	match = regexp.MustCompile(`inviteToken=([0-9a-f]{64})`).FindStringSubmatch(string(body))
	if len(match) != 2 {
		t.Fatalf("existing invite body=%s", body)
	}
	existingToken := match[1]
	inspectInvitation(existingToken, true)
	existingJar, _ := cookiejar.New(nil)
	existingClient := &http.Client{Jar: existingJar}
	if code, _ := request(existingClient, "POST", "/api/v1/invitations/accept", fmt.Sprintf(`{"token":%q,"password":"wrong-password"}`, existingToken), ""); code != 401 {
		t.Fatalf("existing wrong password=%d", code)
	}
	if code, me := request(existingClient, "POST", "/api/v1/invitations/accept", fmt.Sprintf(`{"token":%q,"password":"existing-password"}`, existingToken), ""); code != 200 || me["user"].(map[string]any)["email"] != "existing@invite.test" {
		t.Fatalf("existing accept=%d %+v", code, me)
	}
	code, members := request(owner, "GET", "/api/v1/organizations/"+org+"/members", "", "")
	if code != 200 || len(members["data"].([]any)) != 3 {
		t.Fatalf("members=%d %+v", code, members)
	}
}
