//go:build integration

package store

import (
	"errors"
	"testing"
)

func TestCreateCompatibleOrganizationOwnershipTeamAndAuthority(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	counts := func() (int, int, int) {
		var organizations, projects, teams int
		if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM organizations),(SELECT count(*) FROM projects),(SELECT count(*) FROM teams)`).Scan(&organizations, &projects, &teams); err != nil {
			t.Fatal(err)
		}
		return organizations, projects, teams
	}
	orgsBefore, projectsBefore, teamsBefore := counts()
	created, err := db.CreateCompatibleOrganization(ctx, owner.ID, org.ID, "Created Workspace")
	if err != nil || created.ID == "" || created.ID == org.ID || created.Slug != "created-workspace" || created.Role != "owner" || len(created.Projects) != 0 || len(created.Teams) != 1 || created.Teams[0].Slug != "default" || !created.Teams[0].IsMember {
		t.Fatalf("created organization: %+v %v", created, err)
	}
	orgsAfter, projectsAfter, teamsAfter := counts()
	if orgsAfter != orgsBefore+1 || projectsAfter != projectsBefore || teamsAfter != teamsBefore+1 {
		t.Fatalf("creation changed more than the new organization and its team: organizations %d->%d projects %d->%d teams %d->%d", orgsBefore, orgsAfter, projectsBefore, projectsAfter, teamsBefore, teamsAfter)
	}
	var role string
	var teamMemberships int
	if err := db.pool.QueryRow(ctx, `SELECT m.role,(SELECT count(*) FROM team_memberships tm JOIN teams t ON t.id=tm.team_id WHERE t.organization_id=o.id AND tm.user_id=$2::bigint)
FROM organizations o JOIN memberships m ON m.organization_id=o.id AND m.user_id=$2::bigint WHERE o.id=$1::bigint`, created.ID, owner.ID).Scan(&role, &teamMemberships); err != nil || role != "owner" || teamMemberships != 1 {
		t.Fatalf("created ownership team membership role=%q memberships=%d err=%v", role, teamMemberships, err)
	}
	if _, err := db.CreateCompatibleOrganization(ctx, owner.ID, org.ID, "Created Workspace"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate organization slug: %v", err)
	}
	if _, err := db.CreateCompatibleOrganization(ctx, owner.ID, org.ID, "bad\x00name"); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatalf("invalid organization name: %v", err)
	}
	member := triageMember(t, ctx, db, org, "Organization Member", "member")
	if _, err := db.CreateCompatibleOrganization(ctx, member.ID, org.ID, "Forbidden Workspace"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member organization creation: %v", err)
	}
	manager := triageMember(t, ctx, db, org, "Organization Manager", "manager")
	if _, err := db.CreateCompatibleOrganization(ctx, manager.ID, org.ID, "Manager Workspace"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager organization creation: %v", err)
	}
	admin := triageMember(t, ctx, db, org, "Organization Admin", "admin")
	adminCreated, err := db.CreateCompatibleOrganization(ctx, admin.ID, org.ID, "Admin Workspace")
	if err != nil || adminCreated.Role != "owner" || len(adminCreated.Teams) != 1 || !adminCreated.Teams[0].IsMember {
		t.Fatalf("admin organization creation: %+v %v", adminCreated, err)
	}
	if _, err := db.CreateCompatibleOrganization(ctx, owner.ID, "999999999", "Missing Authority"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing authority organization: %v", err)
	}
	sessionCreated, err := db.CreateCompatibleOrganization(ctx, owner.ID, "", "Session Workspace")
	if err != nil || sessionCreated.Role != "owner" || len(sessionCreated.Teams) != 1 {
		t.Fatalf("session organization creation: %+v %v", sessionCreated, err)
	}
}
