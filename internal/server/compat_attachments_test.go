//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/store"
)

// HTTP owns compatible routes, credentials, strict query grammar, public JSON,
// prefix-aware navigation and download dispatch. Provider tests own immutable
// identities/checksums, SQL isolation and migration preservation; native HTTP
// tests own the full shared safe-filename matrix. Real captures and sessions
// exercise these distinct wire contracts without a production test seam.
func TestCompatibleAttachmentsHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	const eventID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	listPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/events/" + eventID + "/attachments/"
	f.request(t, http.DefaultClient, "GET", listPath, "", nil, 401)
	payload := []byte{0, 255, 1, 254, 2, 0, 128, 42}
	hostile := "..\\../wire-\"é.bin\r\nX-Injected: yes"
	parts := []store.CaptureAttachment{
		{Filename: hostile, ContentType: "text/html", Type: "event.custom", Data: payload},
		{Filename: "SECOND.BIN", ContentType: "application/octet-stream", Data: []byte{}},
		{Filename: "screenshot.png", ContentType: "image/png", Data: []byte{137, 80, 78, 71}},
		{Filename: "SCREENSHOT.PNG", ContentType: "application/json", Type: "event.view_hierarchy", Data: []byte(`{"tree":true}`)},
	}
	ev, err := event.Normalize(json.RawMessage(`{"event_id":"`+eventID+`","message":"Existing parent"}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, store.CaptureItems{Attachments: parts}); err != nil || fresh {
		t.Fatal("retained-parent attachment fixture", fresh, err)
	}
	type attachment struct {
		ID, Type, Name, Mimetype, SHA1 string
		EventID                        string `json:"event_id"`
		DateCreated                    time.Time
		Size                           int
		Headers                        map[string]string
	}
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("attachment JSON: %v body=%s", err, raw)
		}
	}
	get := func(client *http.Client, method, path string, headers http.Header, want int) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+"/good"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if headers != nil {
			req.Header = headers.Clone()
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("%s %s=%d want=%d body=%s error=%v", method, path, res.StatusCode, want, raw, err)
		}
		return res, raw
	}
	res, raw := get(f.client, "GET", listPath, nil, 200)
	if res.Header.Get("Cache-Control") != "private, no-store" || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatal("metadata response headers", res.Header)
	}
	var files []attachment
	decode(raw, &files)
	var fields []map[string]json.RawMessage
	decode(raw, &fields)
	if len(files) != len(parts) {
		t.Fatal("attachment list wire", string(raw))
	}
	for index, want := range parts {
		file := files[index]
		id, err := strconv.ParseInt(file.ID, 10, 64)
		kind := want.Type
		if kind == "" {
			kind = "event.attachment"
		}
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != file.ID || file.EventID != eventID || file.Name != want.Filename || file.Type != kind || file.Mimetype != want.ContentType || file.Size != len(want.Data) || file.SHA1 != fmt.Sprintf("%x", sha1.Sum(want.Data)) || file.DateCreated.IsZero() || !reflect.DeepEqual(file.Headers, map[string]string{"Content-Type": want.ContentType}) {
			t.Fatalf("public attachment metadata=%+v want=%+v error=%v", file, want, err)
		}
		if _, leaked := fields[index]["data"]; leaked {
			t.Fatal("metadata leaked attachment bytes", string(raw))
		}
		var selected attachment
		decode(f.request(t, f.client, "GET", listPath+file.ID+"/", "", nil, 200), &selected)
		if !reflect.DeepEqual(selected, file) {
			t.Fatal("retrieve DTO differs from list", file, selected)
		}
	}
	filePath := listPath + files[0].ID + "/"
	t.Run("download presence and HEAD preserve binary headers and safe name", func(t *testing.T) {
		for _, query := range []string{"?download", "?download=", "?download=false", "?download=arbitrary"} {
			for _, method := range []string{"GET", "HEAD"} {
				res, raw := get(f.client, method, filePath+query, nil, 200)
				disposition, parameters, err := mime.ParseMediaType(res.Header.Get("Content-Disposition"))
				if err != nil || disposition != "attachment" || parameters["filename"] != "wire-\"é.binX-Injected: yes" || res.Header.Get("X-Injected") != "" || res.Header.Get("Content-Type") != "application/octet-stream" || res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Cache-Control") != "private, no-store" || res.ContentLength != int64(len(payload)) {
					t.Fatal("compatible download headers", method, query, res.Header, err)
				}
				if method == "HEAD" && len(raw) != 0 || method == "GET" && !bytes.Equal(raw, payload) {
					t.Fatal("binary/HEAD response bytes", method, query, raw)
				}
			}
		}
		empty, raw := get(f.client, "GET", listPath+files[1].ID+"/?download=1", nil, 200)
		if len(raw) != 0 || empty.ContentLength != 0 {
			t.Fatal("empty binary attachment", empty.Header, raw)
		}
	})
	t.Run("filters and followable public pagination preserve deployment prefix", func(t *testing.T) {
		var screenshot []attachment
		decode(f.request(t, f.client, "GET", listPath+"?query=is%3Ascreenshot", "", nil, 200), &screenshot)
		if len(screenshot) != 1 || screenshot[0].ID != files[2].ID {
			t.Fatal("screenshot query route", screenshot)
		}
		linkPattern := regexp.MustCompile(`<([^>]+)>; rel="([^"]+)"; results="([^"]+)"; cursor="([^"]+)"`)
		follow := func(header, relation string, results bool) string {
			t.Helper()
			for _, match := range linkPattern.FindAllStringSubmatch(header, -1) {
				if match[2] != relation {
					continue
				}
				u, err := url.Parse(match[1])
				if err != nil || u.Scheme != "https" || u.Host != "public.example.test" || u.Path != "/good"+listPath || u.Query().Get("query") != ".bIn" || u.Query().Get("per_page") != "1" || match[3] != strconv.FormatBool(results) || u.Query().Get("cursor") != match[4] {
					t.Fatal("public attachment pagination link", match, err)
				}
				return strings.TrimPrefix(u.RequestURI(), "/good")
			}
			t.Fatal("missing pagination relation", relation, header)
			return ""
		}
		res, first := get(f.client, "GET", listPath+"?query=.bIn&per_page=1", nil, 200)
		var page []attachment
		decode(first, &page)
		if len(page) != 1 || page[0].ID != files[0].ID {
			t.Fatal("case-insensitive substring first page", page)
		}
		next := follow(res.Header.Get("Link"), "next", true)
		follow(res.Header.Get("Link"), "previous", false)
		res, second := get(f.client, "GET", next, nil, 200)
		decode(second, &page)
		if len(page) != 1 || page[0].ID != files[1].ID {
			t.Fatal("following next skipped or repeated a file", page)
		}
		follow(res.Header.Get("Link"), "next", false)
		previous := follow(res.Header.Get("Link"), "previous", true)
		_, returned := get(f.client, "GET", previous, nil, 200)
		if !bytes.Equal(returned, first) {
			t.Fatal("following previous changed the filtered first page", string(returned), string(first))
		}
		if raw := f.request(t, f.client, "GET", listPath+"?query=missing", "", nil, 200); strings.TrimSpace(string(raw)) != "[]" {
			t.Fatal("no matches must return an array", string(raw))
		}
	})
	t.Run("strict route and query admission", func(t *testing.T) {
		for _, query := range []string{"unknown=1", "download=1", "query=x&query=y", "query=%FF", "query=%00", "query=%0A", "query=" + url.QueryEscape(strings.Repeat("é", 201)), "query=%ZZ", "per_page=", "per_page=0", "per_page=01", "per_page=101", "per_page=1&per_page=1", "cursor=invalid", "cursor=0:01:0", "cursor=0:0:2", "cursor=0:1:0&cursor=0:2:0"} {
			f.request(t, f.client, "GET", listPath+"?"+query, "", nil, 400)
		}
		for _, query := range []string{"query=x", "download=1&download=1", "download=%ZZ", "unknown=1"} {
			f.request(t, f.client, "GET", filePath+"?"+query, "", nil, 400)
		}
		for _, id := range []string{"0", "01", "invalid", strings.Repeat("a", 64)} {
			f.request(t, f.client, "GET", listPath+id+"/", "", nil, 400)
		}
		f.request(t, f.client, "GET", strings.Replace(listPath, eventID, "latest", 1), "", nil, 400)
		f.request(t, f.client, "GET", strings.Replace(listPath, eventID, strings.Repeat("f", 32), 1), "", nil, 404)
		f.request(t, f.client, "GET", listPath+"9223372036854775807/", "", nil, 404)
		f.request(t, f.client, "GET", filePath+"extra/", "", nil, 404)
		for _, path := range []string{listPath, filePath} {
			f.request(t, f.client, "POST", path, "", nil, 405)
		}
	})
	t.Run("real project credentials remain authoritative and current", func(t *testing.T) {
		issueToken := func(scope string) struct{ ID, Token string } {
			t.Helper()
			var credential struct{ ID, Token string }
			body := resolutionJSON(t, map[string]any{"name": "Compatible attachment " + scope, "scopes": []string{scope}, "projectIds": []string{f.project.ID}})
			decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201), &credential)
			if credential.ID == "" || credential.Token == "" {
				t.Fatal("real attachment credential missing", credential)
			}
			return credential
		}
		reader, writer, wrongScope := issueToken("project:read"), issueToken("project:write"), issueToken("event:read")
		bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
		for _, path := range []string{listPath, filePath, filePath + "?download=1"} {
			for _, credential := range []string{reader.Token, writer.Token} {
				f.request(t, http.DefaultClient, "GET", path, "", bearer(credential), 200)
			}
			f.request(t, f.client, "GET", path, "", bearer(wrongScope.Token), 403)
			f.request(t, f.client, "GET", path, "", bearer("invalid"), 401)
			f.request(t, http.DefaultClient, "GET", path, "", http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7, sentry_key=" + f.project.PublicKey}}, 401)
		}
		other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other compatible attachments", "go")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.db.Ingest(f.ctx, other.ID, ev, store.CaptureItems{Attachments: parts[:1]}); err != nil {
			t.Fatal(err)
		}
		otherPath := strings.Replace(listPath, "/"+f.project.Slug+"/", "/"+other.Slug+"/", 1)
		var foreign []attachment
		decode(f.request(t, f.client, "GET", otherPath, "", nil, 200), &foreign)
		if len(foreign) != 1 {
			t.Fatal("foreign-project negative control lacks actual file", foreign)
		}
		for _, path := range []string{otherPath, otherPath + foreign[0].ID + "/", otherPath + foreign[0].ID + "/?download=1", listPath + foreign[0].ID + "/?download=1"} {
			f.request(t, http.DefaultClient, "GET", path, "", bearer(reader.Token), 404)
		}
		f.request(t, f.client, "DELETE", "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+reader.ID, "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
		for _, path := range []string{listPath, filePath, filePath + "?download=1"} {
			f.request(t, f.client, "GET", path, "", bearer(reader.Token), 401)
		}
		// Removing live membership also invalidates a still-unrevoked token.
		if _, err := f.sql.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, f.owner.ID); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{listPath, filePath + "?download=1"} {
			f.request(t, http.DefaultClient, "GET", path, "", bearer(writer.Token), 401)
			f.request(t, f.client, "GET", path, "", nil, 404)
		}
	})
}
