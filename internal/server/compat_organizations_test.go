//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

func TestCreateCompatibleOrganizationHTTP(t *testing.T) {
	disabled := newResolutionHTTPFixture(t)
	disabled.request(t, disabled.client, "POST", "/api/0/organizations/", `{"name":"Disabled"}`, http.Header{"X-CSRF-Token": {disabled.csrf}}, 403)
	disabled.request(t, disabled.client, "POST", "/api/0/organizations/", `{"name":`, http.Header{"X-CSRF-Token": {disabled.csrf}}, 403)

	f := newResolutionHTTPFixtureWithConfig(t, func(cfg *config.Config) { cfg.OrganizationCreation = true })
	endpoint := "/api/0/organizations/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	f.request(t, http.DefaultClient, "POST", endpoint, `{"name":"Unauthorized"}`, nil, 401)
	var created store.CompatibleOrganizationDetail
	if err := json.Unmarshal(f.request(t, f.client, "POST", endpoint, `{"name":"Created HTTP Org"}`, csrf, 201), &created); err != nil || created.ID == "" || created.ID == f.org.ID || created.Slug != "created-http-org" || !contains(created.Access, "org:write") || len(created.Projects) != 0 || len(created.Teams) != 1 || created.Teams[0].Slug != "default" || !created.Teams[0].IsMember {
		t.Fatalf("create organization HTTP: %+v %v", created, err)
	}
	f.request(t, f.client, "POST", endpoint, `{"name":"Created HTTP Org"}`, csrf, 409)
	for _, body := range []string{`{}`, `{"name":"bad\u0000name"}`, `{"name":"Valid","unknown":true}`, `{"name":"Valid"} {}`} {
		f.request(t, f.client, "POST", endpoint, body, csrf, 400)
	}
	f.request(t, f.client, "POST", endpoint, `{"name":"No CSRF"}`, nil, 403)
	insertDiscoveryMember(t, f, "organization-creator-member@test", "Organization member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "organization-creator-member@test")
	f.request(t, memberClient, "POST", endpoint, `{"name":"Member org"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	insertDiscoveryMember(t, f, "organization-creator-manager@test", "Organization manager", "manager")
	managerClient, managerCSRF := loginManagementMember(t, f, "organization-creator-manager@test")
	f.request(t, managerClient, "POST", endpoint, `{"name":"Manager org"}`, http.Header{"X-CSRF-Token": {managerCSRF}}, 403)
	adminID := insertDiscoveryMember(t, f, "organization-creator-admin@test", "Organization admin", "admin")
	adminClient, adminCSRF := loginManagementMember(t, f, "organization-creator-admin@test")
	f.request(t, adminClient, "POST", endpoint, `{"name":"Admin session org"}`, http.Header{"X-CSRF-Token": {adminCSRF}}, 201)
	var adminToken struct{ Token string }
	adminBody := resolutionJSON(t, map[string]any{"name": "Creator admin writer", "scopes": []string{"org:write"}, "projectIds": []string{f.project.ID}, "expiresInDays": 1})
	if err := json.Unmarshal(f.request(t, adminClient, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", string(adminBody), http.Header{"X-CSRF-Token": {adminCSRF}}, 201), &adminToken); err != nil || adminToken.Token == "" {
		t.Fatalf("creator admin token: %+v %v", adminToken, err)
	}
	adminBearer := http.Header{"Authorization": {"Bearer " + adminToken.Token}}
	f.request(t, http.DefaultClient, "POST", endpoint, `{"name":"Admin bearer org"}`, adminBearer, 201)
	if _, err := f.sql.Exec(f.ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1::bigint AND user_id=$2::bigint`, f.org.ID, adminID); err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "POST", endpoint, `{"name":"Demoted admin org"}`, adminBearer, 403)

	readToken := issueManagementToken(t, f, f.org.Slug, "Organization read", []string{"org:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "POST", endpoint, `{"name":"Read org"}`, http.Header{"Authorization": {"Bearer " + readToken}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Organization write", []string{"org:write"}, []string{f.project.ID})
	var tokenCreated store.CompatibleOrganizationDetail
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", endpoint, `{"name":"Bearer HTTP Org"}`, http.Header{"Authorization": {"Bearer " + writeToken}}, 201), &tokenCreated); err != nil || tokenCreated.Slug != "bearer-http-org" || !contains(tokenCreated.Access, "org:write") {
		t.Fatalf("bearer organization creation: %+v %v", tokenCreated, err)
	}
}
