//go:build integration

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"goodsentry/internal/store"
)

func TestProjectUpdateAndDeleteHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	native := "/api/v1/organizations/" + f.org.Slug + "/projects/" + f.project.ID
	var updated struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", native, `{"name":"Renamed project","platform":"python"}`, csrf, 200), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != f.project.ID || updated.Name != "Renamed project" || updated.Platform != "python" || updated.Slug != f.project.Slug {
		t.Fatalf("native project update=%+v", updated)
	}
	compatible := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/"
	var compatibleProject map[string]any
	if err := json.Unmarshal(f.request(t, f.client, "GET", compatible, "", nil, 200), &compatibleProject); err != nil || compatibleProject["name"] != "Renamed project" {
		t.Fatalf("compatible project read=%+v err=%v", compatibleProject, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", compatible, `{"name":"Compatible rename","platform":"node","eventThrottleRate":0}`, csrf, 200), &compatibleProject); err != nil || compatibleProject["name"] != "Compatible rename" {
		t.Fatalf("compatible project update=%+v err=%v", compatibleProject, err)
	}
	f.request(t, f.client, "PUT", compatible, `{"name":"Unsupported","eventThrottleRate":10}`, csrf, 400)
	f.request(t, f.client, "DELETE", native, "", csrf, 204)
	f.request(t, f.client, "GET", compatible, "", nil, 404)

	second, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Compatible deletion", "go")
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "DELETE", "/api/0/projects/"+f.org.Slug+"/"+second.Slug+"/", "", csrf, 204)
	if _, err := f.db.ManagementProject(f.ctx, store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("compatible project deletion=%v", err)
	}
}
