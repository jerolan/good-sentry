//go:build integration

package store

import (
	"errors"
	"testing"
)

func TestCompatibleOrganizationPeopleTeamsAndProjects(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	var member User
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('member@compat-people.test',$1,'Compat member') RETURNING id::text,auth_generation`, []byte("already-hashed")).Scan(&member.ID, &member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	var defaultID string
	if err := db.pool.QueryRow(ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND slug='default'`, org.ID).Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	if err := db.AddTeamMember(ctx, owner.ID, org.Slug, defaultID, member.ID); err != nil {
		t.Fatal(err)
	}
	onCall, err := db.CreateTeam(ctx, owner.ID, org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE projects SET team_id=$2::bigint WHERE id=$1::bigint`, project.ID, onCall.ID); err != nil {
		t.Fatal(err)
	}

	members, err := db.CompatibleOrganizationMembers(ctx, owner.ID, org.Slug)
	if err != nil || len(members) != 2 || members[0].Email != "member@compat-people.test" || members[0].Role != "member" || members[0].User == nil || members[0].User.DateJoined.IsZero() || members[1].ID != owner.ID || !members[1].IsOwner || members[1].User == nil {
		t.Fatalf("organization members: %+v %v", members, err)
	}
	memberView, err := db.CompatibleOrganizationMembers(ctx, member.ID, org.Slug)
	if err != nil || len(memberView) != 2 || memberView[0].Pending {
		t.Fatalf("member-visible organization people: %+v %v", memberView, err)
	}

	teams, err := db.CompatibleOrganizationTeams(ctx, owner.ID, org.Slug, 0, 100)
	if err != nil || len(teams) != 2 || teams[0].Slug != "default" || teams[1].Slug != "on-call" || teams[0].Projects == nil || teams[1].Projects == nil {
		t.Fatalf("organization teams: %+v %v", teams, err)
	}
	if len(teams[0].Projects) != 0 || len(teams[1].Projects) != 1 || teams[1].Projects[0].ID != project.ID {
		t.Fatalf("team project relation: default=%+v oncall=%+v", teams[0].Projects, teams[1].Projects)
	}
	probe, err := db.CompatibleOrganizationTeams(ctx, owner.ID, org.Slug, 0, 1)
	if err != nil || len(probe) != 2 {
		t.Fatalf("team probe page: %+v %v", probe, err)
	}
	page, err := db.CompatibleOrganizationTeams(ctx, owner.ID, org.Slug, 1, 1)
	if err != nil || len(page) != 1 || page[0].Slug != "on-call" {
		t.Fatalf("team offset page: %+v %v", page, err)
	}

	team, err := db.CompatibleTeam(ctx, owner.ID, org.Slug, "on-call")
	if err != nil || team.ID != onCall.ID || len(team.Projects) != 1 || team.Projects[0].ID != project.ID {
		t.Fatalf("team detail: %+v %v", team, err)
	}
	if _, err := db.CompatibleTeam(ctx, owner.ID, org.Slug, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing team error: %v", err)
	}
	teamMembers, err := db.CompatibleTeamMembers(ctx, owner.ID, org.Slug, "default")
	if err != nil || len(teamMembers) != 2 || teamMembers[0].ID != member.ID || teamMembers[1].ID != owner.ID {
		t.Fatalf("team members: %+v %v", teamMembers, err)
	}
	teamProjects, err := db.CompatibleTeamProjects(ctx, owner.ID, org.Slug, "on-call", 0, 100)
	if err != nil || len(teamProjects) != 1 || teamProjects[0].ID != project.ID {
		t.Fatalf("team projects: %+v %v", teamProjects, err)
	}
	projectTeams, err := db.CompatibleProjectTeams(ctx, ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}, project.ID)
	if err != nil || len(projectTeams) != 1 || projectTeams[0].ID != onCall.ID {
		t.Fatalf("project teams: %+v %v", projectTeams, err)
	}
	if _, err := db.CompatibleProjectTeams(ctx, ManagementAccess{UserID: member.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}}, "999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project teams error: %v", err)
	}
}

func TestCompatibleTeamMutationsPreserveProjectRelations(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	team, err := db.CreateTeam(ctx, owner.ID, org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE projects SET team_id=$2::bigint WHERE id=$1::bigint`, project.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateCompatibleTeam(ctx, owner.ID, org.Slug, "on-call", "bad slug"); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("invalid team slug error: %v", err)
	}
	renamed, err := db.UpdateCompatibleTeam(ctx, owner.ID, org.Slug, "on-call", "incident-response")
	if err != nil || renamed.Slug != "incident-response" || len(renamed.Projects) != 1 || renamed.Projects[0].ID != project.ID {
		t.Fatalf("rename team: %+v %v", renamed, err)
	}
	if _, err := db.CompatibleTeam(ctx, owner.ID, org.Slug, "on-call"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old team slug remained: %v", err)
	}
	if _, err := db.UpdateCompatibleTeam(ctx, owner.ID, org.Slug, "default", "changed-default"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("default team rename error: %v", err)
	}
	if err := db.DeleteCompatibleTeam(ctx, owner.ID, org.Slug, "default"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("default team deletion error: %v", err)
	}
	if err := db.DeleteCompatibleTeam(ctx, owner.ID, org.Slug, "incident-response"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompatibleTeam(ctx, owner.ID, org.Slug, "incident-response"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted team remained: %v", err)
	}
	var teamID *string
	if err := db.pool.QueryRow(ctx, `SELECT team_id::text FROM projects WHERE id=$1::bigint`, project.ID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if teamID != nil {
		t.Fatalf("project retained deleted team relation: %v", *teamID)
	}
}
