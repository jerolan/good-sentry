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

func TestInvitationSignupCannotDuplicateSuspendedVerifiedSecondaryAccount(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	var inactiveID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('inactive-invite-primary@example.test',$1,'Inactive') RETURNING id::text`, []byte("hash")).Scan(&inactiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO user_emails(user_id,email,is_verified) VALUES($1::bigint,'inactive-invite-secondary@example.test',true)`, inactiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1::bigint`, inactiveID); err != nil {
		t.Fatal(err)
	}
	_, hash := sentInvitation(t, ctx, db, owner.ID, org.Slug, "inactive-invite-secondary@example.test", "member", 4203)
	if _, _, err := db.AcceptInvitation(ctx, hash, "", 0, "Duplicate", []byte("new-password-hash")); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("suspended secondary invitation signup error=%v", err)
	}
	var duplicatePrimary int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE lower(email)=lower('inactive-invite-secondary@example.test')`).Scan(&duplicatePrimary); err != nil || duplicatePrimary != 0 {
		t.Fatalf("invitation created a duplicate of a suspended secondary account: count=%d err=%v", duplicatePrimary, err)
	}
}

func TestInvitationTeamAssignmentsFollowTeamIDsThroughAcceptance(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	admin := triageMember(t, ctx, db, org, "Invitation admin", "admin")
	onCall, err := db.CreateTeam(ctx, owner.ID, org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	doomed, err := db.CreateTeam(ctx, owner.ID, org.Slug, "Temporary")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrgID string
	if err = db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('invitation-team-foreign','Foreign') RETURNING id::text`).Scan(&foreignOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO teams(organization_id,slug,name) VALUES($1::bigint,'foreign-team','Foreign team')`, foreignOrgID); err != nil {
		t.Fatal(err)
	}

	issue := func(actorID, email string, role string, slugs []string, seed int) (Invitation, string, error) {
		hash := fmt.Sprintf("%064x", seed)
		item, inviteErr := db.InviteMemberWithTeams(ctx, actorID, org.Slug, email, role, slugs, hash,
			bytes.Repeat([]byte{byte(seed)}, 92), 7*24*time.Hour)
		if inviteErr == nil {
			_, inviteErr = db.pool.Exec(ctx, `UPDATE organization_invitations SET status='sent',sent_at=clock_timestamp() WHERE id=$1`, item.ID)
		}
		return item, hash, inviteErr
	}
	if _, _, err = issue(admin.ID, "unjoined-team@invite.test", "member", []string{onCall.Slug}, 4301); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin assigned an unjoined team: %v", err)
	}
	if err = db.AddTeamMember(ctx, owner.ID, org.Slug, onCall.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = issue(admin.ID, "other-unjoined-team@invite.test", "member", []string{onCall.Slug, doomed.Slug}, 4302); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin assigned another unjoined team: %v", err)
	}
	item, hash, err := issue(admin.ID, "assigned@invite.test", "member", []string{onCall.Slug, onCall.Slug, "missing-team", "foreign-team"}, 4303)
	if err != nil {
		t.Fatal(err)
	}
	var pending CompatibleOrganizationUser
	pending, err = db.CompatibleOrganizationInvitationMember(ctx, owner.ID, org.Slug, item.ID)
	if err != nil || !pending.Pending || len(pending.Teams) != 1 || pending.Teams[0] != onCall.Slug {
		t.Fatalf("pending team assignments=%+v err=%v", pending, err)
	}
	if _, err = db.UpdateCompatibleTeam(ctx, owner.ID, org.Slug, onCall.Slug, "incident-response"); err != nil {
		t.Fatal(err)
	}
	pending, err = db.CompatibleOrganizationInvitationMember(ctx, owner.ID, org.Slug, item.ID)
	if err != nil || len(pending.Teams) != 1 || pending.Teams[0] != "incident-response" {
		t.Fatalf("renamed pending team assignment=%+v err=%v", pending, err)
	}
	accepted, acceptedOrg, err := db.AcceptInvitation(ctx, hash, "", 0, "Assigned invitee", []byte("password-hash"))
	if err != nil || acceptedOrg.ID != org.ID || acceptedOrg.Role != "member" {
		t.Fatalf("assigned invitation acceptance=%+v org=%+v err=%v", accepted, acceptedOrg, err)
	}
	var assigned int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM team_memberships tm JOIN teams t ON t.id=tm.team_id
		WHERE tm.user_id=$1::bigint AND t.id=$2::bigint AND t.slug='incident-response'`, accepted.ID, onCall.ID).Scan(&assigned); err != nil || assigned != 1 {
		t.Fatalf("accepted team assignment count=%d err=%v", assigned, err)
	}
	var retainedInviteAssignments int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM organization_invitation_teams WHERE invitation_id=$1`, item.ID).Scan(&retainedInviteAssignments); err != nil || retainedInviteAssignments != 0 {
		t.Fatalf("consumed invitation retained assignments=%d err=%v", retainedInviteAssignments, err)
	}

	doomedInvite, doomedHash, err := issue(owner.ID, "deleted-team@invite.test", "member", []string{doomed.Slug}, 4304)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.DeleteTeam(ctx, owner.ID, org.Slug, doomed.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.AcceptInvitation(ctx, doomedHash, "", 0, "Deleted team invitee", []byte("password-hash")); err != nil {
		t.Fatalf("accept after assigned team deletion: %v", err)
	}
	if _, err = db.CompatibleInvitation(ctx, doomedInvite.ID, doomedHash); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("consumed deleted-team invitation remained valid: %v", err)
	}
}
