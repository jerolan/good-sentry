//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/store"
)

func TestCompatibleSetOrganizationOwnerHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	adminID := insertDiscoveryMember(t, f, "set-owner-admin@test", "Set owner admin", "admin")
	memberID := insertDiscoveryMember(t, f, "set-owner-member@test", "Set owner member", "member")
	orgBase := "/api/0/organizations/" + f.org.Slug + "/members/"
	adminPath := orgBase + adminID + "/set_owner/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}

	adminClient, adminCSRF := loginManagementMember(t, f, "set-owner-admin@test")
	f.request(t, adminClient, "POST", adminPath, "", http.Header{"X-CSRF-Token": {adminCSRF}}, 403)
	f.request(t, f.client, "POST", adminPath, "", nil, 403)
	f.request(t, f.client, "POST", adminPath+"?unknown=1", "", csrf, 400)
	f.request(t, f.client, "POST", orgBase+"not-a-number/set_owner/", "", csrf, 400)
	f.request(t, f.client, "POST", orgBase+"999999999/set_owner/", "", csrf, 404)
	var owner store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "POST", adminPath, "", csrf, 200), &owner); err != nil || owner.ID != adminID || owner.Role != "owner" || !owner.IsOwner {
		t.Fatalf("set owner HTTP: %+v %v", owner, err)
	}
	f.request(t, f.client, "POST", orgBase+memberID+"/set_owner/", "", csrf, 403)
	var owners []store.CompatibleOrganizationUser
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgBase, "", nil, 200), &owners); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, member := range owners {
		if member.Role == "owner" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("owner count=%d members=%+v", count, owners)
	}
}
