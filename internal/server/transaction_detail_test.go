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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// HTTP owns authenticated occurrence routing, canonical wire parameters and
// capture-to-aggregate-to-detail access. Existing storage tests own paging,
// timestamp normalization and trace selection; they cannot detect these HTTP
// regressions. SQL below only arranges memberships and discovers fixture IDs.
func TestTransactionOccurrenceHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("goodsentry_transaction_http_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(ctx)
	password := "occurrence-long-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@occurrence.test", hash, "Owner", "Occurrence HTTP")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "HTTP checkout", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	var member, foreignOrg string
	if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES('member@occurrence.test',$1,'Actual member') RETURNING id::text", hash).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-occurrences','Foreign occurrences') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, owner.ID); err != nil {
		t.Fatal(err)
	}
	otherProject, err := db.CreateProject(ctx, owner.ID, "foreign-occurrences", "Other checkout", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(config.Config{Domain: "http://example.test", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20, Logs: true}, db))
	defer srv.Close()
	request := func(client *http.Client, method, path, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("non-JSON response %s %s: %d %s", method, path, res.StatusCode, raw)
		}
		return res.StatusCode, raw
	}
	api := "/api/v1/organizations/" + org.Slug + "/transactions"
	// This assertion fails with 404 while the detail route is unregistered.
	if code, raw := request(http.DefaultClient, "GET", api+"/event/1", ""); code != 401 {
		t.Fatalf("anonymous occurrence route=%d want=401 body=%s", code, raw)
	}
	login := func(email string) *http.Client {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		if code, raw := request(client, "POST", "/api/v1/login", string(body)); code != 200 {
			t.Fatalf("login=%d %s", code, raw)
		}
		return client
	}
	ownerClient, memberClient := login(owner.Email), login("member@occurrence.test")
	capture := func(p store.Project, kind, body string) {
		t.Helper()
		header := `{"type":"` + kind + `"}`
		if kind == "span" {
			header = `{"type":"span","item_count":2,"content_type":"application/vnd.sentry.items.span.v2+json"}`
		}
		endpoint := "/api/" + p.ID + "/envelope/?sentry_version=7&sentry_key=" + p.PublicKey
		if code, raw := request(http.DefaultClient, "POST", endpoint, "{}\n"+header+"\n"+body); code != 200 {
			t.Fatalf("capture %s=%d %s", kind, code, raw)
		}
	}
	const legacy = `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1","type":"transaction","transaction":"Legacy checkout","start_timestamp":1577836800,"timestamp":1577836802,"environment":"staging","release":"checkout@1","contexts":{"trace":{"trace_id":"11111111111111111111111111111111","span_id":"2222222222222222","parent_span_id":"9999999999999999","op":"http.server","status":"ok"}},"extra":{"receipt":9223372036854775807},"spans":[{"span_id":"3333333333333333","parent_span_id":"2222222222222222","description":"Load cart","op":"db.query","start_timestamp":1577836800.25,"timestamp":1577836800.5,"custom_context":"retained"}]}`
	const streamed = `{"version":2,"items":[{"trace_id":"44444444444444444444444444444444","span_id":"5555555555555555","parent_span_id":"8888888888888888","name":"Stream checkout","status":"ok","is_segment":true,"start_timestamp":1577836800,"end_timestamp":1577836801,"attributes":{"sentry.op":{"type":"string","value":"http.server"},"sentry.environment":{"type":"string","value":"staging"},"sentry.release":{"type":"string","value":"checkout@2"},"receipt":{"type":"integer","value":9223372036854775807}}},{"trace_id":"44444444444444444444444444444444","span_id":"6666666666666666","parent_span_id":"5555555555555555","name":"Pay order","status":"ok","is_segment":false,"start_timestamp":1577836800.1,"end_timestamp":1577836800.5,"attributes":{"sentry.op":{"type":"string","value":"http.client"}}}]}`
	capture(project, "transaction", legacy)
	capture(project, "span", streamed)
	capture(project, "event", `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2","message":"A real nontransaction error"}`)
	capture(otherProject, "transaction", strings.Replace(legacy, "Legacy checkout", "Foreign checkout", 1))
	var errorRowID string
	if err := fixture.QueryRow(ctx, "SELECT id::text FROM events WHERE project_id::text=$1 AND event_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2'", project.ID).Scan(&errorRowID); err != nil {
		t.Fatalf("nontransaction fixture identity: %v", err)
	}
	list := func(client *http.Client, path string) map[string]store.TransactionReference {
		t.Helper()
		code, raw := request(client, "GET", path, "")
		if code != 200 {
			t.Fatalf("aggregate=%d %s", code, raw)
		}
		var result struct {
			Data []struct {
				Transaction   string                     `json:"transaction"`
				LatestTraceID string                     `json:"latestTraceId"`
				LatestSample  store.TransactionReference `json:"latestSample"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		refs := map[string]store.TransactionReference{}
		for _, group := range result.Data {
			if group.LatestSample.ID == "" || group.LatestSample.ProjectID == "" {
				t.Fatalf("aggregate lost concrete sample: %s", raw)
			}
			refs[group.Transaction] = group.LatestSample
		}
		return refs
	}
	window := "?start=2020-01-01T00:00:00Z&end=2020-01-01T00:00:03Z"
	refs := list(ownerClient, api+window)
	if len(refs) != 2 {
		t.Fatalf("expected both captured formats: %+v", refs)
	}
	path := func(ref store.TransactionReference) string { return api + "/" + ref.Source + "/" + ref.ID }
	var childRowID string
	for _, tc := range []struct {
		name, source, scope, trace string
		duration                   float64
		spanCount                  int64
	}{
		{"Legacy checkout", "event", "transaction", "11111111111111111111111111111111", 2000, 1},
		{"Stream checkout", "span", "projectTrace", "44444444444444444444444444444444", 1000, 2},
	} {
		t.Run(tc.source+" capture to detail", func(t *testing.T) {
			ref, ok := refs[tc.name]
			if !ok || ref.Source != tc.source || ref.ProjectID != project.ID {
				t.Fatalf("wrong aggregate occurrence: %+v", ref)
			}
			code, raw := request(ownerClient, "GET", path(ref)+"?page=1&start=2010-01-01T00:00:00Z&end=2010-01-01T00:00:01Z", "")
			if code != 200 {
				t.Fatalf("detail=%d %s", code, raw)
			}
			var detail store.TransactionDetail
			if err := json.Unmarshal(raw, &detail); err != nil {
				t.Fatal(err)
			}
			if detail.Sample != ref || detail.ProjectName != project.Name || detail.Name != tc.name || detail.TraceID != tc.trace || detail.Op != "http.server" || detail.Duration != tc.duration || detail.SpanScope != tc.scope || detail.SpanCount != tc.spanCount || len(detail.Spans) != int(tc.spanCount) || detail.Page != 1 || detail.PageSize != 50 || detail.HasMore {
				t.Fatalf("wire detail lost captured occurrence/context: %s", raw)
			}
			if !bytes.Contains(detail.Data, []byte("9223372036854775807")) {
				t.Fatalf("HTTP lost original large integer: %s", detail.Data)
			}
			for _, span := range detail.Spans {
				if span.SpanID == "6666666666666666" {
					childRowID = span.ID
					if span.ParentSpanID != "5555555555555555" {
						t.Fatalf("HTTP lost explicit parent: %s", raw)
					}
				}
			}
			code, raw = request(memberClient, "GET", path(ref), "")
			if code != 200 {
				t.Fatalf("actual member read=%d %s", code, raw)
			}
			var memberDetail store.TransactionDetail
			if err := json.Unmarshal(raw, &memberDetail); err != nil || memberDetail.Sample != ref || memberDetail.Page != 1 {
				t.Fatalf("member/default page response: %s error=%v", raw, err)
			}
		})
	}
	legacyRef := refs["Legacy checkout"]
	if code, raw := request(ownerClient, "GET", path(legacyRef)+"?page=1000000", ""); code != 200 {
		t.Fatalf("valid maximum page rejected=%d %s", code, raw)
	}
	for _, source := range []string{"Event", "SPAN", "transaction", "unknown"} {
		t.Run("invalid source "+source, func(t *testing.T) {
			if code, raw := request(ownerClient, "GET", api+"/"+source+"/"+legacyRef.ID, ""); code != 400 {
				t.Fatalf("invalid source=%d %s", code, raw)
			}
		})
	}
	for _, id := range []string{"0", "-1", "+1", "01", "9223372036854775808", "abc", "1.0"} {
		t.Run("invalid id "+id, func(t *testing.T) {
			if code, raw := request(ownerClient, "GET", api+"/event/"+url.PathEscape(id), ""); code != 400 {
				t.Fatalf("invalid id=%d %s", code, raw)
			}
		})
	}
	for _, page := range []string{"", "0", "-1", "%2B1", "01", "1000001", "9223372036854775808", "1.0"} {
		t.Run("invalid page "+page, func(t *testing.T) {
			if code, raw := request(ownerClient, "GET", path(legacyRef)+"?page="+page, ""); code != 400 {
				t.Fatalf("invalid page=%d %s", code, raw)
			}
		})
	}
	if childRowID == "" {
		t.Fatal("captured nonsegment fixture not exposed in trace detail")
	}
	foreignRefs := list(ownerClient, "/api/v1/organizations/foreign-occurrences/transactions"+window)
	foreignRef, ok := foreignRefs["Foreign checkout"]
	if !ok {
		t.Fatalf("foreign occurrence fixture missing: %+v", foreignRefs)
	}
	for name, detailPath := range map[string]string{
		"absent event":                     api + "/event/9223372036854775807",
		"absent span":                      api + "/span/9223372036854775807",
		"existing nontransaction":          api + "/event/" + errorRowID,
		"existing nonsegment":              api + "/span/" + childRowID,
		"foreign occurrence in member org": api + "/" + foreignRef.Source + "/" + foreignRef.ID,
		"member lacks foreign org":         "/api/v1/organizations/foreign-occurrences/transactions/" + foreignRef.Source + "/" + foreignRef.ID,
	} {
		t.Run(name, func(t *testing.T) {
			if code, raw := request(memberClient, "GET", detailPath, ""); code != 404 {
				t.Fatalf("expected hidden/ineligible occurrence=%d %s", code, raw)
			}
		})
	}
}
