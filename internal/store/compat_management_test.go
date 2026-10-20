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
