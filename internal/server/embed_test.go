//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func embedTestPath(f *resolutionHTTPFixture, key, eventID string, extra url.Values) string {
	values := url.Values{"dsn": {"http://" + key + "@example.test/" + f.project.ID}, "eventId": {eventID}}
	for name, items := range extra {
		for _, item := range items {
			values.Add(name, item)
		}
	}
	return "/api/embed/error-page/?" + values.Encode()
}

func TestEmbedErrorPageHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	eventID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path := embedTestPath(f, f.project.PublicKey, eventID, url.Values{"title": {"Public embed title"}, "labelComments": {"Tell us more"}})
	res, err := http.Get(f.server.URL + "/good" + path)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/javascript") || !strings.Contains(string(raw), `"title":"Public embed title"`) || !strings.Contains(string(raw), "sentry-error-embed-wrapper") {
		t.Fatalf("embed page=%d type=%q body=%s", res.StatusCode, res.Header.Get("Content-Type"), raw[:min(len(raw), 500)])
	}
	bad, err := http.Get(f.server.URL + "/good/api/embed/error-page/?dsn=bad&eventId=" + eventID)
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != 401 {
		t.Fatalf("bad DSN=%d", bad.StatusCode)
	}
	post := func(path, body string, want int) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("embed POST=%d want=%d body=%s", res.StatusCode, want, raw)
		}
		return res, raw
	}
	post(path, url.Values{"name": {"Embed User"}, "email": {"embed@example.test"}, "comments": {"The application crashed"}}.Encode(), 200)
	var comments string
	if err := f.sql.QueryRow(f.ctx, `SELECT comments FROM user_reports WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, eventID).Scan(&comments); err != nil || comments != "The application crashed" {
		t.Fatalf("stored embed report=%q err=%v", comments, err)
	}
	_, validation := post(path, url.Values{"name": {"Embed User"}, "comments": {""}}.Encode(), 400)
	var formError struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(validation, &formError); err != nil || formError.Errors["comments"] == "" {
		t.Fatalf("embed validation=%s err=%v", validation, err)
	}
	post(embedTestPath(f, "wrong", eventID, nil), url.Values{"comments": {"Wrong key"}}.Encode(), 401)
	if _, err := f.sql.Exec(f.ctx, `UPDATE project_keys SET enabled=false WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	post(path, url.Values{"comments": {"Disabled key"}}.Encode(), 401)
}
