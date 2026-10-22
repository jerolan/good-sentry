//go:build integration

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"goodsentry/internal/config"
)

func TestOpenAPIIsOptInAndReflectsRegisteredRoutes(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		f := newResolutionHTTPFixture(t)
		f.request(t, f.client, "GET", "/api/openapi.json", "", nil, 404)
		f.request(t, f.client, "GET", "/api/docs/", "", nil, 404)
	})

	t.Run("enabled", func(t *testing.T) {
		f := newResolutionHTTPFixtureWithConfig(t, func(c *config.Config) { c.OpenAPI = true })
		raw := f.request(t, f.client, "GET", "/api/openapi.json", "", nil, 200)
		var document struct {
			OpenAPI string                    `json:"openapi"`
			Info    map[string]any            `json:"info"`
			Paths   map[string]map[string]any `json:"paths"`
			Servers []map[string]any          `json:"servers"`
			Routes  int                       `json:"x-goodsentry-route-count"`
		}
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatalf("document is not JSON: %v", err)
		}
		if document.OpenAPI != "3.1.0" || document.Info["title"] != "GoodSentry API" || document.Paths["/api/v1/issues/{issue}"] == nil || document.Paths["/api/0/organizations/{org}/issues/{issue}/"] == nil || document.Paths["/api/v1/issues/{issue}"]["get"] == nil {
			t.Fatalf("incomplete OpenAPI document: %+v", document)
		}
		if len(document.Servers) != 1 || document.Servers[0]["url"] != "https://public.example.test/good" || document.Routes < 100 {
			t.Fatalf("server or route matrix missing: %+v routes=%d", document.Servers, document.Routes)
		}
		operation, _ := document.Paths["/api/v1/issues/{issue}"]["get"].(map[string]any)
		authentication, _ := operation["x-goodsentry-authentication"].(string)
		if authentication == "" || strings.Contains(authentication, "Bearer") {
			t.Fatalf("native session route advertised unsupported Bearer authority: %q", authentication)
		}
		for path := range document.Paths {
			if strings.Contains(path, "{$}") {
				t.Fatalf("ServeMux sentinel leaked into OpenAPI path %q", path)
			}
		}
		page := f.request(t, f.client, "GET", "/api/docs/", "", nil, 200)
		if !strings.Contains(string(page), "GoodSentry API") || !strings.Contains(string(page), "/api/v1/issues/{issue}") || strings.Contains(string(page), "/api/v1/admin/overview") {
			t.Fatalf("documentation page does not reflect enabled routes: %s", page)
		}
	})
}

func TestAdministrationIsOptInAndOrganizationScoped(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		f := newResolutionHTTPFixture(t)
		f.request(t, f.client, "GET", "/api/v1/admin/overview", "", nil, 404)
	})

	t.Run("enabled", func(t *testing.T) {
		f := newResolutionHTTPFixtureWithConfig(t, func(c *config.Config) { c.Admin = true })
		var me struct {
			Features map[string]bool `json:"features"`
		}
		if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/v1/me", "", nil, 200), &me); err != nil || !me.Features["admin"] {
			t.Fatalf("admin feature not advertised: %+v %v", me, err)
		}
		raw := f.request(t, f.client, "GET", "/api/v1/admin/overview", "", nil, 200)
		var overview struct {
			Instance struct {
				Name, Version, AuthMode string
			} `json:"instance"`
			Organizations []struct {
				ID, Name, Slug, Role      string
				Projects, Members, Issues int64
				Events                    int64
			} `json:"organizations"`
			Retention map[string]int `json:"retention"`
		}
		if err := json.Unmarshal(raw, &overview); err != nil {
			t.Fatal(err)
		}
		if overview.Instance.Name != "GoodSentry" || overview.Instance.Version == "" || overview.Instance.AuthMode != "required" || len(overview.Organizations) != 1 || overview.Organizations[0].Slug != f.org.Slug || overview.Organizations[0].Role != "owner" || overview.Organizations[0].Projects != 1 || overview.Organizations[0].Members != 1 || overview.Organizations[0].Issues != 1 || overview.Organizations[0].Events != 1 || overview.Retention["events"] != 90 {
			t.Fatalf("unexpected administration overview: %+v", overview)
		}
		if strings.Contains(string(raw), f.project.PublicKey) || strings.Contains(strings.ToLower(string(raw)), "password") || strings.Contains(strings.ToLower(string(raw)), "secret") {
			t.Fatalf("administration overview exposed credentials: %s", raw)
		}
		// The endpoint is for owners/admins. Exercise the admin role, then demote
		// the caller and verify the server rechecks the current role instead of
		// trusting organization state already loaded in the browser.
		if _, err := f.sql.Exec(f.ctx, `UPDATE memberships SET role='admin' WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
			t.Fatal(err)
		}
		f.request(t, f.client, "GET", "/api/v1/admin/overview", "", nil, 200)
		if _, err := f.sql.Exec(f.ctx, `UPDATE memberships SET role='member' WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
			t.Fatal(err)
		}
		f.request(t, f.client, "GET", "/api/v1/admin/overview", "", nil, 403)
	})
}
