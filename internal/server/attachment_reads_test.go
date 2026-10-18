//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/event"
	"goodsentry/internal/store"
)

// HTTP owns session-purpose/route/query admission and the actual download wire,
// including safe names, byte length and HEAD. Store scope/paging tests cannot
// detect a missing route, JSON-encoded binary or unsafe disposition. Reuse the
// real PostgreSQL/session fixture without an injected Store or production seam.
func TestNativeIssueAttachmentsHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	const eventID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path := "/api/v1/issues/" + f.issueID + "/events/" + eventID + "/attachments"
	f.request(t, http.DefaultClient, "GET", path, "", nil, 401)
	payload := []byte{0, 255, 1, 254, 2, 0, 128, 42}
	hostile := "..\\../<img src=x onerror=window.attachmentUnsafe=1>.html\r\nInjected: yes"
	unicodeName := strings.Repeat("é", 300) + ".txt"
	ev, err := event.Normalize(json.RawMessage(`{"event_id":"`+eventID+`","message":"Ignored duplicate parent"}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, store.CaptureItems{Attachments: []store.CaptureAttachment{{Filename: hostile, ContentType: "text/html", Data: payload}, {Filename: "empty.bin", ContentType: "application/octet-stream", Data: []byte{}}, {Filename: unicodeName, ContentType: "text/plain", Data: []byte{}}, {Filename: "../", ContentType: "text/plain", Data: []byte{}}}}); err != nil || fresh {
		t.Fatal("duplicate parent fixture did not attach files", fresh, err)
	}
	var page struct {
		Data []struct {
			ID, Filename, ContentType, DownloadName string
			Size                                    int
			DateCreated                             time.Time
		}
		HasMore  bool
		PageSize int
	}
	raw := f.request(t, f.client, "GET", path, "", nil, 200)
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Data) != 4 || page.HasMore || page.PageSize != 25 {
		t.Fatal("native attachment metadata wiring", string(raw), err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(decoded["data"], &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, leaked := entry["data"]; leaked {
			t.Fatal("metadata leaked a blob")
		}
	}
	get := func(method, target string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+target, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, body
	}
	for _, file := range page.Data {
		if len(file.ID) != 64 || file.DateCreated.IsZero() || file.DownloadName == "" || utf8.RuneCountInString(file.DownloadName) > 255 || strings.ContainsAny(file.DownloadName, "/\\") || strings.IndexFunc(file.DownloadName, unicode.IsControl) >= 0 {
			t.Fatal("unsafe or incomplete download metadata", file)
		}
		switch file.Filename {
		case "empty.bin":
			if file.DownloadName != "empty.bin" {
				t.Fatal("valid file name changed", file)
			}
		case unicodeName:
			if utf8.RuneCountInString(file.DownloadName) != 255 {
				t.Fatal("Unicode download name is not capped", file)
			}
		case "../":
			if file.DownloadName != "attachment" {
				t.Fatal("dot basename has no safe fallback", file)
			}
		case hostile:
		default:
			t.Fatal("unexpected metadata file", file)
		}
		for _, method := range []string{"GET", "HEAD"} {
			response, body := get(method, path+"/"+file.ID)
			disposition, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
			if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/octet-stream" || response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Cache-Control") != "private, no-store" || response.ContentLength != int64(file.Size) || err != nil || disposition != "attachment" || parameters["filename"] != file.DownloadName {
				t.Fatal("binary download headers", method, response.StatusCode, response.Header, err)
			}
			if method == "HEAD" || file.Size == 0 {
				if len(body) != 0 {
					t.Fatal("HEAD/empty attachment wrote a body", method, body)
				}
			} else if !bytes.Equal(body, payload) {
				t.Fatal("binary response changed bytes", body)
			}
		}
		for _, query := range []string{"?download=1", "?page=1", "?bad=%zz"} {
			f.request(t, f.client, "GET", path+"/"+file.ID+query, "", nil, 400)
		}
		f.request(t, f.client, "POST", path+"/"+file.ID, "", nil, 405)
	}
	for _, query := range []string{"?page=", "?page=01", "?page=0", "?page=1000001", "?page=1&page=2", "?unknown=1", "?bad=%zz"} {
		f.request(t, f.client, "GET", path+query, "", nil, 400)
	}
	f.request(t, f.client, "GET", strings.Replace(path, eventID, "latest", 1), "", nil, 400)
	f.request(t, f.client, "GET", path+"/invalid", "", nil, 400)
	f.request(t, f.client, "GET", path+"/"+strings.Repeat("0", 64), "", nil, 404)
	f.request(t, f.client, "POST", path, "", nil, 405)
	// A real SDK key works for ingestion, but grants no native read purpose.
	sdkHeader := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7, sentry_key=" + f.project.PublicKey}}
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/", "{}\n{\"type\":\"profile\",\"length\":5}\nhello\n", sdkHeader, 200)
	f.request(t, http.DefaultClient, "GET", path, "", sdkHeader, 401)
	var credential struct{ Token string }
	if err := json.Unmarshal(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", resolutionJSON(t, map[string]any{"name": "Attachment purpose control", "scopes": []string{"event:read", "project:read"}, "projectIds": []string{f.project.ID}}), http.Header{"X-CSRF-Token": {f.csrf}}, 201), &credential); err != nil || credential.Token == "" {
		t.Fatal("real bearer fixture missing", err)
	}
	bearer := http.Header{"Authorization": {"Bearer " + credential.Token}}
	f.request(t, http.DefaultClient, "GET", "/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/events/"+eventID+"/", "", bearer, 200)
	f.request(t, http.DefaultClient, "GET", path, "", bearer, 401)
	var member string
	if err := f.sql.QueryRow(f.ctx, "INSERT INTO users(email,password_hash,name) VALUES('member@attachment-http.test',$1,'Attachment member') RETURNING id::text", f.hash).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", f.org.ID, member); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	memberClient := &http.Client{Jar: jar}
	f.request(t, memberClient, "POST", "/api/v1/login", `{"email":"member@attachment-http.test","password":"resolution-http-password"}`, nil, 200)
	f.request(t, memberClient, "GET", path+"/"+page.Data[0].ID, "", nil, 200)
	if _, err := f.sql.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, member); err != nil {
		t.Fatal(err)
	}
	f.request(t, memberClient, "GET", path, "", nil, 404)
	f.request(t, memberClient, "GET", path+"/"+page.Data[0].ID, "", nil, 404)
	if _, err := f.sql.Exec(f.ctx, "UPDATE attachments SET created_at=now()-interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	if maps, files, items, err := f.db.CleanupFiles(f.ctx, 1); err != nil || maps != 0 || files != 4 || items != 0 {
		t.Fatal("fixture cleanup", maps, files, items, err)
	}
	for _, file := range page.Data {
		f.request(t, f.client, "GET", path+"/"+file.ID, "", nil, 404)
	}
	var empty struct {
		Data []json.RawMessage
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &empty); err != nil || empty.Data == nil || len(empty.Data) != 0 {
		t.Fatal("retained event not readable with empty attachments", empty, err)
	}
	f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200)
}
