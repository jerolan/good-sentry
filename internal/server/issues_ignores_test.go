//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// HTTP owns the JSON tokens, session/CSRF and nullable DTO wiring. Store tests
// own mute expiration, unique captures, concurrency and notification delivery.
func TestIssueIgnoreHTTPContract(t *testing.T) {
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
	schema := fmt.Sprintf("goodsentry_issue_ignore_http_%d", time.Now().UnixNano())
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
	hash, err := bcrypt.GenerateFromPassword([]byte("ignore-http-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@ignore.test", hash, "Mute owner", "Mute HTTP")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "Mute transport", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("b", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, db))
	defer srv.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	request := func(t *testing.T, who *http.Client, method, path, body string, headers http.Header, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+cfg.Prefix+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for key, values := range headers {
			req.Header[key] = values
		}
		res, err := who.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
		}
		return raw
	}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(request(t, client, "POST", "/api/v1/login", `{"email":"owner@ignore.test","password":"ignore-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("login failed to return session: %v", err)
	}
	csrf := http.Header{"X-CSRF-Token": {session.CSRFToken}}
	envelope := "{}\n{\"type\":\"event\"}\n" + `{"event_id":"abababababababababababababababab","message":"Mute transport contract","fingerprint":["mute-http"]}`
	request(t, http.DefaultClient, "POST", "/api/"+project.ID+"/envelope/?sentry_version=7&sentry_key="+project.PublicKey, envelope, nil, 200)
	var issues struct{ Data []struct{ ID string } }
	if err := json.Unmarshal(request(t, client, "GET", "/api/v1/organizations/"+org.Slug+"/issues?project="+project.ID, "", nil, 200), &issues); err != nil || len(issues.Data) != 1 {
		t.Fatalf("capture fixture issue: %v %+v", err, issues)
	}
	path := "/api/v1/issues/" + issues.Data[0].ID
	type ignoreDTO struct {
		Minutes, Count *int32
		Until          *time.Time
		Remaining      *int64
	}
	type issueDTO struct {
		Status     string
		Ignore     *ignoreDTO
		AssignedTo *struct{ Type, ID string }
	}
	decode := func(t *testing.T, raw []byte) issueDTO {
		t.Helper()
		var item issueDTO
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("decode issue: %v body=%s", err, raw)
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		condition, present := fields["ignore"]
		if !present {
			t.Fatal("issue DTO omitted nullable ignore")
		}
		if item.Ignore != nil {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(condition, &values); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"minutes", "count", "until", "remaining"} {
				if _, present := values[key]; !present {
					t.Fatalf("conditional ignore omitted nullable %s: %s", key, condition)
				}
			}
			if len(values) != 4 {
				t.Fatalf("conditional ignore fields=%s", condition)
			}
			if item.Ignore.Until != nil {
				_, offset := item.Ignore.Until.Zone()
				if offset != 0 || !item.Ignore.Until.Equal(item.Ignore.Until.Truncate(time.Microsecond)) {
					t.Fatalf("until must be a microsecond UTC instant: %s", item.Ignore.Until.Format(time.RFC3339Nano))
				}
			}
		}
		return item
	}
	patch := func(t *testing.T, body string) issueDTO {
		t.Helper()
		return decode(t, request(t, client, "PATCH", path, body, csrf, 200))
	}
	detail := func(t *testing.T) issueDTO {
		t.Helper()
		var result struct{ Issue json.RawMessage }
		if err := json.Unmarshal(request(t, client, "GET", path, "", nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		return decode(t, result.Issue)
	}
	t.Run("valid conditional mute response", func(t *testing.T) {
		before := time.Now().UTC()
		item := patch(t, `{"status":"ignored","ignore":{"minutes":2,"count":3}}`)
		if item.Status != "ignored" || item.Ignore == nil || item.Ignore.Minutes == nil || *item.Ignore.Minutes != 2 || item.Ignore.Count == nil || *item.Ignore.Count != 3 || item.Ignore.Remaining == nil || *item.Ignore.Remaining != 3 || item.Ignore.Until == nil {
			t.Fatalf("combined mute response=%+v ignore=%+v", item, item.Ignore)
		}
		if item.Ignore.Until.Before(before.Add(2*time.Minute-time.Second)) || item.Ignore.Until.After(time.Now().UTC().Add(2*time.Minute+time.Second)) {
			t.Fatalf("two-minute deadline=%s", item.Ignore.Until)
		}
		if got := detail(t); !reflect.DeepEqual(got.Ignore, item.Ignore) {
			t.Fatalf("detail ignore=%+v want=%+v", got.Ignore, item.Ignore)
		}
		var result struct{ Data []json.RawMessage }
		if err := json.Unmarshal(request(t, client, "GET", "/api/v1/organizations/"+org.Slug+"/issues?project="+project.ID+"&status=all", "", nil, 200), &result); err != nil || len(result.Data) != 1 {
			t.Fatalf("list response: %v count=%d", err, len(result.Data))
		}
		if got := decode(t, result.Data[0]); !reflect.DeepEqual(got.Ignore, item.Ignore) {
			t.Fatalf("list ignore=%+v want=%+v", got.Ignore, item.Ignore)
		}
	})
	t.Run("single conditions nullable fields and integer boundaries", func(t *testing.T) {
		for _, field := range []string{"minutes", "count"} {
			for _, value := range []int32{1, 2147483647} {
				t.Run(fmt.Sprintf("%s=%d", field, value), func(t *testing.T) {
					item := patch(t, fmt.Sprintf(`{"status":"ignored","ignore":{"%s": %d }}`, field, value))
					if item.Ignore == nil {
						t.Fatal("single condition became indefinite")
					}
					condition := item.Ignore
					if field == "minutes" {
						if condition.Minutes == nil || *condition.Minutes != value || condition.Until == nil || condition.Count != nil || condition.Remaining != nil {
							t.Fatalf("minutes condition=%+v", condition)
						}
					} else if condition.Count == nil || *condition.Count != value || condition.Remaining == nil || *condition.Remaining != int64(value) || condition.Minutes != nil || condition.Until != nil {
						t.Fatalf("count condition=%+v", condition)
					}
				})
			}
		}
	})
	t.Run("omission preserves and explicit null clears", func(t *testing.T) {
		original := patch(t, `{"status":"ignored","ignore":{"minutes":3,"count":8}}`)
		assigned := patch(t, fmt.Sprintf(`{"assignedTo":{"type":"user","id":%q}}`, owner.ID))
		if assigned.AssignedTo == nil || assigned.AssignedTo.ID != owner.ID || !reflect.DeepEqual(assigned.Ignore, original.Ignore) {
			t.Fatalf("assignment-only update lost condition: %+v", assigned)
		}
		if same := patch(t, `{"status":"ignored"}`); !reflect.DeepEqual(same.Ignore, original.Ignore) {
			t.Fatalf("omitted ignore replaced condition: %+v", same.Ignore)
		}
		if indefinite := patch(t, `{"status":"ignored","ignore":null}`); indefinite.Status != "ignored" || indefinite.Ignore != nil || indefinite.AssignedTo == nil || indefinite.AssignedTo.ID != owner.ID {
			t.Fatalf("explicit indefinite response=%+v", indefinite)
		}
		patch(t, `{"status":"unresolved"}`)
		if indefinite := patch(t, `{"status":"ignored"}`); indefinite.Ignore != nil {
			t.Fatalf("new mute without condition=%+v", indefinite.Ignore)
		}
	})
	t.Run("strict JSON and compatible status admission", func(t *testing.T) {
		original := patch(t, `{"status":"ignored","ignore":{"count":7},"assignedTo":null}`)
		invalid := []string{
			`{"ignore":{"count":1}}`,
			`{"ignore":null}`,
			`{"status":"resolved","ignore":{"count":1}}`,
			`{"status":"unresolved","ignore":null}`,
			`{"status":null,"ignore":{"count":1}}`,
			`{"status":true,"ignore":{"count":1}}`,
			`{"status":"ignored","ignore":{}}`,
			`{"status":"ignored","ignore":[]}`,
			`{"status":"ignored","ignore":true}`,
			`{"status":"ignored","ignore":"forever"}`,
			`{"status":"ignored","ignore":1}`,
			`{"status":"ignored","ignore":{"count":1,"window":2}}`,
			`{"status":"ignored","ignore":{"count":1,"count":2}}`,
			`{"status":"ignored","ignore":{"minutes":1,"m\u0069nutes":2}}`,
			`{"status":"ignored","ignore":{"count":1},"ignore":null}`,
			`{"status":"ignored","status":"ignored","ignore":{"count":1}}`,
			`{"status":"ignored","ignore":{"count":1},"extra":true}`,
			`{"status":"ignored","ignore":{"count":1},"assignedTo":true}`,
			`{"status":"ignored","ignore":{"count":1},"assignedTo":{"type":"user","id":"+1"}}`,
			`{"status":"ignored","ignore":{"count":1}} {}`,
			"{\"status\":\"ignored\",\"ignore\":{\"count\":1},\"\xff\":0}",
			`{"status":"ignored","ignore":{"count":1,"\u0000":2}}`,
		}
		for _, field := range []string{"minutes", "count"} {
			for _, value := range []string{"null", "0", "-0", "-1", "2147483648", "999999999999999999999999", "1.0", "1.5", "1e0", "1E1", "\"1\"", "true", "false", "[]", "{}", "+1", "01"} {
				invalid = append(invalid, fmt.Sprintf(`{"status":"ignored","ignore":{"%s":%s}}`, field, value))
			}
		}
		for index, body := range invalid {
			t.Run(fmt.Sprintf("reject %02d", index), func(t *testing.T) {
				request(t, client, "PATCH", path, body, csrf, 400)
			})
		}
		if got := detail(t); got.Status != original.Status || got.AssignedTo != nil || !reflect.DeepEqual(got.Ignore, original.Ignore) {
			t.Fatalf("invalid requests changed issue=%+v ignore=%+v", got, got.Ignore)
		}
	})
	t.Run("native session CSRF and target guards", func(t *testing.T) {
		const valid = `{"status":"ignored","ignore":{"count":9},"assignedTo":null}`
		original := patch(t, valid)
		request(t, http.DefaultClient, "PATCH", path, valid, nil, 401)
		request(t, client, "PATCH", path, valid, nil, 403)
		request(t, client, "PATCH", path, valid, http.Header{"X-CSRF-Token": {"wrong"}}, 403)
		request(t, client, "PATCH", path, valid, http.Header{"X-CSRF-Token": {session.CSRFToken}, "Origin": {"https://foreign.example.test"}}, 403)
		request(t, client, "PATCH", "/api/v1/issues/9223372036854775807", valid, csrf, 404)
		request(t, client, "PATCH", path, `{"status":"ignored","ignore":{"count":1},"assignedTo":{"type":"user","id":"9223372036854775807"}}`, csrf, 404)
		if got := detail(t); !reflect.DeepEqual(got.Ignore, original.Ignore) || got.Status != "ignored" || got.AssignedTo != nil {
			t.Fatalf("denied request changed issue=%+v ignore=%+v", got, got.Ignore)
		}
	})
}
