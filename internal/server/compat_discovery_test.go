//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

func TestCompatibleDiscoveryHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	var secondOrgID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('discovery-http-second','Discovery HTTP second') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrgID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	var secondProjectID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Discovery second project','discovery-http-project','go') RETURNING id::text`, secondOrgID).Scan(&secondProjectID); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/0/", "/api/0/users/", "/api/0/organizations/", "/api/0/projects/", "/api/0/instance-license/", "/api/0/instance-license/support-link/"} {
		f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	}
	f.request(t, http.DefaultClient, "GET", "/api/settings/", "", nil, 200)
	f.request(t, http.DefaultClient, "GET", "/api/settings/unknown/", "", nil, 200)

	var root struct {
		Version string
		User    store.CompatibleUser
		Auth    any
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/", "", nil, 200), &root); err != nil {
		t.Fatal(err)
	}
	if root.Version != "0" || root.User.ID != f.owner.ID || root.User.Email != f.owner.Email || root.User.Username != f.owner.Email || root.User.Name != f.owner.Name || root.User.DateJoined.IsZero() || !root.User.HasPasswordAuth || root.User.Identities == nil || root.Auth != nil {
		t.Fatalf("API root/user DTO: %+v", root)
	}
	var users []store.CompatibleUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/users/", "", nil, 200), &users); err != nil || len(users) != 1 || users[0].ID != f.owner.ID {
		t.Fatalf("current-user list: %+v %v", users, err)
	}
	for _, path := range []string{"/api/0/users/me/", "/api/0/users/" + f.owner.ID + "/"} {
		var user store.CompatibleUser
		if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &user); err != nil || user.ID != f.owner.ID {
			t.Fatalf("current-user detail %s: %+v %v", path, user, err)
		}
	}
	for _, path := range []string{"/api/0/users/01/", "/api/0/users/999999999/", "/api/0/users/0/"} {
		f.request(t, f.client, "GET", path, "", nil, 404)
	}

	var organizations []store.CompatibleOrganization
	organizationResponse := f.request(t, f.client, "GET", "/api/0/organizations/", "", nil, 200)
	if err := json.Unmarshal(organizationResponse, &organizations); err != nil || len(organizations) != 2 {
		t.Fatalf("organization discovery: %+v %v", organizations, err)
	}
	// Sentry CLI requires this exact wire spelling. Go's case-insensitive
	// struct decoder would hide an incompatible lowercase JSON field.
	var organizationWire []map[string]any
	if err := json.Unmarshal(organizationResponse, &organizationWire); err != nil {
		t.Fatal(err)
	}
	for _, organization := range organizationWire {
		if required, exists := organization["require2FA"]; !exists || required != false {
			t.Fatalf("organization lacks the CLI-compatible require2FA boolean: %s", organizationResponse)
		}
	}
	if organizations[0].Slug != "discovery-http-second" || organizations[1].ID != f.org.ID || organizations[1].DateCreated.IsZero() || !organizations[1].IsAcceptingEvents {
		t.Fatalf("organization DTO/order: %+v", organizations)
	}
	var filtered []store.CompatibleOrganization
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/organizations/?query=resolution&owner=true", "", nil, 200), &filtered); err != nil || len(filtered) != 1 || filtered[0].ID != f.org.ID {
		t.Fatalf("organization filter: %+v %v", filtered, err)
	}
	for _, query := range []string{"unknown=x", "owner=maybe", "limit=0", "limit=101", "limit=1&per_page=2", "cursor=bad", "query=x&query=y", "sortBy=bad", "query=%zz"} {
		f.request(t, f.client, "GET", "/api/0/organizations/?"+query, "", nil, 400)
	}

	response := f.request(t, f.client, "GET", "/api/0/organizations/?limit=1", "", nil, 200)
	if err := json.Unmarshal(response, &filtered); err != nil || len(filtered) != 1 {
		t.Fatalf("organization page: %+v %v", filtered, err)
	}
	linkFor := func(path string) string {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("link page %s=%d", path, res.StatusCode)
		}
		return res.Header.Get("Link")
	}
	if link := linkFor("/api/0/organizations/?limit=1"); !strings.Contains(link, "/good/api/0/organizations/?") || !strings.Contains(link, `rel="next"`) || !strings.Contains(link, "cursor=") {
		t.Fatalf("organization pagination Link: %q", link)
	}

	var projects []store.CompatibleProject
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/projects/", "", nil, 200), &projects); err != nil || len(projects) != 2 {
		t.Fatalf("project discovery: %+v %v", projects, err)
	}
	if projects[0].ID != f.project.ID || projects[0].Organization.ID != f.org.ID || projects[1].ID != secondProjectID || projects[1].Organization.ID != secondOrgID || !projects[0].IsMember || projects[0].Features == nil {
		t.Fatalf("project DTO/scope: %+v", projects)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/projects/?limit=1", "", nil, 200), &projects); err != nil || len(projects) != 1 {
		t.Fatalf("project page: %+v %v", projects, err)
	}
	if link := linkFor("/api/0/projects/?limit=1"); !strings.Contains(link, "/good/api/0/projects/?") || !strings.Contains(link, `rel="next"`) {
		t.Fatalf("project pagination Link: %q", link)
	}

	issueToken := func(orgSlug, name string, scopes []string, projects []string) (string, string) {
		t.Helper()
		body := resolutionJSON(t, map[string]any{"name": name, "scopes": scopes, "projectIds": projects, "expiresInDays": 1})
		var created struct{ ID, Token string }
		if err := json.Unmarshal(f.request(t, f.client, "POST", "/api/v1/organizations/"+orgSlug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &created); err != nil || created.ID == "" || created.Token == "" {
			t.Fatalf("issue token: %+v %v", created, err)
		}
		return created.ID, created.Token
	}
	_, firstToken := issueToken(f.org.Slug, "Discovery first project", []string{"org:read", "project:read"}, []string{f.project.ID})
	firstHeaders := http.Header{"Authorization": {"Bearer " + firstToken}}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/projects/", "", firstHeaders, 200), &projects); err != nil || len(projects) != 1 || projects[0].ID != f.project.ID {
		t.Fatalf("project token scope: %+v %v", projects, err)
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/organizations/", "", firstHeaders, 200), &organizations); err != nil || len(organizations) != 1 || organizations[0].ID != f.org.ID {
		t.Fatalf("organization token scope: %+v %v", organizations, err)
	}
	_, secondToken := issueToken("discovery-http-second", "Discovery second project", []string{"project:read"}, []string{secondProjectID})
	secondHeaders := http.Header{"Authorization": {"Bearer " + secondToken}}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/projects/", "", secondHeaders, 200), &projects); err != nil || len(projects) != 1 || projects[0].ID != secondProjectID {
		t.Fatalf("second project token scope: %+v %v", projects, err)
	}
	f.request(t, http.DefaultClient, "GET", "/api/0/organizations/", "", secondHeaders, 403)
	eventID, eventToken := issueToken(f.org.Slug, "Discovery event reader", []string{"event:read"}, []string{f.project.ID})
	eventHeaders := http.Header{"Authorization": {"Bearer " + eventToken}}
	f.request(t, http.DefaultClient, "GET", "/api/0/projects/", "", eventHeaders, 403)
	f.request(t, http.DefaultClient, "GET", "/api/0/", "", eventHeaders, 200)
	f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+eventID, "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
	f.request(t, http.DefaultClient, "GET", "/api/0/", "", eventHeaders, 401)

	var settings struct {
		SocialApps                 []any    `json:"socialApps"`
		BillingEnabled             bool     `json:"billingEnabled"`
		EnableUserRegistration     bool     `json:"enableUserRegistration"`
		EnableOrganizationCreation bool     `json:"enableOrganizationCreation"`
		Environment                string   `json:"environment"`
		Version                    string   `json:"version"`
		GlitchtipInstanceName      string   `json:"glitchtipInstanceName"`
		EnabledFeatures            []string `json:"enabledFeatures"`
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/settings/", "", nil, 200), &settings); err != nil || settings.SocialApps == nil || settings.BillingEnabled || settings.EnableUserRegistration || settings.EnableOrganizationCreation || settings.Environment != "production" || settings.Version == "" || settings.GlitchtipInstanceName != "GoodSentry" || len(settings.EnabledFeatures) == 0 {
		t.Fatalf("public settings: %+v %v", settings, err)
	}
	var organizationSettings struct {
		SocialApps []any `json:"socialApps"`
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/settings/unknown/", "", nil, 200), &organizationSettings); err != nil || organizationSettings.SocialApps == nil || len(organizationSettings.SocialApps) != 0 {
		t.Fatalf("organization settings: %+v %v", organizationSettings, err)
	}
	var license map[string]string
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/instance-license/", "", nil, 200), &license); err != nil || license["billingEmail"] != "" {
		t.Fatalf("instance license: %+v %v", license, err)
	}
	var support map[string]string
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/0/instance-license/support-link/", "", nil, 200), &support); err != nil || !strings.HasPrefix(support["url"], "https://github.com/jerolan/good-sentry/") {
		t.Fatalf("support link: %+v %v", support, err)
	}
}

func TestGitLabErrorTrackingDiscoveryContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	token := issueManagementToken(t, f, f.org.Slug, "GitLab error tracking", []string{"org:read", "project:read", "event:read"}, []string{})
	headers := http.Header{"Authorization": {"Bearer " + token}}
	var projects []store.CompatibleProject
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/projects/", "", headers, 200), &projects); err != nil || len(projects) != 1 || projects[0].Organization.Slug != f.org.Slug {
		t.Fatalf("GitLab project discovery: %+v %v", projects, err)
	}
	issuesPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/issues/?query=is%3Aunresolved&limit=10&sort=freq"
	var issues []map[string]any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", issuesPath, "", headers, 200), &issues); err != nil || len(issues) != 1 || issues[0]["id"] != f.issueID {
		t.Fatalf("GitLab issue discovery: %+v %v", issues, err)
	}
	var detail map[string]any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/issues/"+f.issueID+"/", "", headers, 200), &detail); err != nil || detail["shortId"] == "" {
		t.Fatalf("GitLab issue detail: %+v %v", detail, err)
	}
	var event map[string]any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/issues/"+f.issueID+"/events/latest/", "", headers, 200), &event); err != nil || event["groupID"] != f.issueID {
		t.Fatalf("GitLab latest event: %+v %v", event, err)
	}
	var repos []any
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", "/api/0/organizations/"+f.org.Slug+"/repos/", "", headers, 200), &repos); err != nil || len(repos) != 0 {
		t.Fatalf("GitLab repository contract: %+v %v", repos, err)
	}
}
