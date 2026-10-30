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
	t.Run("invalid resource and scope", func(t *testing.T) {
		verifier := strings.Repeat("a", 43)
		digest := sha256.Sum256([]byte(verifier))
		form := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"invalid"}, "scope": {"org:read"}, "resource": {cfg.Domain + "/mcp"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "csrf": {f.csrf}, "organization": {f.org.Slug}, "project": {f.project.ID}, "decision": {"approve"}}
		var before int
		if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1`, id).Scan(&before); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ name, scope, resource, problem string }{
			{"unknown scope", "org:read unknown:read", cfg.Domain + "/mcp", "invalid_scope"},
			{"disabled log scope", "org:read log:read", cfg.Domain + "/mcp", "invalid_scope"},
			{"foreign resource", "org:read", cfg.Domain + "/other", "invalid_request"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				form.Set("scope", tc.scope)
				form.Set("resource", tc.resource)
				response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
				callback, err := url.Parse(response.Header.Get("Location"))
				if err != nil || response.StatusCode != http.StatusSeeOther || callback.Query().Get("error") != tc.problem || callback.Query().Get("code") != "" {
					t.Fatalf("invalid authorization status=%d error=%v", response.StatusCode, err)
				}
				var after int
				if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1`, id).Scan(&after); err != nil || after != before {
					t.Fatalf("invalid authorization created code: %v", err)
				}
			})
		}
		form.Set("scope", "org:read")
		form.Set("resource", cfg.Domain+"/mcp")
		for _, tc := range []struct{ name, clientID, redirectURI string }{
			{"unknown client", "unknown-client", redirect},
			{"unregistered redirect", id, redirect + "-other"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				form.Set("client_id", tc.clientID)
				form.Set("redirect_uri", tc.redirectURI)
				response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
				if response.StatusCode != http.StatusBadRequest || response.Header.Get("Location") != "" {
					t.Fatalf("untrusted client or redirect status=%d", response.StatusCode)
				}
				var after int
				if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1`, id).Scan(&after); err != nil || after != before {
					t.Fatalf("untrusted authorization created code: %v", err)
				}
			})
		}
		form.Set("client_id", id)
		form.Set("redirect_uri", redirect)
		response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
		callback, err := url.Parse(response.Header.Get("Location"))
		if err != nil || response.StatusCode != http.StatusSeeOther || callback.Query().Get("code") == "" {
			t.Fatalf("valid authorization status=%d error=%v", response.StatusCode, err)
		}
	})
	t.Run("foreign organization and project consent", func(t *testing.T) {
		var foreignOrg, foreignProject string
		if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('oauth-foreign','OAuth foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
			t.Fatal(err)
		}
		if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'OAuth foreign','oauth-foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
			t.Fatal(err)
		}
		verifier := strings.Repeat("a", 43)
		digest := sha256.Sum256([]byte(verifier))
		form := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"foreign"}, "scope": {"org:read"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "csrf": {f.csrf}, "organization": {"oauth-foreign"}, "project": {foreignProject}, "decision": {"approve"}}
		var beforeCodes, beforeAccess, beforeRefresh int
		counts := func() (int, int, int) {
			t.Helper()
			var codes, access, refresh int
			if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM oauth_authorization_codes WHERE client_id=$1), (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&codes, &access, &refresh); err != nil {
				t.Fatal(err)
			}
			return codes, access, refresh
		}
		beforeCodes, beforeAccess, beforeRefresh = counts()
		reject := func() {
			t.Helper()
			response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
			callback, err := url.Parse(response.Header.Get("Location"))
			if err != nil || response.StatusCode != http.StatusSeeOther || callback.Query().Get("error") != "access_denied" || callback.Query().Get("code") != "" {
				t.Fatalf("forged consent status=%d error=%v", response.StatusCode, err)
			}
			codes, access, refresh := counts()
			if codes != beforeCodes || access != beforeAccess || refresh != beforeRefresh {
				t.Fatal("forged consent created a code or credentials")
			}
		}
		reject() // No membership in the submitted organization.
		if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, foreignOrg); err != nil {
			t.Fatal(err)
		}
		form.Set("organization", f.org.Slug)
		reject() // A project from a different organization, even though the user owns both.
		form.Set("project", f.project.ID)
		form["organization"] = []string{f.org.Slug, "oauth-foreign"}
		response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
		callback, err := url.Parse(response.Header.Get("Location"))
		if err != nil || response.StatusCode != http.StatusSeeOther || callback.Query().Get("error") != "invalid_request" || callback.Query().Get("code") != "" {
			t.Fatalf("ambiguous organization status=%d error=%v", response.StatusCode, err)
		}
		codes, access, refresh := counts()
		if codes != beforeCodes || access != beforeAccess || refresh != beforeRefresh {
			t.Fatal("ambiguous consent created a code or credentials")
		}
		form.Set("organization", f.org.Slug)
		response, _ = oauthForm(t, client, base+"/oauth/authorize", form)
		callback, err = url.Parse(response.Header.Get("Location"))
		if err != nil || response.StatusCode != http.StatusSeeOther || callback.Query().Get("code") == "" {
			t.Fatalf("legitimate consent rejected: status=%d error=%v", response.StatusCode, err)
		}
		tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {callback.Query().Get("code")}, "code_verifier": {verifier}}
		response, _ = oauthForm(t, client, base+"/oauth/token", tokenForm)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("legitimate exchange rejected: %d", response.StatusCode)
		}
	})
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
	t.Run("concurrent refresh", func(t *testing.T) {
		code := authorize(verifier)
		exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
		response, body := oauthForm(t, client, base+"/oauth/token", exchange)
		var initial struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		}
		if response.StatusCode != http.StatusOK || json.Unmarshal(body, &initial) != nil || initial.Access == "" || initial.Refresh == "" {
			t.Fatal("initial OAuth grant failed")
		}
		wrongClient := url.Values{"grant_type": {"refresh_token"}, "client_id": {otherID}, "refresh_token": {initial.Refresh}}
		response, _ = oauthForm(t, client, base+"/oauth/token", wrongClient)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("foreign refresh client status=%d", response.StatusCode)
		}
		var accessBefore, refreshBefore int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessBefore, &refreshBefore); err != nil {
			t.Fatal(err)
		}
		tx, err := f.sql.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		server := &Server{cfg: cfg}
		var lockedID int64
		if err := tx.QueryRow(f.ctx, `SELECT a.id FROM api_tokens a JOIN oauth_refresh_tokens r ON r.access_token_id=a.id WHERE r.secret_hash=$1 FOR UPDATE OF a`, server.hash(initial.Refresh)).Scan(&lockedID); err != nil {
			t.Fatal(err)
		}
		values := url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {initial.Refresh}}
		type result struct {
			status int
			body   []byte
			err    error
		}
		const workers = 8
		results := make(chan result, workers)
		start := make(chan struct{})
		var ready, finished sync.WaitGroup
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
				body, err := io.ReadAll(res.Body)
				res.Body.Close()
				results <- result{status: res.StatusCode, body: body, err: err}
			}()
		}
		ready.Wait()
		close(start)
		defer func() { _ = tx.Rollback(context.Background()); finished.Wait() }()
		deadline, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := tx.Exec(f.ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
				t.Fatal(err)
			}
			var waiting int
			if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT revoked_at FROM api_tokens%FOR UPDATE%'`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting >= 2 {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("concurrent refresh requests did not reach the access-token lock")
			case <-ticker.C:
			}
		}
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		statuses := map[int]int{}
		var rotated struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		}
		for range workers {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			statuses[result.status]++
			if result.status == http.StatusOK && json.Unmarshal(result.body, &rotated) != nil {
				t.Fatal("invalid rotated token response")
			}
		}
		if statuses[http.StatusOK] != 1 || statuses[http.StatusBadRequest] != workers-1 || len(statuses) != 2 {
			t.Fatalf("concurrent refresh statuses=%v", statuses)
		}
		if rotated.Access == "" || rotated.Refresh == "" || rotated.Access == initial.Access || rotated.Refresh == initial.Refresh {
			t.Fatal("refresh did not rotate both credentials")
		}
		var accessAfter, refreshAfter int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessAfter, &refreshAfter); err != nil || accessAfter != accessBefore+1 || refreshAfter != refreshBefore+1 {
			t.Fatalf("refresh token counts=%d/%d to %d/%d: %v", accessBefore, refreshBefore, accessAfter, refreshAfter, err)
		}
		req, err := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+initial.Access)
		response, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("old access survived refresh: %d", response.StatusCode)
		}
		response, _ = oauthForm(t, client, base+"/oauth/token", values)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("refresh replay status=%d", response.StatusCode)
		}
	})
	t.Run("expired refresh", func(t *testing.T) {
		code := authorize(verifier)
		exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
		response, body := oauthForm(t, client, base+"/oauth/token", exchange)
		var initial struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		}
		if response.StatusCode != http.StatusOK || json.Unmarshal(body, &initial) != nil || initial.Access == "" || initial.Refresh == "" {
			t.Fatal("initial OAuth grant failed")
		}
		server := &Server{cfg: cfg}
		result, err := f.sql.Exec(f.ctx, `UPDATE oauth_refresh_tokens SET expires_at=clock_timestamp()-interval '1 second' WHERE secret_hash=$1`, server.hash(initial.Refresh))
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("expire refresh setup: %v", err)
		}
		var accessBefore, refreshBefore int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessBefore, &refreshBefore); err != nil {
			t.Fatal(err)
		}
		response, _ = oauthForm(t, client, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {initial.Refresh}})
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expired refresh accepted: %d", response.StatusCode)
		}
		var accessAfter, refreshAfter int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&accessAfter, &refreshAfter); err != nil || accessAfter != accessBefore || refreshAfter != refreshBefore {
			t.Fatalf("expired refresh changed token counts: %v", err)
		}
		if _, err := f.db.AuthenticateAPIToken(f.ctx, server.hash(initial.Access)); err != nil {
			t.Fatalf("expired refresh revoked still-valid access: %v", err)
		}
	})
	t.Run("membership removal invalidates grants", func(t *testing.T) {
		pendingCode := authorize(verifier)
		issuedCode := authorize(verifier)
		exchange := func(code string) url.Values {
			return url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
		}
		response, body := oauthForm(t, client, base+"/oauth/token", exchange(issuedCode))
		var issued struct {
			Refresh string `json:"refresh_token"`
		}
		if response.StatusCode != http.StatusOK || json.Unmarshal(body, &issued) != nil || issued.Refresh == "" {
			t.Fatal("initial grant failed")
		}
		if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(user_id,organization_id,role) VALUES($1::bigint,$2::bigint,'owner')`, f.owner.ID, f.org.ID); err != nil {
				t.Errorf("restore membership: %v", err)
			}
		})
		var beforeAccess, beforeRefresh int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&beforeAccess, &beforeRefresh); err != nil {
			t.Fatal(err)
		}
		response, _ = oauthForm(t, client, base+"/oauth/token", exchange(pendingCode))
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("code after membership removal status=%d", response.StatusCode)
		}
		response, _ = oauthForm(t, client, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {issued.Refresh}})
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("refresh after membership removal status=%d", response.StatusCode)
		}
		var afterAccess, afterRefresh int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&afterAccess, &afterRefresh); err != nil || afterAccess != beforeAccess || afterRefresh != beforeRefresh {
			t.Fatalf("removed membership created credentials: %v", err)
		}
	})
	t.Run("authentication generation invalidates grants", func(t *testing.T) {
		pendingCode := authorize(verifier)
		issuedCode := authorize(verifier)
		exchange := func(code string) url.Values {
			return url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
		}
		response, body := oauthForm(t, client, base+"/oauth/token", exchange(issuedCode))
		var issued struct {
			Refresh string `json:"refresh_token"`
		}
		if response.StatusCode != http.StatusOK || json.Unmarshal(body, &issued) != nil || issued.Refresh == "" {
			t.Fatal("initial grant failed")
		}
		var beforeAccess, beforeRefresh int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&beforeAccess, &beforeRefresh); err != nil {
			t.Fatal(err)
		}
		if _, err := f.sql.Exec(f.ctx, `UPDATE users SET auth_generation=auth_generation+1 WHERE id=$1::bigint`, f.owner.ID); err != nil {
			t.Fatal(err)
		}
		response, _ = oauthForm(t, client, base+"/oauth/token", exchange(pendingCode))
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("stale authorization code status=%d", response.StatusCode)
		}
		response, _ = oauthForm(t, client, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {issued.Refresh}})
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("stale refresh status=%d", response.StatusCode)
		}
		var afterAccess, afterRefresh int
		if err := f.sql.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM api_tokens WHERE name='OAuth binding client'), (SELECT count(*) FROM oauth_refresh_tokens WHERE client_id=$1)`, id).Scan(&afterAccess, &afterRefresh); err != nil || afterAccess != beforeAccess || afterRefresh != beforeRefresh {
			t.Fatalf("stale grants created credentials: %v", err)
		}
	})
}
