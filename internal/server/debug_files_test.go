//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/store"
)

func TestCompatibleDebugFileUploadListAndPermissions(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	base := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug
	content := []byte("MODULE Linux x86_64 0123456789ABCDEF0 fixture.sym\n")
	upload := func(client *http.Client, csrf, authorization, name string, data []byte, want int) store.DebugFile {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
		_ = writer.WriteField("type", "sym")
		_ = writer.WriteField("debug_id", "0123456789ABCDEF0")
		_ = writer.WriteField("cpu_name", "x86_64")
		_ = writer.WriteField("object_name", "fixture.sym")
		_ = writer.Close()
		req, err := http.NewRequestWithContext(f.ctx, "POST", f.server.URL+"/good"+base+"/files/dsyms/", &body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("debug upload=%d want=%d body=%s", res.StatusCode, want, raw)
		}
		if want != 200 {
			return store.DebugFile{}
		}
		var file store.DebugFile
		if err := json.Unmarshal(raw, &file); err != nil {
			t.Fatalf("debug response: %v %s", err, raw)
		}
		return file
	}
	file := upload(f.client, f.csrf, "", "fixture.sym", content, 200)
	if file.ID == "" || file.ObjectName != "fixture.sym" || file.CPUName != "x86_64" || file.DebugID == nil || *file.DebugID != "0123456789ABCDEF0" || file.Size != int64(len(content)) {
		t.Fatalf("debug file DTO: %+v", file)
	}
	duplicate := upload(f.client, f.csrf, "", "duplicate.sym", content, 200)
	if duplicate.ID != file.ID {
		t.Fatalf("debug duplicate identity: %+v %+v", file, duplicate)
	}
	var files []store.DebugFile
	if err := json.Unmarshal(f.request(t, f.client, "GET", base+"/files/dsyms/", "", nil, 200), &files); err != nil || len(files) != 1 || files[0].ID != file.ID {
		t.Fatalf("debug list: %+v %v", files, err)
	}
	f.request(t, f.client, "POST", base+"/files/difs/assemble/", "{}", http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	if body := f.request(t, f.client, "POST", base+"/reprocessing/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 200); len(body) != 0 {
		t.Fatalf("debug-file reprocessing response must be empty, got %q", body)
	}

	insertDiscoveryMember(t, f, "debug-member@management.test", "Debug member", "member")
	memberClient, memberCSRF := loginManagementMember(t, f, "debug-member@management.test")
	upload(memberClient, memberCSRF, "", "member.sym", []byte("member file"), 403)
	readToken := issueManagementToken(t, f, f.org.Slug, "Debug reader", []string{"project:read"}, []string{f.project.ID})
	f.request(t, http.DefaultClient, "POST", base+"/files/dsyms/", "not multipart", http.Header{"Authorization": {"Bearer " + readToken}}, 403)
	writeToken := issueManagementToken(t, f, f.org.Slug, "Debug writer", []string{"project:releases"}, []string{f.project.ID})
	if got := upload(http.DefaultClient, "", "Bearer "+writeToken, "bearer.sym", []byte("bearer file"), 200); got.ID == "" {
		t.Fatal("bearer upload returned empty identity")
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "GET", base+"/files/dsyms/", "", http.Header{"Authorization": {"Bearer " + writeToken}}, 200), &files); err != nil || len(files) != 2 {
		t.Fatalf("bearer list: %+v %v", files, err)
	}
}

func TestCompatibleDebugFilePagingBoundsAndCSRF(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	base := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug
	type uploadFile struct {
		name string
		data []byte
	}
	send := func(files []uploadFile, csrf string, want int) []byte {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for _, file := range files {
			part, err := writer.CreateFormFile("file", file.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(file.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(f.ctx, "POST", f.server.URL+"/good"+base+"/files/dsyms/", &body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("debug upload=%d want=%d body=%s", res.StatusCode, want, raw)
		}
		return raw
	}
	for index, name := range []string{"page-one.sym", "page-two.sym", "page-three.sym"} {
		send([]uploadFile{{name: name, data: []byte(fmt.Sprintf("page file %d", index))}}, f.csrf, 200)
	}
	page := func(query string, want int) ([]store.DebugFile, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, "GET", f.server.URL+"/good"+base+"/files/dsyms/?"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("debug list %s=%d want=%d body=%s", query, res.StatusCode, want, raw)
		}
		if want != 200 {
			return nil, ""
		}
		var files []store.DebugFile
		if err := json.Unmarshal(raw, &files); err != nil {
			t.Fatalf("debug list %s: %v %s", query, err, raw)
		}
		return files, res.Header.Get("Link")
	}
	nextCursor := func(link string) string {
		t.Helper()
		for _, item := range strings.Split(link, ",") {
			if !strings.Contains(item, `rel="next"`) {
				continue
			}
			start := strings.Index(item, `cursor="`)
			if start < 0 {
				return ""
			}
			start += len(`cursor="`)
			end := strings.Index(item[start:], `"`)
			if end < 0 {
				return ""
			}
			return item[start : start+end]
		}
		return ""
	}
	first, link := page("limit=1", 200)
	if len(first) != 1 || nextCursor(link) == "" {
		t.Fatalf("first page: %+v link=%q", first, link)
	}
	second, link := page("limit=1&cursor="+nextCursor(link), 200)
	if len(second) != 1 || second[0].ID == first[0].ID || nextCursor(link) == "" {
		t.Fatalf("second page: %+v link=%q", second, link)
	}
	third, link := page("limit=1&cursor="+nextCursor(link), 200)
	if len(third) != 1 || third[0].ID == first[0].ID || third[0].ID == second[0].ID || nextCursor(link) != "" {
		t.Fatalf("third page: %+v link=%q", third, link)
	}
	for _, query := range []string{"limit=0", "per_page=101", "limit=1&per_page=2", "cursor=not-a-cursor", "unknown=1"} {
		page(query, 400)
	}
	send(nil, f.csrf, 400)
	send([]uploadFile{{name: "one.sym", data: []byte("first file")}, {name: "two.sym", data: []byte("second file")}}, f.csrf, 400)
	send([]uploadFile{{name: "csrf.sym", data: []byte("csrf file")}}, "", 403)
	send([]uploadFile{{name: "big.sym", data: bytes.Repeat([]byte("x"), (1<<20)+1024)}}, f.csrf, 413)
}

func TestChunkedDebugFileAssemblyState(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	base := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug
	token := issueManagementToken(t, f, f.org.Slug, "DIF chunk reader", []string{"project:releases"}, []string{f.project.ID})
	headers := http.Header{"Authorization": {"Bearer " + token}}
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nFUNC 1010 20 0 CrashFunction\n")
	digest := sha1.Sum(symbol)
	checksum := hex.EncodeToString(digest[:])
	body := fmt.Sprintf(`{"%s":{"name":"test_app.sym","debug_id":"5a9832e5-2872-41c1-838e-d98914e9b7ff-1","chunks":["%s"]}}`, checksum, checksum)
	type state struct {
		State         string           `json:"state"`
		MissingChunks []string         `json:"missingChunks"`
		DIF           *store.DebugFile `json:"dif"`
	}
	decodeState := func(raw []byte) state {
		t.Helper()
		var values map[string]state
		if err := json.Unmarshal(raw, &values); err != nil || values[checksum].State == "" {
			t.Fatalf("assembly response: %s %v", raw, err)
		}
		return values[checksum]
	}
	first := decodeState(f.request(t, http.DefaultClient, "POST", base+"/files/difs/assemble/", body, headers, 200))
	if first.State != "not_found" || len(first.MissingChunks) != 1 || first.MissingChunks[0] != checksum || first.DIF != nil {
		t.Fatalf("missing state: %+v", first)
	}
	projectID, valid := occurrenceNumber(f.project.ID, 9223372036854775807)
	if !valid {
		t.Fatal("fixture project ID is not canonical")
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: false, ProjectIDs: []int64{projectID}}
	if err := f.db.SaveArtifactChunks(f.ctx, access, []store.ArtifactChunk{{Checksum: checksum, Data: symbol}}, store.ArtifactStagingPolicy{MaxBytes: 1 << 20, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	second := decodeState(f.request(t, http.DefaultClient, "POST", base+"/files/difs/assemble/", body, headers, 200))
	if second.State != "ok" || len(second.MissingChunks) != 0 || second.DIF == nil || second.DIF.DebugID == nil || second.DIF.Size != int64(len(symbol)) {
		t.Fatalf("assembled state: %+v", second)
	}
}
