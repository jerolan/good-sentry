//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
)

func TestCompatibleEnvironmentsVisibilityAndGrants(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	compatibleCapture(t, ctx, db, project, 1, base, map[string]any{"environment": "production", "message": "production error"})
	compatibleCapture(t, ctx, db, project, 2, base.Add(time.Second), map[string]any{"environment": "staging", "message": "staging error"})
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}

	visible, err := db.CompatibleProjectEnvironments(ctx, access, project.ID, "visible", 0, 100)
	if err != nil || len(visible) != 2 || visible[0].Name != "production" || visible[0].IsHidden || visible[1].Name != "staging" {
		t.Fatalf("initial project environments: %+v %v", visible, err)
	}
	hidden, err := db.SetCompatibleProjectEnvironmentVisibility(ctx, access, project.ID, "production", true)
	if err != nil || !hidden.IsHidden || hidden.Name != "production" || hidden.ID != nil {
		t.Fatalf("hide environment: %+v %v", hidden, err)
	}
	visible, err = db.CompatibleProjectEnvironments(ctx, access, project.ID, "visible", 0, 100)
	if err != nil || len(visible) != 1 || visible[0].Name != "staging" {
		t.Fatalf("visible after hide: %+v %v", visible, err)
	}
	hiddenList, err := db.CompatibleProjectEnvironments(ctx, access, project.ID, "hidden", 0, 100)
	if err != nil || len(hiddenList) != 1 || hiddenList[0].Name != "production" || !hiddenList[0].IsHidden {
		t.Fatalf("hidden after hide: %+v %v", hiddenList, err)
	}
	all, err := db.CompatibleProjectEnvironments(ctx, access, project.ID, "all", 0, 100)
	if err != nil || len(all) != 2 || all[0].Name != "production" || !all[0].IsHidden || all[1].Name != "staging" {
		t.Fatalf("all after hide: %+v %v", all, err)
	}
	detail, err := db.CompatibleProjectEnvironment(ctx, access, project.ID, "production")
	if err != nil || !detail.IsHidden || detail.Name != "production" {
		t.Fatalf("hidden environment detail: %+v %v", detail, err)
	}
	if _, err := db.SetCompatibleProjectEnvironmentVisibility(ctx, access, project.ID, "missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing environment update: %v", err)
	}

	orgVisible, err := db.CompatibleOrganizationEnvironments(ctx, access, "visible", 0, 100)
	if err != nil || len(orgVisible) != 1 || orgVisible[0].Name != "staging" || orgVisible[0].ID != nil {
		t.Fatalf("organization visible: %+v %v", orgVisible, err)
	}
	orgHidden, err := db.CompatibleOrganizationEnvironments(ctx, access, "hidden", 0, 100)
	if err != nil || len(orgHidden) != 1 || orgHidden[0].Name != "production" {
		t.Fatalf("organization hidden: %+v %v", orgHidden, err)
	}
	restricted := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", ProjectIDs: []int64{referenceID(project.ID) + 1}}
	if _, err := db.CompatibleProjectEnvironments(ctx, restricted, project.ID, "all", 0, 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant-excluded environment project: %v", err)
	}
	if list, err := db.CompatibleOrganizationEnvironments(ctx, restricted, "all", 0, 100); err != nil || len(list) != 0 {
		t.Fatalf("grant-excluded organization environments: %+v %v", list, err)
	}
}
