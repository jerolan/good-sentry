//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCompatibleUserPreferencesHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	userPath := "/api/0/users/" + f.owner.ID + "/"
	var user struct {
		Name    string `json:"name"`
		Options struct {
			Timezone     *string `json:"timezone"`
			Clock24Hours *bool   `json:"clock24Hours"`
		} `json:"options"`
	}
	body := "{\"name\":\"HTTP Owner\",\"options\":{\"timezone\":\"UTC\",\"clock24Hours\":true}}"
	if err := json.Unmarshal(f.request(t, f.client, "PUT", userPath, body, csrf, 200), &user); err != nil {
		t.Fatal(err)
	}
	if user.Name != "HTTP Owner" || user.Options.Timezone == nil || *user.Options.Timezone != "UTC" || user.Options.Clock24Hours == nil || !*user.Options.Clock24Hours {
		t.Fatalf("updated user=%+v", user)
	}
	emails := f.request(t, f.client, "GET", userPath+"emails/", "", nil, 200)
	if !strings.Contains(string(emails), `"isPrimary":true`) || !strings.Contains(string(emails), f.owner.Email) {
		t.Fatalf("emails=%s", emails)
	}
	f.request(t, f.client, "POST", userPath+"emails/", "{\"email\":\"second@example.test\"}", csrf, 400)
	var notifications map[string]bool
	if err := json.Unmarshal(f.request(t, f.client, "PUT", userPath+"notifications/", "{\"subscribeByDefault\":false}", csrf, 200), &notifications); err != nil || notifications["subscribeByDefault"] {
		t.Fatalf("notifications=%+v err=%v", notifications, err)
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", userPath+"notifications/alerts/", "", nil, 200), &notifications); err != nil || notifications["subscribeByDefault"] {
		t.Fatalf("stored notifications=%+v err=%v", notifications, err)
	}
	f.request(t, f.client, "PUT", "/api/0/users/999999999/", "{\"name\":\"Other\",\"options\":{}}", csrf, 404)
}
