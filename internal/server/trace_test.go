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

// Storage tests own graph reconstruction, ordering and timing. HTTP owns
// capture-to-trace routing, canonical wire inputs, membership, raw JSON and
// retryable historical indexing. These requests use real PostgreSQL and SDK
// envelopes, without production test seams or mocked storage.
func TestTraceHTTPContract(t *testing.T) {
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
	schema := fmt.Sprintf("goodsentry_trace_http_%d", time.Now().UnixNano())
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
	const password = "trace-http-long-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@trace.test", hash, "Owner", "Trace HTTP")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "HTTP trace", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	newServer := func(logs bool) *httptest.Server {
		return httptest.NewServer(New(config.Config{Domain: "http://example.test", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20, Logs: logs}, db))
	}
	srv := newServer(true)
	defer srv.Close()
	request := func(client *http.Client, target *httptest.Server, method, path, body string) (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, target.URL+path, strings.NewReader(body))
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
	const trace = "abcdefabcdefabcdefabcdefabcdefab"
	api := "/api/v1/organizations/" + org.Slug + "/traces/"
	for _, suffix := range []string{"", "/nodes/event-1", "/errors"} {
		if code, _, raw := request(http.DefaultClient, srv, "GET", api+trace+suffix, ""); code != 401 {
			t.Fatalf("anonymous trace route %s=%d want=401 body=%s", suffix, code, raw)
		}
	}
	var member, foreignOrg string
	if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES('member@trace.test',$1,'Actual member') RETURNING id::text", hash).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-traces','Foreign traces') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := db.CreateProject(ctx, owner.ID, "foreign-traces", "Foreign trace", "javascript")
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
		if code, _, raw := request(client, srv, "POST", "/api/v1/login", string(body)); code != 200 {
			t.Fatalf("login=%d %s", code, raw)
		}
		return client
	}
	ownerClient, memberClient := login(owner.Email), login("member@trace.test")
	capture := func(p store.Project, kind, body string) {
		t.Helper()
		header := `{"type":"` + kind + `"}`
		if kind == "span" {
			header = `{"type":"span","item_count":2,"content_type":"application/vnd.sentry.items.span.v2+json"}`
		}
		if code, _, raw := request(http.DefaultClient, srv, "POST", "/api/"+p.ID+"/envelope/?sentry_version=7&sentry_key="+p.PublicKey, "{}\n"+header+"\n"+body); code != 200 {
			t.Fatalf("capture %s=%d %s", kind, code, raw)
		}
	}
	legacy := `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1","type":"transaction","transaction":"Legacy checkout","start_timestamp":1577836800,"timestamp":1577836802,"contexts":{"trace":{"trace_id":"` + strings.ToUpper(trace) + `","span_id":"1111111111111111","op":"http.server","status":"ok"}},"extra":{"receipt":9223372036854775807},"spans":[{"span_id":"2222222222222222","parent_span_id":"1111111111111111","description":"Load cart","op":"db.query","start_timestamp":1577836800.25,"timestamp":1577836800.5,"receipt":9223372036854775807}]}`
	streamed := `{"version":2,"items":[{"trace_id":"` + trace + `","span_id":"3333333333333333","parent_span_id":"2222222222222222","name":"Stream checkout","status":"ok","is_segment":true,"start_timestamp":1577836800.5,"end_timestamp":1577836801,"attributes":{"sentry.op":{"type":"string","value":"http.server"},"sentry.segment.id":{"type":"string","value":"3333333333333333"},"receipt":{"type":"integer","value":9223372036854775807}}},{"trace_id":"` + trace + `","span_id":"4444444444444444","parent_span_id":"3333333333333333","name":"Pay order","status":"ok","is_segment":false,"start_timestamp":1577836800.6,"end_timestamp":1577836800.9,"attributes":{"sentry.op":{"type":"string","value":"http.client"},"sentry.segment.id":{"type":"string","value":"3333333333333333"},"receipt":{"type":"integer","value":9223372036854775807}}}]}`
	capture(project, "transaction", legacy)
	capture(project, "span", streamed)
	capture(project, "event", `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2","message":"Payment failed","timestamp":1577836801,"contexts":{"trace":{"trace_id":"`+strings.ToUpper(trace)+`","span_id":"4444444444444444"}}}`)
	capture(project, "log", `{"items":[{"timestamp":1577836801,"level":"info","body":"Trace HTTP payment","trace_id":"`+strings.ToUpper(trace)+`"}]}`)
	const logsOnly = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const errorsOnly = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	const foreignOnly = "ffffffffffffffffffffffffffffffff"
	const absent = "99999999999999999999999999999999"
	capture(project, "log", `{"items":[{"timestamp":1577836801,"level":"info","body":"Logs-only trace","trace_id":"`+logsOnly+`"}]}`)
	capture(project, "event", `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa3","message":"Errors-only trace","contexts":{"trace":{"trace_id":"`+errorsOnly+`"}}}`)
	capture(foreignProject, "transaction", strings.ReplaceAll(legacy, strings.ToUpper(trace), foreignOnly))
	read := func(client *http.Client, target *httptest.Server, path string, dst any) []byte {
		t.Helper()
		code, _, raw := request(client, target, "GET", path, "")
		if code != 200 {
			t.Fatalf("read %s=%d %s", path, code, raw)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			t.Fatalf("decode %s: %v body=%s", path, err, raw)
		}
		return raw
	}
	var detail store.TraceDetail
	// Trace navigation uses canonical identifier matching; the ordinary Logs
	// filter keeps its existing exact semantics, including arbitrary trace text.
	logsPath := "/api/v1/organizations/" + org.Slug + "/logs?project=" + project.ID + "&trace="
	var logs struct {
		Data []store.Log `json:"data"`
	}
	read(ownerClient, srv, logsPath+trace+"&traceMatch=canonical", &logs)
	if len(logs.Data) != 1 || logs.Data[0].TraceID != strings.ToUpper(trace) {
		t.Fatalf("trace's related logs lost historical uppercase identifiers: %+v", logs.Data)
	}
	read(ownerClient, srv, logsPath+trace, &logs)
	if len(logs.Data) != 0 {
		t.Fatalf("ordinary exact trace matching changed: %+v", logs.Data)
	}
	read(ownerClient, srv, logsPath+strings.ToUpper(trace)+"&traceMatch=exact", &logs)
	if len(logs.Data) != 1 {
		t.Fatalf("explicit exact matching lost original identifier: %+v", logs.Data)
	}
	for _, query := range []string{"traceMatch=other", "traceMatch=canonical", "traceMatch=canonical&trace=invalid", "traceMatch=canonical&trace=" + trace + "0"} {
		if code, _, raw := request(ownerClient, srv, "GET", "/api/v1/organizations/"+org.Slug+"/logs?"+query, ""); code != 400 {
			t.Fatalf("invalid canonical logs filter=%d %s", code, raw)
		}
	}
	wire := read(ownerClient, srv, api+strings.ToUpper(trace)+"?project=&page=1&start=2010-01-01T00:00:00Z&end=2010-01-01T00:00:01Z", &detail)
	if detail.TraceID != trace || detail.ProjectID != "" || detail.NodeCount != 4 || detail.ProjectCount != 1 || detail.ErrorCount != 1 || detail.LogCount != 1 || !detail.LogsEnabled || len(detail.Data) != 4 || detail.Page != 1 || detail.PageSize != 50 || detail.HasMore || detail.StartTimestamp == nil || detail.EndTimestamp == nil || detail.Duration == nil {
		t.Fatalf("captured trace wire metadata=%s", wire)
	}
	if bytes.Contains(wire, []byte("9223372036854775807")) {
		t.Fatalf("trace list leaked original raw payload: %s", wire)
	}
	bySpan := make(map[string]store.TraceNode)
	for _, node := range detail.Data {
		if node.ProjectID != project.ID || node.ProjectName != project.Name || node.RecordID == "" || node.ID == "" || node.StartTimestamp == nil || node.EndTimestamp == nil || node.Duration == nil {
			t.Fatalf("trace node lost captured context=%+v", node)
		}
		bySpan[node.SpanID] = node
		var raw store.TraceNodeData
		read(memberClient, srv, api+strings.ToUpper(trace)+"/nodes/"+node.ID+"?project="+project.ID, &raw)
		if raw.ID != node.ID || raw.Source != node.Source || raw.RecordID != node.RecordID || raw.Ordinal != node.Ordinal || raw.ProjectID != project.ID || !bytes.Contains(raw.Data, []byte("9223372036854775807")) || bytes.Contains(raw.Data, []byte(`"9223372036854775807"`)) {
			t.Fatalf("raw node identity or numeric fidelity=%+v data=%s", raw, raw.Data)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if node.Source == "span" {
			var attributes map[string]map[string]json.RawMessage
			if err := json.Unmarshal(payload["attributes"], &attributes); err != nil {
				t.Fatal(err)
			}
			if string(attributes["receipt"]["type"]) != `"integer"` || string(attributes["receipt"]["value"]) != "9223372036854775807" {
				t.Fatalf("standalone raw lost typed integer: %s", raw.Data)
			}
		}
	}
	root, child, segment := bySpan["1111111111111111"], bySpan["2222222222222222"], bySpan["3333333333333333"]
	if root.Source != "event" || root.Kind != "transaction" || root.Ordinal != 0 || child.Source != "event" || child.Kind != "span" || child.Ordinal != 1 || segment.Source != "span" || segment.Kind != "segment" || segment.Ordinal != 0 || root.ID == child.ID {
		t.Fatalf("mixed capture identities root=%+v child=%+v segment=%+v", root, child, segment)
	}
	var scoped store.TraceDetail
	read(memberClient, srv, api+trace+"?project="+project.ID, &scoped)
	if scoped.ProjectID != project.ID || scoped.NodeCount != 4 {
		t.Fatalf("member project scope=%+v", scoped)
	}
	read(memberClient, srv, api+trace+"?page=1000000", &scoped)
	if scoped.Page != 1000000 || scoped.NodeCount != 4 || scoped.Data == nil || len(scoped.Data) != 0 || scoped.HasMore {
		t.Fatalf("maximum canonical page=%+v", scoped)
	}
	var errors store.TraceErrors
	read(memberClient, srv, api+strings.ToUpper(trace)+"/errors?project="+project.ID+"&page=1", &errors)
	if errors.Total != 1 || errors.Page != 1 || errors.PageSize != 25 || errors.HasMore || len(errors.Data) != 1 || errors.Data[0].ProjectID != project.ID || errors.Data[0].EventID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2" || errors.Data[0].Title != "Payment failed" || errors.Data[0].SpanID != "4444444444444444" || errors.Data[0].IssueID == "" {
		t.Fatalf("related error HTTP wire=%+v", errors)
	}
	errorRow := errors.Data[0].ID
	for _, path := range []string{api + absent + "/errors", api + absent + "/errors?project=" + project.ID, api + foreignOnly + "/errors", api + trace + "/errors?page=1000000"} {
		read(memberClient, srv, path, &errors)
		if errors.Data == nil || len(errors.Data) != 0 || errors.HasMore || (strings.Contains(path, "page=") && (errors.Total != 1 || errors.Page != 1000000)) || (!strings.Contains(path, "page=") && errors.Total != 0) {
			t.Fatalf("authorized empty error scope %s=%+v", path, errors)
		}
	}
	disabled := newServer(false)
	defer disabled.Close()
	read(memberClient, disabled, api+trace, &scoped)
	if scoped.LogsEnabled || scoped.LogCount != 0 || scoped.NodeCount != 4 {
		t.Fatalf("disabled logs leaked through trace response=%+v", scoped)
	}
	read(memberClient, srv, api+logsOnly, &scoped)
	if scoped.NodeCount != 0 || scoped.LogCount != 1 || scoped.Data == nil || scoped.StartTimestamp != nil || scoped.EndTimestamp != nil || scoped.Duration != nil {
		t.Fatalf("enabled logs-only trace=%+v", scoped)
	}
	read(memberClient, disabled, api+errorsOnly, &scoped)
	if scoped.NodeCount != 0 || scoped.ErrorCount != 1 || scoped.LogCount != 0 || scoped.LogsEnabled || scoped.Data == nil {
		t.Fatalf("error-only trace with disabled logs=%+v", scoped)
	}
	var foreignRow string
	if err := fixture.QueryRow(ctx, "SELECT id::text FROM events WHERE project_id=$1 AND event_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1'", foreignProject.ID).Scan(&foreignRow); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		target *httptest.Server
		path   string
	}{
		{srv, api + absent}, {srv, api + foreignOnly}, {srv, api + trace + "?project=" + foreignProject.ID},
		{srv, api + trace + "/errors?project=" + foreignProject.ID},
		{disabled, api + logsOnly}, {srv, api + absent + "/nodes/" + root.ID},
		{srv, api + trace + "/nodes/" + root.ID + "?project=" + foreignProject.ID},
		{srv, api + trace + "/nodes/event-" + errorRow}, {srv, api + trace + "/nodes/event-9223372036854775807"},
		{srv, api + trace + "/nodes/" + root.ID + "-2147483647"},
		{srv, api + foreignOnly + "/nodes/event-" + foreignRow},
		{srv, "/api/v1/organizations/foreign-traces/traces/" + foreignOnly},
		{srv, "/api/v1/organizations/foreign-traces/traces/" + foreignOnly + "/nodes/event-" + foreignRow},
		{srv, "/api/v1/organizations/foreign-traces/traces/" + foreignOnly + "/errors"},
		{srv, "/api/v1/organizations/unknown-trace-org/traces/" + trace + "/errors"},
	} {
		if code, _, raw := request(memberClient, tc.target, "GET", tc.path, ""); code != 404 {
			t.Fatalf("hidden/absent/ineligible scope %s=%d %s", tc.path, code, raw)
		}
	}
	for _, suffix := range []string{"", "/errors", "/nodes/" + root.ID} {
		for _, invalidTrace := range []string{trace[:31], trace + "a", strings.Repeat("g", 32)} {
			if code, _, raw := request(memberClient, srv, "GET", api+invalidTrace+suffix, ""); code != 400 {
				t.Fatalf("invalid trace %q suffix %q=%d %s", invalidTrace, suffix, code, raw)
			}
		}
		for _, project := range []string{"0", "-1", "+1", "01", "9223372036854775808", "1.0", "abc"} {
			if code, _, raw := request(memberClient, srv, "GET", api+trace+suffix+"?project="+url.QueryEscape(project), ""); code != 400 {
				t.Fatalf("invalid project %q suffix %q=%d %s", project, suffix, code, raw)
			}
		}
	}
	for _, suffix := range []string{"", "/errors"} {
		for _, page := range []string{"", "0", "-1", "+1", "01", "1000001", "9223372036854775808", "1.0"} {
			if code, _, raw := request(memberClient, srv, "GET", api+trace+suffix+"?page="+url.QueryEscape(page), ""); code != 400 {
				t.Fatalf("invalid page %q suffix %q=%d %s", page, suffix, code, raw)
			}
		}
	}
	for _, node := range []string{"event-0", "event-01", "event-+1", "event-9223372036854775808", "span-0", "span-01", "span-1-1", "EVENT-1", "other-1", root.ID + "-0", root.ID + "-01", root.ID + "-2147483648", root.ID + "-1-1"} {
		if code, _, raw := request(memberClient, srv, "GET", api+trace+"/nodes/"+url.PathEscape(node), ""); code != 400 {
			t.Fatalf("invalid node key %q=%d %s", node, code, raw)
		}
	}
	for _, version := range []int{0, 1} {
		if _, err := fixture.Exec(ctx, "UPDATE events SET span_projection_version=$1 WHERE id=$2", version, root.RecordID); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"", "/nodes/" + root.ID, "/nodes/" + child.ID} {
			code, headers, raw := request(memberClient, srv, "GET", api+trace+suffix, "")
			var body map[string]json.RawMessage
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			var message string
			if err := json.Unmarshal(body["detail"], &message); err != nil {
				t.Fatal(err)
			}
			if code != 503 || headers.Get("Retry-After") != "2" || !strings.Contains(strings.ToLower(message), "index") || body["data"] != nil {
				t.Fatalf("pending projection v%d suffix %q code=%d retry=%q body=%s", version, suffix, code, headers.Get("Retry-After"), raw)
			}
		}
		var raw store.TraceNodeData
		read(memberClient, srv, api+trace+"/nodes/"+segment.ID, &raw)
		if raw.ID != segment.ID || !bytes.Contains(raw.Data, []byte("9223372036854775807")) {
			t.Fatalf("unrelated legacy indexing blocked standalone raw=%+v", raw)
		}
		read(memberClient, srv, api+trace+"/errors", &errors)
		if errors.Total != 1 || len(errors.Data) != 1 {
			t.Fatalf("legacy indexing blocked related errors=%+v", errors)
		}
	}
}
