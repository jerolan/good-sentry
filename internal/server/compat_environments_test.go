//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"goodsentry/internal/store"
)

func TestCompatibleEnvironmentsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	capture := func(id, environment string) {
		body := "{}\n{\"type\":\"event\"}\n" + fmt.Sprintf(`{"event_id":%q,"message":"environment %s","environment":%q}`, id, environment, environment)
		f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/?sentry_version=7&sentry_key="+f.project.PublicKey, body, nil, 200)
	}
	capture("e0000000000000000000000000000001", "production")
	capture("e0000000000000000000000000000002", "staging")
	orgBase := "/api/0/organizations/" + f.org.Slug + "/environments/"
	projectBase := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/environments/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}

	f.request(t, http.DefaultClient, "GET", orgBase, "", nil, 401)
	f.request(t, http.DefaultClient, "GET", projectBase, "", nil, 401)
	var environments []store.CompatibleEnvironmentProject
	if err := json.Unmarshal(f.request(t, f.client, "GET", projectBase, "", nil, 200), &environments); err != nil || len(environments) != 2 || environments[0].Name != "production" || environments[0].ID != nil || environments[0].IsHidden || environments[1].Name != "staging" {
		t.Fatalf("project environments: %+v %v", environments, err)
	}
	var orgEnvironments []store.CompatibleEnvironment
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase, "", nil, 200), &orgEnvironments); err != nil || len(orgEnvironments) != 2 || orgEnvironments[0].Name != "production" || orgEnvironments[0].ID != nil {
		t.Fatalf("organization environments: %+v %v", orgEnvironments, err)
	}
	for _, query := range []string{"visibility=bad", "limit=0", "limit=101", "cursor=bad", "unknown=x", "visibility=all&visibility=hidden"} {
		f.request(t, f.client, "GET", projectBase+"?"+query, "", nil, 400)
	}
	f.request(t, f.client, "GET", projectBase+"?limit=1", "", nil, 200)
	if link := keyLink(t, f, projectBase+"?limit=1"); link == "" {
		t.Fatal("environment pagination link missing")
	}

	memberPath := projectBase + "production/"
	insertDiscoveryMember(t, f, "environment-member@test", "Environment member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "environment-member@test")
	memberHeaders := http.Header{"X-CSRF-Token": {memberCSRF}}
	f.request(t, memberClient, "GET", projectBase, "", nil, 200)
	f.request(t, memberClient, "PUT", memberPath, `{"isHidden":true}`, memberHeaders, 403)
	f.request(t, f.client, "PUT", memberPath, `{"isHidden":true}`, nil, 403)
	var hidden store.CompatibleEnvironmentProject
	if err := json.Unmarshal(f.request(t, f.client, "PUT", memberPath, `{"isHidden":true}`, csrf, 200), &hidden); err != nil || !hidden.IsHidden || hidden.Name != "production" || hidden.ID != nil {
		t.Fatalf("hide environment: %+v %v", hidden, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", projectBase, "", nil, 200), &environments); err != nil || len(environments) != 1 || environments[0].Name != "staging" {
		t.Fatalf("visible environments after hide: %+v %v", environments, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", projectBase+"?visibility=hidden", "", nil, 200), &environments); err != nil || len(environments) != 1 || environments[0].Name != "production" || !environments[0].IsHidden {
		t.Fatalf("hidden environments: %+v %v", environments, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", memberPath, "", nil, 200), &hidden); err != nil || !hidden.IsHidden {
		t.Fatalf("environment detail: %+v %v", hidden, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", memberPath, `{"name":"production","isHidden":false}`, csrf, 200), &hidden); err != nil || hidden.IsHidden {
		t.Fatalf("restore environment: %+v %v", hidden, err)
	}
	for _, body := range []string{`{}`, `{"isHidden":true,"name":"staging"}`, `{"isHidden":true,"unknown":true}`, `{"isHidden":true} {}`} {
		f.request(t, f.client, "PUT", memberPath, body, csrf, 400)
	}
	f.request(t, f.client, "PUT", projectBase+"missing/", `{"isHidden":true}`, csrf, 404)
	f.request(t, f.client, "GET", projectBase+"missing/", "", nil, 404)

	orgToken := issueManagementToken(t, f, f.org.Slug, "Environment org read", []string{"org:read"}, []string{f.project.ID})
	orgHeaders := http.Header{"Authorization": {"Bearer " + orgToken}}
	f.request(t, http.DefaultClient, "GET", orgBase, "", orgHeaders, 200)
	projectToken := issueManagementToken(t, f, f.org.Slug, "Environment project read", []string{"project:read"}, []string{f.project.ID})
	projectHeaders := http.Header{"Authorization": {"Bearer " + projectToken}}
	f.request(t, http.DefaultClient, "GET", projectBase, "", projectHeaders, 200)
	f.request(t, http.DefaultClient, "PUT", memberPath, `{"isHidden":true}`, projectHeaders, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Environment write", []string{"project:write"}, []string{f.project.ID})
	writeHeaders := http.Header{"Authorization": {"Bearer " + writeToken}}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "PUT", memberPath, `{"isHidden":true}`, writeHeaders, 200), &hidden); err != nil || !hidden.IsHidden {
		t.Fatalf("bearer environment update: %+v %v", hidden, err)
	}

	var secondOrg, secondProject string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('environment-second','Environment second') RETURNING id::text`).Scan(&secondOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, secondOrg, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO projects(organization_id,name,slug,platform) VALUES($1::bigint,'Environment foreign','environment-foreign','go') RETURNING id::text`, secondOrg).Scan(&secondProject); err != nil {
		t.Fatal(err)
	}
	foreignToken := issueManagementToken(t, f, "environment-second", "Environment foreign", []string{"project:write", "org:read"}, []string{secondProject})
	foreignHeaders := http.Header{"Authorization": {"Bearer " + foreignToken}}
	f.request(t, http.DefaultClient, "GET", orgBase, "", foreignHeaders, 404)
	f.request(t, http.DefaultClient, "PUT", memberPath, `{"isHidden":true}`, foreignHeaders, 404)
}
