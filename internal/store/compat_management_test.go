//go:build integration

package store

import (
	"errors"
	"strings"
	"testing"
)

func TestCompatibleManagementOrganizationAndProjectKeys(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	if _, err := db.pool.Exec(ctx, `UPDATE projects SET team_id=(SELECT id FROM teams WHERE organization_id=$1::bigint AND slug='default') WHERE id=$2::bigint`, org.ID, project.ID); err != nil {
		t.Fatal(err)
	}

	detail, err := db.CompatibleOrganizationDetail(ctx, user.ID, org.Slug)
	if err != nil || detail.ID != org.ID || detail.Role != "owner" || len(detail.Projects) != 1 || len(detail.Teams) != 1 || detail.OpenMembership {
		t.Fatalf("organization detail: %+v %v", detail, err)
	}
	if detail.Projects[0].ID != project.ID || len(detail.Projects[0].Teams) != 1 || detail.Projects[0].Teams[0].Slug != "default" || detail.Teams[0].Slug != "default" || detail.Teams[0].MemberCount != 1 || !detail.Teams[0].IsMember || detail.Teams[0].DateCreated.IsZero() {
		t.Fatalf("organization project/team relation: %+v %+v", detail.Projects, detail.Teams)
	}
	if !containsString(detail.Access, "project:write") || !containsString(detail.Access, "event:admin") || !containsString(detail.Access, "org:write") {
		t.Fatalf("organization access: %v", detail.Access)
	}

	keys, err := db.CompatibleProjectKeys(ctx, access, project.ID, nil, 0, 100)
	if err != nil || len(keys) != 1 || !validKeyUUID(keys[0].ID) || keys[0].ProjectID != referenceID(project.ID) || keys[0].Public == "" || !keys[0].Enabled || keys[0].RateLimit != nil {
		t.Fatalf("initial project keys: %+v %v", keys, err)
	}
	initial, err := db.CompatibleProjectKey(ctx, access, project.ID, keys[0].ID)
	if err != nil || initial.Public != keys[0].Public || initial.ID != keys[0].ID {
		t.Fatalf("project key detail: %+v %v", initial, err)
	}
	created, err := db.CreateCompatibleProjectKey(ctx, access, project.ID, "CI delivery", nil)
	if err != nil || created.Public == "" || created.Label != "CI delivery" || created.Name != "CI delivery" || !validKeyUUID(created.ID) {
		t.Fatalf("create project key: %+v %v", created, err)
	}
	if created.Public == initial.Public {
		t.Fatal("new project key reused the existing public key")
	}
	updated, err := db.UpdateCompatibleProjectKey(ctx, access, project.ID, created.ID, "CI updated", nil)
	if err != nil || updated.ID != created.ID || updated.Label != "CI updated" || updated.Public != created.Public {
		t.Fatalf("update project key: %+v %v", updated, err)
	}
	if _, err := db.CreateCompatibleProjectKey(ctx, access, project.ID, strings.Repeat("x", 201), nil); !errors.Is(err, ErrInvalidProjectKey) {
		t.Fatalf("overlong key label error: %v", err)
	}
	if _, err := db.CreateCompatibleProjectKey(ctx, access, project.ID, "invalid rate", &CompatibleKeyRateLimit{}); !errors.Is(err, ErrInvalidProjectKey) {
		t.Fatalf("invalid rate error: %v", err)
	}
	if _, err := db.CompatibleProjectKey(ctx, access, project.ID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown key error: %v", err)
	}
	if _, err := db.CompatibleProjectKey(ctx, access, project.ID, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed key error: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE project_keys SET enabled=false WHERE api_id::text=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	enabled, disabled := true, false
	if page, err := db.CompatibleProjectKeys(ctx, access, project.ID, &enabled, 0, 100); err != nil || len(page) != 1 || page[0].ID != initial.ID {
		t.Fatalf("active key filter: %+v %v", page, err)
	}
	if page, err := db.CompatibleProjectKeys(ctx, access, project.ID, &disabled, 0, 100); err != nil || len(page) != 1 || page[0].ID != created.ID {
		t.Fatalf("inactive key filter: %+v %v", page, err)
	}
	if err := db.DeleteCompatibleProjectKey(ctx, access, project.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompatibleProjectKey(ctx, access, project.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted key remained visible: %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCreateCompatibleProjectInTeam(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	team, err := db.CreateTeam(ctx, owner.ID, org.Slug, "Backend")
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateCompatibleProject(ctx, owner.ID, org.Slug, team.Slug, "Checkout API", "checkout-api", "go")
	if err != nil || created.ID == "" || created.Name != "Checkout API" || created.Slug != "checkout-api" || created.Platform != "go" || len(created.Teams) != 1 || created.Teams[0].ID != team.ID {
		t.Fatalf("create compatible project: %+v %v", created, err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	keys, err := db.CompatibleProjectKeys(ctx, access, created.ID, nil, 0, 100)
	if err != nil || len(keys) != 1 || !keys[0].Enabled || keys[0].Public == "" {
		t.Fatalf("created project key: %+v %v", keys, err)
	}
	var teamID int64
	var member bool
	if err := db.pool.QueryRow(ctx, `SELECT p.team_id::bigint,EXISTS(SELECT 1 FROM team_memberships tm WHERE tm.team_id=p.team_id AND tm.user_id=$2::bigint)
FROM projects p WHERE p.id=$1::bigint`, created.ID, owner.ID).Scan(&teamID, &member); err != nil || !member || teamID == 0 {
		t.Fatalf("project/team membership: %d %t %v", teamID, member, err)
	}

	derived, err := db.CreateCompatibleProject(ctx, owner.ID, org.Slug, team.Slug, "Web Client", "", "")
	if err != nil || derived.Slug != "web-client" || derived.Platform != "other" {
		t.Fatalf("derive project slug/platform: %+v %v", derived, err)
	}
	if _, err := db.CreateCompatibleProject(ctx, owner.ID, org.Slug, team.Slug, "Duplicate", "checkout-api", "go"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate project slug: %v", err)
	}
	if _, err := db.CreateCompatibleProject(ctx, owner.ID, org.Slug, team.Slug, "Invalid", "Bad Slug", "go"); !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("invalid project slug: %v", err)
	}
	if _, err := db.CreateCompatibleProject(ctx, owner.ID, org.Slug, "missing", "Missing team", "missing-team", "go"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project team: %v", err)
	}
	var memberID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('project-creator-member@test',$1,'Project member') RETURNING id::text`, []byte("already-hashed")).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, org.ID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCompatibleProject(ctx, memberID, org.Slug, team.Slug, "Forbidden", "forbidden", "go"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member project creation: %v", err)
	}
}
