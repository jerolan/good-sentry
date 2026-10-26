//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// HTTP owns actual credential consumers, exact wire identities and public
// pagination. Provider tests own release persistence, observation and locking.
func TestReleaseHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	schema := fmt.Sprintf("goodsentry_release_http_%d", time.Now().UnixNano())
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
	res, err := http.Get(srv.URL + cfg.Prefix + "/api/0/organizations/releases-http/releases/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("anonymous release collection=%d want=401 body=%s", res.StatusCode, raw)
	}
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	type response struct {
		code   int
		header http.Header
		raw    []byte
	}
	request := func(client *http.Client, method, path, body string, headers http.Header) response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
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
		return response{res.StatusCode, res.Header.Clone(), raw}
	}
	want := func(label string, r response, code int) response {
		t.Helper()
		if r.code != code {
			t.Fatalf("%s=%d want=%d body=%s", label, r.code, code, r.raw)
		}
		return r
	}
	encode := func(value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	decode := func(r response, value any) {
		t.Helper()
		if err := json.Unmarshal(r.raw, value); err != nil {
			t.Fatalf("response JSON: %v", err)
		}
	}
	const password = "release-http-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@releases.test", hash, "Owner", "Release HTTP")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"admin", "member"} {
		var id string
		if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text", role+"@releases.test", hash, role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3)", org.ID, id, role); err != nil {
			t.Fatal(err)
		}
	}
	projects := make([]store.Project, 3)
	for i := range projects {
		projects[i], err = db.CreateProject(ctx, owner.ID, org.Slug, fmt.Sprintf("Release project %d", i), "javascript")
		if err != nil {
			t.Fatal(err)
		}
	}
	var foreignOrg string
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('release-foreign','Foreign') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := db.CreateProject(ctx, owner.ID, "release-foreign", "Foreign release project", "go")
	if err != nil {
		t.Fatal(err)
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
		r := want("login", request(client, "POST", "/api/v1/login", encode(map[string]string{"email": email, "password": password}), nil), 200)
		var result struct{ CSRFToken string }
		decode(r, &result)
		if result.CSRFToken == "" {
			t.Fatal("login omitted CSRF token")
		}
		return actor{client, result.CSRFToken}
	}
	ownerActor, adminActor, memberActor := login(owner.Email), login("admin@releases.test"), login("member@releases.test")
	csrf := func(who actor) http.Header { return http.Header{"X-CSRF-Token": {who.csrf}} }
	bearer := func(value string) http.Header { return http.Header{"Authorization": {"Bearer " + value}} }
	issue := func(scopes, projectIDs []string) string {
		t.Helper()
		body := map[string]any{"name": "Release HTTP credential"}
		if scopes != nil {
			body["scopes"] = scopes
		}
		if projectIDs != nil {
			body["projectIds"] = projectIDs
		}
		r := want("issue actual credential", request(ownerActor.client, "POST", "/api/v1/organizations/"+org.Slug+"/api-tokens", encode(body), csrf(ownerActor)), 201)
		var result struct{ Token string }
		decode(r, &result)
		if !strings.HasPrefix(result.Token, "gs_") {
			t.Fatal("issuance omitted credential")
		}
		return result.Token
	}
	readToken := issue(nil, nil)
	writeToken := issue([]string{"project:releases", "org:read"}, nil)
	writeOnly := issue([]string{"project:releases"}, nil)
	restricted := issue([]string{"project:releases", "org:read"}, []string{projects[0].ID})
	hidden := issue([]string{"project:read"}, []string{projects[2].ID})
	hiddenWriter := issue([]string{"project:releases"}, []string{projects[2].ID})
	call := func(method, path, body, secret string) response {
		return request(http.DefaultClient, method, path, body, bearer(secret))
	}
	collection := "/api/0/organizations/" + org.Slug + "/releases/"
	projectCollection := "/api/0/projects/" + org.ID + "/" + projects[0].ID + "/releases/"
	want("unregistered organization project descendant", call("GET", "/api/0/organizations/"+org.Slug+"/projects/unregistered/", "", readToken), 404)
	want("unregistered project release descendant", call("GET", "/api/0/projects/"+org.Slug+"/"+projects[0].Slug+"/releases/missing/unregistered/", "", readToken), 404)
	version := " Release/版本 @1 "
	detail := collection + url.PathEscape(version) + "/"
	projectDetail := projectCollection + url.PathEscape(version) + "/"
	create := func(version string, refs []string, extra map[string]any) string {
		body := map[string]any{"version": version}
		if refs != nil {
			body["projects"] = refs
		}
		for key, value := range extra {
			body[key] = value
		}
		return encode(body)
	}
	type wireDeploy struct {
		ID           int64      `json:"id"`
		DateCreated  time.Time  `json:"dateCreated"`
		DateStarted  *time.Time `json:"dateStarted"`
		DateFinished time.Time  `json:"dateFinished"`
		Projects     []string   `json:"projects"`
	}
	type wireRelease struct {
		store.Release
		LastDeploy *wireDeploy `json:"lastDeploy"`
	}
	var shared wireRelease
	decode(want("create shared exact version", call("POST", collection, create(version, []string{projects[0].Slug, projects[1].ID}, map[string]any{"ref": "first", "dateStarted": "2024-01-02T03:04:05.123456789+02:00"}), writeToken), 201), &shared)
	if string(shared.Version) != version || string(shared.ShortVersion) != version || shared.ID == "" || shared.Status != "open" || shared.DateReleased != nil || !shared.CanWrite || len(shared.Projects) != 2 || shared.DateStarted == nil || shared.DateStarted.Format(time.RFC3339Nano) != "2024-01-02T01:04:05.123456Z" {
		t.Fatalf("release response version=%q shortVersion=%q id=%q status=%q released=%v canWrite=%v projects=%d started=%v", shared.Version, shared.ShortVersion, shared.ID, shared.Status, shared.DateReleased, shared.CanWrite, len(shared.Projects), shared.DateStarted)
	}
	want("existing release merge", call("POST", collection, create(version, []string{projects[0].ID}, nil), writeToken), 208)
	var archived wireRelease
	archiveBody := create(version, []string{}, map[string]any{"status": "archived"})
	restoreBody := create(version, []string{}, map[string]any{"status": "open"})
	for _, change := range []struct{ label, body, status string }{
		{"CLI archive existing release", archiveBody, "archived"},
		{"CLI restore existing release", restoreBody, "open"},
	} {
		decode(want(change.label, call("POST", collection, change.body, writeToken), 208), &archived)
		if archived.Status != change.status || len(archived.Projects) != 2 || archived.Projects[0].ID != projects[0].ID || archived.Projects[1].ID != projects[1].ID {
			t.Fatal("collection status update lost its status or changed release associations")
		}
	}
	want("collection status update requires full coverage", call("POST", collection, archiveBody, restricted), 403)
	want("collection status update hides inaccessible release", call("POST", collection, archiveBody, hiddenWriter), 404)
	want("collection status update requires existing release", call("POST", collection, create("Missing archive version", []string{}, map[string]any{"status": "archived"}), writeToken), 404)
	want("project collection cannot use empty projects for status", call("POST", projectCollection, archiveBody, writeToken), 400)
	decode(want("denied collection status update preserved release", call("GET", detail, "", writeToken), 200), &archived)
	if archived.Status != "open" || len(archived.Projects) != 2 || archived.Projects[0].ID != projects[0].ID || archived.Projects[1].ID != projects[1].ID {
		t.Fatal("denied collection status update mutated the release")
	}
	var finalized wireRelease
	finalizeBody := encode(map[string]any{"projects": []string{projects[0].Slug}, "dateReleased": "2024-01-02T03:04:05Z"})
	decode(want("CLI finalize with project assertion", call("PUT", detail, finalizeBody, writeToken), 200), &finalized)
	if finalized.DateReleased == nil || finalized.DateReleased.Format(time.RFC3339Nano) != "2024-01-02T03:04:05Z" || len(finalized.Projects) != 2 || finalized.Projects[0].ID != projects[0].ID || finalized.Projects[1].ID != projects[1].ID {
		t.Fatal("CLI finalize changed associations or lost the asserted release date")
	}
	decode(want("project path finalize assertion", call("PUT", projectDetail, finalizeBody, writeToken), 200), &finalized)
	if len(finalized.Projects) != 2 {
		t.Fatal("project path assertion removed another release association")
	}
	want("unassociated project assertion", call("PUT", detail, encode(map[string]any{"projects": []string{projects[2].ID}, "ref": "must-not-change"}), writeToken), 404)
	want("foreign project assertion", call("PUT", detail, encode(map[string]any{"projects": []string{foreignProject.ID}, "ref": "must-not-change"}), writeToken), 404)
	want("project path conflicting assertion", call("PUT", projectDetail, encode(map[string]any{"projects": []string{projects[1].ID}}), writeToken), 400)
	var readOnlyView wireRelease
	decode(want("numeric project family exact detail", call("GET", projectDetail, "", readToken), 200), &readOnlyView)
	if readOnlyView.CanWrite {
		t.Fatal("read-only owner bearer was offered release write controls")
	}
	want("project create derives association", call("POST", projectCollection, create("Project-only version", nil, nil), writeOnly), 201)
	want("project create same project", call("POST", projectCollection, create("Project-only version", []string{projects[0].Slug}, nil), writeToken), 208)
	want("project create conflicting project", call("POST", projectCollection, create("Conflict", []string{projects[1].ID}, nil), writeToken), 400)
	want("project family update", call("PUT", projectDetail, `{"ref":"first"}`, writeToken), 200)
	for _, mutation := range []struct{ method, path, body string }{
		{"POST", collection, create(version, []string{projects[0].ID}, nil)}, {"PUT", detail, `{"ref":"forbidden"}`}, {"DELETE", detail, ""},
		{"POST", projectCollection, create(version, nil, nil)}, {"PUT", projectDetail, `{"ref":"forbidden"}`}, {"DELETE", projectDetail, ""},
		{"POST", detail + "commits/", `[]`}, {"POST", detail + "deploys/", `{"environment":"prod"}`},
	} {
		want("default read cannot mutate "+mutation.method+" "+mutation.path, call(mutation.method, mutation.path, mutation.body, readToken), 403)
	}
	want("release scope can read", call("GET", detail, "", writeOnly), 200)
	want("release scope does not grant project listing", call("GET", "/api/0/organizations/"+org.Slug+"/projects/", "", writeOnly), 403)
	want("repository scope required", call("GET", "/api/0/organizations/"+org.Slug+"/repos/", "", writeOnly), 403)
	want("member cookie read", request(memberActor.client, "GET", detail, "", nil), 200)
	want("member cookie mutation", request(memberActor.client, "PUT", detail, `{"ref":"forbidden"}`, csrf(memberActor)), 403)
	want("cookie mutation CSRF", request(ownerActor.client, "PUT", detail, `{"ref":"forbidden"}`, nil), 403)
	badOrigin := csrf(ownerActor)
	badOrigin.Set("Origin", "https://foreign.example.test")
	want("cookie mutation origin", request(ownerActor.client, "PUT", detail, `{"ref":"forbidden"}`, badOrigin), 403)
	want("bad bearer cannot use owner cookie", request(ownerActor.client, "GET", detail, "", http.Header{"Authorization": {"Bearer invalid"}}), 401)
	want("SDK key cannot manage releases", call("GET", detail, "", projects[0].PublicKey), 401)
	want("admin cookie mutation", request(adminActor.client, "PUT", detail, `{"status":"archived","dateReleased":"2020-01-01T00:00:00Z"}`, csrf(adminActor)), 200)
	var visible wireRelease
	decode(want("restricted visible release", call("GET", detail, "", restricted), 200), &visible)
	if visible.CanWrite || len(visible.Projects) != 1 || visible.Projects[0].ID != projects[0].ID {
		t.Fatal("shared release response leaked projects or write coverage")
	}
	for _, method := range []string{"PUT", "DELETE"} {
		want("partial coverage "+method, call(method, detail, `{"ref":"forbidden"}`, restricted), 403)
	}
	for _, path := range []string{detail, projectDetail} {
		want("project assertions retain full coverage", call("PUT", path, encode(map[string]any{"projects": []string{projects[0].Slug}, "ref": "must-not-change"}), restricted), 403)
	}
	want("existing POST requires full coverage", call("POST", collection, create(version, []string{projects[0].ID}, nil), restricted), 403)
	want("fully hidden release", call("GET", detail, "", hidden), 404)
	var hiddenReleases []wireRelease
	decode(want("hidden release collection", call("GET", collection, "", hidden), 200), &hiddenReleases)
	if len(hiddenReleases) != 0 {
		t.Fatal("release collection exposed associations outside the issued project grant")
	}
	want("other organization grant", call("GET", "/api/0/organizations/release-foreign/releases/", "", writeToken), 404)
	want("foreign requested project", call("POST", collection, create("Foreign selection", []string{projects[0].ID, foreignProject.ID}, nil), writeToken), 404)
	decode(want("clear supplied dates only", call("PUT", detail, `{"dateStarted":null,"dateReleased":null}`, writeToken), 200), &visible)
	if visible.DateStarted != nil || visible.DateReleased != nil || visible.Ref == nil || *visible.Ref != "first" || visible.Status != "archived" {
		t.Fatal("nullable update changed omitted fields")
	}

	commitPath := detail + "commits/"
	commitBody := `[{"id":"head","repository":"wire-repo","author_name":"Wire author","author_email":"wire@example.invalid","timestamp":"2024-01-01T01:02:03.123456789+02:00","patch_set":[{"path":"../metadata-only.js","type":"M"}],"receipt":9223372036854775807}]`
	var commitResult wireRelease
	decode(want("bare commit metadata", call("POST", commitPath, commitBody, writeToken), 200), &commitResult)
	if commitResult.CommitCount != 1 || commitResult.LastCommit == nil || commitResult.LastCommit.ID != "head" {
		t.Fatal("commit POST did not return the actual updated release summary")
	}
	var commits []store.ReleaseCommit
	decode(want("project commits family", call("GET", projectDetail+"commits/", "", readToken), 200), &commits)
	if len(commits) != 1 || commits[0].AuthorName != "Wire author" || commits[0].AuthorEmail != "wire@example.invalid" || commits[0].DateCreated.Format(time.RFC3339Nano) != "2023-12-31T23:02:03.123456Z" || len(commits[0].PatchSet) != 1 || commits[0].PatchSet[0].Path != "../metadata-only.js" {
		t.Fatal("CLI aliases or commit timestamp were not preserved")
	}
	want("omitted commits preserve metadata", call("PUT", detail, `{"url":null}`, writeToken), 200)
	decode(want("commits after metadata update", call("GET", commitPath, "", readToken), 200), &commits)
	if len(commits) != 1 || commits[0].ID != "head" {
		t.Fatal("omitted inline commits cleared the existing set")
	}
	var original map[string]any
	decoder := json.NewDecoder(bytes.NewReader(commits[0].Data))
	decoder.UseNumber()
	if err := decoder.Decode(&original); err != nil {
		t.Fatal(err)
	}
	if number, ok := original["receipt"].(json.Number); !ok || number.String() != "9223372036854775807" {
		t.Fatal("raw commit metadata lost its int64 JSON number")
	}
	var repositories []store.ReleaseRepository
	decode(want("actual repository metadata", call("GET", "/api/0/organizations/"+org.Slug+"/repos/", "", writeToken), 200), &repositories)
	if len(repositories) != 1 || repositories[0].ID == "" || repositories[0].Name != "wire-repo" || repositories[0].DateCreated.IsZero() || repositories[0].Provider.ID != "manual" || repositories[0].Provider.Name != "Manual commit metadata" || repositories[0].Status != "active" {
		t.Fatal("repository endpoint did not describe actual manual metadata")
	}
	want("hidden repository release", call("POST", collection, create("Hidden repo release", []string{projects[2].ID}, map[string]any{"commits": []map[string]any{{"id": "hidden-commit", "repository": "hidden-repo"}}}), writeToken), 201)
	var restrictedRepositories []store.ReleaseRepository
	decode(want("restricted repository list", call("GET", "/api/0/organizations/"+org.Slug+"/repos/", "", restricted), 200), &restrictedRepositories)
	if len(restrictedRepositories) != 1 || restrictedRepositories[0].Name != "wire-repo" {
		t.Fatalf("repository from an invisible project leaked: %+v", restrictedRepositories)
	}
	var orderedRepositories []store.ReleaseRepository
	decode(want("ordered repository list", call("GET", "/api/0/organizations/"+org.Slug+"/repos/", "", writeToken), 200), &orderedRepositories)
	if len(orderedRepositories) != 2 {
		t.Fatalf("ordered repository list=%+v", orderedRepositories)
	}
	previousRepositoryID := int64(0)
	for _, repository := range orderedRepositories {
		id, err := strconv.ParseInt(repository.ID, 10, 64)
		if err != nil || id <= previousRepositoryID {
			t.Fatalf("repositories are not ordered by numeric id: %+v", orderedRepositories)
		}
		previousRepositoryID = id
	}
	want("no earlier commit release", call("GET", detail+"previous-with-commits/", "", writeToken), 404)
	want("create previous candidate", call("POST", collection, create("Previous head", []string{projects[0].ID}, map[string]any{"commits": []map[string]any{{"id": "prior-head", "repository": "wire-repo"}}}), writeToken), 201)
	want("create target after prior", call("POST", projectCollection, create("Next head", nil, nil), writeToken), 201)
	var previous wireRelease
	decode(want("actual previous head", call("GET", collection+"Next%20head/previous-with-commits/", "", writeToken), 200), &previous)
	if string(previous.Version) != "Previous head" || previous.LastCommit == nil || previous.LastCommit.ID != "prior-head" {
		t.Fatal("previous-with-commits did not return the actual prior head")
	}
	deployPath := detail + "deploys/"
	var deploy wireDeploy
	decode(want("completed deploy null finish", call("POST", deployPath, encode(map[string]any{"environment": "production", "dateStarted": "2020-01-01T00:00:00+03:00", "dateFinished": nil, "projects": []string{projects[0].ID}}), restricted), 201), &deploy)
	if deploy.ID <= 0 || deploy.DateFinished.IsZero() || deploy.DateCreated.IsZero() || deploy.DateStarted == nil || deploy.DateStarted.Format(time.RFC3339Nano) != "2019-12-31T21:00:00Z" || len(deploy.Projects) != 1 || deploy.Projects[0] != projects[0].Slug {
		t.Fatal("deploy response must expose actual projects and a concrete completed finish")
	}
	want("implicit deploy projects require full coverage", call("POST", deployPath, `{"environment":"production"}`, restricted), 403)
	var deploys []wireDeploy
	decode(want("actual deploy listing", call("GET", deployPath, "", readToken), 200), &deploys)
	if len(deploys) != 1 || deploys[0].ID != deploy.ID || deploys[0].DateFinished.IsZero() || len(deploys[0].Projects) != 1 || deploys[0].Projects[0] != projects[0].Slug {
		t.Fatal("deploy listing lost completed metadata")
	}
	var deployedRelease wireRelease
	decode(want("compatible release last deploy", call("GET", detail, "", readToken), 200), &deployedRelease)
	if deployedRelease.LastDeploy == nil || deployedRelease.LastDeploy.ID != deploy.ID || len(deployedRelease.LastDeploy.Projects) != 1 || deployedRelease.LastDeploy.Projects[0] != projects[0].Slug {
		t.Fatal("release last deploy lost compatible project slug metadata")
	}
	var nativeDeployPage struct {
		Data []store.ReleaseDeploy `json:"data"`
	}
	decode(want("native deploy project metadata", request(memberActor.client, "GET", "/api/v1/organizations/"+org.Slug+"/releases/"+url.PathEscape(version)+"/deploys", "", nil), 200), &nativeDeployPage)
	if len(nativeDeployPage.Data) != 1 || nativeDeployPage.Data[0].ID != deploy.ID || len(nativeDeployPage.Data[0].Projects) != 1 || nativeDeployPage.Data[0].Projects[0].ID != projects[0].ID || nativeDeployPage.Data[0].Projects[0].Slug != projects[0].Slug || nativeDeployPage.Data[0].Projects[0].Name != projects[0].Name {
		t.Fatal("native deploy lost its project metadata objects")
	}
	var environmentReleases []wireRelease
	decode(want("deploy environment release filter", call("GET", collection+"?environment=production", "", readToken), 200), &environmentReleases)
	if len(environmentReleases) != 1 || string(environmentReleases[0].Version) != version {
		t.Fatal("environment query lost its release association")
	}
	decode(want("environment filter is exact", call("GET", collection+"?environment=Production", "", readToken), 200), &environmentReleases)
	if len(environmentReleases) != 0 {
		t.Fatal("environment filtering ignored exact case")
	}

	invalidCreates := map[string]any{
		"missing version":    map[string]any{"projects": []string{projects[0].ID}},
		"unknown body field": map[string]any{"version": "bad", "projects": []string{projects[0].ID}, "unknown": true},
		"empty projects":     map[string]any{"version": "bad", "projects": []string{}},
		"number version":     map[string]any{"version": 1, "projects": []string{projects[0].ID}},
	}
	tooManyProjects := make([]string, 1001)
	for i := range tooManyProjects {
		tooManyProjects[i] = projects[0].ID
	}
	invalidCreates["too many projects"] = map[string]any{"version": "bad", "projects": tooManyProjects}
	for _, value := range []string{"", ".", "..", "bad\x01version", strings.Repeat("界", 256)} {
		invalidCreates["version "+value] = map[string]any{"version": value, "projects": []string{projects[0].ID}}
	}
	for label, body := range invalidCreates {
		want("invalid release create "+label, call("POST", collection, encode(body), writeToken), 400)
	}
	for label, body := range map[string]string{
		"version immutable": `{"version":"changed"}`, "empty project assertion": `{"projects":[]}`, "null project assertion": `{"projects":null}`, "unsupported refs": `{"refs":[]}`, "unsupported head": `{"headCommits":[]}`, "unsupported owner": `{"owner":"owner"}`,
		"number ref": `{"ref":1}`, "invalid status": `{"status":"released"}`, "number date": `{"dateReleased":1}`, "missing date offset": `{"dateReleased":"2024-01-01T00:00:00"}`, "year zero": `{"dateReleased":"0000-01-01T00:00:00Z"}`, "UTC year overflow": `{"dateReleased":"9999-12-31T23:59:59-14:00"}`, "null commits": `{"commits":null}`, "extra JSON": `{} {}`,
		"long ref": encode(map[string]string{"ref": strings.Repeat("界", 2001)}), "long URL": encode(map[string]string{"url": strings.Repeat("x", 2001)}),
	} {
		want("invalid update "+label, call("PUT", detail, body, writeToken), 400)
	}
	invalidCommits := map[string]string{
		"not array": `{}`, "null set": `null`, "missing ID": `[{}]`, "numeric ID": `[{"id":1}]`, "duplicate pair": `[{"id":"same"},{"id":"same"}]`,
		"author conflict": `[{"id":"c","authorName":"one","author_name":"two"}]`, "email conflict": `[{"id":"c","authorEmail":"one","author_email":"two"}]`,
		"timestamp conflict": `[{"id":"c","dateCreated":"2020-01-01T00:00:00Z","timestamp":"2021-01-01T00:00:00Z"}]`, "null timestamp": `[{"id":"c","timestamp":null}]`,
		"patch conflict": `[{"id":"c","patchSet":[],"patch_set":[{"path":"x","type":"M"}]}]`, "patch NUL": `[{"id":"c","patch_set":[{"path":"bad\u0000path","type":"M"}]}]`,
	}
	invalidCommits["large integer patch alias conflict"] = `[{"id":"alias-case","patchSet":[{"path":"x.js","type":"M","receipt":9007199254740992}],"patch_set":[{"path":"x.js","type":"M","receipt":9007199254740993}]}]`
	for field, bound := range map[string]int{"id": 200, "repository": 200, "message": 10000, "authorName": 200, "authorEmail": 320} {
		invalidCommits["long "+field] = encode([]map[string]any{{"id": "c", field: strings.Repeat("界", bound+1)}})
	}
	invalidCommits["long patch path"] = encode([]map[string]any{{"id": "c", "patchSet": []map[string]string{{"path": strings.Repeat("x", 4097), "type": "M"}}}})
	invalidCommits["long patch type"] = encode([]map[string]any{{"id": "c", "patchSet": []map[string]string{{"path": "x", "type": strings.Repeat("M", 17)}}}})
	tooManyCommits := make([]map[string]string, 1001)
	for i := range tooManyCommits {
		tooManyCommits[i] = map[string]string{"id": fmt.Sprintf("extra-%d", i)}
	}
	invalidCommits["too many commits"] = encode(tooManyCommits)
	tooManyPatches := make([]map[string]string, 1001)
	for i := range tooManyPatches {
		tooManyPatches[i] = map[string]string{"path": "metadata.js", "type": "M"}
	}
	invalidCommits["too many patches"] = encode([]map[string]any{{"id": "c", "patchSet": tooManyPatches}})
	for label, body := range invalidCommits {
		want("invalid commit "+label, call("POST", commitPath, body, writeToken), 400)
	}
	var hugeAlias wireRelease
	decode(want("matching large exponent patch aliases", call("POST", commitPath, `[{"id":"huge-alias","patchSet":[{"path":"x.js","type":"M","receipt":1e1000}],"patch_set":[{"path":"x.js","type":"M","receipt":1e1000}]}]`, writeToken), 200), &hugeAlias)
	if hugeAlias.CommitCount != 1 || hugeAlias.LastCommit == nil || hugeAlias.LastCommit.ID != "huge-alias" {
		t.Fatal("matching large JSON number aliases did not produce actual commit metadata")
	}
	var camel wireRelease
	decode(want("camelCase inline commit metadata", call("PUT", detail, `{"commits":[{"id":"camel","authorName":"Camel author","authorEmail":"camel@example.invalid","dateCreated":"2024-01-01T01:00:00+01:00","patchSet":[]}]}`, writeToken), 200), &camel)
	if camel.LastCommit == nil || camel.LastCommit.ID != "camel" || camel.LastCommit.AuthorName != "Camel author" || camel.LastCommit.DateCreated.Format(time.RFC3339Nano) != "2024-01-01T00:00:00Z" {
		t.Fatal("camelCase aliases did not produce actual lastCommit metadata")
	}
	want("explicit empty commits clear", call("POST", commitPath, `[]`, writeToken), 200)
	decode(want("cleared commit list", call("GET", commitPath, "", readToken), 200), &commits)
	if len(commits) != 0 {
		t.Fatal("explicit empty array did not clear commit metadata")
	}
	for label, body := range map[string]string{
		"empty environment": `{"environment":""}`, "numeric environment": `{"environment":1}`, "unknown deploy field": `{"environment":"prod","unknown":true}`, "empty projects": `{"environment":"prod","projects":[]}`,
		"finish before start": `{"environment":"prod","dateStarted":"2024-01-02T00:00:00Z","dateFinished":"2024-01-01T00:00:00Z"}`,
		"long environment":    encode(map[string]string{"environment": strings.Repeat("界", 65)}), "long name": encode(map[string]string{"environment": "prod", "name": strings.Repeat("界", 201)}), "long URL": encode(map[string]string{"environment": "prod", "url": strings.Repeat("x", 2001)}),
	} {
		want("invalid deploy "+label, call("POST", deployPath, body, writeToken), 400)
	}
	for _, suffix := range []string{"?per_page=0", "?per_page=01", "?per_page=101", "?per_page=", "?limit=", "?limit=01", "?limit=2&per_page=1", "?per_page=%GG", "?cursor=%GG", "?query=%GG", "?cursor=b2Zmc2V0OjAx", "?cursor=b2Zmc2V0OjA=", "?query=" + url.QueryEscape(strings.Repeat("界", 201))} {
		want("invalid release query "+suffix, call("GET", collection+suffix, "", readToken), 400)
	}
	want("project route conflicting filter", call("GET", projectCollection+"?project="+projects[1].ID, "", readToken), 400)
	for _, suffix := range []string{"?page=", "?page=01", "?page=1000001", "?page=%GG"} {
		want("invalid native release page", request(ownerActor.client, "GET", "/api/v1/organizations/"+org.Slug+"/releases"+suffix, "", nil), 400)
	}

	for i := 0; i < 26; i++ {
		want("paged release fixture", call("POST", projectCollection, create(fmt.Sprintf("HTTP page %% %02d", i), nil, nil), writeToken), 201)
	}
	query := "hTtP PAGE %"
	first := want("limit alias literal filtered release list", call("GET", collection+"?query="+url.QueryEscape(query)+"&limit=1&cursor=", "", readToken), 200)
	var releasePage []wireRelease
	decode(first, &releasePage)
	if len(releasePage) != 1 || string(releasePage[0].Version) != "HTTP page % 25" {
		t.Fatal("release list lost literal filtering or newest ordering")
	}
	link := func(header, relation, path string, results bool) *url.URL {
		t.Helper()
		for _, entry := range strings.Split(header, ",") {
			if !strings.Contains(entry, `rel="`+relation+`"`) {
				continue
			}
			entry = strings.TrimSpace(entry)
			end := strings.Index(entry, ">")
			if end < 0 || !strings.Contains(entry, `results="`) || !strings.Contains(entry, `cursor="`) {
				t.Fatal("incomplete pagination Link")
			}
			value, err := url.Parse(entry[1:end])
			if err != nil {
				t.Fatal(err)
			}
			decodedPath, err := url.PathUnescape(cfg.Prefix + path)
			if err != nil {
				t.Fatal(err)
			}
			if value.Scheme != "http" || value.Host != "public.example.test" || value.Path != decodedPath {
				t.Fatal("pagination Link lost public domain/prefix or exact encoded version")
			}
			if !strings.Contains(entry, fmt.Sprintf(`results="%t"`, results)) || !strings.Contains(entry, `cursor="`+value.Query().Get("cursor")+`"`) {
				t.Fatal("pagination Link results/cursor attributes do not describe its actual page")
			}
			return value
		}
		t.Fatal("missing pagination relation " + relation)
		return nil
	}
	next := link(first.header.Get("Link"), "next", collection, true)
	if next.Query().Get("query") != query || next.Query().Get("limit") != "1" || next.Query().Get("cursor") != base64.RawURLEncoding.EncodeToString([]byte("offset:1")) {
		t.Fatal("release Link lost filter/alias/page size")
	}
	decode(want("follow release next Link", call("GET", strings.TrimPrefix(next.RequestURI(), cfg.Prefix), "", readToken), 200), &releasePage)
	if len(releasePage) != 1 || string(releasePage[0].Version) != "HTTP page % 24" {
		t.Fatal("release Link did not continue authorized result")
	}
	type nativePage struct {
		Data     []json.RawMessage `json:"data"`
		Page     int               `json:"page"`
		PageSize int               `json:"pageSize"`
		HasMore  bool              `json:"hasMore"`
	}
	native := func(path string, expected, pageNumber int, more bool) {
		t.Helper()
		var page nativePage
		decode(want("native release paging", request(memberActor.client, "GET", path, "", nil), 200), &page)
		if len(page.Data) != expected || page.PageSize != 25 || page.HasMore != more || page.Page != pageNumber {
			t.Fatalf("native page shape=%+v", page)
		}
	}
	nativeBase := "/api/v1/organizations/" + org.Slug + "/releases"
	for _, suffix := range []string{"", "/" + url.PathEscape(version) + "/commits", "/" + url.PathEscape(version) + "/deploys"} {
		want("native release reads remain session-only", call("GET", nativeBase+suffix, "", writeToken), 401)
	}
	native(nativeBase+"?query="+url.QueryEscape(query), 25, 1, true)
	native(nativeBase+"?query="+url.QueryEscape(query)+"&page=2", 1, 2, false)
	largeCommits := make([]map[string]any, 26)
	for i := range largeCommits {
		largeCommits[i] = map[string]any{"id": fmt.Sprintf("ordered-%02d", i)}
	}
	want("ordered commit fixture", call("POST", commitPath, encode(largeCommits), writeToken), 200)
	native(nativeBase+"/"+url.PathEscape(version)+"/commits", 25, 1, true)
	native(nativeBase+"/"+url.PathEscape(version)+"/commits?page=2", 1, 2, false)
	commitFirst := want("compatible commit pagination", call("GET", commitPath+"?per_page=1", "", readToken), 200)
	commitNext := link(commitFirst.header.Get("Link"), "next", commitPath, true)
	decode(want("follow encoded commit Link", call("GET", strings.TrimPrefix(commitNext.RequestURI(), cfg.Prefix), "", readToken), 200), &commits)
	if len(commits) != 1 || commits[0].ID != "ordered-01" {
		t.Fatal("commit Link changed replacement ordering")
	}
	for range 26 {
		want("paged deploy fixture", call("POST", deployPath, `{"environment":"prod"}`, writeToken), 201)
	}
	native(nativeBase+"/"+url.PathEscape(version)+"/deploys", 25, 1, true)
	native(nativeBase+"/"+url.PathEscape(version)+"/deploys?page=2", 2, 2, false)

	var multipartBody bytes.Buffer
	upload := func(release string) response {
		t.Helper()
		multipartBody.Reset()
		writer := multipart.NewWriter(&multipartBody)
		for key, value := range map[string]string{"name": "  ~/exact.js.map  ", "release": release, "dist": "  web  ", "debugId": "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"} {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		part, err := writer.CreateFormFile("file", "exact.js.map")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, `{"version":3,"sources":["a.js"],"names":[],"mappings":"AAAA","sourcesContent":["const x=1;"]}`); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		headers := csrf(ownerActor)
		headers.Set("Content-Type", writer.FormDataContentType())
		return request(ownerActor.client, "POST", "/api/v1/organizations/"+org.Slug+"/projects/"+projects[0].ID+"/sourcemaps", multipartBody.String(), headers)
	}
	exactVersion := " " + strings.Repeat("界", 253) + " "
	var uploaded struct {
		Artifact struct{ Release, Name, Dist, DebugID string }
	}
	decode(want("255 Unicode exact source-map release", upload(exactVersion), 201), &uploaded)
	if uploaded.Artifact.Release != exactVersion || uploaded.Artifact.Name != "~/exact.js.map" || uploaded.Artifact.Dist != "web" || uploaded.Artifact.DebugID != "aaaaaaaabbbbccccddddeeeeeeeeeeee" {
		t.Fatal("source-map upload changed exact release identity or other metadata normalization")
	}
	want("source map observed exact release", call("GET", collection+url.PathEscape(exactVersion)+"/", "", readToken), 200)
	want("256 Unicode source-map release", upload(" "+strings.Repeat("界", 254)+" "), 400)
	want("project DELETE removes only association", call("DELETE", projectDetail, "", restricted), 204)
	want("removed project association hidden", call("GET", projectDetail, "", writeToken), 404)
	decode(want("shared release remains", call("GET", detail, "", writeToken), 200), &visible)
	if len(visible.Projects) != 1 || visible.Projects[0].ID != projects[1].ID {
		t.Fatal("project DELETE removed the shared entity or wrong association")
	}
	want("organization DELETE", call("DELETE", detail, "", writeToken), 204)
	want("organization release absent after deletion", call("GET", detail, "", writeToken), 404)
}
