//go:build integration

package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/store"
)

type compatibleInvitationWire struct {
	AcceptInvite bool `json:"acceptInvite"`
	OrgUser      struct {
		ID           string                       `json:"id"`
		Role         string                       `json:"role"`
		RoleName     string                       `json:"roleName"`
		DateCreated  time.Time                    `json:"dateCreated"`
		Email        string                       `json:"email"`
		User         *store.CompatibleUser        `json:"user"`
		Pending      bool                         `json:"pending"`
		IsOwner      bool                         `json:"isOwner"`
		Teams        []string                     `json:"teams"`
		Organization store.CompatibleOrganization `json:"organization"`
	} `json:"orgUser"`
}

func compatibleInvitationHash(value string) string {
	mac := hmac.New(sha256.New, []byte(strings.Repeat("r", 64)))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func createSentInvitation(t *testing.T, f *resolutionHTTPFixture, email, role string, teamSlugs ...string) (store.Invitation, string) {
	t.Helper()
	tokenBytes := sha256.Sum256([]byte(email))
	token := hex.EncodeToString(tokenBytes[:])
	hash := compatibleInvitationHash(token)
	box, err := accountmail.NewSecretBox(strings.Repeat("r", 64))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Seal(hash, token)
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.db.InviteMemberWithTeams(f.ctx, f.owner.ID, f.org.Slug, email, role, teamSlugs, hash, ciphertext, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.sql.Exec(f.ctx, `UPDATE organization_invitations SET status='sent',sent_at=clock_timestamp() WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	return item, token
}

func TestCompatibleInvitationHTTPAcceptance(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	onCall, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	item, token := createSentInvitation(t, f, "compat-http@example.test", "member", onCall.Slug)
	path := "/api/0/accept/" + item.ID + "/" + token + "/"
	var pending compatibleInvitationWire
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", path, "", nil, 200), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.AcceptInvite || pending.OrgUser.ID != item.ID || pending.OrgUser.Email != item.Email || pending.OrgUser.Role != "member" || !pending.OrgUser.Pending || pending.OrgUser.User != nil || len(pending.OrgUser.Teams) != 1 || pending.OrgUser.Teams[0] != onCall.Slug || pending.OrgUser.Organization.Slug != f.org.Slug {
		t.Fatalf("pending invitation=%+v", pending)
	}
	f.request(t, http.DefaultClient, "GET", "/api/0/accept/"+strings.Repeat("0", 32)+"/"+token+"/", "", nil, 403)
	f.request(t, http.DefaultClient, "GET", "/api/0/accept/"+item.ID+"/"+strings.Repeat("0", 64)+"/", "", nil, 403)
	f.request(t, http.DefaultClient, "POST", path, `{"acceptInvite":true}`, nil, 401)

	var inviteeID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('compat-http@example.test',$1,'Compatible HTTP invitee') RETURNING id::text`, f.hash).Scan(&inviteeID); err != nil {
		t.Fatal(err)
	}
	invitee, _ := loginManagementMember(t, f, "compat-http@example.test")
	f.request(t, invitee, "POST", path, `{"acceptInvite":true}`, http.Header{"Sec-Fetch-Site": {"cross-site"}}, 403)
	f.request(t, invitee, "POST", path, `{}`, nil, 400)
	var declined compatibleInvitationWire
	if err := json.Unmarshal(f.request(t, invitee, "POST", path, `{"acceptInvite":false}`, nil, 200), &declined); err != nil || declined.AcceptInvite || !declined.OrgUser.Pending {
		t.Fatalf("declined=%+v err=%v", declined, err)
	}
	var accepted compatibleInvitationWire
	if err := json.Unmarshal(f.request(t, invitee, "POST", path, `{"acceptInvite":true}`, nil, 200), &accepted); err != nil {
		t.Fatal(err)
	}
	if !accepted.AcceptInvite || accepted.OrgUser.ID != inviteeID || accepted.OrgUser.Pending || accepted.OrgUser.User == nil || accepted.OrgUser.User.Email != "compat-http@example.test" || accepted.OrgUser.Organization.Slug != f.org.Slug || len(accepted.OrgUser.Teams) != 1 || accepted.OrgUser.Teams[0] != onCall.Slug {
		t.Fatalf("accepted=%+v", accepted)
	}
	f.request(t, invitee, "POST", path, `{"acceptInvite":true}`, nil, 403)

	other, otherToken := createSentInvitation(t, f, "expected-other@example.test", "member")
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('other-invite@example.test',$1,'Other invitee')`, f.hash); err != nil {
		t.Fatal(err)
	}
	otherClient, _ := loginManagementMember(t, f, "other-invite@example.test")
	f.request(t, otherClient, "POST", "/api/0/accept/"+other.ID+"/"+otherToken+"/", `{"acceptInvite":true}`, nil, 403)
}

func TestCompatibleInvitationHTTPBearerAndSecondaryEmail(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	item, token := createSentInvitation(t, f, "bearer-invite@example.test", "admin")
	var userID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('bearer-invite@example.test',$1,'Bearer invitee') RETURNING id::text`, f.hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("b", 64)
	if _, err := f.db.CreateUserAPIToken(f.ctx, userID, "Invite bearer", store.UserTokenMask([]string{"org:read"}), compatibleInvitationHash(secret), []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	var accepted compatibleInvitationWire
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", "/api/0/accept/"+item.ID+"/"+token+"/", `{"acceptInvite":true}`, http.Header{"Authorization": {"Bearer " + secret}}, 200), &accepted); err != nil {
		t.Fatal(err)
	}
	if !accepted.AcceptInvite || accepted.OrgUser.ID != userID || accepted.OrgUser.Role != "admin" || accepted.OrgUser.User == nil {
		t.Fatalf("bearer accepted=%+v", accepted)
	}

	secondaryInvite, secondaryToken := createSentInvitation(t, f, "secondary-invite@example.test", "member")
	var secondaryID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,password_hash,name) VALUES('secondary-primary@example.test',$1,'Secondary primary') RETURNING id::text`, f.hash).Scan(&secondaryID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO user_emails(user_id,email,is_verified) VALUES($1::bigint,'secondary-invite@example.test',true)`, secondaryID); err != nil {
		t.Fatal(err)
	}
	primary, _ := loginManagementMember(t, f, "secondary-primary@example.test")
	var secondaryAccepted compatibleInvitationWire
	if err := json.Unmarshal(f.request(t, primary, "POST", "/api/0/accept/"+secondaryInvite.ID+"/"+secondaryToken+"/", `{"acceptInvite":true}`, nil, 200), &secondaryAccepted); err != nil || !secondaryAccepted.AcceptInvite || secondaryAccepted.OrgUser.ID != secondaryID {
		t.Fatalf("secondary accepted=%+v err=%v", secondaryAccepted, err)
	}
}
