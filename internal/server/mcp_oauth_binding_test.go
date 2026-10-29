//go:build integration

package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/config"
)

func TestOAuthCodeExchangeBindingAndVerifierAlphabet(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	cfg := config.Config{MCP: true, Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, f.db))
	defer srv.Close()
	base, redirect := srv.URL+"/good", srv.URL+"/good/oauth/callback"
	client := &http.Client{Jar: f.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	register := func(name string) string {
		t.Helper()
		response, body := oauthJSON(t, client, base+"/oauth/register", `{"client_name":"`+name+`","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("registration status=%d", response.StatusCode)
		}
		var value struct {
			ID string `json:"client_id"`
		}
		if json.Unmarshal(body, &value) != nil || value.ID == "" {
			t.Fatal("registration missing client ID")
		}
		return value.ID
	}
	id, otherID := register("binding client"), register("other binding client")
	authorize := func(verifier string) string {
		t.Helper()
		digest := sha256.Sum256([]byte(verifier))
		form := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"binding"}, "scope": {"org:read"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "csrf": {f.csrf}, "organization": {f.org.Slug}, "project": {f.project.ID}, "decision": {"approve"}}
		response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("authorization status=%d", response.StatusCode)
		}
		callback, err := url.Parse(response.Header.Get("Location"))
		if err != nil || callback.Query().Get("code") == "" {
			t.Fatalf("authorization missing code: %v", err)
		}
		return callback.Query().Get("code")
	}
	verifier := strings.Repeat("a", 43)
	code := authorize(verifier)
	for _, tc := range []struct{ name, client, redirect, verifier string }{
		{"wrong client", otherID, redirect, verifier},
		{"wrong redirect", id, redirect + "-other", verifier},
		{"wrong verifier", id, redirect, strings.Repeat("b", 43)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {tc.client}, "redirect_uri": {tc.redirect}, "code": {code}, "code_verifier": {tc.verifier}}
			response, _ := oauthForm(t, client, base+"/oauth/token", form)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("mismatched grant status=%d", response.StatusCode)
			}
		})
	}
	valid := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
	response, _ := oauthForm(t, client, base+"/oauth/token", valid)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("legitimate code consumed after mismatches: %d", response.StatusCode)
	}
	for _, tc := range []struct{ name, verifier string }{
		{"punctuation", strings.Repeat("a", 42) + "!"},
		{"padding", strings.Repeat("a", 42) + "="},
		{"short", strings.Repeat("a", 42)},
		{"long", strings.Repeat("a", 129)},
		{"non-ASCII", strings.Repeat("a", 41) + "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1 AND consumed_at IS NULL`, id).Scan(&before); err != nil {
				t.Fatal(err)
			}
			invalidCode := authorize(tc.verifier)
			invalidForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {invalidCode}, "code_verifier": {tc.verifier}}
			response, body := oauthForm(t, client, base+"/oauth/token", invalidForm)
			var problem struct {
				Error string `json:"error"`
			}
			if response.StatusCode != http.StatusBadRequest || response.Header.Get("Cache-Control") != "no-store" || json.Unmarshal(body, &problem) != nil || problem.Error != "invalid_grant" {
				t.Fatalf("invalid verifier status=%d error=%q", response.StatusCode, problem.Error)
			}
			var after int
			if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1 AND consumed_at IS NULL`, id).Scan(&after); err != nil || after != before+1 {
				t.Fatalf("invalid verifier consumed a code: %d -> %d: %v", before, after, err)
			}
		})
	}
	maximum := strings.Repeat("-_", 64)
	maxForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {authorize(maximum)}, "code_verifier": {maximum}}
	response, _ = oauthForm(t, client, base+"/oauth/token", maxForm)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid maximum verifier rejected: %d", response.StatusCode)
	}
	t.Run("concurrent single use", func(t *testing.T) {
		var accessBefore, refreshBefore int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessBefore, &refreshBefore); err != nil {
			t.Fatal(err)
		}
		concurrentCode := authorize(verifier)
		tx, err := f.sql.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		server := &Server{cfg: cfg}
		var locked string
		if err := tx.QueryRow(f.ctx, `SELECT code_hash FROM oauth_authorization_codes WHERE code_hash=$1 FOR UPDATE`, server.hash(concurrentCode)).Scan(&locked); err != nil {
			t.Fatal(err)
		}
		values := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {concurrentCode}, "code_verifier": {verifier}}
		type result struct {
			status int
			err    error
		}
		const workers = 8
		results := make(chan result, workers)
		start := make(chan struct{})
		var ready sync.WaitGroup
		var finished sync.WaitGroup
		ready.Add(workers)
		finished.Add(workers)
		for range workers {
			go func() {
				defer finished.Done()
				ready.Done()
				<-start
				req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, base+"/oauth/token", strings.NewReader(values.Encode()))
				if err != nil {
					results <- result{err: err}
					return
				}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					results <- result{err: err}
					return
				}
				_, err = io.Copy(io.Discard, res.Body)
				res.Body.Close()
				results <- result{status: res.StatusCode, err: err}
			}()
		}
		ready.Wait()
		close(start)
		defer func() {
			_ = tx.Rollback(context.Background())
			finished.Wait()
		}()
		deadline, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting int
			if _, err := tx.Exec(f.ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FOR UPDATE OF c%'`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting >= 2 {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("concurrent exchanges did not reach the code lock")
			case <-ticker.C:
			}
		}
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		statuses := map[int]int{}
		for range workers {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			statuses[result.status]++
		}
		if statuses[http.StatusOK] != 1 || statuses[http.StatusBadRequest] != workers-1 || len(statuses) != 2 {
			t.Fatalf("concurrent exchange statuses=%v", statuses)
		}
		var accessAfter, refreshAfter int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessAfter, &refreshAfter); err != nil || accessAfter != accessBefore+1 || refreshAfter != refreshBefore+1 {
			t.Fatalf("concurrent token counts=%d/%d to %d/%d: %v", accessBefore, refreshBefore, accessAfter, refreshAfter, err)
		}
	})
	t.Run("expired code", func(t *testing.T) {
		var accessBefore, refreshBefore int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessBefore, &refreshBefore); err != nil {
			t.Fatal(err)
		}
		expiredCode := authorize(verifier)
		server := &Server{cfg: cfg}
		result, err := f.sql.Exec(f.ctx, `UPDATE oauth_authorization_codes SET created_at=clock_timestamp()-interval '1 hour', expires_at=clock_timestamp()-interval '1 second' WHERE code_hash=$1`, server.hash(expiredCode))
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("expire code setup: %v", err)
		}
		form := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {expiredCode}, "code_verifier": {verifier}}
		res, _ := oauthForm(t, client, base+"/oauth/token", form)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expired code accepted: %d", res.StatusCode)
		}
		var accessAfter, refreshAfter int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessAfter, &refreshAfter); err != nil || accessAfter != accessBefore || refreshAfter != refreshBefore {
			t.Fatalf("expired exchange changed token counts: %v", err)
		}
	})
}
