//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

// A worker must never reuse a project's local symbols for another project,
// even when both have the same event and native image identity.
func TestNativeRemoteReprocessingProjectIsolation(t *testing.T) {
	const debugID = "5A9832E5287241C1838ED98914E9B7FF1"
	var calls atomic.Int32
	var wrongPath atomic.Bool
	pdb := nativepdb.WithSourceLines(false).Data
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/symbols/app.pdb/"+debugID+"/app.pdb" || r.URL.RawQuery != "" {
			wrongPath.Store(true)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(pdb)
	}))
	defer source.Close()
	f := newResolutionHTTPFixture(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other symbol isolation", "native")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	upload := multipart.NewWriter(&body)
	part, err := upload.CreateFormFile("file", "app.sym")
	if err != nil {
		t.Fatal(err)
	}
	_, err = part.Write([]byte("MODULE windows x86_64 " + debugID + " app.pdb\nFILE 1 C:\\src\\local.cc\nFUNC 1010 20 0 LocalProjectFunction\n1010 20 7 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"type": "sym", "debug_id": debugID, "cpu_name": "x86_64", "object_name": "app.pdb"} {
		if err := upload.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/files/dsyms/", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", upload.FormDataContentType())
	req.Header.Set("X-CSRF-Token", f.csrf)
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(res.Body)
	res.Body.Close()
	if readErr != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d body=%s err=%v", res.StatusCode, content, readErr)
	}
	event := `{"platform":"native","debug_meta":{"images":[{"type":"pe","code_file":"app.exe","debug_id":"` + debugID + `","debug_file":"app.pdb","image_addr":"0x20000000","image_size":65536}]},"stacktrace":{"frames":[{"instruction_addr":"0x20001015","package":"app.exe"}]}}`
	for _, project := range []string{f.project.ID, other.ID} {
		if _, err := f.sql.Exec(f.ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,created_at,data) VALUES($1,'same-event','error','isolation','error',now(),now(),$2::jsonb)`, project, event); err != nil {
			t.Fatal(err)
		}
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	for _, project := range []string{f.project.ID, other.ID} {
		if err := f.db.EnqueueNativeReprocessing(f.ctx, access, project); err != nil {
			t.Fatal(err)
		}
	}
	// Both jobs must exist before the same worker runs, with the first requested first.
	var queued int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id IN ($1::bigint,$2::bigint)`, f.project.ID, other.ID).Scan(&queued); err != nil || queued != 2 {
		t.Fatalf("queue rows=%d err=%v", queued, err)
	}
	stop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour, config.Config{SymStoreURLs: []string{source.URL + "/symbols"}, SymStoreAllowPrivate: true})
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id IN ($1::bigint,$2::bigint)`, f.project.ID, other.ID).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if queued == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if queued != 0 {
		t.Fatal("project queues did not drain")
	}
	readFrame := func(project string) map[string]any {
		t.Helper()
		var data []byte
		if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id='same-event'`, project).Scan(&data); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Stacktrace struct {
				Frames []map[string]any `json:"frames"`
			} `json:"stacktrace"`
		}
		if err := json.Unmarshal(data, &result); err != nil || len(result.Stacktrace.Frames) != 1 {
			t.Fatalf("invalid result %s: %v", data, err)
		}
		return result.Stacktrace.Frames[0]
	}
	local, remote := readFrame(f.project.ID), readFrame(other.ID)
	localFile, _ := local["filename"].(string)
	remoteFile, _ := remote["filename"].(string)
	if local["function"] != "LocalProjectFunction" || !strings.HasSuffix(localFile, "local.cc") || local["lineno"] != float64(7) {
		t.Fatalf("local project frame=%v", local)
	}
	if remote["function"] != "CrashFunction" || !strings.HasSuffix(remoteFile, "crash.cc") || remote["lineno"] != float64(42) {
		t.Fatalf("remote project frame=%v", remote)
	}
	if calls.Load() != 1 || wrongPath.Load() {
		t.Fatalf("remote requests=%d wrong path=%v; want exactly one for the project without local symbols", calls.Load(), wrongPath.Load())
	}
}
