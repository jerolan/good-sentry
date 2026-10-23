//go:build integration

package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func assignProjectTeam(t *testing.T, ctx context.Context, db *Store, projectID, teamID string) {
	t.Helper()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE projects SET team_id=$2::bigint WHERE id=$1::bigint`, projectID, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM project_teams WHERE project_id=$1::bigint`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_teams(organization_id,project_id,team_id,created_at)
		SELECT organization_id,id,$2::bigint,created_at FROM projects WHERE id=$1::bigint`, projectID, teamID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPeopleMembershipsTeamsAndLastOwner(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	admin := triageMember(t, ctx, db, org, "Admin", "admin")
	member := triageMember(t, ctx, db, org, "Member", "member")
	members, err := db.Members(ctx, member.ID, org.Slug)
	if err != nil || len(members) != 3 {
		t.Fatalf("member list=%+v err=%v", members, err)
	}
	if _, err := db.UpdateMemberRole(ctx, member.ID, org.Slug, admin.ID, "member"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member role mutation error=%v", err)
	}
	updated, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, member.ID, "manager")
	if err != nil || updated.Role != "manager" {
		t.Fatalf("promote member to manager=%+v err=%v", updated, err)
	}
	if _, err := db.UpdateMemberRole(ctx, admin.ID, org.Slug, member.ID, "member"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin changed manager role error=%v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, admin.ID, org.Slug, admin.ID, "manager"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin granted manager role error=%v", err)
	}
	if err := db.RemoveMember(ctx, admin.ID, org.Slug, member.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin removed manager error=%v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, member.ID, org.Slug, admin.ID, "member"); err != nil {
		t.Fatalf("manager could not update a non-owner role: %v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, admin.ID, "admin"); err != nil {
		t.Fatalf("owner could not restore admin role: %v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, member.ID, org.Slug, owner.ID, "member"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager changed owner error=%v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, member.ID, org.Slug, member.ID, "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager promoted self to owner error=%v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, owner.ID, "member"); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("last owner demotion error=%v", err)
	}
	if err := db.RemoveMember(ctx, owner.ID, org.Slug, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("last owner removal error=%v", err)
	}
	var defaultID string
	if err := db.pool.QueryRow(ctx, `SELECT id::text FROM teams WHERE organization_id=$1::bigint AND slug='default'`, org.ID).Scan(&defaultID); err != nil {
		t.Fatal(err)
	}

	team, err := db.CreateTeam(ctx, owner.ID, org.Slug, "On call")
	if err != nil || team.Name != "On call" || team.MemberCount != 0 {
		t.Fatalf("create team=%+v err=%v", team, err)
	}
	assignProjectTeam(t, ctx, db, project.ID, team.ID)
	if err := db.AddTeamMember(ctx, admin.ID, org.Slug, team.ID, member.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin changed an unjoined team's membership: %v", err)
	}
	if err := db.AddTeamMember(ctx, member.ID, org.Slug, team.ID, admin.ID); err != nil {
		t.Fatalf("manager changed an unjoined team's membership: %v", err)
	}
	if err := db.AddTeamMember(ctx, admin.ID, org.Slug, team.ID, member.ID); err != nil {
		t.Fatalf("admin could not manage a joined team's membership: %v", err)
	}
	if err := db.AddTeamMember(ctx, admin.ID, org.Slug, defaultID, member.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin changed an unjoined default team's membership: %v", err)
	}
	if err := db.AddTeamMember(ctx, member.ID, org.Slug, defaultID, admin.ID); err != nil {
		t.Fatalf("manager could not change any team membership: %v", err)
	}
	detail, err := db.Team(ctx, admin.ID, org.Slug, team.ID)
	if err != nil || len(detail.Members) != 2 || detail.ProjectCount != 1 {
		t.Fatalf("team detail after role-based assignments=%+v err=%v", detail, err)
	}
	if err := db.RemoveTeamMember(ctx, admin.ID, org.Slug, team.ID, member.ID); err != nil {
		t.Fatalf("admin could not remove a member from its team: %v", err)
	}
	if err := db.AddTeamMember(ctx, owner.ID, org.Slug, team.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	detail, err = db.Team(ctx, admin.ID, org.Slug, team.ID)
	if err != nil || len(detail.Members) != 2 || detail.Members[0].ID != admin.ID || detail.Members[1].ID != member.ID || detail.ProjectCount != 1 {
		t.Fatalf("team detail=%+v err=%v", detail, err)
	}
	renamed, err := db.UpdateTeam(ctx, member.ID, org.Slug, team.ID, "Incident response")
	if err != nil || renamed.Name != "Incident response" {
		t.Fatalf("rename team=%+v err=%v", renamed, err)
	}
	if err := db.DeleteTeam(ctx, member.ID, org.Slug, team.ID); err != nil {
		t.Fatal(err)
	}
	var assigned *string
	if err := db.pool.QueryRow(ctx, `SELECT team_id::text FROM projects WHERE id=$1::bigint`, project.ID).Scan(&assigned); err != nil || assigned != nil {
		t.Fatalf("project team assignment=%v err=%v", assigned, err)
	}
	if err := db.DeleteTeam(ctx, owner.ID, org.Slug, defaultID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("default team deletion error=%v", err)
	}

	if err := db.AddTeamMember(ctx, owner.ID, org.Slug, defaultID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, member.ID, "admin"); err != nil {
		t.Fatalf("owner could not restore admin role before token checks: %v", err)
	}
	current, err := db.UserByID(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, tokenHash, prefix := apiTokenCredentials("people-member")
	if _, err := db.CreateAPIToken(ctx, member.ID, current.AuthGeneration, org.Slug, APITokenInput{Name: "People member", Scopes: []string{"project:read"}, ExpiresInDays: 1}, tokenHash, prefix); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveMember(ctx, owner.ID, org.Slug, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, tokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed member token survived: %v", err)
	}
	var teamMembership int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM team_memberships WHERE user_id=$1::bigint`, member.ID).Scan(&teamMembership); err != nil || teamMembership != 0 {
		t.Fatalf("removed member remained on team: count=%d err=%v", teamMembership, err)
	}
	members, err = db.Members(ctx, owner.ID, org.Slug)
	if err != nil || len(members) != 2 {
		t.Fatalf("members after removal=%+v err=%v", members, err)
	}

	if _, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, admin.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, owner.ID, "admin"); err != nil {
		t.Fatalf("demote owner with another owner: %v", err)
	}
}

func TestConcurrentOwnerDemotionsRetainOneOwner(t *testing.T) {
	ctx, db, first, org, _ := telemetryStore(t)
	second := triageMember(t, ctx, db, org, "Second owner", "owner")
	type result struct {
		owner string
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var group sync.WaitGroup
	for _, owner := range []User{first, second} {
		group.Go(func() {
			<-start
			_, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, owner.ID, "member")
			results <- result{owner.ID, err}
		})
	}
	close(start)
	group.Wait()
	close(results)
	success, retained := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			success++
		case errors.Is(result.err, ErrLastOwner):
			retained++
		default:
			t.Fatalf("owner %s demotion error=%v", result.owner, result.err)
		}
	}
	if success != 1 || retained != 1 {
		t.Fatalf("concurrent owner demotions success=%d retained=%d", success, retained)
	}
	var owners int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE organization_id=$1::bigint AND role='owner'`, org.ID).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners=%d err=%v", owners, err)
	}
}
