//go:build integration

package store

import (
	"errors"
	"testing"
)

func TestUpdateAndDeleteManagementProject(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	before, err := db.ManagementProject(ctx, access, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := db.UpdateManagementProject(ctx, access, project.ID, "Renamed backend", "python")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != project.ID || updated.Name != "Renamed backend" || updated.Platform != "python" || updated.Slug != before.Slug || !updated.DateCreated.Equal(before.DateCreated) {
		t.Fatalf("updated project=%+v before=%+v", updated, before)
	}
	if _, err = db.UpdateManagementProject(ctx, access, project.ID, "", "go"); err == nil {
		t.Fatal("empty project name accepted")
	}
	if err = db.DeleteManagementProject(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ManagementProject(ctx, access, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted project read=%v", err)
	}
}
