//go:build integration

package store

import (
	"errors"
	"strings"
	"testing"
)

func TestCompatibleOrganizationUpdateDeleteAndRoleBoundaries(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	var admin, member User
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('admin@org-write.test',$1,'Org admin') RETURNING id::text,auth_generation`, []byte("already-hashed")).Scan(&admin.ID, &admin.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('member@org-write.test',$1,'Org member') RETURNING id::text,auth_generation`, []byte("already-hashed")).Scan(&member.ID, &member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin'),($1::bigint,$3::bigint,'member')`, org.ID, admin.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", " ", "bad\x00name", strings.Repeat("界", 201)} {
		if _, err := db.UpdateCompatibleOrganization(ctx, owner.ID, org.Slug, invalid, nil); !errors.Is(err, ErrInvalidOrganization) {
			t.Fatalf("invalid organization name %q: %v", invalid, err)
		}
	}
	updated, err := db.UpdateCompatibleOrganization(ctx, admin.ID, org.Slug, "  Renamed organization  ", nil)
	if err != nil || updated.Name != "Renamed organization" || updated.Slug != org.Slug || len(updated.Projects) != 1 || updated.Projects[0].ID != project.ID {
		t.Fatalf("organization rename: %+v %v", updated, err)
	}
	if _, err := db.UpdateCompatibleOrganization(ctx, member.ID, org.Slug, "Member rename", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member rename error: %v", err)
	}
	require := true
	if _, err := db.UpdateCompatibleOrganization(ctx, owner.ID, org.Slug, "", &require); !errors.Is(err, ErrTwoFactorRequired) {
		t.Fatalf("require2FA without enrollment error: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE users SET totp_confirmed_at=now() WHERE id=$1::bigint`, owner.ID); err != nil {
		t.Fatal(err)
	}
	secured, err := db.UpdateCompatibleOrganization(ctx, owner.ID, org.Slug, "", &require)
	if err != nil || !secured.Require2FA {
		t.Fatalf("require2FA update: %+v %v", secured, err)
	}
	requirement, err := db.TwoFactorRequirement(ctx, member.ID)
	if err != nil || !requirement.Organization || requirement.Confirmed {
		t.Fatalf("member two-factor requirement=%+v err=%v", requirement, err)
	}
	requirement, err = db.TwoFactorRequirement(ctx, owner.ID)
	if err != nil || !requirement.Organization || !requirement.Confirmed {
		t.Fatalf("owner two-factor requirement=%+v err=%v", requirement, err)
	}
	disable := false
	cleared, err := db.UpdateCompatibleOrganization(ctx, owner.ID, org.Slug, "", &disable)
	if err != nil || cleared.Require2FA {
		t.Fatalf("clear require2FA: %+v %v", cleared, err)
	}
	if err := db.DeleteCompatibleOrganization(ctx, admin.ID, org.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin delete error: %v", err)
	}
	if err := db.DeleteCompatibleOrganization(ctx, member.ID, org.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member delete error: %v", err)
	}
	if err := db.DeleteCompatibleOrganization(ctx, owner.ID, org.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Organization(ctx, owner.ID, org.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted organization remained: %v", err)
	}
	var projects int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM projects WHERE id=$1::bigint`, project.ID).Scan(&projects); err != nil || projects != 0 {
		t.Fatalf("organization cascade left projects: %d %v", projects, err)
	}
}
