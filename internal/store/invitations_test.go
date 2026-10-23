//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/notify"
)

func TestInvitationMigration037PreservesPriorRowsAndReopens(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "before037-session", "before037-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, hash, prefix := apiTokenCredentials("before037-token")
	if _, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug, APITokenInput{Name: "Before037", Scopes: []string{"project:read"}, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}, ExpiresInDays: 1}, hash, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP TABLE organization_invitations; DELETE FROM schema_migrations WHERE version=37`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 36)
	databaseURL := db.pool.Config().ConnString()
	for i := range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reopened.Close)
		after := groupDatabaseSnapshot(t, ctx, reopened, 36)
		delete(after, "organization_invitations")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("037 rewrote prior users, credentials or receipts")
		}
		if user, csrf, err := reopened.Session(ctx, "before037-session"); err != nil || user.ID != owner.ID || csrf != "before037-csrf" {
			t.Fatalf("prior session changed: %+v %q %v", user, csrf, err)
		}
		if grant, err := reopened.AuthenticateAPIToken(ctx, hash); err != nil || grant.User.ID != owner.ID {
			t.Fatalf("prior token changed: %+v %v", grant, err)
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=37`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/037_organization_invitations.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatalf("037 checksum receipt differs: %q %v", receipt, err)
		}
		if i == 0 {
			reopened.Close()
		}
	}
}

func TestManagerInvitationTeamsMigrationPreservesStateAndReopens(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "before062-session", "before062-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, tokenHash, prefix := apiTokenCredentials("before062-token")
	if _, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug,
		APITokenInput{Name: "Before062", Scopes: []string{"project:read"}, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}, ExpiresInDays: 1}, tokenHash, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP TABLE organization_invitation_teams;
		ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;
		ALTER TABLE memberships ADD CONSTRAINT memberships_role_check CHECK (role IN ('owner','admin','member'));
		ALTER TABLE organization_invitations DROP CONSTRAINT organization_invitations_role_check;
		ALTER TABLE organization_invitations ADD CONSTRAINT organization_invitations_role_check CHECK (role IN ('owner','admin','member'));
		DELETE FROM schema_migrations WHERE version=62`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 61)
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	after := groupDatabaseSnapshot(t, ctx, reopened, 61)
	delete(after, "organization_invitation_teams")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("062 rewrote prior users, organization, projects, credentials or receipts")
	}
	if user, csrf, err := reopened.Session(ctx, "before062-session"); err != nil || user.ID != owner.ID || csrf != "before062-csrf" {
		t.Fatalf("prior session changed: %+v %q %v", user, csrf, err)
	}
	if grant, err := reopened.AuthenticateAPIToken(ctx, tokenHash); err != nil || grant.User.ID != owner.ID {
		t.Fatalf("prior token changed: %+v %v", grant, err)
	}
	var receipt string
	if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=62`).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/062_manager_invitation_teams.sql")
	if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("062 checksum receipt differs: %q %v", receipt, err)
	}
}

func TestInvitationDeliveryAcceptanceReplayAndCancellation(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	admin := triageMember(t, ctx, db, org, "Invite admin", "admin")
	box, err := accountmail.NewSecretBox(strings.Repeat("s", 64))
	if err != nil {
		t.Fatal(err)
	}
	invite := func(email, role string, seed byte) (Invitation, string, []byte) {
		t.Helper()
		token := strings.Repeat(string([]byte{'a' + seed}), 64)
		hash := strings.Repeat(string([]byte{'f' - seed}), 64)
		ciphertext, err := box.Seal(hash, token)
		if err != nil {
			t.Fatal(err)
		}
		item, err := db.InviteMember(ctx, owner.ID, org.Slug, email, role, hash, ciphertext, 7*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return item, hash, ciphertext
	}
	item, hash, ciphertext := invite("new@invite.test", "member", 0)
	if item.Email != "new@invite.test" || item.OrganizationName != org.Name {
		t.Fatalf("invite=%+v", item)
	}
	secondCipher, _ := box.Seal(strings.Repeat("e", 64), strings.Repeat("b", 64))
	if _, err := db.InviteMember(ctx, owner.ID, org.Slug, "new@invite.test", "member", strings.Repeat("e", 64), secondCipher, 7*24*time.Hour); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active invitation=%v", err)
	}
	if _, err := db.InviteMember(ctx, admin.ID, org.Slug, "owner@invite.test", "owner", strings.Repeat("d", 64), mustInviteCipher(t, box, strings.Repeat("d", 64), strings.Repeat("d", 64)), 7*24*time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin owner invite=%v", err)
	}
	if _, err := db.InviteMember(ctx, admin.ID, org.Slug, "manager@invite.test", "manager", strings.Repeat("c", 64), mustInviteCipher(t, box, strings.Repeat("c", 64), strings.Repeat("c", 64)), 7*24*time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin manager invite=%v", err)
	}
	list, err := db.Invitations(ctx, owner.ID, org.Slug)
	if err != nil || len(list) != 1 || list[0].ID != item.ID {
		t.Fatalf("invite list=%+v %v", list, err)
	}
	lease, err := db.ClaimInvitation(ctx, 3, time.Second)
	if err != nil || lease.ID != item.ID || !reflect.DeepEqual(lease.TokenCiphertext, ciphertext) {
		t.Fatalf("claim=%+v %v", lease, err)
	}
	if err := db.CompleteInvitation(ctx, lease, notify.Outcome{Disposition: notify.Delivered, Reason: "accepted", ElapsedMS: 2}, 3); err != nil {
		t.Fatal(err)
	}
	inspect, err := db.InspectInvitation(ctx, hash)
	if err != nil || inspect.Email != "new@invite.test" || inspect.ExistingAccount {
		t.Fatalf("inspect=%+v %v", inspect, err)
	}
	user, invitedOrg, err := db.AcceptInvitation(ctx, hash, "", 0, "New Member", []byte("new-password-hash"))
	if err != nil || user.Email != "new@invite.test" || user.AuthGeneration != 1 || invitedOrg.ID != org.ID || invitedOrg.Role != "member" {
		t.Fatalf("accept=%+v %+v %v", user, invitedOrg, err)
	}
	if _, _, err := db.AcceptInvitation(ctx, hash, "", 0, "Replay", []byte("replay")); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("replay=%v", err)
	}
	if _, err := db.InspectInvitation(ctx, hash); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("consumed inspect=%v", err)
	}

	var existing User
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('existing@invite.test',$1,'Existing') RETURNING id::text,email,name,auth_generation`, []byte("existing-hash")).Scan(&existing.ID, &existing.Email, &existing.Name, &existing.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	item2, hash2, _ := invite("existing@invite.test", "admin", 2)
	lease2, err := db.ClaimInvitation(ctx, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteInvitation(ctx, lease2, notify.Outcome{Disposition: notify.Delivered, Reason: "accepted", ElapsedMS: 1}, 2); err != nil {
		t.Fatal(err)
	}
	inspect2, err := db.InspectInvitation(ctx, hash2)
	if err != nil || !inspect2.ExistingAccount {
		t.Fatalf("existing inspect=%+v %v", inspect2, err)
	}
	accepted, acceptedOrg, err := db.AcceptInvitation(ctx, hash2, existing.ID, existing.AuthGeneration, "", nil)
	if err != nil || accepted.ID != existing.ID || acceptedOrg.Role != "admin" {
		t.Fatalf("existing accept=%+v %+v %v", accepted, acceptedOrg, err)
	}
	if item2.ID == "" {
		t.Fatal("invitation identity missing")
	}

	item3, hash3, _ := invite("cancel@invite.test", "member", 3)
	if err := db.CancelInvitation(ctx, owner.ID, org.Slug, item3.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteInvitation(ctx, InvitationLease{Invitation: item3}, notify.Outcome{Disposition: notify.Delivered}, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("canceled completion=%v", err)
	}
	if _, err := db.InspectInvitation(ctx, hash3); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("canceled inspect=%v", err)
	}
	if _, err := db.UpdateMemberRole(ctx, owner.ID, org.Slug, admin.ID, "manager"); err != nil {
		t.Fatalf("promote invitation manager=%v", err)
	}
	managerHash := strings.Repeat("b", 64)
	managerInvite, err := db.InviteMember(ctx, admin.ID, org.Slug, "invited-manager@invite.test", "manager", managerHash,
		mustInviteCipher(t, box, managerHash, strings.Repeat("c", 64)), 7*24*time.Hour)
	if err != nil || managerInvite.Role != "manager" {
		t.Fatalf("manager invite=%+v err=%v", managerInvite, err)
	}
}

func mustInviteCipher(t *testing.T, box *accountmail.SecretBox, hash, token string) []byte {
	t.Helper()
	ciphertext, err := box.Seal(hash, token)
	if err != nil {
		t.Fatal(err)
	}
	return ciphertext
}
