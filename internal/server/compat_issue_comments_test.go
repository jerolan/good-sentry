//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCompatibleIssueCommentsContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	path := "/api/0/issues/" + f.issueID + "/comments/"
	var first struct {
		ID   string            `json:"id"`
		Type string            `json:"type"`
		Data map[string]string `json:"data"`
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "POST", path, `{"data":{"text":"Compatible comment"}}`, csrf, 201), &first); err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.Type != "note" || first.Data["text"] != "Compatible comment" || first.User.ID != f.owner.ID || first.User.Email != f.owner.Email {
		t.Fatalf("compatible comment=%+v", first)
	}
	f.request(t, f.client, "POST", path, `{"data":{"text":"Second comment"}}`, csrf, 201)
	list := f.request(t, f.client, "GET", path+"?limit=1", "", nil, 200)
	var comments []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(list, &comments); err != nil || len(comments) != 1 || comments[0].ID == "" {
		t.Fatalf("comment page=%s err=%v", list, err)
	}
	pageRequest, err := http.NewRequest("GET", f.server.URL+"/good"+path+"?limit=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	pageResponse, err := f.client.Do(pageRequest)
	if err != nil {
		t.Fatal(err)
	}
	pageResponse.Body.Close()
	if !strings.Contains(pageResponse.Header.Get("Link"), `rel="next"; results="true"`) {
		t.Fatalf("comment next link=%q", pageResponse.Header.Get("Link"))
	}
	orgPath := "/api/0/organizations/" + f.org.Slug + "/issues/" + f.issueID + "/comments/"
	var edited struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(f.request(t, f.client, "PUT", orgPath+first.ID+"/", `{"data":{"text":"Edited compatible comment"}}`, csrf, 200), &edited); err != nil || edited.Data["text"] != "Edited compatible comment" {
		t.Fatalf("edited comment=%+v err=%v", edited, err)
	}
	f.request(t, f.client, "POST", path, `{"data":{"text":"bad","unknown":true}}`, csrf, 400)
	f.request(t, f.client, "DELETE", path+first.ID+"/", "", csrf, 204)
	f.request(t, f.client, "PUT", path+first.ID+"/", `{"data":{"text":"Gone"}}`, csrf, 404)
}
