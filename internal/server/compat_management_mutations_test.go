//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

func TestCompatibleTeamMutationsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	base := "/api/0/organizations/" + f.org.Slug + "/teams/"
	created := f.request(t, f.client, "POST", base, `{"slug":"incident-response"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 201)
	var team store.CompatibleTeamProject
	if err := json.Unmarshal(created, &team); err != nil || team.Slug != "incident-response" || team.Projects == nil {
		t.Fatalf("create compatible team: %+v %v", team, err)
	}
	f.request(t, f.client, "POST", base, `{"slug":"incident-response"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 409)
	for _, body := range []string{`{"slug":"Bad Slug"}`, `{"slug":""}`, `{"slug":"` + strings.Repeat("x", 51) + `"}`, `{"slug":"ok","unknown":true}`} {
		f.request(t, f.client, "POST", base, body, http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	}
	teamPath := "/api/0/teams/" + f.org.Slug + "/incident-response/"
	if err := json.Unmarshal(f.request(t, f.client, "PUT", teamPath, `{"slug":"operations"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200), &team); err != nil || team.Slug != "operations" {
		t.Fatalf("update compatible team: %+v %v", team, err)
	}
	f.request(t, f.client, "GET", "/api/0/teams/"+f.org.Slug+"/incident-response/", "", nil, 404)
	f.request(t, f.client, "GET", "/api/0/teams/"+f.org.Slug+"/operations/", "", nil, 200)
	f.request(t, f.client, "PUT", "/api/0/teams/"+f.org.Slug+"/default/", `{"slug":"default"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
	f.request(t, f.client, "PUT", "/api/0/teams/"+f.org.Slug+"/default/", `{"slug":"renamed-default"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 403)
	f.request(t, f.client, "DELETE", "/api/0/teams/"+f.org.Slug+"/default/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 403)

	memberID := insertDiscoveryMember(t, f, "team-member@management.test", "Team member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "team-member@management.test")
	if memberID == "" {
		t.Fatal("member fixture missing")
	}
	f.request(t, memberClient, "POST", base, `{"slug":"member-team"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	f.request(t, memberClient, "PUT", "/api/0/teams/"+f.org.Slug+"/operations/", `{"slug":"member-team"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	f.request(t, memberClient, "DELETE", "/api/0/teams/"+f.org.Slug+"/operations/", "", http.Header{"X-CSRF-Token": {memberCSRF}}, 403)

	readToken := issueManagementToken(t, f, f.org.Slug, "Team reader", []string{"org:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "POST", base, `{"slug":"read-denied"}`, http.Header{"Authorization": {"Bearer " + readToken}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Team writer", []string{"org:write"}, []string{f.project.ID})
	writeHeader := http.Header{"Authorization": {"Bearer " + writeToken}}
	f.request(t, http.DefaultClient, "POST", base, `{"slug":"bearer-team"}`, writeHeader, 201)
	f.request(t, http.DefaultClient, "PUT", "/api/0/teams/"+f.org.Slug+"/bearer-team/", `{"slug":"bearer-renamed"}`, writeHeader, 200)
	f.request(t, http.DefaultClient, "DELETE", "/api/0/teams/"+f.org.Slug+"/bearer-renamed/", "", writeHeader, 204)
	f.request(t, f.client, "DELETE", "/api/0/teams/"+f.org.Slug+"/operations/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
}

func TestCompatibleOrganizationInvitationMutationHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	mailServer, mailClient, mailCSRF := newManagementMailServer(t, f)
	mailPath := "/api/0/organizations/" + f.org.Slug + "/members/"
	onCall, err := f.db.CreateTeam(f.ctx, f.owner.ID, f.org.Slug, "On call")
	if err != nil {
		t.Fatal(err)
	}
	var invitation compatibleInvitationView
	if err := json.Unmarshal(mailCall(t, f.ctx, mailClient, mailServer, "POST", mailPath, `{"email":"invitee@management.test","orgRole":"admin"}`, http.Header{"X-CSRF-Token": {mailCSRF}}, 201), &invitation); err != nil || invitation.ID == "" || invitation.Email != "invitee@management.test" || !invitation.Pending || invitation.User != nil || invitation.InviteLink != nil || invitation.Role != "admin" || invitation.RoleName != "Admin" {
		t.Fatalf("compatible invitation: %+v %v", invitation, err)
	}
	mailCall(t, f.ctx, mailClient, mailServer, "POST", mailPath, `{"email":"invitee@management.test","orgRole":"admin"}`, http.Header{"X-CSRF-Token": {mailCSRF}}, 409)
	for _, body := range []string{
		`{"email":"bad","orgRole":"member"}`,
		`{"email":"x@management.test","orgRole":"member","teamRoles":[{}]}`,
		`{"email":"x@management.test","orgRole":"member","teamRoles":[{"teamSlug":7}]}`,
		`{"email":"x@management.test","orgRole":"member","teamRoles":[{"teamSlug":"default","role":7}]}`,
		`{"email":"x@management.test","orgRole":"member","teamRoles":[{"teamSlug":"default","extra":true}]}`,
		`{"email":"x@management.test","orgRole":"member","sendInvite":false}`,
		`{"email":"x@management.test","orgRole":"member","reinvite":false}`,
		`{"email":"x@management.test","orgRole":"member","unknown":true}`,
	} {
		mailCall(t, f.ctx, mailClient, mailServer, "POST", mailPath, body, http.Header{"X-CSRF-Token": {mailCSRF}}, 400)
	}
	var managerInvitation compatibleInvitationView
	if err := json.Unmarshal(mailCall(t, f.ctx, mailClient, mailServer, "POST", mailPath,
		`{"email":"manager@management.test","orgRole":"manager","teamRoles":[{"teamSlug":"on-call","role":"contributor"},{"teamSlug":"on-call","role":"anything"},{"teamSlug":"missing"}]}`,
		http.Header{"X-CSRF-Token": {mailCSRF}}, 201), &managerInvitation); err != nil || managerInvitation.Role != "manager" || managerInvitation.RoleName != "Manager" || len(managerInvitation.Teams) != 1 || managerInvitation.Teams[0] != onCall.Slug {
		t.Fatalf("manager invitation with team assignments: %+v %v", managerInvitation, err)
	}
	var members []store.CompatibleOrganizationUser
	if err := json.Unmarshal(mailCall(t, f.ctx, mailClient, mailServer, "GET", mailPath, "", nil, 200), &members); err != nil || len(members) != 3 {
		t.Fatalf("pending invitation visibility: %+v %v", members, err)
	}
	invitationPath := mailPath + invitation.ID + "/"
	var pending store.CompatibleOrganizationUser
	if err := json.Unmarshal(mailCall(t, f.ctx, mailClient, mailServer, "GET", invitationPath, "", nil, 200), &pending); err != nil || !pending.Pending || pending.ID != invitation.ID || pending.Email != "invitee@management.test" || pending.User != nil || pending.Teams == nil || len(pending.Teams) != 0 {
		t.Fatalf("pending invitation detail: %+v %v", pending, err)
	}
	managerInvitationPath := mailPath + managerInvitation.ID + "/"
	if err := json.Unmarshal(mailCall(t, f.ctx, mailClient, mailServer, "GET", managerInvitationPath, "", nil, 200), &pending); err != nil || !pending.Pending || pending.Role != "manager" || len(pending.Teams) != 1 || pending.Teams[0] != onCall.Slug {
		t.Fatalf("pending manager assignment detail: %+v %v", pending, err)
	}
	mailCall(t, f.ctx, mailClient, mailServer, "PUT", invitationPath, `{"orgRole":"member"}`, http.Header{"X-CSRF-Token": {mailCSRF}}, 400)

	insertDiscoveryMember(t, f, "invitation-member@management.test", "Invitation member", "member")
	memberClient, memberCSRF := loginManagementMemberOn(t, f, mailServer, "invitation-member@management.test")
	mailCall(t, f.ctx, memberClient, mailServer, "GET", invitationPath, "", nil, 404)
	mailCall(t, f.ctx, memberClient, mailServer, "POST", mailPath, `{"email":"member-invite@management.test","orgRole":"member"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	adminID := insertDiscoveryMember(t, f, "invite-admin@management.test", "Invitation admin", "admin")
	adminClient, adminCSRF := loginManagementMemberOn(t, f, mailServer, "invite-admin@management.test")
	adminHeaders := http.Header{"X-CSRF-Token": {adminCSRF}}
	mailCall(t, f.ctx, adminClient, mailServer, "POST", mailPath, `{"email":"forbidden-manager@management.test","orgRole":"manager"}`, adminHeaders, 403)
	mailCall(t, f.ctx, adminClient, mailServer, "POST", mailPath, `{"email":"unjoined-team@management.test","orgRole":"member","teamRoles":[{"teamSlug":"on-call"}]}`, adminHeaders, 403)
	if err := f.db.AddTeamMember(f.ctx, f.owner.ID, f.org.Slug, onCall.ID, adminID); err != nil {
		t.Fatal(err)
	}
	mailCall(t, f.ctx, adminClient, mailServer, "POST", mailPath, `{"email":"joined-team@management.test","orgRole":"member","teamRoles":[{"teamSlug":"on-call"}]}`, adminHeaders, 201)
	readToken := issueManagementToken(t, f, f.org.Slug, "Invitation reader", []string{"org:read"}, []string{f.project.ID})
	mailCall(t, f.ctx, http.DefaultClient, mailServer, "POST", mailPath, `{"email":"read-denied@management.test","orgRole":"member"}`, http.Header{"Authorization": {"Bearer " + readToken}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Invitation writer", []string{"org:write"}, []string{f.project.ID})
	mailCall(t, f.ctx, http.DefaultClient, mailServer, "POST", mailPath, `{"email":"bearer-invite@management.test","orgRole":"member"}`, http.Header{"Authorization": {"Bearer " + writeToken}}, 201)
	mailCall(t, f.ctx, http.DefaultClient, mailServer, "DELETE", invitationPath, "", http.Header{"Authorization": {"Bearer " + writeToken}}, 204)
	mailCall(t, f.ctx, mailClient, mailServer, "GET", invitationPath, "", nil, 404)
}

func TestCompatibleOrganizationUpdateDeleteHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	orgPath := "/api/0/organizations/" + f.org.Slug + "/"
	updated := f.request(t, f.client, "PUT", orgPath, `{"name":"  Renamed HTTP organization  "}`, http.Header{"X-CSRF-Token": {f.csrf}}, 200)
	var detail store.CompatibleOrganizationDetail
	if err := json.Unmarshal(updated, &detail); err != nil || detail.ID == "" || detail.Name != "Renamed HTTP organization" || detail.Slug != f.org.Slug || detail.DateCreated.IsZero() || len(detail.Teams) != 1 || len(detail.Projects) != 1 {
		t.Fatalf("organization update response: %+v %v", detail, err)
	}
	for _, body := range []string{`{"name":""}`, `{"name":"` + strings.Repeat("界", 201) + `"}`, `{"name":"x","unknown":true}`} {
		f.request(t, f.client, "PUT", orgPath, body, http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	}
	insertDiscoveryMember(t, f, "org-admin@management.test", "Org admin", "admin")
	insertDiscoveryMember(t, f, "org-manager@management.test", "Org manager", "manager")
	insertDiscoveryMember(t, f, "org-member@management.test", "Org member", "member")
	adminClient, adminCSRF := loginManagementMember(t, f, "org-admin@management.test")
	managerClient, managerCSRF := loginManagementMember(t, f, "org-manager@management.test")
	memberClient, memberCSRF := loginManagementMember(t, f, "org-member@management.test")
	f.request(t, adminClient, "PUT", orgPath, `{"name":"Admin rename"}`, http.Header{"X-CSRF-Token": {adminCSRF}}, 200)
	f.request(t, managerClient, "PUT", orgPath, `{"name":"Manager rename"}`, http.Header{"X-CSRF-Token": {managerCSRF}}, 200)
	f.request(t, memberClient, "PUT", orgPath, `{"name":"Member rename"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	readToken := issueManagementToken(t, f, f.org.Slug, "Org reader", []string{"org:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "PUT", orgPath, `{"name":"Read denied"}`, http.Header{"Authorization": {"Bearer " + readToken}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Org writer", []string{"org:write"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "PUT", orgPath, `{"name":"Bearer rename"}`, http.Header{"Authorization": {"Bearer " + writeToken}}, 200)
	f.request(t, adminClient, "DELETE", orgPath, "", http.Header{"X-CSRF-Token": {adminCSRF}}, 403)
	f.request(t, managerClient, "DELETE", orgPath, "", http.Header{"X-CSRF-Token": {managerCSRF}}, 403)
	f.request(t, memberClient, "DELETE", orgPath, "", http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	var adminToken struct{ Token string }
	adminBody := resolutionJSON(t, map[string]any{"name": "Admin org writer", "scopes": []string{"org:write"}, "projectIds": []string{f.project.ID}, "expiresInDays": 1})
	if err := json.Unmarshal(f.request(t, adminClient, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", string(adminBody), http.Header{"X-CSRF-Token": {adminCSRF}}, 201), &adminToken); err != nil || adminToken.Token == "" {
		t.Fatalf("admin token: %+v %v", adminToken, err)
	}
	adminBearer := http.Header{"Authorization": {"Bearer " + adminToken.Token}}
	f.request(t, http.DefaultClient, "DELETE", orgPath, "", adminBearer, 403)
	f.request(t, http.DefaultClient, "PUT", orgPath, `{"name":"Admin bearer rename"}`, adminBearer, 200)
	f.request(t, f.client, "DELETE", orgPath, "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
	f.request(t, f.client, "GET", orgPath, "", nil, 404)
}

func newManagementMailServer(t *testing.T, f *resolutionHTTPFixture) (*httptest.Server, *http.Client, string) {
	t.Helper()
	cfg := config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	var console bytes.Buffer
	sender, err := notify.NewSender(notify.Options{Domain: cfg.Domain, EmailURL: "consolemail://", FromEmail: "accounts@example.test", Timeout: time.Second, Console: &console})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sender.Close)
	box, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewWithAccountMail(cfg, f.db, sender, box))
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "POST", "/api/v1/login", `{"email":"owner@resolution.test","password":"resolution-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("mail fixture login: %+v %v", session, err)
	}
	return server, client, session.CSRFToken
}

func loginManagementMemberOn(t *testing.T, f *resolutionHTTPFixture, server *httptest.Server, email string) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	var session struct{ CSRFToken string }
	if err := json.Unmarshal(mailCall(t, f.ctx, client, server, "POST", "/api/v1/login", `{"email":"`+email+`","password":"resolution-http-password"}`, nil, 200), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("member login: %+v %v", session, err)
	}
	return client, session.CSRFToken
}

func mailCall(t *testing.T, ctx context.Context, client *http.Client, server *httptest.Server, method, path, body string, headers http.Header, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, server.URL+"/good"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
	}
	return raw
}

func TestOrgWriteScopeUsesCurrentMembershipHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	teams := "/api/0/organizations/" + f.org.Slug + "/teams/"
	adminID := insertDiscoveryMember(t, f, "scope-admin@management.test", "Scope admin", "admin")
	adminClient, adminCSRF := loginManagementMember(t, f, "scope-admin@management.test")
	adminHeaders := http.Header{"X-CSRF-Token": {adminCSRF}}
	var issued struct{ Token string }
	body := resolutionJSON(t, map[string]any{"name": "Admin org writer", "scopes": []string{"org:write"}, "projectIds": []string{f.project.ID}, "expiresInDays": 1})
	if err := json.Unmarshal(f.request(t, adminClient, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, adminHeaders, 201), &issued); err != nil || issued.Token == "" {
		t.Fatalf("admin token: %+v %v", issued, err)
	}
	bearer := http.Header{"Authorization": {"Bearer " + issued.Token}}
	f.request(t, http.DefaultClient, "POST", teams, `{"slug":"scope-token-team"}`, bearer, 201)
	f.request(t, adminClient, "POST", teams, `{"slug":"scope-session-team"}`, adminHeaders, 201)

	if _, err := f.sql.Exec(f.ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1::bigint AND user_id=$2::bigint`, f.org.ID, adminID); err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "POST", teams, `{"slug":"demoted-token-team"}`, bearer, 403)
	f.request(t, adminClient, "POST", teams, `{"slug":"demoted-session-team"}`, adminHeaders, 403)

	if _, err := f.sql.Exec(f.ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, f.org.ID, adminID); err != nil {
		t.Fatal(err)
	}
	// Removing the creator's membership invalidates the credential itself, so the
	// Bearer request is rejected before any organization lookup.
	f.request(t, http.DefaultClient, "POST", teams, `{"slug":"removed-token-team"}`, bearer, 401)
	f.request(t, adminClient, "POST", teams, `{"slug":"removed-session-team"}`, adminHeaders, 404)
}
