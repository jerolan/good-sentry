//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
	if _, err := f.sql.Exec(f.ctx, `UPDATE projects SET team_id=$2::bigint WHERE id=$1::bigint`, f.project.ID, onCall.ID); err != nil {
		t.Fatal(err)
	}
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
