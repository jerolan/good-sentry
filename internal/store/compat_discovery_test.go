//go:build integration

package store

import (
	"errors"
	"strings"
	"testing"
)

func TestCompatibleDiscoveryMembershipScopeSearchAndPagination(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	var secondID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('discovery-second','Discovery second') RETURNING id::text`).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondID, user.ID); err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateProject(ctx, user.ID, "discovery-second", "Visible second", "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('discovery-foreign','Foreign secret') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Foreign project','foreign-project','go')`, foreignOrg); err != nil {
		t.Fatal(err)
	}

	all, err := db.CompatibleOrganizations(ctx, user.ID, "", "", false, "name", 0, 100)
	if err != nil || len(all) != 2 || all[0].Slug != "discovery-second" || all[1].Slug != org.Slug {
		t.Fatalf("membership organization scope/order: %+v %v", all, err)
	}
	if all[1].DateCreated.IsZero() || all[1].Status.ID != "active" || !all[1].IsAcceptingEvents || all[1].Avatar.UUID != nil {
		t.Fatalf("organization DTO fields: %+v", all[1])
	}
	search, err := db.CompatibleOrganizations(ctx, user.ID, "", "second", false, "name", 0, 100)
	if err != nil || len(search) != 1 || search[0].ID != secondID {
		t.Fatalf("organization name search: %+v %v", search, err)
	}
	owners, err := db.CompatibleOrganizations(ctx, user.ID, "", "", true, "name", 0, 100)
	if err != nil || len(owners) != 1 || owners[0].Slug != org.Slug {
		t.Fatalf("owner filter: %+v %v", owners, err)
	}
	page, err := db.CompatibleOrganizations(ctx, user.ID, "", "", false, "name", 0, 1)
	if err != nil || len(page) != 2 {
		t.Fatalf("25+1 style probe row: %+v %v", page, err)
	}
	next, err := db.CompatibleOrganizations(ctx, user.ID, "", "", false, "name", 1, 1)
	if err != nil || len(next) != 1 || next[0].ID != org.ID {
		t.Fatalf("organization cursor offset: %+v %v", next, err)
	}

	projects, err := db.CompatibleProjectsAcrossOrganizations(ctx, user.ID, "", true, nil, 0, 100)
	if err != nil || len(projects) != 2 || projects[0].ID != project.ID || projects[1].ID != second.ID {
		t.Fatalf("project membership scope/order: %+v %v", projects, err)
	}
	if projects[0].Organization.Slug != org.Slug || projects[0].Platform != project.Platform || !projects[0].IsMember || !projects[0].HasAccess || projects[0].Features == nil || projects[0].FirstEvent != nil {
		t.Fatalf("project DTO fields: %+v", projects[0])
	}
	for _, item := range projects {
		if strings.Contains(item.Organization.Slug, "foreign") {
			t.Fatalf("foreign project escaped discovery: %+v", item)
		}
	}
	projectProbe, err := db.CompatibleProjectsAcrossOrganizations(ctx, user.ID, "", true, nil, 0, 1)
	if err != nil || len(projectProbe) != 2 {
		t.Fatalf("project probe row: %+v %v", projectProbe, err)
	}

	compatible, err := db.CompatibleUser(ctx, user.ID)
	if err != nil || compatible.ID != user.ID || compatible.Email != user.Email || compatible.Username != user.Email || compatible.Name != user.Name || compatible.DateJoined.IsZero() || !compatible.HasPasswordAuth || compatible.Identities == nil || !compatible.IsActive || compatible.IsSuperuser {
		t.Fatalf("compatible user: %+v %v", compatible, err)
	}
	if _, err := db.CompatibleUser(ctx, "999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user error: %v", err)
	}
}
