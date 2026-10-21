//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
)

func TestWizardTransferIsScopedAtomicAndExpires(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	member := triageMember(t, ctx, db, org, "wizard-member", "member")
	if err := db.pool.QueryRow(ctx, `SELECT auth_generation FROM users WHERE id=$1::bigint`, member.ID).Scan(&member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	secret := "wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"
	transferHash := "1111111111111111111111111111111111111111111111111111111111111111"
	apiHash := "2222222222222222222222222222222222222222222222222222222222222222"
	if err := db.CreateWizardTransfer(ctx, owner.ID, owner.AuthGeneration, org.Slug, transferHash, apiHash, "gs_22222222", []byte(secret), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	record, err := db.WizardTransfer(ctx, transferHash)
	if err != nil || record.UserID != owner.ID || record.OrganizationID != org.ID || string(record.TokenCiphertext) != secret {
		t.Fatalf("transfer=%+v err=%v", record, err)
	}
	projects, err := db.WizardProjects(ctx, owner.ID, org.ID)
	if err != nil || len(projects) != 1 || projects[0].ID != project.ID || projects[0].PublicKey != project.PublicKey || projects[0].OrganizationSlug != org.Slug {
		t.Fatalf("wizard projects=%+v err=%v", projects, err)
	}
	if err := db.CreateWizardTransfer(ctx, owner.ID, owner.AuthGeneration, org.Slug, transferHash, "3333333333333333333333333333333333333333333333333333333333333333", "gs_33333333", []byte(secret), time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate transfer error=%v", err)
	}
	if err := db.CreateWizardTransfer(ctx, member.ID, member.AuthGeneration, org.Slug, transferHash, "4444444444444444444444444444444444444444444444444444444444444444", "gs_44444444", []byte(secret), time.Minute); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member transfer error=%v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE wizard_transfers SET created_at=now()-interval '2 seconds',expires_at=now()-interval '1 second' WHERE token_hash=$1`, transferHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.WizardTransfer(ctx, transferHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired transfer error=%v", err)
	}
}

func TestWizardTransferDeletionAndMembershipCascade(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	secret := "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	for index, hash := range []string{
		"5555555555555555555555555555555555555555555555555555555555555555",
		"6666666666666666666666666666666666666666666666666666666666666666",
	} {
		apiHash := "7777777777777777777777777777777777777777777777777777777777777777"
		if index == 1 {
			apiHash = "8888888888888888888888888888888888888888888888888888888888888888"
		}
		if err := db.CreateWizardTransfer(ctx, owner.ID, owner.AuthGeneration, org.Slug, hash, apiHash, "gs_"+apiHash[:8], []byte(secret), 10*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeleteWizardTransfer(ctx, "5555555555555555555555555555555555555555555555555555555555555555"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.WizardTransfer(ctx, "5555555555555555555555555555555555555555555555555555555555555555"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted transfer error=%v", err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, org.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.WizardTransfer(ctx, "6666666666666666666666666666666666666666666666666666666666666666"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("membership-cascade transfer error=%v", err)
	}
}
