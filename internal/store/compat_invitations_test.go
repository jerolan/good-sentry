//go:build integration

package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func sentInvitation(t *testing.T, ctx context.Context, db *Store, ownerID, orgSlug, email, role string, seed int) (Invitation, string) {
	t.Helper()
	hash := fmt.Sprintf("%064x", seed)
	item, err := db.InviteMember(ctx, ownerID, orgSlug, email, role, hash, bytes.Repeat([]byte{byte(seed)}, 92), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE organization_invitations SET status='sent',sent_at=clock_timestamp() WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	return item, hash
}

func TestCompatibleInvitationValidation(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	item, hash := sentInvitation(t, ctx, db, owner.ID, org.Slug, "compat-invite@example.test", "member", 4101)
	found, err := db.CompatibleInvitation(ctx, item.ID, hash)
	if err != nil || found.ID != item.ID || found.Email != item.Email || found.Role != "member" || found.Organization.ID != org.ID || found.Organization.Slug != org.Slug || found.Organization.Name != org.Name {
		t.Fatalf("compatible invitation=%+v err=%v", found, err)
	}
	if _, err = db.CompatibleInvitation(ctx, strings.Repeat("0", 32), hash); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("wrong identity error=%v", err)
	}
	if _, err = db.CompatibleInvitation(ctx, item.ID, strings.Repeat("0", 64)); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("wrong token error=%v", err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE organization_invitations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompatibleInvitation(ctx, item.ID, hash); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("expired invitation error=%v", err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE organization_invitations SET expires_at=clock_timestamp()+interval '1 hour',status='canceled' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompatibleInvitation(ctx, item.ID, hash); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("canceled invitation error=%v", err)
	}
}

func TestCompatibleInvitationAcceptsExistingAndVerifiedSecondaryAccounts(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	primary := "compat-primary@example.test"
	var primaryID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,'Primary') RETURNING id::text`, primary, []byte("hash")).Scan(&primaryID); err != nil {
		t.Fatal(err)
	}
	item, hash := sentInvitation(t, ctx, db, owner.ID, org.Slug, primary, "admin", 4201)
	primaryUser, err := db.UserByEmail(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	accepted, invitedOrg, err := db.AcceptInvitation(ctx, hash, primaryUser.ID, primaryUser.AuthGeneration, "", nil)
	if err != nil || accepted.ID != primaryID || invitedOrg.ID != org.ID || invitedOrg.Role != "admin" {
		t.Fatalf("primary acceptance=%+v %+v err=%v", accepted, invitedOrg, err)
	}
	if _, err = db.CompatibleInvitation(ctx, item.ID, hash); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("accepted invitation remained valid: %v", err)
	}
	if _, _, err = db.AcceptInvitation(ctx, hash, primaryUser.ID, primaryUser.AuthGeneration, "", nil); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("replay error=%v", err)
	}

	secondaryPrimary := "compat-secondary-primary@example.test"
	secondary := "compat-secondary@example.test"
	var secondaryID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,'Secondary') RETURNING id::text`, secondaryPrimary, []byte("hash")).Scan(&secondaryID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO user_emails(user_id,email,is_verified) VALUES($1::bigint,$2,true)`, secondaryID, secondary); err != nil {
		t.Fatal(err)
	}
	_, secondaryHash := sentInvitation(t, ctx, db, owner.ID, org.Slug, secondary, "member", 4202)
	secondaryUser, err := db.UserByEmail(ctx, secondaryPrimary)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, invitedOrg, err = db.AcceptInvitation(ctx, secondaryHash, secondaryUser.ID, secondaryUser.AuthGeneration, "", nil); err != nil || accepted.ID != secondaryID || invitedOrg.Role != "member" {
		t.Fatalf("secondary acceptance=%+v %+v err=%v", accepted, invitedOrg, err)
	}
}
