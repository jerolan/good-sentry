//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

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
	f.request(t, f.client, "POST", base+"/files/difs/assemble/", "{}", http.Header{"X-CSRF-Token": {f.csrf}}, 501)
	f.request(t, f.client, "POST", base+"/reprocessing/", "{}", http.Header{"X-CSRF-Token": {f.csrf}}, 200)

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
