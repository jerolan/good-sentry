//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

func TestNativeReprocessingUsesLateOMAPPDB(t *testing.T) {
	const id = "5A9832E5287241C1838ED98914E9B7FF1"
	f := newResolutionHTTPFixture(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other OMAP reprocessing", "native")
	if err != nil {
		t.Fatal(err)
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	fixture := nativepdb.WithOMAPInlineFrames()
	pdb := fixture.Data
	save := func(projectID string, data []byte, debugID string) {
		t.Helper()
		digest := sha1.Sum(data)
		if _, err := f.db.SaveDebugFile(f.ctx, access, projectID, "late.pdb", "pdb", debugID, "x86_64", "crash.exe", hex.EncodeToString(digest[:]), nil, nil, data); err != nil {
			t.Fatal(err)
		}
	}
	save(other.ID, pdb, id)
	wrong := append([]byte(nil), pdb...)
	binary.LittleEndian.PutUint32(wrong[fixture.InfoBlock*512+8:], 3)
	save(f.project.ID, wrong, "5A9832E5287241C1838ED98914E9B7FF2")

	dump := buildNativeModuleMinidump(t, "C:\\test\\crash.exe", id, 0x2045)
	body, contentType := minidumpForm(t, dump, map[string]string{"crash_context": "keep-me"})
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var accepted struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(res.Body).Decode(&accepted)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
		t.Fatalf("capture status=%d id=%q err=%v", res.StatusCode, accepted.ID, err)
	}
	read := func() []byte {
		t.Helper()
		var data []byte
		if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, accepted.ID).Scan(&data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	path := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/reprocessing/"
	run := func() {
		t.Helper()
		f.request(t, f.client, http.MethodPost, path, "", http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusOK)
		stop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour)
		defer stop()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var queued int
			if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued); err != nil {
				t.Fatal(err)
			}
			if queued == 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("reprocessing queue did not drain")
	}
	before := read()
	run()
	if got := read(); !bytes.Equal(got, before) {
		t.Fatal("foreign or mismatched PDB modified the event")
	}
	save(f.project.ID, pdb, id)
	run()
	after := read()
	var old, updated map[string]any
	if json.Unmarshal(before, &old) != nil || json.Unmarshal(after, &updated) != nil {
		t.Fatal("invalid event JSON")
	}
	frames := func(event map[string]any) []any {
		t.Helper()
		exception := event["exception"].(map[string]any)
		values := exception["values"].([]any)
		return values[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	}
	oldFrames, newFrames := frames(old), frames(updated)
	if len(oldFrames) != 1 || len(newFrames) != len(oldFrames) {
		t.Fatalf("frame count before=%d after=%d", len(oldFrames), len(newFrames))
	}
	if value, exists := oldFrames[0].(map[string]any)["function"]; exists && value != "" && value != nil {
		t.Fatalf("capture unexpectedly symbolicated before upload: %v", value)
	}
	frame := newFrames[0].(map[string]any)
	if frame["function"] != "CrashFunction" || frame["filename"] != `C:\src\crash.cc` || frame["lineno"] != float64(42) {
		t.Fatalf("reprocessed frame=%+v", frame)
	}
	newFrames[0] = oldFrames[0]
	if !reflect.DeepEqual(old, updated) {
		t.Fatal("reprocessing modified unrelated event fields")
	}
	run()
	if got := read(); !bytes.Equal(got, after) {
		t.Fatal("reprocessing was not idempotent")
	}
}
