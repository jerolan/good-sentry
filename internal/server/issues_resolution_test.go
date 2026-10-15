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

	"goodsentry/internal/config"
	"goodsentry/internal/event"
	"goodsentry/internal/store"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// HTTP owns admission, session/CSRF and wire shape; storage owns release order,
// immutable references, recurrence, concurrent binding and coherent25+1 pages.
type resolutionHTTPFixture struct {
	ctx     context.Context
	db      *store.Store
	sql     *pgx.Conn
	owner   store.User
	org     store.Organization
	project store.Project
	issueID string
	server  *httptest.Server
	client  *http.Client
	csrf    string
	hash    []byte
}

func newResolutionHTTPFixture(t *testing.T) *resolutionHTTPFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	schema := fmt.Sprintf("goodsentry_issue_resolution_http_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
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
	t.Cleanup(db.Close)
	sql, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sql.Close(context.Background()) })
	hash, err := bcrypt.GenerateFromPassword([]byte("resolution-http-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@resolution.test", hash, "Resolution owner", "Resolution HTTP")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, owner.ID, org.Slug, "Resolution project", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(json.RawMessage(`{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","message":"Resolution HTTP fixture","release":"app@1.0"}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	issue, _, err := db.Ingest(ctx, project.ID, ev)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = db.CreateRelease(ctx, store.ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}, store.ReleaseCreate{Version: "app@2.0", Projects: []string{project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, db))
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &resolutionHTTPFixture{ctx: ctx, db: db, sql: sql, owner: owner, org: org, project: project, issueID: issue, server: srv, client: &http.Client{Jar: jar}, hash: hash}
	var me struct{ CSRFToken string }
	if err = json.Unmarshal(f.request(t, f.client, "POST", "/api/v1/login", `{"email":"owner@resolution.test","password":"resolution-http-password"}`, nil, 200), &me); err != nil || me.CSRFToken == "" {
		t.Fatalf("login response missing CSRF: %v", err)
	}
	f.csrf = me.CSRFToken
	return f
}

func (f *resolutionHTTPFixture) request(t *testing.T, client *http.Client, method, path, body string, headers http.Header, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := client.Do(req)
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

func resolutionJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func resolutionWire(t *testing.T, raw []byte) *store.IssueResolution {
	t.Helper()
	var issue map[string]json.RawMessage
	if err := json.Unmarshal(raw, &issue); err != nil {
		t.Fatal(err)
	}
	policy, present := issue["resolution"]
	if !present {
		t.Fatalf("issue omitted nullable resolution: %s", raw)
	}
	if string(policy) == "null" {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(policy, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatalf("resolution must expose only mode/comparison/release/baseline: %s", policy)
	}
	for _, name := range []string{"mode", "comparison", "release", "baseline"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("resolution omitted %s: %s", name, policy)
		}
	}
	var result store.IssueResolution
	if err := json.Unmarshal(policy, &result); err != nil {
		t.Fatal(err)
	}
	if result.Comparison != "semantic" && result.Comparison != "created" {
		t.Fatalf("invalid comparison DTO=%s", policy)
	}
	for _, name := range []string{"release", "baseline"} {
		if string(fields[name]) == "null" {
			continue
		}
		var ref map[string]json.RawMessage
		if err := json.Unmarshal(fields[name], &ref); err != nil {
			t.Fatal(err)
		}
		if len(ref) != 3 {
			t.Fatalf("reference exposed internal state: %s", fields[name])
		}
		for _, key := range []string{"version", "dateCreated", "dateReleased"} {
			if _, ok := ref[key]; !ok {
				t.Fatalf("reference omitted %s: %s", key, fields[name])
			}
		}
		for _, key := range []string{"dateCreated", "dateReleased"} {
			if string(ref[key]) == "null" {
				if key == "dateCreated" {
					t.Fatal("creation timestamp cannot be null")
				}
				continue
			}
			var value time.Time
			if err := json.Unmarshal(ref[key], &value); err != nil {
				t.Fatal(err)
			}
			_, offset := value.Zone()
			if offset != 0 || !value.Equal(value.Truncate(time.Microsecond)) {
				t.Fatalf("reference date must be UTC/microsecond: %s", ref[key])
			}
		}
	}
	return &result
}

func TestIssueResolutionHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/v1/issues/" + f.issueID
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	patch := func(t *testing.T, body string) []byte {
		t.Helper()
		return f.request(t, f.client, "PATCH", path, body, csrf, 200)
	}
	detail := func(t *testing.T) []byte {
		t.Helper()
		var result struct{ Issue json.RawMessage }
		if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		return result.Issue
	}
	chosen := `{"status":"resolved","resolution":{"mode":"release","version":"app@2.0"}}`
	initial := resolutionWire(t, patch(t, chosen))
	if initial == nil || initial.Mode != "release" || initial.Release == nil || initial.Release.Version != "app@2.0" || initial.Baseline != nil {
		t.Fatalf("chosen release wire=%+v", initial)
	}

	t.Run("nullable policy and omitted field are distinct", func(t *testing.T) {
		preserved := resolutionWire(t, patch(t, `{"status":"resolved"}`))
		if !reflect.DeepEqual(initial, preserved) {
			t.Fatalf("omission lost selected policy: %+v", preserved)
		}
		if resolutionWire(t, patch(t, `{"status":"resolved","resolution":null}`)) != nil {
			t.Fatal("explicit null did not select simple resolution")
		}
		next := resolutionWire(t, patch(t, `{"status":"resolved","resolution":{"mode":"next_release"}}`))
		if next == nil || next.Mode != "next_release" || next.Release != nil || next.Baseline == nil || next.Baseline.Version != "app@1.0" {
			t.Fatalf("pending next wire=%+v", next)
		}
		if resolutionWire(t, detail(t)) == nil {
			t.Fatal("detail omitted pending policy")
		}
		patch(t, `{"status":"resolved","resolution":null}`)
	})

	t.Run("strict resolution JSON retains editable issue on errors", func(t *testing.T) {
		patch(t, chosen)
		before := detail(t)
		history := f.request(t, f.client, "GET", path+"/activity", "", nil, 200)
		invalid := []string{
			`{"resolution":null}`, `{"status":"unresolved","resolution":null}`, `{"status":"ignored","resolution":{"mode":"release","version":"app@2.0"}}`,
			`{"status":"resolved","ignore":null,"resolution":null}`, `{"status":"resolved","resolution":false}`, `{"status":"resolved","resolution":[]}`, `{"status":"resolved","resolution":1}`, `{"status":"resolved","resolution":"next_release"}`, `{"status":"resolved","resolution":{}}`,
			`{"status":"resolved","resolution":{"mode":null}}`, `{"status":"resolved","resolution":{"mode":true}}`, `{"status":"resolved","resolution":{"mode":1}}`, `{"status":"resolved","resolution":{"mode":[]}}`, `{"status":"resolved","resolution":{"mode":"unknown"}}`,
			`{"status":"resolved","resolution":{"mode":"release"}}`, `{"status":"resolved","resolution":{"version":"app@2.0"}}`, `{"status":"resolved","resolution":{"mode":"release","version":null}}`, `{"status":"resolved","resolution":{"mode":"release","version":2}}`, `{"status":"resolved","resolution":{"mode":"release","version":true}}`, `{"status":"resolved","resolution":{"mode":"release","version":{}}}`, `{"status":"resolved","resolution":{"mode":"release","version":[]}}`,
			`{"status":"resolved","resolution":{"mode":"next_release","version":null}}`, `{"status":"resolved","resolution":{"mode":"next_release","version":"app@2.0"}}`, `{"status":"resolved","resolution":{"mode":"next_release","id":"1"}}`, `{"status":"resolved","resolution":{"mode":"release","version":"app@2.0","comparison":"created"}}`,
			`{"status":"resolved","resolution":{"mode":"next_release","mode":"release"}}`, `{"status":"resolved","resolution":{"mode":"release","version":"app@2.0","version":"app@1.0"}}`, `{"status":"resolved","resolution":null,"resolution":{"mode":"next_release"}}`,
			`{"status":"resolved","resolution":{"mode":"release","version":"."}}`, `{"status":"resolved","resolution":{"mode":"release","version":".."}}`, `{"status":"resolved","resolution":{"mode":"release","version":""}}`, `{"status":"resolved","resolution":{"mode":"release","version":"bad\u0000name"}}`, `{"status":"resolved","resolution":{"mode":"release","version":"bad\nname"}}`,
		}
		invalid = append(invalid, resolutionJSON(t, map[string]any{"status": "resolved", "resolution": map[string]any{"mode": "release", "version": strings.Repeat("界", 256)}}), `{"status":"resolved","resolution":{"mode":"release","version":"`+string([]byte{0xff})+`"}}`)
		for _, body := range invalid {
			raw := f.request(t, f.client, "PATCH", path, body, csrf, 400)
			var failure struct{ Detail string }
			if err := json.Unmarshal(raw, &failure); err != nil || failure.Detail == "" {
				t.Fatalf("400 omitted editable detail: %s err=%v", raw, err)
			}
		}
		if !bytes.Equal(before, detail(t)) || !bytes.Equal(history, f.request(t, f.client, "GET", path+"/activity", "", nil, 200)) {
			t.Fatal("invalid transport input mutated policy or activity")
		}
	})

	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	padded := " " + strings.Repeat("界", 253) + " "
	released := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.FixedZone("fixture", 2*60*60))
	archived := "archived"
	for _, input := range []store.ReleaseCreate{
		{Version: store.ReleaseVersion(padded), Projects: []string{f.project.ID}, ReleaseUpdate: store.ReleaseUpdate{Status: &archived, DateReleased: store.ReleaseTime{Set: true, Value: &released}}},
		{Version: store.ReleaseVersion(` literal_%\ 版本 `), Projects: []string{f.project.ID}},
	} {
		if _, _, err := f.db.CreateRelease(f.ctx, access, input); err != nil {
			t.Fatal(err)
		}
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Another project", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.CreateRelease(f.ctx, access, store.ReleaseCreate{Version: "other-project-only", Projects: []string{other.ID}}); err != nil {
		t.Fatal(err)
	}

	t.Run("exact Unicode identity and archived selection", func(t *testing.T) {
		policy := resolutionWire(t, patch(t, resolutionJSON(t, map[string]any{"status": "resolved", "resolution": map[string]any{"mode": "release", "version": padded}})))
		if policy == nil || policy.Release == nil || string(policy.Release.Version) != padded || policy.Release.DateReleased == nil || !policy.Release.DateReleased.Equal(released) {
			t.Fatalf("exact archived selected reference=%+v", policy)
		}
	})

	t.Run("candidate route strict query and public DTO", func(t *testing.T) {
		candidates := path + "/releases"
		f.request(t, http.DefaultClient, "GET", candidates, "", nil, 401)
		f.request(t, f.client, "GET", "/api/v1/issues/9223372036854775807/releases", "", nil, 404)
		invalid := []string{"page=0", "page=-1", "page=01", "page=%2B1", "page=1.0", "page=1e0", "page=1000001", "page=", "page=1&page=1", "query=a&query=a", "project=" + other.ID, "organization=wrong", "unknown=x", "query=%GG", "page=%GG", "query=%FF", "query=%00", "query=a;page=1", "query=" + url.QueryEscape(strings.Repeat("é", 101))}
		for _, query := range invalid {
			f.request(t, f.client, "GET", candidates+"?"+query, "", nil, 400)
		}
		f.request(t, f.client, "GET", candidates+"?query="+url.QueryEscape(strings.Repeat("é", 100)), "", nil, 200)
		type pageDTO struct {
			Data                  []map[string]json.RawMessage
			Total, Page, PageSize int
		}
		var first pageDTO
		raw := f.request(t, f.client, "GET", candidates+"?query=&page=1", "", nil, 200)
		if err := json.Unmarshal(raw, &first); err != nil || first.Total != 4 || first.Page != 1 || first.PageSize != 25 || len(first.Data) != 4 {
			t.Fatalf("candidate page=%s err=%v", raw, err)
		}
		foundArchived := false
		for _, candidate := range first.Data {
			if len(candidate) != 4 {
				t.Fatalf("candidate leaks internal fields=%+v", candidate)
			}
			for _, key := range []string{"version", "status", "dateCreated", "dateReleased"} {
				if _, present := candidate[key]; !present {
					t.Fatalf("candidate omitted %s", key)
				}
			}
			var version, status string
			if json.Unmarshal(candidate["version"], &version) != nil || json.Unmarshal(candidate["status"], &status) != nil || version == "other-project-only" {
				t.Fatalf("candidate scope/type invalid=%+v", candidate)
			}
			for _, key := range []string{"dateCreated", "dateReleased"} {
				if string(candidate[key]) != "null" {
					var stamp time.Time
					if err := json.Unmarshal(candidate[key], &stamp); err != nil {
						t.Fatal(err)
					}
					_, offset := stamp.Zone()
					if offset != 0 || !stamp.Equal(stamp.Truncate(time.Microsecond)) {
						t.Fatalf("candidate date must be UTC/microsecond=%s", candidate[key])
					}
				}
			}
			if version == padded {
				foundArchived = status == "archived" && string(candidate["dateReleased"]) != "null"
			}
		}
		if !foundArchived {
			t.Fatal("candidate API omitted selectable archived version")
		}
		var exact pageDTO
		raw = f.request(t, f.client, "GET", candidates+"?query="+url.QueryEscape(`LITERAL_%\`), "", nil, 200)
		if err := json.Unmarshal(raw, &exact); err != nil || exact.Total != 1 || len(exact.Data) != 1 || string(exact.Data[0]["version"]) != resolutionJSON(t, ` literal_%\ 版本 `) {
			t.Fatalf("literal candidate query=%s err=%v", raw, err)
		}
		var past pageDTO
		raw = f.request(t, f.client, "GET", candidates+"?page=1000000", "", nil, 200)
		if err := json.Unmarshal(raw, &past); err != nil || past.Total != 4 || past.Page != 1000000 || past.PageSize != 25 || len(past.Data) != 0 || past.Data == nil {
			t.Fatalf("past end page metadata=%s err=%v", raw, err)
		}
	})

	t.Run("session CSRF and provider errors", func(t *testing.T) {
		f.request(t, http.DefaultClient, "PATCH", path, chosen, nil, 401)
		f.request(t, f.client, "PATCH", path, chosen, nil, 403)
		cross := csrf.Clone()
		cross.Set("Origin", "https://attacker.example.test")
		f.request(t, f.client, "PATCH", path, chosen, cross, 403)
		f.request(t, f.client, "PATCH", path, `{"status":"resolved","resolution":{"mode":"release","version":"missing-version"}}`, csrf, 404)
		f.request(t, f.client, "PATCH", path, `{"status":"resolved","resolution":{"mode":"release","version":"other-project-only"}}`, csrf, 404)
		var foreignOrg, foreignProject string
		if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-resolution','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
			t.Fatal(err)
		}
		if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'foreign','Foreign','go') RETURNING id::text`, foreignOrg).Scan(&foreignProject); err != nil {
			t.Fatal(err)
		}
		ev, err := event.Normalize(json.RawMessage(`{"event_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","message":"Foreign resolution issue","release":"app@2.0"}`), "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		foreignIssue, _, err := f.db.Ingest(f.ctx, foreignProject, ev)
		if err != nil {
			t.Fatal(err)
		}
		f.request(t, f.client, "GET", "/api/v1/issues/"+foreignIssue+"/releases", "", nil, 404)
		f.request(t, f.client, "PATCH", "/api/v1/issues/"+foreignIssue, chosen, csrf, 404)
		unversioned, err := event.Normalize(json.RawMessage(`{"event_id":"cccccccccccccccccccccccccccccccc","message":"Unversioned resolution issue"}`), "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		noRelease, _, err := f.db.Ingest(f.ctx, f.project.ID, unversioned)
		if err != nil {
			t.Fatal(err)
		}
		raw := f.request(t, f.client, "PATCH", "/api/v1/issues/"+noRelease, `{"status":"resolved","resolution":{"mode":"next_release"}}`, csrf, 400)
		var failure struct{ Detail string }
		if err = json.Unmarshal(raw, &failure); err != nil || failure.Detail == "" {
			t.Fatalf("missing-baseline error must remain editable: %s err=%v", raw, err)
		}
		var member string
		if err = f.sql.QueryRow(f.ctx, `INSERT INTO users(email,name,password_hash) VALUES('member@resolution.test','Member',$1) RETURNING id::text`, f.hash).Scan(&member); err != nil {
			t.Fatal(err)
		}
		if _, err = f.sql.Exec(f.ctx, `INSERT INTO memberships VALUES($1,$2,'member')`, f.org.ID, member); err != nil {
			t.Fatal(err)
		}
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		var login struct{ CSRFToken string }
		if err = json.Unmarshal(f.request(t, client, "POST", "/api/v1/login", `{"email":"member@resolution.test","password":"resolution-http-password"}`, nil, 200), &login); err != nil {
			t.Fatal(err)
		}
		f.request(t, client, "GET", path+"/releases", "", nil, 200)
		raw = f.request(t, client, "PATCH", path, resolutionJSON(t, map[string]any{"status": "resolved", "resolution": map[string]any{"mode": "release", "version": "app@2.0"}, "assignedTo": map[string]string{"type": "user", "id": f.owner.ID}}), http.Header{"X-CSRF-Token": {login.CSRFToken}}, 200)
		if resolutionWire(t, raw) == nil {
			t.Fatal("current ordinary member resolution was not wired")
		}
		var assigned struct{ AssignedTo *store.IssueActor }
		if err = json.Unmarshal(raw, &assigned); err != nil || assigned.AssignedTo == nil || assigned.AssignedTo.ID != f.owner.ID {
			t.Fatalf("combined assignment response=%s err=%v", raw, err)
		}
	})
}
