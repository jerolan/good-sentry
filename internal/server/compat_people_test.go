//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/store"
)

func TestCompatiblePeopleTeamsAndProjectRelationsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	memberID := insertDiscoveryMember(t, f, "member@compat-people-http.test", "Compat member", "member")
	var defaultID string
	if err := f.sql.QueryRow(f.ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND slug='default'`, f.org.ID).Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.AddTeamMember(f.ctx, f.owner.ID, f.org.Slug, defaultID, memberID); err != nil {
		t.Fatal(err)
	}
	onCall, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.AddTeamMember(f.ctx, f.owner.ID, f.org.Slug, onCall.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	setFixtureProjectTeam(t, f, f.project.ID, onCall.ID)
	orgBase := "/api/0/organizations/" + f.org.Slug
	teamBase := "/api/0/teams/" + f.org.Slug + "/on-call"
	projectTeams := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/teams/"
	f.request(t, http.DefaultClient, "GET", orgBase+"/members/", "", nil, 401)
	f.request(t, http.DefaultClient, "GET", teamBase+"/", "", nil, 401)

	var members []store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase+"/members/", "", nil, 200), &members); err != nil || len(members) != 2 || members[0].ID != memberID || members[0].User == nil || members[0].Pending || members[1].ID != f.owner.ID || !members[1].IsOwner {
		t.Fatalf("organization members HTTP: %+v %v", members, err)
	}
	for _, query := range []string{"limit=0", "limit=101", "cursor=bad", "unknown=x", "limit=1&per_page=2"} {
		f.request(t, f.client, "GET", orgBase+"/members/?"+query, "", nil, 400)
	}
	var memberPage []store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase+"/members/?limit=1", "", nil, 200), &memberPage); err != nil || len(memberPage) != 1 {
		t.Fatalf("member page: %+v %v", memberPage, err)
	}
	if _, err := f.db.InviteMember(f.ctx, f.owner.ID, f.org.Slug, "pending-person@test", "member", strings.Repeat("ab", 32), bytes.Repeat([]byte{1}, 92), 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	var people []map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase+"/members/", "", nil, 200), &people); err != nil {
		t.Fatal(err)
	}
	pending := false
	for _, person := range people {
		if person["pending"] == true && person["email"] == "pending-person@test" {
			pending = true
		}
	}
	if !pending {
		t.Fatalf("owner did not see the pending invitation: %+v", people)
	}
	if link := keyLink(t, f, orgBase+"/members/?limit=1"); !strings.Contains(link, "/good"+orgBase+"/members/?") || !strings.Contains(link, `rel="next"`) {
		t.Fatalf("member Link: %q", link)
	}

	var teams []store.CompatibleTeamProject
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase+"/teams/", "", nil, 200), &teams); err != nil || len(teams) != 2 || teams[0].Slug != "default" || teams[1].Slug != "on-call" || len(teams[1].Projects) != 1 || teams[1].Projects[0].ID != f.project.ID {
		t.Fatalf("organization teams HTTP: %+v %v", teams, err)
	}
	var team store.CompatibleTeamProject
	if err := json.Unmarshal(f.request(t, f.client, "GET", teamBase+"/", "", nil, 200), &team); err != nil || team.ID != onCall.ID || len(team.Projects) != 1 || team.Projects[0].ID != f.project.ID {
		t.Fatalf("team detail HTTP: %+v %v", team, err)
	}
	f.request(t, f.client, "GET", teamBase+"/?unknown=1", "", nil, 400)
	f.request(t, f.client, "GET", "/api/0/teams/"+f.org.Slug+"/missing/", "", nil, 404)
	var teamMembers []store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", teamBase+"/members/", "", nil, 200), &teamMembers); err != nil || len(teamMembers) != 1 || teamMembers[0].ID != f.owner.ID {
		t.Fatalf("team members HTTP: %+v %v", teamMembers, err)
	}
	var teamProjects []store.CompatibleOrganizationProject
	if err := json.Unmarshal(f.request(t, f.client, "GET", teamBase+"/projects/", "", nil, 200), &teamProjects); err != nil || len(teamProjects) != 1 || teamProjects[0].ID != f.project.ID {
		t.Fatalf("team projects HTTP: %+v %v", teamProjects, err)
	}
	var projectTeamList []store.CompatibleTeam
	if err := json.Unmarshal(f.request(t, f.client, "GET", projectTeams, "", nil, 200), &projectTeamList); err != nil || len(projectTeamList) != 1 || projectTeamList[0].ID != onCall.ID {
		t.Fatalf("project teams HTTP: %+v %v", projectTeamList, err)
	}

	memberClient, _ := loginManagementMember(t, f, "member@compat-people-http.test")
	if err := json.Unmarshal(f.request(t, memberClient, "GET", orgBase+"/members/", "", nil, 200), &members); err != nil || len(members) != 2 {
		t.Fatalf("member reads organization people: %+v %v", members, err)
	}
	var memberPeople []map[string]any
	if err := json.Unmarshal(f.request(t, memberClient, "GET", orgBase+"/members/", "", nil, 200), &memberPeople); err != nil {
		t.Fatal(err)
	}
	for _, person := range memberPeople {
		if person["pending"] == true {
			t.Fatalf("member saw a pending invitation: %+v", person)
		}
	}
	f.request(t, memberClient, "GET", orgBase+"/teams/", "", nil, 200)

	orgToken := issueManagementToken(t, f, f.org.Slug, "People org read", []string{"org:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "GET", orgBase+"/members/", "", http.Header{"Authorization": {"Bearer " + orgToken}}, 200)
	f.request(t, http.DefaultClient, "GET", teamBase+"/projects/", "", http.Header{"Authorization": {"Bearer " + orgToken}}, 200)
	projectToken := issueManagementToken(t, f, f.org.Slug, "People project read", []string{"project:read"}, []string{f.project.ID})
	projectHeaders := http.Header{"Authorization": {"Bearer " + projectToken}}
	f.request(t, http.DefaultClient, "GET", projectTeams, "", projectHeaders, 200)
	f.request(t, http.DefaultClient, "GET", orgBase+"/members/", "", projectHeaders, 403)

	var secondOrgID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('people-second','People second') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrgID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	var secondProjectID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'People second project','people-second-project','go') RETURNING id::text`, secondOrgID).Scan(&secondProjectID); err != nil {
		t.Fatal(err)
	}
	foreign := issueManagementToken(t, f, "people-second", "People foreign", []string{"org:read"}, []string{secondProjectID})
	f.request(t, http.DefaultClient, "GET", orgBase+"/members/", "", http.Header{"Authorization": {"Bearer " + foreign}}, 404)
}

func TestCompatibleOrganizationMemberManagementHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	memberID := insertDiscoveryMember(t, f, "member@compat-member-http.test", "Compat member", "member")
	onCall, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	orgBase := "/api/0/organizations/" + f.org.Slug
	memberBase := orgBase + "/members/" + memberID + "/"
	teamMemberBase := memberBase + "teams/" + onCall.Slug + "/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}

	f.request(t, http.DefaultClient, "GET", memberBase, "", nil, 401)
	for _, path := range []string{orgBase + "/members/not-a-number/", orgBase + "/members/01/"} {
		f.request(t, f.client, "GET", path, "", nil, 400)
	}
	f.request(t, f.client, "GET", orgBase+"/members/999999999/", "", nil, 404)
	f.request(t, f.client, "GET", memberBase+"?unknown=1", "", nil, 400)
	var detail store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", memberBase, "", nil, 200), &detail); err != nil || detail.ID != memberID || detail.Role != "member" || detail.Teams == nil || len(detail.Teams) != 0 {
		t.Fatalf("member detail HTTP: %+v %v", detail, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase+"/members/me/", "", nil, 200), &detail); err != nil || detail.ID != f.owner.ID || !detail.IsOwner || detail.Teams == nil {
		t.Fatalf("current member detail HTTP: %+v %v", detail, err)
	}
	var members []store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase+"/members/", "", nil, 200), &members); err != nil || len(members) != 2 || members[0].Teams == nil || members[1].Teams == nil {
		t.Fatalf("member list teams HTTP: %+v %v", members, err)
	}

	memberClient, memberCSRF := loginManagementMember(t, f, "member@compat-member-http.test")
	memberCSRFHeader := http.Header{"X-CSRF-Token": {memberCSRF}}
	if err := json.Unmarshal(f.request(t, memberClient, "GET", memberBase, "", nil, 200), &detail); err != nil || detail.ID != memberID {
		t.Fatalf("member-visible detail HTTP: %+v %v", detail, err)
	}
	f.request(t, memberClient, "PUT", memberBase, `{"orgRole":"owner"}`, memberCSRFHeader, 403)
	f.request(t, memberClient, "DELETE", memberBase, "", memberCSRFHeader, 403)
	f.request(t, memberClient, "POST", teamMemberBase, "", memberCSRFHeader, 403)
	f.request(t, f.client, "POST", teamMemberBase, "", nil, 403)

	for _, body := range []string{
		`{"orgRole":"member","teamRoles":[{}]}`,
		`{"orgRole":"member","teamRoles":[{"teamSlug":7}]}`,
		`{"orgRole":"member","teamRoles":[{"teamSlug":"on-call","role":7}]}`,
		`{"orgRole":"member","teamRoles":[{"teamSlug":"on-call","unknown":true}]}`,
		`{"orgRole":"member","unknown":true}`,
		`{"orgRole":"member"} {}`,
	} {
		f.request(t, f.client, "PUT", memberBase, body, csrf, 400)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", memberBase, `{"orgRole":"manager","teamRoles":[{"teamSlug":"on-call","role":"contributor"}]}`, csrf, 200), &detail); err != nil || detail.Role != "manager" || len(detail.Teams) != 0 {
		t.Fatalf("manager update accepts but ignores active teamRoles: %+v %v", detail, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", memberBase, `{"orgRole":"member"}`, csrf, 200), &detail); err != nil || detail.Role != "member" {
		t.Fatalf("restore member role: %+v %v", detail, err)
	}
	f.request(t, f.client, "PUT", orgBase+"/members/"+f.owner.ID+"/", `{"orgRole":"member"}`, csrf, 409)
	f.request(t, f.client, "DELETE", orgBase+"/members/"+f.owner.ID+"/", "", csrf, 409)

	adminID := insertDiscoveryMember(t, f, "admin@compat-member-http.test", "Compat admin", "admin")
	adminClient, adminCSRF := loginManagementMember(t, f, "admin@compat-member-http.test")
	adminHeaders := http.Header{"X-CSRF-Token": {adminCSRF}}
	f.request(t, adminClient, "POST", teamMemberBase, "", adminHeaders, 403)
	f.request(t, f.client, "POST", orgBase+"/members/"+adminID+"/teams/"+onCall.Slug+"/", "", csrf, 201)
	var team store.CompatibleTeamProject
	if err := json.Unmarshal(f.request(t, adminClient, "POST", teamMemberBase, "", adminHeaders, 201), &team); err != nil || team.MemberCount != 2 {
		t.Fatalf("admin changed membership on joined team: %+v %v", team, err)
	}
	defaultTeam, err := f.db.CompatibleTeam(f.ctx, f.owner.ID, f.org.Slug, "default")
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, adminClient, "POST", orgBase+"/members/"+memberID+"/teams/default/", "", adminHeaders, 403)
	managerID := insertDiscoveryMember(t, f, "manager@compat-member-http.test", "Compat manager", "member")
	managerBase := orgBase + "/members/" + managerID + "/"
	f.request(t, f.client, "PUT", managerBase, `{"orgRole":"manager"}`, csrf, 200)
	managerClient, managerCSRF := loginManagementMember(t, f, "manager@compat-member-http.test")
	managerHeaders := http.Header{"X-CSRF-Token": {managerCSRF}}
	if err := json.Unmarshal(f.request(t, managerClient, "POST", orgBase+"/members/"+adminID+"/teams/default/", "", managerHeaders, 201), &team); err != nil || team.ID != defaultTeam.ID {
		t.Fatalf("manager changed membership on another team: %+v %v", team, err)
	}

	if err := json.Unmarshal(f.request(t, f.client, "POST", teamMemberBase, "", csrf, 201), &team); err != nil || team.ID != onCall.ID || team.MemberCount != 2 {
		t.Fatalf("add member to team HTTP: %+v %v", team, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", teamMemberBase, "", csrf, 201), &team); err != nil || team.MemberCount != 2 {
		t.Fatalf("idempotent add member HTTP: %+v %v", team, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", memberBase, "", nil, 200), &detail); err != nil || len(detail.Teams) != 1 || detail.Teams[0] != onCall.Slug {
		t.Fatalf("detail after team add: %+v %v", detail, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "DELETE", teamMemberBase, "", csrf, 200), &team); err != nil || team.MemberCount != 1 {
		t.Fatalf("remove member from team HTTP: %+v %v", team, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "DELETE", teamMemberBase, "", csrf, 200), &team); err != nil || team.MemberCount != 1 {
		t.Fatalf("idempotent remove member HTTP: %+v %v", team, err)
	}
	f.request(t, f.client, "POST", memberBase+"teams/missing/", "", csrf, 404)

	readToken := issueManagementToken(t, f, f.org.Slug, "Member read", []string{"org:read"}, []string{f.project.ID})
	readHeaders := http.Header{"Authorization": {"Bearer " + readToken}}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", memberBase, "", readHeaders, 200), &detail); err != nil || detail.ID != memberID {
		t.Fatalf("org read token detail: %+v %v", detail, err)
	}
	f.request(t, http.DefaultClient, "PUT", memberBase, `{"orgRole":"admin"}`, readHeaders, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Member write", []string{"org:write"}, []string{f.project.ID})
	writeHeaders := http.Header{"Authorization": {"Bearer " + writeToken}}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "PUT", memberBase, `{"orgRole":"admin"}`, writeHeaders, 200), &detail); err != nil || detail.Role != "admin" {
		t.Fatalf("org write role update: %+v %v", detail, err)
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "PUT", memberBase, `{"orgRole":"member"}`, writeHeaders, 200), &detail); err != nil || detail.Role != "member" {
		t.Fatalf("org write role restore: %+v %v", detail, err)
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", teamMemberBase, "", writeHeaders, 201), &team); err != nil || team.MemberCount != 2 {
		t.Fatalf("org write team add: %+v %v", team, err)
	}
	var secondOrgID, secondProjectID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('member-management-second','Member management second') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrgID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Member management second','member-management-second-project','go') RETURNING id::text`, secondOrgID).Scan(&secondProjectID); err != nil {
		t.Fatal(err)
	}
	foreignToken := issueManagementToken(t, f, "member-management-second", "Foreign member management", []string{"org:read", "org:write"}, []string{secondProjectID})
	foreignHeaders := http.Header{"Authorization": {"Bearer " + foreignToken}}
	f.request(t, http.DefaultClient, "GET", memberBase, "", foreignHeaders, 404)
	f.request(t, http.DefaultClient, "PUT", memberBase, `{"orgRole":"admin"}`, foreignHeaders, 404)
	f.request(t, http.DefaultClient, "POST", teamMemberBase, "", foreignHeaders, 404)

	f.request(t, f.client, "DELETE", memberBase, "", csrf, 204)
	f.request(t, f.client, "GET", memberBase, "", nil, 404)
	f.request(t, memberClient, "GET", orgBase+"/", "", nil, 404)
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/teams/"+f.org.Slug+"/on-call/", "", nil, 200), &team); err != nil || team.MemberCount != 1 {
		t.Fatalf("team member cascade: %+v %v", team, err)
	}
}
