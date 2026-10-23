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
	if before.EventThrottleRate != 0 {
		t.Fatalf("new project throttle default=%d", before.EventThrottleRate)
	}
	rate := 65
	updated, err := db.UpdateManagementProject(ctx, access, project.ID, "Renamed backend", "python", &rate)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != project.ID || updated.Name != "Renamed backend" || updated.Platform != "python" || updated.EventThrottleRate != rate || updated.Slug != before.Slug || !updated.DateCreated.Equal(before.DateCreated) {
		t.Fatalf("updated project=%+v before=%+v", updated, before)
	}
	updated, err = db.UpdateManagementProject(ctx, access, project.ID, "Renamed backend", "python", nil)
	if err != nil || updated.EventThrottleRate != rate {
		t.Fatalf("omitted throttle changed saved value: project=%+v err=%v", updated, err)
	}
	if _, err = db.UpdateManagementProject(ctx, access, project.ID, "", "go", nil); err == nil {
		t.Fatal("empty project name accepted")
	}
	invalidRate := 101
	if _, err = db.UpdateManagementProject(ctx, access, project.ID, "Renamed backend", "python", &invalidRate); err == nil {
		t.Fatal("out-of-range project throttle accepted")
	}
	if err = db.DeleteManagementProject(ctx, access, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ManagementProject(ctx, access, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted project read=%v", err)
	}
}
