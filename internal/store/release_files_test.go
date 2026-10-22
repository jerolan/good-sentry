//go:build integration

package store

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"testing"
)

func TestReleaseFilesListDetailDeleteAndScope(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, owner.ID, org.Slug, "Release files other", "go")
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	firstData := []byte(`{"version":3,"sources":["first.ts"],"names":[],"mappings":"AAAA"}`)
	secondData := []byte(`{"version":3,"sources":["second.ts"],"names":[],"mappings":"BBBB"}`)
	first, _, err := db.SaveSourceMap(ctx, owner.ID, org.Slug, project.ID, "~/first.map", "release-files", "", "", firstData)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := db.SaveSourceMap(ctx, owner.ID, org.Slug, other.ID, "~/second.map", "release-files", "", "", secondData)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.SaveSourceMap(ctx, owner.ID, org.Slug, project.ID, "~/v2.map", "release-files-v2", "", "", firstData); err != nil {
		t.Fatal(err)
	}
	files, more, err := db.ReleaseFiles(ctx, access, org.Slug, "", "release-files", 0, 50)
	if err != nil || more || len(files) != 2 {
		t.Fatalf("organization files=%+v more=%t err=%v", files, more, err)
	}
	byID := map[string]ReleaseFile{}
	for _, file := range files {
		byID[file.ID] = file
	}
	firstWire, secondWire := byID[first.ID], byID[second.ID]
	firstSHA := sha1.Sum(firstData)
	secondSHA := sha1.Sum(secondData)
	if firstWire.Name != "~/first.map" || firstWire.Size != int64(len(firstData)) || firstWire.SHA1 != hex.EncodeToString(firstSHA[:]) || firstWire.DateCreated.IsZero() {
		t.Fatalf("first wire=%+v", firstWire)
	}
	if secondWire.Name != "~/second.map" || secondWire.SHA1 != hex.EncodeToString(secondSHA[:]) || secondWire.Size != int64(len(secondData)) {
		t.Fatalf("second wire=%+v", secondWire)
	}
	projectFiles, more, err := db.ReleaseFiles(ctx, access, org.Slug, project.ID, "release-files", 0, 50)
	if err != nil || more || len(projectFiles) != 1 || projectFiles[0].ID != first.ID {
		t.Fatalf("project files=%+v more=%t err=%v", projectFiles, more, err)
	}
	restricted := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: false, ProjectIDs: []int64{referenceID(project.ID)}}
	restrictedFiles, _, err := db.ReleaseFiles(ctx, restricted, org.Slug, "", "release-files", 0, 50)
	if err != nil || len(restrictedFiles) != 1 || restrictedFiles[0].ID != first.ID {
		t.Fatalf("restricted files=%+v err=%v", restrictedFiles, err)
	}
	page, more, err := db.ReleaseFiles(ctx, access, org.Slug, "", "release-files", 0, 1)
	if err != nil || len(page) != 1 || !more {
		t.Fatalf("page=%+v more=%t err=%v", page, more, err)
	}
	detail, err := db.ReleaseFile(ctx, access, org.Slug, "", "release-files", first.ID)
	if err != nil || detail.SHA1 != firstWire.SHA1 || detail.Name != firstWire.Name {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	if _, err = db.ReleaseFile(ctx, access, org.Slug, "", "release-files", second.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.DeleteReleaseFile(ctx, access, org.Slug, project.ID, "release-files", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReleaseFile(ctx, access, org.Slug, "", "release-files", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted detail error=%v", err)
	}
	if err = db.DeleteReleaseFile(ctx, access, org.Slug, project.ID, "release-files", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error=%v", err)
	}
	if _, err = db.ReleaseFile(ctx, access, org.Slug, "", "release-files", "999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing id error=%v", err)
	}
	if _, _, err = db.ReleaseFiles(ctx, access, org.Slug, "", "bad\x00version", 0, 50); !errors.Is(err, ErrInvalidReleaseInput) {
		t.Fatalf("invalid version error=%v", err)
	}
}
