//go:build integration

package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type releaseFileWire struct {
	ID          string            `json:"id"`
	DateCreated string            `json:"dateCreated"`
	SHA1        string            `json:"sha1"`
	Headers     map[string]string `json:"headers"`
	Name        string            `json:"name"`
	Size        int64             `json:"size"`
}

func TestCompatibleReleaseFilesHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Release files HTTP other", "go")
	if err != nil {
		t.Fatal(err)
	}
	version := "release-files-http"
	firstData := []byte(`{"version":3,"sources":["first.ts"],"names":[],"mappings":"AAAA"}`)
	secondData := []byte(`{"version":3,"sources":["second.ts"],"names":[],"mappings":"BBBB"}`)
	first, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/first.map", version, "", "", firstData)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, other.ID, "~/second.map", version, "", "", secondData)
	if err != nil {
		t.Fatal(err)
	}
	orgPath := "/api/0/organizations/" + f.org.Slug + "/releases/" + version + "/files/"
	projectPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/releases/" + version + "/files/"
	otherProjectPath := "/api/0/projects/" + f.org.Slug + "/" + other.Slug + "/releases/" + version + "/files/"
	var organizationFiles []releaseFileWire
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgPath, "", nil, 200), &organizationFiles); err != nil || len(organizationFiles) != 2 {
		t.Fatalf("organization files=%+v err=%v", organizationFiles, err)
	}
	if organizationFiles[0].ID != second.ID || organizationFiles[1].ID != first.ID {
		t.Fatalf("organization files are not newest-first: %+v", organizationFiles)
	}
	byID := map[string]releaseFileWire{}
	for _, file := range organizationFiles {
		byID[file.ID] = file
	}
	firstSHA, secondSHA := sha1.Sum(firstData), sha1.Sum(secondData)
	if file := byID[first.ID]; file.Name != "~/first.map" || file.Size != int64(len(firstData)) || file.SHA1 != hex.EncodeToString(firstSHA[:]) || file.Headers != nil || file.DateCreated == "" {
		t.Fatalf("first wire=%+v", file)
	}
	if file := byID[second.ID]; file.Name != "~/second.map" || file.SHA1 != hex.EncodeToString(secondSHA[:]) {
		t.Fatalf("second wire=%+v", file)
	}
	var projectFiles []releaseFileWire
	if err := json.Unmarshal(f.request(t, f.client, "GET", projectPath, "", nil, 200), &projectFiles); err != nil || len(projectFiles) != 1 || projectFiles[0].ID != first.ID {
		t.Fatalf("project files=%+v err=%v", projectFiles, err)
	}
	if link := keyLink(t, f, orgPath+"?limit=1"); !strings.Contains(link, "cursor=") || !strings.Contains(link, `rel="next"`) {
		t.Fatalf("release file Link=%q", link)
	}
	var detail releaseFileWire
	if err := json.Unmarshal(f.request(t, f.client, "GET", orgPath+first.ID+"/", "", nil, 200), &detail); err != nil || detail.ID != first.ID || detail.SHA1 != byID[first.ID].SHA1 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	f.request(t, f.client, "GET", orgPath+first.ID+"/?unknown=1", "", nil, 400)
	f.request(t, f.client, "GET", orgPath+"not-a-number/", "", nil, 400)
	f.request(t, f.client, "GET", orgPath+"999999999/", "", nil, 404)
	f.request(t, f.client, "GET", orgPath+"?unknown=1", "", nil, 400)
	f.request(t, f.client, "GET", "/api/0/organizations/"+f.org.Slug+"/releases/bad%00version/files/", "", nil, 400)
	for _, query := range []string{"limit=0", "limit=101", "per_page=0", "per_page=101", "limit=1&per_page=2", "cursor=bad"} {
		f.request(t, f.client, "GET", orgPath+"?"+query, "", nil, 400)
	}
	f.request(t, f.client, "GET", projectPath+second.ID+"/", "", nil, 404)

	exactVersion := " " + strings.Repeat("界", 253) + " "
	exactMap, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/exact.map", exactVersion, "", "", []byte(`{"version":3,"sources":[],"names":[],"mappings":""}`))
	if err != nil {
		t.Fatal(err)
	}
	exactPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/releases/" + url.PathEscape(exactVersion) + "/files/"
	var exactFiles []releaseFileWire
	if err := json.Unmarshal(f.request(t, f.client, "GET", exactPath, "", nil, 200), &exactFiles); err != nil || len(exactFiles) != 1 || exactFiles[0].ID != exactMap.ID {
		t.Fatalf("255-character exact release files=%+v err=%v", exactFiles, err)
	}

	readToken := issueManagementToken(t, f, f.org.Slug, "Release file reader", []string{"project:releases"}, []string{f.project.ID})
	readHeaders := http.Header{"Authorization": {"Bearer " + readToken}}
	f.request(t, http.DefaultClient, "GET", orgPath, "", readHeaders, 200)
	f.request(t, http.DefaultClient, "GET", projectPath, "", readHeaders, 200)
	noWrite := issueManagementToken(t, f, f.org.Slug, "Release file read only", []string{"project:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "GET", orgPath, "", http.Header{"Authorization": {"Bearer " + noWrite}}, 403)

	insertDiscoveryMember(t, f, "release-file-member@test", "Release file member", "member")
	member, memberCSRF := loginManagementMember(t, f, "release-file-member@test")
	f.request(t, member, "GET", orgPath, "", nil, 200)
	f.request(t, member, "DELETE", orgPath+first.ID+"/", "", http.Header{"X-CSRF-Token": {memberCSRF}}, 403)
	insertDiscoveryMember(t, f, "release-file-manager@test", "Release file manager", "manager")
	manager, managerCSRF := loginManagementMember(t, f, "release-file-manager@test")
	f.request(t, manager, "DELETE", orgPath+second.ID+"/", "", http.Header{"X-CSRF-Token": {managerCSRF}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Release file writer", []string{"project:releases"}, []string{f.project.ID})
	third, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/third.map", version, "", "", []byte(`{"version":3,"sources":["third.ts"],"names":[],"mappings":"CCCC"}`))
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "DELETE", orgPath+third.ID+"/", "", http.Header{"Authorization": {"Bearer " + writeToken}}, 204)
	f.request(t, f.client, "GET", orgPath+third.ID+"/", "", nil, 404)
	f.request(t, f.client, "DELETE", orgPath+first.ID+"/?unknown=1", "", http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	f.request(t, f.client, "DELETE", orgPath+first.ID+"/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
	f.request(t, f.client, "GET", orgPath+first.ID+"/", "", nil, 404)
	f.request(t, f.client, "DELETE", otherProjectPath+second.ID+"/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
	f.request(t, f.client, "GET", otherProjectPath+second.ID+"/", "", nil, 404)
}
