//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/store"
)

func TestCompatibleRepositoriesHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/0/organizations/" + f.org.Slug + "/repos/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	f.request(t, http.DefaultClient, "POST", path, `{"name":"acme/backend"}`, nil, 401)
	insertDiscoveryMember(t, f, "repo-member@test", "Repo member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "repo-member@test")
	f.request(t, memberClient, "POST", path, `{"name":"acme/backend"}`, http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	f.request(t, f.client, "POST", path, `{"name":"bad\u0000name"}`, csrf, 400)
	f.request(t, f.client, "POST", path, `{"name":"acme/backend","unknown":true}`, csrf, 400)
	var repository store.ReleaseRepository
	if err := json.Unmarshal(f.request(t, f.client, "POST", path, `{"name":"acme/backend","url":"https://git.example.test/acme/backend","provider":{"id":"gitlab"}}`, csrf, 201), &repository); err != nil || repository.ID == "" || repository.Name != "acme/backend" || repository.URL == nil {
		t.Fatalf("create repository HTTP: %+v %v", repository, err)
	}
	f.request(t, f.client, "POST", path, `{"name":"acme/backend"}`, csrf, 409)
	var repositories []store.ReleaseRepository
	if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &repositories); err != nil || len(repositories) != 1 || repositories[0].ID != repository.ID {
		t.Fatalf("list repositories HTTP: %+v %v", repositories, err)
	}
	readToken := issueManagementToken(t, f, f.org.Slug, "Repo read", []string{"org:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "POST", path, `{"name":"read/denied"}`, http.Header{"Authorization": {"Bearer " + readToken}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Repo write", []string{"org:write"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "POST", path, `{"name":"acme/worker"}`, http.Header{"Authorization": {"Bearer " + writeToken}}, 201)
}
