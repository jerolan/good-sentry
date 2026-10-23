//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

func setFixtureProjectTeam(t *testing.T, f *resolutionHTTPFixture, projectID, teamID string) {
	t.Helper()
	tx, err := f.sql.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `UPDATE projects SET team_id=$2::bigint WHERE id=$1::bigint`, projectID, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, `DELETE FROM project_teams WHERE project_id=$1::bigint`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, `INSERT INTO project_teams(organization_id,project_id,team_id,created_at)
		SELECT organization_id,id,$2::bigint,created_at FROM projects WHERE id=$1::bigint`, projectID, teamID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleManagementOrganizationAndProjectKeysHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	var defaultTeam string
	if err := f.sql.QueryRow(f.ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND slug='default'`, f.org.ID).Scan(&defaultTeam); err != nil {
		t.Fatal(err)
	}
	setFixtureProjectTeam(t, f, f.project.ID, defaultTeam)
	keysPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/keys/"
	orgPath := "/api/0/organizations/" + f.org.Slug + "/"
	f.request(t, http.DefaultClient, "GET", orgPath, "", nil, 401)
	f.request(t, http.DefaultClient, "GET", keysPath, "", nil, 401)

	var detail store.CompatibleOrganizationDetail
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgPath, "", nil, 200), &detail); err != nil || detail.ID != f.org.ID || len(detail.Projects) != 1 || len(detail.Teams) != 1 || detail.OpenMembership || !contains(detail.Access, "project:write") {
		t.Fatalf("organization detail HTTP: %+v %v", detail, err)
	}
	if detail.Projects[0].ID != f.project.ID || len(detail.Projects[0].Teams) != 1 || detail.Projects[0].Teams[0].Slug != "default" || detail.Teams[0].DateCreated.IsZero() {
		t.Fatalf("organization relation HTTP: %+v %+v", detail.Projects, detail.Teams)
	}

	var keys []compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, f.client, "GET", keysPath, "", nil, 200), &keys); err != nil || len(keys) != 1 || keys[0].ID == "" || keys[0].Public == "" || !keys[0].Enabled || keys[0].DSN.Public != keys[0].Public || keys[0].DSN.Secret != "" || !strings.Contains(keys[0].DSN.CSP, "/good/api/"+f.project.ID+"/security/?sentry_key=") {
		t.Fatalf("project key list/DSN: %+v %v", keys, err)
	}
	first := keys[0]
	var single compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, f.client, "GET", keysPath+first.ID+"/", "", nil, 200), &single); err != nil || single.ID != first.ID || single.Public != first.Public {
		t.Fatalf("project key detail: %+v %v", single, err)
	}
	for _, path := range []string{keysPath + "not-a-uuid/", keysPath + "00000000-0000-0000-0000-000000000000/"} {
		f.request(t, f.client, "GET", path, "", nil, 404)
	}
	for _, query := range []string{"unknown=x", "status=bogus", "limit=0", "limit=101", "cursor=bad"} {
		f.request(t, f.client, "GET", keysPath+"?"+query, "", nil, 400)
	}

	var created compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, f.client, "POST", keysPath, `{"name":"CI"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &created); err != nil || created.ID == "" || created.Name != "CI" || created.Label != "CI" || created.Public == "" || created.DSN.Public != created.Public {
		t.Fatalf("create key HTTP: %+v %v", created, err)
	}
	for _, body := range []string{
		`{"name":"one","label":"two"}`,
		`{"name":"` + strings.Repeat("x", 201) + `"}`,
		`{"name":"inactive at creation","isActive":false}`,
		`{"name":"x","unknown":true}`,
	} {
		f.request(t, f.client, "POST", keysPath, body, http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	}
	var updated compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, f.client, "PUT", keysPath+created.ID+"/", `{"label":"CI updated"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200), &updated); err != nil || updated.ID != created.ID || updated.Label != "CI updated" || updated.Public != created.Public {
		t.Fatalf("update key HTTP: %+v %v", updated, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", keysPath+created.ID+"/", `{"isActive":false}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200), &updated); err != nil || updated.Enabled {
		t.Fatalf("deactivate key HTTP: %+v %v", updated, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", keysPath+created.ID+"/", `{"label":"CI inactive"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200), &updated); err != nil || updated.Enabled || updated.Label != "CI inactive" {
		t.Fatalf("omitted isActive did not preserve inactive state: %+v %v", updated, err)
	}
	inactiveEnvelope := "{}\n{\"type\":\"event\"}\n" + `{"event_id":"e0000000000000000000000000000001","message":"inactive key rejection"}`
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+created.Public, inactiveEnvelope, nil, 403)
	if err := json.Unmarshal(f.request(t, f.client, "PUT", keysPath+created.ID+"/", `{"label":"CI restored","isActive":true}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200), &updated); err != nil || !updated.Enabled || updated.Label != "CI restored" {
		t.Fatalf("reactivate key HTTP: %+v %v", updated, err)
	}
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+created.Public, inactiveEnvelope, nil, 200)
	if err := json.Unmarshal(f.request(t, f.client, "GET", keysPath+"?limit=1", "", nil, 200), &keys); err != nil || len(keys) != 1 {
		t.Fatalf("key page: %+v %v", keys, err)
	}
	if link := keyLink(t, f, keysPath+"?limit=1"); !strings.Contains(link, "/good/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/keys/?") || !strings.Contains(link, `rel="next"`) {
		t.Fatalf("key pagination Link: %q", link)
	}

	memberID := insertDiscoveryMember(t, f, "member@management.test", "Management member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "member@management.test")
	if memberID == "" || memberCSRF == "" {
		t.Fatal("member fixture missing")
	}
	var memberDetail store.CompatibleOrganizationDetail
	if err := json.Unmarshal(f.request(t, memberClient, "GET", orgPath, "", nil, 200), &memberDetail); err != nil || contains(memberDetail.Access, "project:write") || !contains(memberDetail.Access, "project:read") {
		t.Fatalf("member organization access: %+v %v", memberDetail.Access, err)
	}
	f.request(t, memberClient, "GET", keysPath, "", nil, 200)
	f.request(t, memberClient, "POST", keysPath, `{"name":"Member key"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	f.request(t, memberClient, "PUT", keysPath+created.ID+"/", `{"label":"Member edit"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	f.request(t, memberClient, "DELETE", keysPath+created.ID+"/", "", http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	managerID := insertDiscoveryMember(t, f, "manager@management.test", "Management manager", "member")
	managerPath := orgPath + "members/" + managerID + "/"
	f.request(t, f.client, "PUT", managerPath, `{"orgRole":"manager"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
	managerClient, managerCSRF := loginManagementMember(t, f, "manager@management.test")
	managerHeaders := http.Header{"X-CSRF-Token": {managerCSRF}}
	if err := json.Unmarshal(f.request(t, managerClient, "GET", orgPath, "", nil, 200), &memberDetail); err != nil || !contains(memberDetail.Access, "org:write") || !contains(memberDetail.Access, "member:write") || !contains(memberDetail.Access, "team:write") || !contains(memberDetail.Access, "project:write") {
		t.Fatalf("manager organization access: %+v %v", memberDetail.Access, err)
	}
	var managerKey compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, managerClient, "POST", keysPath, `{"name":"Manager key"}`, managerHeaders, 201), &managerKey); err != nil || managerKey.ID == "" {
		t.Fatalf("manager key create: %+v %v", managerKey, err)
	}
	f.request(t, managerClient, "PUT", keysPath+managerKey.ID+"/", `{"label":"Manager updated"}`, managerHeaders, 200)
	f.request(t, managerClient, "DELETE", keysPath+managerKey.ID+"/", "", managerHeaders, 204)
	f.request(t, managerClient, "PUT", "/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/", `{"name":"Manager project","platform":"go"}`, managerHeaders, 200)

	readToken := issueManagementToken(t, f, f.org.Slug, "Read key", []string{"project:read"}, []string{f.project.ID})
	readHeaders := http.Header{"Authorization": {"Bearer " + readToken}}
	f.request(t, http.DefaultClient, "GET", keysPath, "", readHeaders, 200)
	f.request(t, http.DefaultClient, "POST", keysPath, `{"name":"Read denied"}`, readHeaders, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Write key", []string{"project:write"}, []string{f.project.ID})
	writeHeaders := http.Header{"Authorization": {"Bearer " + writeToken}}
	var bearerKey compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", keysPath, `{"name":"Bearer key"}`, writeHeaders, 201), &bearerKey); err != nil || bearerKey.ID == "" {
		t.Fatalf("bearer key create: %+v %v", bearerKey, err)
	}
	f.request(t, http.DefaultClient, "PUT", keysPath+bearerKey.ID+"/", `{"name":"Bearer updated"}`, writeHeaders, 200)
	f.request(t, http.DefaultClient, "DELETE", keysPath+bearerKey.ID+"/", "", writeHeaders, 204)
	if err := json.Unmarshal(f.request(t, f.client, "GET", keysPath+created.ID+"/", "", nil, 200), &single); err != nil || single.Label != "CI restored" {
		t.Fatalf("owner detail after bearer operations: %+v %v", single, err)
	}
	if code, _ := rawStatus(t, f, f.client, "DELETE", keysPath+created.ID+"/", http.Header{"X-CSRF-Token": {f.csrf}}); code != 204 {
		t.Fatalf("delete key=%d", code)
	}
	f.request(t, f.client, "GET", keysPath+created.ID+"/", "", nil, 404)

	var secondOrgID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('management-second','Management second') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrgID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	var secondProjectID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Second management project','management-second-project','go') RETURNING id::text`, secondOrgID).Scan(&secondProjectID); err != nil {
		t.Fatal(err)
	}
	foreignToken := issueManagementToken(t, f, "management-second", "Foreign read", []string{"org:read", "project:read"}, []string{secondProjectID})
	foreignHeaders := http.Header{"Authorization": {"Bearer " + foreignToken}}
	f.request(t, http.DefaultClient, "GET", orgPath, "", foreignHeaders, 404)
	f.request(t, http.DefaultClient, "GET", "/api/0/projects/management-second/management-second-project/keys/", "", foreignHeaders, 200)
}

func insertDiscoveryMember(t *testing.T, f *resolutionHTTPFixture, email, name, role string) string {
	t.Helper()
	var id string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text`, email, f.hash, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,$3)`, f.org.ID, id, role); err != nil {
		t.Fatal(err)
	}
	return id
}

func loginManagementMember(t *testing.T, f *resolutionHTTPFixture, email string) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(f.request(t, client, "POST", "/api/v1/login", `{"email":"`+email+`","password":"resolution-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("member login: %+v %v", session, err)
	}
	return client, session.CSRFToken
}

func issueManagementToken(t *testing.T, f *resolutionHTTPFixture, orgSlug, name string, scopes, projects []string) string {
	t.Helper()
	body := resolutionJSON(t, map[string]any{"name": name, "scopes": scopes, "projectIds": projects, "expiresInDays": 1})
	var created struct{ Token string }
	if err := json.Unmarshal(f.request(t, f.client, "POST", "/api/v1/organizations/"+orgSlug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &created); err != nil || created.Token == "" {
		t.Fatalf("issue management token: %+v %v", created, err)
	}
	return created.Token
}

func keyLink(t *testing.T, f *resolutionHTTPFixture, path string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("key link request=%d", res.StatusCode)
	}
	return res.Header.Get("Link")
}

func rawStatus(t *testing.T, f *resolutionHTTPFixture, client *http.Client, method, path string, headers http.Header) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, body
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
