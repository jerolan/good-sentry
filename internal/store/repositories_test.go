//go:build integration

package store

import (
	"errors"
	"testing"
)

func TestReleaseRepositoryCreateAndScopedList(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	repository, err := db.CreateReleaseRepository(ctx, access, "acme/backend", "https://git.example.test/acme/backend")
	if err != nil || repository.ID == "" || repository.Name != "acme/backend" || repository.URL == nil || repository.Status != "active" || repository.Provider.ID != "manual" {
		t.Fatalf("create repository: %+v %v", repository, err)
	}
	page, err := db.ReleaseRepositories(ctx, access, "", 0, 100)
	if err != nil || len(page.Data) != 1 || page.Data[0].ID != repository.ID {
		t.Fatalf("list repository: %+v %v", page, err)
	}
	restricted := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}}
	page, err = db.ReleaseRepositories(ctx, restricted, "", 0, 100)
	if err != nil || len(page.Data) != 0 {
		t.Fatalf("unlinked repository leaked to project token: %+v %v", page, err)
	}
	if _, err := db.CreateReleaseRepository(ctx, access, "acme/backend", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate repository: %v", err)
	}
	if _, err := db.CreateReleaseRepository(ctx, access, "bad\x00name", ""); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("invalid repository: %v", err)
	}
}
