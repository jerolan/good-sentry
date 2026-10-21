//go:build integration

package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/accountmail"
)

func TestCompatibleUserEmailsVerificationAndPrimarySwap(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	box, err := accountmail.NewSecretBox(strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := accountmail.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	sealed, err := box.SealEmailVerification(hash, secondary)
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateCompatibleUserEmail(ctx, owner.ID, "secondary@example.test", hash, sealed, time.Now().Add(time.Hour))
	if err != nil || created.Email != "secondary@example.test" || created.IsPrimary || created.IsVerified {
		t.Fatalf("create secondary email: %+v %v", created, err)
	}
	if _, err := db.CreateCompatibleUserEmail(ctx, owner.ID, "secondary@example.test", hash, sealed, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("resending existing unverified email: %v", err)
	}
	if _, err := db.CreateCompatibleUserEmail(ctx, owner.ID, owner.Email, hash, sealed, time.Now().Add(time.Hour)); !errors.Is(err, ErrConflict) {
		t.Fatalf("primary email added as secondary: %v", err)
	}
	if _, _, _, err := db.CompatibleUserEmailVerification(ctx, owner.ID, "secondary@example.test"); err != nil {
		t.Fatal(err)
	}
	userID, confirmed, err := db.ConfirmCompatibleUserEmail(ctx, hash)
	if err != nil || userID != owner.ID || confirmed != "secondary@example.test" {
		t.Fatalf("confirm email: %s %s %v", userID, confirmed, err)
	}
	if byEmail, err := db.UserByEmail(ctx, "SECONDARY@example.test"); err != nil || byEmail.ID != owner.ID || byEmail.Email != owner.Email {
		t.Fatalf("login lookup by secondary: %+v %v", byEmail, err)
	}
	emails, err := db.CompatibleUserEmails(ctx, owner.ID)
	if err != nil || len(emails) != 2 || !emails[0].IsPrimary || !emails[1].IsVerified {
		t.Fatalf("email list after verify: %+v %v", emails, err)
	}
	primary, err := db.SetCompatiblePrimaryEmail(ctx, owner.ID, "secondary@example.test")
	if err != nil || primary.Email != "secondary@example.test" || !primary.IsPrimary || !primary.IsVerified {
		t.Fatalf("primary swap: %+v %v", primary, err)
	}
	emails, err = db.CompatibleUserEmails(ctx, owner.ID)
	if err != nil || len(emails) != 2 || emails[0].Email != "secondary@example.test" || emails[1].Email != owner.Email || !emails[1].IsVerified {
		t.Fatalf("email list after primary swap: %+v %v", emails, err)
	}
	if byOld, err := db.UserByEmail(ctx, owner.Email); err != nil || byOld.ID != owner.ID {
		t.Fatalf("login lookup by former primary: %+v %v", byOld, err)
	}
	if err := db.DeleteCompatibleUserEmail(ctx, owner.ID, "secondary@example.test"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("delete primary email: %v", err)
	}
	if err := db.DeleteCompatibleUserEmail(ctx, owner.ID, owner.Email); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserByEmail(ctx, owner.Email); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted secondary remained login-capable: %v", err)
	}
}
