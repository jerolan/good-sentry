//go:build integration

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// HTTP owns credential issuance/consumption, session and bearer separation,
// wire validation and public pagination links. Store tests own token lifecycle.
func TestAPITokenHTTPContract(t *testing.T) {
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
	schema := fmt.Sprintf("goodsentry_api_token_http_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
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
	cfg := config.Config{Domain: "http://public.example.test/sentry", Prefix: "/sentry", Secret: strings.Repeat("a", 64), MaxBodyBytes: 1 << 20}
	srv := httptest.NewServer(New(cfg, db))
	defer srv.Close()
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	type response struct {
		status int
		header http.Header
		raw    []byte
	}
	request := func(client *http.Client, method, path, body, csrf string, headers http.Header) response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
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
		return response{res.StatusCode, res.Header.Clone(), raw}
	}
	want := func(label string, r response, status int) response {
		t.Helper()
		if r.status != status {
			t.Fatalf("%s=%d want=%d body=%s", label, r.status, status, r.raw)
		}
		return r
	}
	encode := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	decode := func(raw []byte, value any) {
		t.Helper()
		if err := json.Unmarshal(raw, value); err != nil {
			t.Fatalf("invalid JSON response: %v", err)
		}
	}
	management := "/api/v1/organizations/token-http/api-tokens"
	want("anonymous token management", request(http.DefaultClient, "GET", management, "", "", nil), 401)
	want("anonymous compatible list", request(http.DefaultClient, "GET", "/api/0/organizations/token-http/projects/", "", "", nil), 401)
	want("anonymous compatible detail", request(http.DefaultClient, "GET", "/api/0/projects/token-http/project/", "", "", nil), 401)

	const password = "api-token-http-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@tokens.test", hash, "Owner", "Token HTTP")
	if err != nil {
		t.Fatal(err)
	}
	management = "/api/v1/organizations/" + org.Slug + "/api-tokens"
	for _, role := range []string{"admin", "member"} {
		var userID string
		if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text", role+"@tokens.test", hash, role).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3)", org.ID, userID, role); err != nil {
			t.Fatal(err)
		}
	}
	var foreignOrgID string
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-tokens','Foreign tokens') RETURNING id::text").Scan(&foreignOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrgID, owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := db.CreateProject(ctx, owner.ID, "foreign-tokens", "Foreign project", "go")
	if err != nil {
		t.Fatal(err)
	}
	projects := make([]store.Project, 12)
	for i := range projects {
		name := fmt.Sprintf("Project %02d", i)
		if i < 4 {
			name = []string{"Filter % Zebra", "filter % alpha", "FILTER % middle", "Filter X other"}[i]
		}
		projects[i], err = db.CreateProject(ctx, owner.ID, org.Slug, name, "javascript")
		if err != nil {
			t.Fatal(err)
		}
	}
	type actor struct {
		client *http.Client
		csrf   string
	}
	login := func(email string) actor {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		r := want("login "+email, request(client, "POST", "/api/v1/login", encode(map[string]string{"email": email, "password": password}), "", nil), 200)
		var me struct{ CSRFToken string }
		decode(r.raw, &me)
		if me.CSRFToken == "" {
			t.Fatal("login omitted CSRF token")
		}
		return actor{client, me.CSRFToken}
	}
	ownerActor, adminActor, memberActor := login(owner.Email), login("admin@tokens.test"), login("member@tokens.test")
	type projectMetadata struct {
		ID          string    `json:"id"`
		Slug        string    `json:"slug"`
		Name        string    `json:"name"`
		Platform    string    `json:"platform"`
		DateCreated time.Time `json:"dateCreated"`
	}
	type credential struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Prefix      string            `json:"prefix"`
		Token       string            `json:"token"`
		Scopes      []string          `json:"scopes"`
		AllProjects bool              `json:"allProjects"`
		Projects    []projectMetadata `json:"projects"`
		CreatedBy   string            `json:"createdBy"`
		DateCreated time.Time         `json:"dateCreated"`
		ExpiresAt   time.Time         `json:"expiresAt"`
		RevokedAt   *time.Time        `json:"revokedAt"`
	}
	issued := []credential{}
	issue := func(who actor, body string) credential {
		t.Helper()
		r := want("issue token", request(who.client, "POST", management, body, who.csrf, nil), 201)
		var c credential
		decode(r.raw, &c)
		if c.ID == "" || !regexp.MustCompile(`^gs_[0-9a-f]{64}$`).MatchString(c.Token) || c.Prefix != c.Token[:11] {
			t.Fatal("issuance must return flat metadata and one correctly formatted token")
		}
		for _, key := range []string{`"hash"`, `"tokenHash"`, `"token_hash"`} {
			if strings.Contains(string(r.raw), key) {
				t.Fatal("issuance disclosed a stored credential hash")
			}
		}
		issued = append(issued, c)
		return c
	}
	bearer := func(value string) http.Header { return http.Header{"Authorization": {"Bearer " + value}} }
	all := issue(ownerActor, `{"name":"All projects"}`)
	scopes := slices.Clone(all.Scopes)
	slices.Sort(scopes)
	if all.Name != "All projects" || !all.AllProjects || len(all.Projects) != 0 || all.CreatedBy != owner.ID || !slices.Equal(scopes, []string{"org:read", "project:read"}) || all.DateCreated.IsZero() || all.ExpiresAt.Sub(all.DateCreated) < 90*24*time.Hour-time.Second || all.ExpiresAt.Sub(all.DateCreated) > 90*24*time.Hour+time.Second || all.RevokedAt != nil {
		t.Fatal("issued default metadata does not match the credential contract")
	}
	listPath := "/api/0/organizations/" + org.Slug + "/projects/"
	detailPath := "/api/0/projects/" + org.Slug + "/" + projects[0].Slug + "/"
	readProjects := func(path string, headers http.Header) []projectMetadata {
		t.Helper()
		r := want("compatible project read", request(http.DefaultClient, "GET", path, "", "", headers), 200)
		var result []projectMetadata
		decode(r.raw, &result)
		if strings.Contains(string(r.raw), `"dsn"`) || strings.Contains(string(r.raw), `"publicKey"`) || strings.Contains(string(r.raw), projects[0].PublicKey) {
			t.Fatal("compatible project list disclosed SDK credentials")
		}
		return result
	}
	items := readProjects(listPath+"?cursor=", bearer(all.Token))
	if len(items) != len(projects) {
		t.Fatalf("issued token saw %d projects want=%d", len(items), len(projects))
	}
	for i, item := range items {
		if item.ID != projects[i].ID || item.Slug != projects[i].Slug || item.Name != projects[i].Name || item.Platform != projects[i].Platform || !item.DateCreated.Equal(projects[i].DateCreated) {
			t.Fatalf("project %d lost its actual fields or numeric ID ordering", i)
		}
	}
	detail := want("numeric project detail", request(http.DefaultClient, "GET", "/api/0/projects/"+org.ID+"/"+projects[0].ID+"/", "", "", bearer(all.Token)), 200)
	var actualProject projectMetadata
	decode(detail.raw, &actualProject)
	if actualProject.ID != projects[0].ID || actualProject.Slug != projects[0].Slug || actualProject.Platform != projects[0].Platform || !actualProject.DateCreated.Equal(projects[0].DateCreated) || strings.Contains(string(detail.raw), `"dsn"`) || strings.Contains(string(detail.raw), projects[0].PublicKey) {
		t.Fatal("compatible detail lost actual project metadata or disclosed an SDK key")
	}
	want("numeric organization list", request(http.DefaultClient, "GET", "/api/0/organizations/"+org.ID+"/projects/", "", "", bearer(all.Token)), 200)
	want("owner browser list", request(ownerActor.client, "GET", listPath, "", "", nil), 200)
	want("member browser detail", request(memberActor.client, "GET", detailPath, "", "", nil), 200)

	orgOnly := issue(ownerActor, `{"name":"Organization read","scopes":["org:read"]}`)
	projectOnly := issue(ownerActor, `{"name":"Project read","scopes":["project:read"]}`)
	want("org scope list", request(http.DefaultClient, "GET", listPath, "", "", bearer(orgOnly.Token)), 200)
	want("org scope cannot read detail", request(http.DefaultClient, "GET", detailPath, "", "", bearer(orgOnly.Token)), 403)
	want("project scope detail", request(http.DefaultClient, "GET", detailPath, "", "", bearer(projectOnly.Token)), 200)
	want("project scope cannot list", request(http.DefaultClient, "GET", listPath, "", "", bearer(projectOnly.Token)), 403)
	restricted := issue(ownerActor, encode(map[string]any{"name": "Selected projects", "projectIds": []string{projects[10].ID, projects[0].ID}}))
	if restricted.AllProjects || len(restricted.Projects) != 2 {
		t.Fatal("selected project grant was not represented as restricted metadata")
	}
	selected := readProjects(listPath, bearer(restricted.Token))
	if len(selected) != 2 || selected[0].ID != projects[0].ID || selected[1].ID != projects[10].ID {
		t.Fatal("selected-project bearer was not intersected with the project list")
	}
	want("selected project detail", request(http.DefaultClient, "GET", detailPath, "", "", bearer(restricted.Token)), 200)
	want("unselected project detail", request(http.DefaultClient, "GET", "/api/0/projects/"+org.Slug+"/"+projects[1].Slug+"/", "", "", bearer(restricted.Token)), 404)
	want("other organization despite owner membership", request(http.DefaultClient, "GET", "/api/0/organizations/foreign-tokens/projects/", "", "", bearer(all.Token)), 404)
	want("foreign project despite owner membership", request(http.DefaultClient, "GET", "/api/0/projects/foreign-tokens/"+foreignProject.Slug+"/", "", "", bearer(all.Token)), 404)
	want("foreign project under current organization", request(http.DefaultClient, "GET", "/api/0/projects/"+org.Slug+"/"+foreignProject.ID+"/", "", "", bearer(all.Token)), 404)
	want("absent project", request(http.DefaultClient, "GET", "/api/0/projects/"+org.Slug+"/9223372036854775807/", "", "", bearer(all.Token)), 404)

	for label, values := range map[string][]string{
		"empty": {""}, "basic": {"Basic invalid"}, "missing credential": {"Bearer"},
		"unknown": {"Bearer gs_" + strings.Repeat("0", 64)}, "extra credential": {"Bearer " + all.Token + " extra"},
		"duplicate headers":  {"Bearer " + all.Token, "Bearer " + all.Token},
		"joined credentials": {"Bearer " + all.Token + ", Bearer " + all.Token},
		"SDK key":            {"Bearer " + projects[0].PublicKey},
	} {
		want("cookie must not rescue "+label, request(ownerActor.client, "GET", listPath, "", "", http.Header{"Authorization": values}), 401)
	}
	for _, c := range []credential{all, orgOnly, restricted} {
		want("bearer cannot read native account", request(http.DefaultClient, "GET", "/api/v1/me", "", "", bearer(c.Token)), 401)
		want("bearer cannot list credentials", request(http.DefaultClient, "GET", management, "", "", bearer(c.Token)), 401)
		want("bearer cannot issue credentials", request(http.DefaultClient, "POST", management, `{"name":"Forbidden"}`, ownerActor.csrf, bearer(c.Token)), 401)
		want("bearer cannot revoke credentials", request(http.DefaultClient, "DELETE", management+"/"+all.ID, "", ownerActor.csrf, bearer(c.Token)), 401)
	}
	want("session mutation requires CSRF", request(ownerActor.client, "POST", management, `{"name":"No CSRF"}`, "", nil), 403)
	want("session mutation requires same origin", request(ownerActor.client, "POST", management, `{"name":"Cross origin"}`, ownerActor.csrf, http.Header{"Origin": {"https://other.example.test"}}), 403)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		path := management
		if method == "DELETE" {
			path += "/" + all.ID
		}
		want("member cannot manage credentials "+method, request(memberActor.client, method, path, `{"name":"Member"}`, memberActor.csrf, nil), 403)
	}
	adminToken := issue(adminActor, `{"name":"Admin credential","expiresInDays":1}`)
	want("admin can list credentials", request(adminActor.client, "GET", management, "", "", nil), 200)
	want("revoke requires CSRF", request(ownerActor.client, "DELETE", management+"/"+adminToken.ID, "", "", nil), 403)

	envelope := "{}\n{\"type\":\"event\"}\n{\"event_id\":\"1234567890abcdef1234567890abcdef\",\"message\":\"SDK identity control\"}\n"
	ingestion := "/api/" + projects[0].ID + "/envelope/?sentry_version=7&sentry_key="
	want("valid SDK control", request(http.DefaultClient, "POST", ingestion+projects[0].PublicKey, envelope, "", nil), 200)
	want("management token is not an SDK key", request(http.DefaultClient, "POST", ingestion+all.Token, envelope, "", nil), 403)

	invalidBodies := map[string]string{
		"missing name": `{}`, "empty name": `{"name":""}`, "number name": `{"name":12}`,
		"blank name": `{"name":"   "}`, "null scopes": `{"name":"Bad","scopes":null}`,
		"null projects": `{"name":"Bad","projectIds":null}`, "null expiry": `{"name":"Bad","expiresInDays":null}`,
		"long Unicode name": encode(map[string]string{"name": strings.Repeat("界", 101)}),
		"control in name":   `{"name":"bad\u0001name"}`, "unknown field": `{"name":"Bad","unknown":true}`,
		"empty scopes": `{"name":"Bad","scopes":[]}`, "duplicate scopes": `{"name":"Bad","scopes":["org:read","org:read"]}`,
		"unknown scope":      `{"name":"Bad","scopes":["project:write"]}`,
		"numeric project ID": `{"name":"Bad","projectIds":[1]}`, "zero project ID": `{"name":"Bad","projectIds":["0"]}`,
		"noncanonical project ID": `{"name":"Bad","projectIds":["01"]}`, "overflow project ID": `{"name":"Bad","projectIds":["9223372036854775808"]}`,
		"zero expiry": `{"name":"Bad","expiresInDays":0}`, "large expiry": `{"name":"Bad","expiresInDays":367}`,
		"fractional expiry": `{"name":"Bad","expiresInDays":1.5}`, "string expiry": `{"name":"Bad","expiresInDays":"90"}`,
		"multiple JSON values": `{"name":"Bad"} {}`,
	}
	for label, body := range invalidBodies {
		want("invalid credential input "+label, request(ownerActor.client, "POST", management, body, ownerActor.csrf, nil), 400)
	}
	want("foreign selected project", request(ownerActor.client, "POST", management, encode(map[string]any{"name": "Foreign selection", "projectIds": []string{projects[0].ID, foreignProject.ID}}), ownerActor.csrf, nil), 404)
	issue(ownerActor, encode(map[string]any{"name": strings.Repeat("界", 100), "expiresInDays": 366}))

	for _, value := range []string{"", "0", "-1", "01", "+1", "1.0", "101", "10000000000000000000000"} {
		want("invalid per_page "+value, request(http.DefaultClient, "GET", listPath+"?per_page="+url.QueryEscape(value), "", "", bearer(all.Token)), 400)
	}
	for _, value := range []string{"", "0", "-1", "01", "+1", "1000001"} {
		want("invalid credential page "+value, request(ownerActor.client, "GET", management+"?page="+url.QueryEscape(value), "", "", nil), 400)
	}
	cursor := func(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
	for _, value := range []string{"%%%", cursor("offset:0") + "=", "b2Zmc2V0OjB", cursor("offset:01"), cursor("offset:-1"), cursor("offset:100000001"), cursor("offset:"), cursor("other:0"), cursor("offset: 1")} {
		want("invalid project cursor "+value, request(http.DefaultClient, "GET", listPath+"?cursor="+url.QueryEscape(value), "", "", bearer(all.Token)), 400)
	}
	want("Unicode query bound", request(http.DefaultClient, "GET", listPath+"?query="+url.QueryEscape(strings.Repeat("界", 200)), "", "", bearer(all.Token)), 200)
	want("Unicode query over bound", request(http.DefaultClient, "GET", listPath+"?query="+url.QueryEscape(strings.Repeat("界", 201)), "", "", bearer(all.Token)), 400)
	want("control in query", request(http.DefaultClient, "GET", listPath+"?query=%00", "", "", bearer(all.Token)), 400)
	for _, path := range []string{"/api/0/organizations/0" + org.ID + "/projects/", "/api/0/organizations/0/projects/", "/api/0/projects/" + org.Slug + "/0" + projects[0].ID + "/"} {
		want("noncanonical numeric path", request(http.DefaultClient, "GET", path, "", "", bearer(all.Token)), 404)
	}
	if empty := readProjects(listPath+"?query=no-matching-project", bearer(all.Token)); len(empty) != 0 {
		t.Fatal("authorized unmatched query must return an empty array")
	}
	for _, parameter := range []string{"per_page", "cursor", "query"} {
		r := request(http.DefaultClient, "GET", listPath+"?"+parameter+"=%GG", "", "", bearer(all.Token))
		if r.status != 400 {
			t.Errorf("raw malformed %s encoding=%d want=400", parameter, r.status)
		}
	}
	if r := request(ownerActor.client, "GET", management+"?page=%GG", "", "", nil); r.status != 400 {
		t.Errorf("raw malformed credential page encoding=%d want=400", r.status)
	}

	type link struct {
		URL     *url.URL
		Results string
		Cursor  string
	}
	links := func(header string) map[string]link {
		t.Helper()
		result := map[string]link{}
		for _, entry := range strings.Split(header, ",") {
			parts := strings.Split(entry, ";")
			if len(parts) < 4 {
				t.Fatalf("missing pagination Link attributes: %s", header)
			}
			u, err := url.Parse(strings.Trim(strings.TrimSpace(parts[0]), "<>"))
			if err != nil {
				t.Fatal(err)
			}
			attributes := map[string]string{}
			for _, part := range parts[1:] {
				key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
				if ok {
					attributes[key] = strings.Trim(value, `"`)
				}
			}
			if u.Scheme != "http" || u.Host != "public.example.test" || u.Path != cfg.Prefix+listPath || u.Query().Get("query") != "fIlTeR %" || u.Query().Get("per_page") != "2" || u.Query().Get("cursor") != attributes["cursor"] {
				t.Fatalf("Link lost public origin/prefix or original query/size: %s", header)
			}
			result[attributes["rel"]] = link{u, attributes["results"], attributes["cursor"]}
		}
		if _, previous := result["previous"]; !previous {
			t.Fatalf("missing previous Link: %s", header)
		}
		if _, next := result["next"]; !next {
			t.Fatalf("missing next Link: %s", header)
		}
		return result
	}
	first := want("filtered first page", request(http.DefaultClient, "GET", listPath+"?query="+url.QueryEscape("fIlTeR %")+"&per_page=2&cursor=", "", "", bearer(all.Token)), 200)
	var page []projectMetadata
	decode(first.raw, &page)
	if len(page) != 2 || page[0].ID != projects[0].ID || page[1].ID != projects[1].ID {
		t.Fatal("first page did not apply literal case-insensitive filtering and numeric ordering")
	}
	firstLinks := links(first.header.Get("Link"))
	if firstLinks["previous"].Results != "false" || firstLinks["next"].Results != "true" || firstLinks["next"].Cursor != cursor("offset:2") {
		t.Fatal("first page Link results/cursor do not describe the filtered result")
	}
	second := want("follow next public Link", request(http.DefaultClient, "GET", strings.TrimPrefix(firstLinks["next"].URL.RequestURI(), cfg.Prefix), "", "", bearer(all.Token)), 200)
	decode(second.raw, &page)
	if len(page) != 1 || page[0].ID != projects[2].ID {
		t.Fatal("next Link did not continue the filtered authorized result")
	}
	secondLinks := links(second.header.Get("Link"))
	if secondLinks["previous"].Results != "true" || secondLinks["next"].Results != "false" || secondLinks["previous"].Cursor != cursor("offset:0") {
		t.Fatal("last page Link results/cursor are incorrect")
	}
	back := readProjects(strings.TrimPrefix(secondLinks["previous"].URL.RequestURI(), cfg.Prefix), bearer(all.Token))
	if len(back) != 2 || back[0].ID != projects[0].ID || back[1].ID != projects[1].ID {
		t.Fatal("previous Link did not return the first page")
	}
	maximum := want("maximum allowed project cursor", request(http.DefaultClient, "GET", listPath+"?query="+url.QueryEscape("fIlTeR %")+"&per_page=2&cursor="+cursor("offset:100000000"), "", "", bearer(all.Token)), 200)
	maximumNext := links(maximum.header.Get("Link"))["next"]
	if maximumNext.Results != "false" {
		t.Fatal("maximum offset must not advertise more filtered results")
	}
	afterMaximum := readProjects(strings.TrimPrefix(maximumNext.URL.RequestURI(), cfg.Prefix), bearer(all.Token))
	if len(afterMaximum) != 0 {
		t.Fatal("following the maximum-offset next Link must remain a valid empty page")
	}

	for range 26 {
		issue(ownerActor, `{"name":"Duplicate pagination label"}`)
	}
	type tokenPage struct {
		Data     []credential `json:"data"`
		Page     int          `json:"page"`
		PageSize int          `json:"pageSize"`
		HasMore  bool         `json:"hasMore"`
	}
	var newest, older tokenPage
	firstMetadata := want("credential first page", request(ownerActor.client, "GET", management, "", "", nil), 200)
	decode(firstMetadata.raw, &newest)
	secondMetadata := want("credential second page", request(ownerActor.client, "GET", management+"?page=2", "", "", nil), 200)
	decode(secondMetadata.raw, &older)
	if newest.Page != 1 || newest.PageSize != 25 || len(newest.Data) != 25 || !newest.HasMore || older.Page != 2 || older.PageSize != 25 || len(older.Data) != len(issued)-25 || older.HasMore {
		t.Fatal("native credential pagination metadata or row limits are incorrect")
	}
	metadata := append(slices.Clone(newest.Data), older.Data...)
	for i, item := range metadata {
		if item.ID != issued[len(issued)-1-i].ID || item.Token != "" {
			t.Fatal("credential list was not newest-first metadata without tokens")
		}
	}
	for _, r := range []response{firstMetadata, secondMetadata} {
		for _, c := range issued {
			if strings.Contains(string(r.raw), c.Token) {
				t.Fatal("credential list redisclosed an issued token")
			}
		}
		for _, key := range []string{`"token"`, `"hash"`, `"tokenHash"`, `"token_hash"`} {
			if strings.Contains(string(r.raw), key) {
				t.Fatal("credential list disclosed a secret or hash field")
			}
		}
	}
	want("foreign credential ID", request(ownerActor.client, "DELETE", "/api/v1/organizations/foreign-tokens/api-tokens/"+all.ID, "", ownerActor.csrf, nil), 404)
	want("admin revokes credential", request(adminActor.client, "DELETE", management+"/"+all.ID, "", adminActor.csrf, nil), 204)
	want("repeat revoke", request(ownerActor.client, "DELETE", management+"/"+all.ID, "", ownerActor.csrf, nil), 204)
	want("revoked token consumption", request(http.DefaultClient, "GET", listPath, "", "", bearer(all.Token)), 401)
	want("cookie cannot rescue revoked bearer", request(ownerActor.client, "GET", listPath, "", "", bearer(all.Token)), 401)
	want("absent credential ID", request(ownerActor.client, "DELETE", management+"/9223372036854775807", "", ownerActor.csrf, nil), 404)
}
