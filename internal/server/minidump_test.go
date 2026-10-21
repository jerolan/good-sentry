//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

func minidumpForm(t *testing.T, content []byte, extra map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	file, err := w.CreateFormFile("upload_file_minidump", "crash.dmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	for name, value := range extra {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}

func TestNativeMinidumpSymbolicationAndIngestion(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 CrashFunction\n1010 20 42 1\n")
	digest := sha1.Sum(symbol)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "test_app.sym", "sym", "5A9832E5287241C1838ED98914E9B7FF1", "x86_64", "test_app.pdb", hex.EncodeToString(digest[:]), nil, nil, symbol); err != nil {
		t.Fatal(err)
	}
	dump, err := os.ReadFile("testdata/amd64-minidump.dmp")
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := minidumpForm(t, dump, map[string]string{"sentry[release]": "native@1", "sentry[environment]": "production", "crash_context": "unit"})
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
		t.Fatalf("minidump response=%d %+v %v", res.StatusCode, accepted, err)
	}
	events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
	if err != nil || len(events) == 0 {
		t.Fatalf("stored minidump event: %d %v", len(events), err)
	}
	var payload struct {
		Release     string `json:"release"`
		Environment string `json:"environment"`
		Exception   struct {
			Values []struct {
				Type       string `json:"type"`
				Stacktrace struct {
					Frames []struct {
						Function string `json:"function"`
						Filename string `json:"filename"`
						Line     int    `json:"lineno"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
		DebugMeta struct {
			Images []struct {
				CodeFile  string `json:"code_file"`
				ImageAddr string `json:"image_addr"`
			} `json:"images"`
		} `json:"debug_meta"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Exception.Values) != 1 || payload.Exception.Values[0].Type != "EXCEPTION_ACCESS_VIOLATION" || len(payload.Exception.Values[0].Stacktrace.Frames) < 2 {
		t.Fatalf("native exception projection: %+v", payload.Exception)
	}
	first := payload.Exception.Values[0].Stacktrace.Frames[0]
	if first.Function != "CrashFunction" || first.Filename != "C:\\src\\crash.cc" || first.Line != 42 {
		t.Fatalf("native frame symbols: %+v", first)
	}
	if len(payload.DebugMeta.Images) == 0 || !strings.Contains(payload.DebugMeta.Images[0].CodeFile, "test_app.pdb") || payload.DebugMeta.Images[0].ImageAddr != "0x140000000" {
		t.Fatalf("native debug metadata: %+v", payload.DebugMeta)
	}
	files, err := f.db.DebugFiles(f.ctx, access, f.project.ID, 0, 10)
	if err != nil || len(files) != 1 {
		t.Fatalf("symbol storage should contain only the uploaded symbol: %+v %v", files, err)
	}

	badBody, badType := minidumpForm(t, []byte("not a minidump"), nil)
	req, _ = http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, badBody)
	req.Header.Set("Content-Type", badType)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid minidump=%d", res.StatusCode)
	}
	body, contentType = minidumpForm(t, dump, nil)
	req, _ = http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key=wrong", body)
	req.Header.Set("Content-Type", contentType)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid minidump DSN=%d", res.StatusCode)
	}
}
