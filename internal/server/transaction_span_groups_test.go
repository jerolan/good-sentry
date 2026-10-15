//go:build integration

package server

import (
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

// Storage tests own normalization, attribution and percentile math. This test
// owns the independent HTTP contract: capture-to-group access, wire validation,
// membership isolation and retryable historical indexing without partial data.
func TestTransactionSpanGroupsHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	schema := fmt.Sprintf("goodsentry_span_groups_http_%d", time.Now().UnixNano())
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
	const password = "span-groups-long-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@span-groups.test", hash, "Owner", "Span groups HTTP")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "HTTP checkout", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(config.Config{Domain: "http://example.test", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}, db))
	defer srv.Close()
	request := func(client *http.Client, method, path, body string) (int, http.Header, []byte) {
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
			t.Fatalf("non-JSON %s %s: %d %s", method, path, res.StatusCode, raw)
		}
		return res.StatusCode, res.Header, raw
	}
	api := "/api/v1/organizations/" + org.Slug + "/transactions"
	if code, _, raw := request(http.DefaultClient, "GET", api+"/event/1/span-groups", ""); code != 401 {
		t.Fatalf("anonymous span-groups route=%d want=401 body=%s", code, raw)
	}
	var member, foreignOrg string
	if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES('member@span-groups.test',$1,'Actual member') RETURNING id::text", hash).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-span-groups','Foreign span groups') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, owner.ID); err != nil {
		t.Fatal(err)
	}
	otherProject, err := db.CreateProject(ctx, owner.ID, "foreign-span-groups", "Foreign checkout", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string) *http.Client {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		if code, _, raw := request(client, "POST", "/api/v1/login", string(body)); code != 200 {
			t.Fatalf("login=%d %s", code, raw)
		}
		return client
	}
	ownerClient, memberClient := login(owner.Email), login("member@span-groups.test")
	capture := func(p store.Project, kind, body string) {
		t.Helper()
		header := `{"type":"` + kind + `"}`
		if kind == "span" {
			header = `{"type":"span","item_count":2,"content_type":"application/vnd.sentry.items.span.v2+json"}`
		}
		if code, _, raw := request(http.DefaultClient, "POST", "/api/"+p.ID+"/envelope/?sentry_version=7&sentry_key="+p.PublicKey, "{}\n"+header+"\n"+body); code != 200 {
			t.Fatalf("capture %s=%d %s", kind, code, raw)
		}
	}
	const legacy = `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1","type":"transaction","transaction":"Legacy checkout","start_timestamp":1577836800,"timestamp":1577836802,"contexts":{"trace":{"trace_id":"11111111111111111111111111111111","span_id":"2222222222222222","op":"http.server"}},"spans":[{"span_id":"3333333333333333","parent_span_id":"2222222222222222","description":"Load cart","op":"db.query","start_timestamp":1577836800.25,"timestamp":1577836800.5},{"op":"unknown","description":"No clock"}]}`
	const streamed = `{"version":2,"items":[{"trace_id":"44444444444444444444444444444444","span_id":"5555555555555555","name":"Stream checkout","status":"ok","is_segment":true,"start_timestamp":1577836800,"end_timestamp":1577836801,"attributes":{"sentry.op":{"type":"string","value":"http.server"},"sentry.segment.id":{"type":"string","value":"5555555555555555"}}},{"trace_id":"44444444444444444444444444444444","span_id":"6666666666666666","parent_span_id":"5555555555555555","name":"Pay order","status":"ok","is_segment":false,"start_timestamp":1577836800.1,"end_timestamp":1577836800.5,"attributes":{"sentry.op":{"type":"string","value":"http.client"},"sentry.segment.id":{"type":"string","value":"5555555555555555"}}} ]}`
	capture(project, "transaction", legacy)
	capture(project, "span", streamed)
	capture(project, "event", `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2","message":"A nontransaction error"}`)
	capture(otherProject, "transaction", strings.Replace(legacy, "Legacy checkout", "Foreign checkout", 1))
	const window = "start=2020-01-01T00:00:00Z&end=2020-01-01T00:00:03Z"
	list := func(path string) map[string]store.TransactionReference {
		t.Helper()
		code, _, raw := request(ownerClient, "GET", path+"?"+window, "")
		if code != 200 {
			t.Fatalf("aggregate=%d %s", code, raw)
		}
		var wire struct {
			Data []struct {
				Name   string                     `json:"transaction"`
				Sample store.TransactionReference `json:"latestSample"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		refs := make(map[string]store.TransactionReference)
		for _, row := range wire.Data {
			refs[row.Name] = row.Sample
		}
		return refs
	}
	refs := list(api)
	if len(refs) != 2 {
		t.Fatalf("captured aggregate references=%+v", refs)
	}
	path := func(ref store.TransactionReference) string {
		return api + "/" + ref.Source + "/" + ref.ID + "/span-groups"
	}
	type groupResponse struct {
		Sample                      store.TransactionReference `json:"sample"`
		Transaction                 string                     `json:"transaction"`
		Op                          string                     `json:"op"`
		TransactionCount, SpanCount int64
		TimedSpanCount, GroupCount  int64
		Page, PageSize              int
		HasMore                     bool
		Data                        []map[string]json.RawMessage
	}
	read := func(client *http.Client, target string) groupResponse {
		t.Helper()
		code, _, raw := request(client, "GET", target, "")
		if code != 200 {
			t.Fatalf("span groups=%d %s", code, raw)
		}
		var wire groupResponse
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		return wire
	}
	for _, tc := range []struct {
		name, source, childOp, description string
		spanCount, groups                  int64
	}{
		{"Legacy checkout", "event", "db.query", "Load cart", 2, 2},
		{"Stream checkout", "span", "http.client", "Pay order", 1, 1},
	} {
		t.Run(tc.source+" capture to operation groups", func(t *testing.T) {
			ref := refs[tc.name]
			if ref.ID == "" || ref.ProjectID != project.ID || ref.Source != tc.source {
				t.Fatalf("wrong aggregate sample=%+v", ref)
			}
			wire := read(ownerClient, path(ref)+"?"+window+"&project="+project.ID+"&page=1")
			if wire.Sample != ref || wire.Transaction != tc.name || wire.Op != "http.server" || wire.TransactionCount != 1 || wire.SpanCount != tc.spanCount || wire.TimedSpanCount != 1 || wire.GroupCount != tc.groups || wire.Page != 1 || wire.PageSize != 50 || wire.HasMore || len(wire.Data) != int(tc.groups) {
				t.Fatalf("wire metadata=%+v", wire)
			}
			row := wire.Data[0]
			if string(row["op"]) != fmt.Sprintf("%q", tc.childOp) || string(row["description"]) != fmt.Sprintf("%q", tc.description) || string(row["count"]) != "1" || string(row["timedCount"]) != "1" {
				t.Fatalf("lost captured operation group=%s", row)
			}
			for _, key := range []string{"avgDuration", "p95Duration", "totalTime"} {
				var duration float64
				if len(row[key]) == 0 || json.Unmarshal(row[key], &duration) != nil || duration <= 0 {
					t.Fatalf("missing millisecond %s: %s", key, row)
				}
				if tc.source == "event" && string(wire.Data[1][key]) != "null" {
					t.Fatalf("unknown duration must emit explicit null for %s: %s", key, wire.Data[1])
				}
			}
			if got := read(memberClient, path(ref)+"?"+window+"&project="); got.Sample != ref || got.TransactionCount != 1 || got.SpanCount != tc.spanCount {
				t.Fatalf("actual member/empty project response=%+v", got)
			}
		})
	}
	legacyRef := refs["Legacy checkout"]
	for name, query := range map[string]string{
		"default last 24 hours":   "",
		"earlier explicit window": "?start=2019-01-01T00:00:00Z&end=2019-01-01T00:00:01Z",
		"different project":       "?" + window + "&project=" + otherProject.ID,
	} {
		t.Run(name, func(t *testing.T) {
			wire := read(ownerClient, path(legacyRef)+query)
			if wire.Sample != legacyRef || wire.Transaction != "Legacy checkout" || wire.TransactionCount != 0 || wire.SpanCount != 0 || wire.TimedSpanCount != 0 || wire.GroupCount != 0 || wire.Data == nil || len(wire.Data) != 0 || wire.HasMore {
				t.Fatalf("filtered empty group lost immutable sample or counts=%+v", wire)
			}
		})
	}
	if wire := read(ownerClient, path(legacyRef)+"?"+window+"&page=1000000"); wire.Page != 1000000 || wire.TransactionCount != 1 || wire.GroupCount != 2 || wire.Data == nil || len(wire.Data) != 0 || wire.HasMore {
		t.Fatalf("valid maximum page=%+v", wire)
	}
	for _, source := range []string{"Event", "SPAN", "transaction"} {
		if code, _, raw := request(ownerClient, "GET", api+"/"+source+"/"+legacyRef.ID+"/span-groups", ""); code != 400 {
			t.Fatalf("invalid source %q=%d %s", source, code, raw)
		}
	}
	for _, value := range []string{"0", "-1", "+1", "01", "9223372036854775808", "abc", "1.0"} {
		if code, _, raw := request(ownerClient, "GET", api+"/event/"+url.PathEscape(value)+"/span-groups", ""); code != 400 {
			t.Fatalf("invalid occurrence ID %q=%d %s", value, code, raw)
		}
	}
	for key, values := range map[string][]string{
		"page":    {"", "0", "-1", "+1", "01", "1000001", "9223372036854775808", "1.0"},
		"project": {"0", "-1", "+1", "01", "9223372036854775808", "abc", "1.0"},
	} {
		for _, value := range values {
			if code, _, raw := request(ownerClient, "GET", path(legacyRef)+"?"+key+"="+url.QueryEscape(value), ""); code != 400 {
				t.Fatalf("invalid %s %q=%d %s", key, value, code, raw)
			}
		}
	}
	for _, query := range []string{
		"start=2020-01-01T00:00:00Z", "end=2020-01-01T00:00:03Z", "start=&end=",
		"start=2020-01-01&end=2020-01-02", "start=2020-01-01T00:00:00&end=2020-01-01T00:00:03",
		"start=2020-01-01T00:00:03Z&end=2020-01-01T00:00:03Z",
		"start=2020-01-01T00:00:03Z&end=2020-01-01T00:00:00Z",
		"start=2020-01-01T00:00:00Z&end=2022-01-01T00:00:00Z",
	} {
		if code, _, raw := request(ownerClient, "GET", path(legacyRef)+"?"+query, ""); code != 400 {
			t.Fatalf("invalid time window %q=%d %s", query, code, raw)
		}
	}
	var errorID, childID string
	if err := fixture.QueryRow(ctx, "SELECT id::text FROM events WHERE project_id=$1 AND event_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2'", project.ID).Scan(&errorID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRow(ctx, "SELECT id::text FROM spans WHERE project_id=$1 AND span_id='6666666666666666'", project.ID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	foreignRef := list("/api/v1/organizations/foreign-span-groups/transactions")["Foreign checkout"]
	if foreignRef.ID == "" {
		t.Fatal("foreign capture fixture missing")
	}
	for name, target := range map[string]string{
		"absent event":       api + "/event/9223372036854775807/span-groups",
		"absent span":        api + "/span/9223372036854775807/span-groups",
		"nontransaction":     api + "/event/" + errorID + "/span-groups",
		"nonsegment":         api + "/span/" + childID + "/span-groups",
		"foreign occurrence": path(foreignRef),
		"nonmember":          "/api/v1/organizations/foreign-span-groups/transactions/" + foreignRef.Source + "/" + foreignRef.ID + "/span-groups",
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, raw := request(memberClient, "GET", target, ""); code != 404 {
				t.Fatalf("hidden/ineligible occurrence=%d %s", code, raw)
			}
		})
	}
	// Simulate an accepted historical event before the in-process projection
	// worker runs. This fixture changes derived state, never the original event.
	if _, err := fixture.Exec(ctx, "UPDATE events SET span_projection_version=0 WHERE id=$1", legacyRef.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "DELETE FROM transaction_span_operations WHERE event_id=$1", legacyRef.ID); err != nil {
		t.Fatal(err)
	}
	code, headers, raw := request(memberClient, "GET", path(legacyRef)+"?"+window, "")
	var pending map[string]json.RawMessage
	if err := json.Unmarshal(raw, &pending); err != nil {
		t.Fatal(err)
	}
	var detail string
	if err := json.Unmarshal(pending["detail"], &detail); err != nil {
		t.Fatal(err)
	}
	if code != 503 || headers.Get("Retry-After") != "2" || !strings.Contains(strings.ToLower(detail), "index") || pending["data"] != nil {
		t.Fatalf("pending must be descriptive and retryable without partial metrics: code=%d retry=%q body=%s", code, headers.Get("Retry-After"), raw)
	}
	if wire := read(memberClient, path(refs["Stream checkout"])+"?"+window); wire.TransactionCount != 1 || wire.SpanCount != 1 {
		t.Fatalf("unrelated pending group blocked completed stream group=%+v", wire)
	}
	if wire := read(memberClient, path(legacyRef)+"?start=2019-01-01T00:00:00Z&end=2019-01-01T00:00:01Z"); wire.TransactionCount != 0 || wire.SpanCount != 0 {
		t.Fatalf("out-of-window pending event blocked empty group=%+v", wire)
	}
}
