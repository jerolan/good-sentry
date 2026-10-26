//go:build integration

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

func TestProjectUpdateAndDeleteHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	native := "/api/v1/organizations/" + f.org.Slug + "/projects/" + f.project.ID
	var updated struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", native, `{"name":"Renamed project","platform":"python"}`, csrf, 200), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != f.project.ID || updated.Name != "Renamed project" || updated.Platform != "python" || updated.Slug != f.project.Slug {
		t.Fatalf("native project update=%+v", updated)
	}
	compatible := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/"
	var compatibleProject map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", compatible, "", nil, 200), &compatibleProject); err != nil || compatibleProject["name"] != "Renamed project" {
		t.Fatalf("compatible project read=%+v err=%v", compatibleProject, err)
	}
	if compatibleProject["eventThrottleRate"] != float64(0) {
		t.Fatalf("new project throttle default=%+v", compatibleProject)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", compatible, `{"name":"Compatible rename","platform":"node","eventThrottleRate":37}`, csrf, 200), &compatibleProject); err != nil || compatibleProject["name"] != "Compatible rename" || compatibleProject["eventThrottleRate"] != float64(37) {
		t.Fatalf("compatible project update=%+v err=%v", compatibleProject, err)
	}
	if compatibleProject["slug"] != f.project.Slug || compatibleProject["id"] != f.project.ID {
		t.Fatalf("rename changed immutable identity: %+v", compatibleProject)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", compatible, "", nil, 200), &compatibleProject); err != nil || compatibleProject["eventThrottleRate"] != float64(37) {
		t.Fatalf("compatible project throttle persistence=%+v err=%v", compatibleProject, err)
	}
	// Omission and explicit null preserve the saved rate; zero disables it.
	for _, body := range []string{`{"name":"Omit rate"}`, `{"name":"Null rate","eventThrottleRate":null}`} {
		if err := json.Unmarshal(f.request(t, f.client, "PUT", compatible, body, csrf, 200), &compatibleProject); err != nil || compatibleProject["eventThrottleRate"] != float64(37) {
			t.Fatalf("throttle preservation=%+v err=%v body=%s", compatibleProject, err, body)
		}
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", compatible, `{"name":"Zero rate","eventThrottleRate":0}`, csrf, 200), &compatibleProject); err != nil || compatibleProject["eventThrottleRate"] != float64(0) {
		t.Fatalf("throttle disable=%+v err=%v", compatibleProject, err)
	}
	f.request(t, f.client, "PUT", compatible, `{"name":"Invalid rate","platform":"node","eventThrottleRate":-1}`, csrf, 400)
	f.request(t, f.client, "PUT", compatible, `{"name":"Invalid rate","platform":"node","eventThrottleRate":101}`, csrf, 400)
	f.request(t, f.client, "DELETE", native, "", csrf, 204)
	f.request(t, f.client, "GET", compatible, "", nil, 404)

	second, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Compatible deletion", "go")
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "DELETE", "/api/0/projects/"+f.org.Slug+"/"+second.Slug+"/", "", csrf, 204)
	var remainingKeys int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_keys WHERE project_id=$1::bigint`, second.ID).Scan(&remainingKeys); err != nil || remainingKeys != 0 {
		t.Fatalf("deleted project keys=%d err=%v", remainingKeys, err)
	}
	if _, err := f.db.ManagementProject(f.ctx, store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("compatible project deletion=%v", err)
	}
}

func TestCreateCompatibleProjectHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	team, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "Backend")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/0/teams/" + f.org.Slug + "/" + team.Slug + "/projects/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}

	f.request(t, http.DefaultClient, "POST", path, `{"name":"Unauthorized"}`, nil, 401)
	insertDiscoveryMember(t, f, "project-creator@test", "Project creator", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "project-creator@test")
	f.request(t, memberClient, "POST", path, `{"name":"Forbidden"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	for _, body := range []string{
		`{}`,
		`{"name":"` + strings.Repeat("x", 65) + `"}`,
		`{"name":"bad\u0000name"}`,
		`{"name":"Valid","eventThrottleRate":-1}`,
		`{"name":"Valid","eventThrottleRate":101}`,
		`{"name":"Valid","slug":"Bad Slug"}`,
		`{"name":"Valid","unknown":true}`,
		`{"name":"Valid"} {}`,
	} {
		f.request(t, f.client, "POST", path, body, csrf, 400)
	}
	f.request(t, f.client, "POST", path+"?unknown=1", `{"name":"Query"}`, csrf, 400)
	f.request(t, f.client, "POST", "/api/0/teams/"+f.org.Slug+"/missing/projects/", `{"name":"Missing"}`, csrf, 404)

	var created store.CompatibleOrganizationProject
	createdBody := f.request(t, f.client, "POST", path, `{"name":"Checkout API","slug":"checkout-api","platform":"go","eventThrottleRate":42}`, csrf, 201)
	var createdFields map[string]any
	if err := json.Unmarshal(createdBody, &created); err != nil || json.Unmarshal(createdBody, &createdFields) != nil || created.ID == "" || created.Name != "Checkout API" || created.Slug != "checkout-api" || created.Platform != "go" || createdFields["eventThrottleRate"] != float64(42) || len(created.Teams) != 1 || created.Teams[0].ID != team.ID || !created.IsMember || !created.HasAccess {
		t.Fatalf("create project HTTP: %+v %v", created, err)
	}
	f.request(t, f.client, "POST", path, `{"name":"Duplicate","slug":"checkout-api"}`, csrf, 409)
	var readback map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/projects/"+f.org.Slug+"/checkout-api/", "", nil, 200), &readback); err != nil || readback["eventThrottleRate"] != float64(42) {
		t.Fatalf("created project throttle readback=%+v err=%v", readback, err)
	}
	var keys []compatibleProjectKeyView
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/projects/"+f.org.Slug+"/checkout-api/keys/", "", nil, 200), &keys); err != nil || len(keys) != 1 || keys[0].Public == "" || keys[0].ProjectID == 0 {
		t.Fatalf("created project DSN: %+v %v", keys, err)
	}
	var teams []store.CompatibleOrganizationProject
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/teams/"+f.org.Slug+"/"+team.Slug+"/projects/", "", nil, 200), &teams); err != nil || len(teams) != 1 || teams[0].ID != created.ID {
		t.Fatalf("created project team relation: %+v %v", teams, err)
	}

	readToken := issueManagementToken(t, f, f.org.Slug, "Project read", []string{"org:read"}, []string{f.project.ID})
	readHeaders := http.Header{"Authorization": {"Bearer " + readToken}}
	f.request(t, http.DefaultClient, "POST", path, `{"name":"Read denied"}`, readHeaders, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Project write", []string{"org:write"}, []string{f.project.ID})
	writeHeaders := http.Header{"Authorization": {"Bearer " + writeToken}}
	var bearerCreated store.CompatibleOrganizationProject
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", path, `{"name":"Bearer project"}`, writeHeaders, 201), &bearerCreated); err != nil || bearerCreated.Slug != "bearer-project" || bearerCreated.Platform != "other" {
		t.Fatalf("bearer project creation: %+v %v", bearerCreated, err)
	}

	var secondOrgID, secondProjectID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('project-create-second','Project create second') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrgID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Project foreign','project-create-foreign','go') RETURNING id::text`, secondOrgID).Scan(&secondProjectID); err != nil {
		t.Fatal(err)
	}
	foreignToken := issueManagementToken(t, f, "project-create-second", "Foreign project write", []string{"org:write"}, []string{secondProjectID})
	f.request(t, http.DefaultClient, "POST", path, `{"name":"Foreign denied"}`, http.Header{"Authorization": {"Bearer " + foreignToken}}, 404)
}

func TestCompatibleProjectTeamRelationshipHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	first, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "First project team")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "Second project team")
	if err != nil {
		t.Fatal(err)
	}
	project, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Relationship project", "go")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/0/projects/" + f.org.Slug + "/" + project.Slug + "/teams/"
	firstPath := base + first.Slug + "/"
	secondPath := base + second.Slug + "/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}

	f.request(t, http.DefaultClient, "POST", firstPath, "", nil, 401)
	insertDiscoveryMember(t, f, "project-team-member@test", "Project team member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "project-team-member@test")
	f.request(t, memberClient, "POST", firstPath, "", http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	f.request(t, f.client, "POST", firstPath, "", nil, 403)
	f.request(t, f.client, "POST", base+"missing/", "", csrf, 404)
	var attached store.CompatibleOrganizationProject
	if err := json.Unmarshal(f.request(t, f.client, "POST", firstPath, "", csrf, 201), &attached); err != nil || len(attached.Teams) != 1 || attached.Teams[0].ID != first.ID {
		t.Fatalf("attach team HTTP: %+v %v", attached, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", firstPath, "", csrf, 201), &attached); err != nil || len(attached.Teams) != 1 {
		t.Fatalf("idempotent team attach HTTP: %+v %v", attached, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", secondPath, "", csrf, 201), &attached); err != nil || len(attached.Teams) != 2 || attached.Teams[0].ID != first.ID || attached.Teams[1].ID != second.ID {
		t.Fatalf("attach second team HTTP: %+v %v", attached, err)
	}
	var teams []store.CompatibleTeam
	if err := json.Unmarshal(f.request(t, f.client, "GET", base, "", nil, 200), &teams); err != nil || len(teams) != 2 || teams[0].ID != first.ID || teams[1].ID != second.ID {
		t.Fatalf("project team relation read: %+v %v", teams, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "DELETE", firstPath, "", csrf, 200), &attached); err != nil || len(attached.Teams) != 1 || attached.Teams[0].ID != second.ID {
		t.Fatalf("detach first team HTTP: %+v %v", attached, err)
	}
	f.request(t, f.client, "DELETE", firstPath, "", csrf, 200)
	if err := json.Unmarshal(f.request(t, f.client, "DELETE", secondPath, "", csrf, 200), &attached); err != nil || attached.Teams == nil || len(attached.Teams) != 0 {
		t.Fatalf("detach final team HTTP: %+v %v", attached, err)
	}

	readToken := issueManagementToken(t, f, f.org.Slug, "Project team read", []string{"project:read"}, []string{project.ID})
	readHeaders := http.Header{"Authorization": {"Bearer " + readToken}}
	f.request(t, http.DefaultClient, "POST", firstPath, "", readHeaders, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Project team write", []string{"project:write"}, []string{project.ID})
	writeHeaders := http.Header{"Authorization": {"Bearer " + writeToken}}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", firstPath, "", writeHeaders, 201), &attached); err != nil || len(attached.Teams) != 1 {
		t.Fatalf("bearer team attach: %+v %v", attached, err)
	}

	var secondOrgID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('project-team-second','Project team second') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrgID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignTeam, err := f.db.CreateTeam(f.ctx, f.owner.ID, "project-team-second", "Foreign team")
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "POST", base+foreignTeam.Slug+"/", "", csrf, 404)
}
