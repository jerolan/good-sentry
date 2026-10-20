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
	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

type lockedMailBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedMailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedMailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestPasswordRecoveryHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	schema := fmt.Sprintf("goodsentry_recovery_http_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
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
	var console lockedMailBuffer
	cfg := config.Config{Domain: "http://example.test/good", Prefix: "/good", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}
	sender, err := notify.NewSender(notify.Options{Domain: cfg.Domain, EmailURL: "consolemail://", FromEmail: "accounts@example.test", Timeout: time.Second, Console: &console})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	stopMail := jobs.StartAccountMail(ctx, db, sender, box, jobs.AccountMailOptions{Workers: 1, MaxAttempts: 3, Timeout: time.Second, Domain: cfg.Domain})
	defer stopMail()
	srv := httptest.NewServer(NewWithAccountMail(cfg, db, sender, box))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	call := func(method, path, body, csrf string, target *http.Client) (int, map[string]any, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if target == nil {
			target = client
		}
		res, err := target.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		data := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &data)
		}
		return res.StatusCode, data, raw
	}
	if code, setup, _ := call("POST", "/api/v1/setup", `{"email":"owner@recovery.test","password":"old-recovery-password","name":"Owner","organization":"Recovery"}`, "", client); code != 201 || setup["features"].(map[string]any)["passwordRecovery"] != true {
		t.Fatalf("setup=%d", code)
	}
	if code, _, _ := call("POST", "/api/v1/account/password/reset/request", `{"email":"missing@recovery.test"}`, "", http.DefaultClient); code != 202 {
		t.Fatalf("unknown email=%d", code)
	}
	var rows int
	if err := fixture.QueryRow(ctx, `SELECT count(*) FROM account_password_resets`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("unknown email rows=%d err=%v", rows, err)
	}
	if code, _, _ := call("POST", "/api/v1/account/password/reset/request", `{"email":"owner@recovery.test"}`, "", http.DefaultClient); code != 202 {
		t.Fatalf("known email=%d", code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for console.String() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	message, err := mail.ReadMessage(strings.NewReader(console.String()))
	if err != nil {
		t.Fatalf("console mail: %v\n%s", err, console.String())
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`resetToken=([0-9a-f]{64})`).FindStringSubmatch(string(body))
	if len(match) != 2 || !strings.Contains(string(body), cfg.Domain) {
		t.Fatalf("reset link body=%s", body)
	}
	resetToken := match[1]
	if code, _, _ := call("POST", "/api/v1/account/password/reset/request", `{"email":"owner@recovery.test"}`, "", http.DefaultClient); code != 202 {
		t.Fatalf("rate-limited request=%d", code)
	}
	if err := fixture.QueryRow(ctx, `SELECT count(*) FROM account_password_resets`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("request floor rows=%d err=%v", rows, err)
	}
	deliveryDeadline := time.Now().Add(10 * time.Second)
	for {
		var status string
		if err := fixture.QueryRow(ctx, `SELECT status FROM account_password_resets LIMIT 1`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "sent" {
			break
		}
		if time.Now().After(deliveryDeadline) {
			t.Fatalf("reset delivery did not reach sent: %s", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code, _, _ := call("POST", "/api/v1/account/password/reset", fmt.Sprintf(`{"token":%q,"newPassword":"new-recovery-password"}`, resetToken), "", http.DefaultClient); code != 204 {
		t.Fatalf("reset=%d", code)
	}
	if code, _, _ := call("GET", "/api/v1/me", "", "", client); code != 401 {
		t.Fatalf("old session after reset=%d", code)
	}
	if code, _, _ := call("POST", "/api/v1/login", `{"email":"owner@recovery.test","password":"old-recovery-password"}`, "", http.DefaultClient); code != 401 {
		t.Fatalf("old password after reset=%d", code)
	}
	if code, _, _ := call("POST", "/api/v1/login", `{"email":"owner@recovery.test","password":"new-recovery-password"}`, "", http.DefaultClient); code != 200 {
		t.Fatalf("new password after reset=%d", code)
	}
	if code, _, _ := call("POST", "/api/v1/account/password/reset", fmt.Sprintf(`{"token":%q,"newPassword":"replay-recovery-password"}`, resetToken), "", http.DefaultClient); code != 400 {
		t.Fatalf("replayed reset=%d", code)
	}
}
