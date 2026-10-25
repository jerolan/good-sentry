//go:build integration

package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"database/sql"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
	"goodsentry/internal/testfixture/nativesymcache"
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

func TestNativeMinidumpProjectReprocessingUsesLateProjectSymbols(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	otherProject, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other native project", "native")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/0/projects/" + f.org.Slug + "/"
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 CrashFunction\n1010 20 42 1\n")
	uploadSymbol := func(projectSlug string) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "test_app.sym")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(symbol); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{
			"type": "sym", "debug_id": "5A9832E5287241C1838ED98914E9B7FF1",
			"cpu_name": "x86_64", "object_name": "test_app.pdb",
		} {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good"+base+projectSlug+"/files/dsyms/", &body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-CSRF-Token", f.csrf)
		res, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			response, _ := io.ReadAll(res.Body)
			t.Fatalf("debug symbol upload=%d body=%s", res.StatusCode, response)
		}
	}
	uploadSymbol(otherProject.Slug)
	wrongPDB := nativepdb.WithSourceLines(false).Data
	wrongDigest := sha1.Sum(wrongPDB)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, Role: "owner", AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "wrong.pdb", "pdb", "5A9832E5287241C1838ED98914E9B7FF2", "x86_64", "test_app.pdb", hex.EncodeToString(wrongDigest[:]), nil, nil, wrongPDB); err != nil {
		t.Fatal(err)
	}

	dump, err := os.ReadFile("testdata/amd64-minidump.dmp")
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := minidumpForm(t, dump, map[string]string{"sentry[release]": "native@1", "sentry[environment]": "production", "crash_context": "keep-me"})
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
	decodeErr := json.NewDecoder(res.Body).Decode(&accepted)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || decodeErr != nil || accepted.ID == "" {
		t.Fatalf("minidump capture=%d id=%q err=%v", res.StatusCode, accepted.ID, decodeErr)
	}

	readReceipt := func() ([]byte, string, time.Time, string, string, string) {
		t.Helper()
		var data []byte
		var issueID sql.NullString
		var timestamp time.Time
		var eventID, release, environment string
		if err := f.sql.QueryRow(f.ctx, `SELECT data,event_id,timestamp,issue_id::text,release,environment FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, accepted.ID).Scan(&data, &eventID, &timestamp, &issueID, &release, &environment); err != nil {
			t.Fatal(err)
		}
		if !issueID.Valid {
			t.Fatal("native event has no issue link")
		}
		return data, eventID, timestamp, issueID.String, release, environment
	}
	before, beforeID, beforeTimestamp, beforeIssue, beforeRelease, beforeEnvironment := readReceipt()
	var unrelatedBefore []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`, f.project.ID).Scan(&unrelatedBefore); err != nil {
		t.Fatal(err)
	}
	var original struct {
		Release     string `json:"release"`
		Environment string `json:"environment"`
		Extra       struct {
			CrashContext string `json:"crash_context"`
		} `json:"extra"`
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []struct {
						Function string `json:"function"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(before, &original); err != nil {
		t.Fatal(err)
	}
	if len(original.Exception.Values) != 1 || len(original.Exception.Values[0].Stacktrace.Frames) < 2 || original.Exception.Values[0].Stacktrace.Frames[0].Function != "" {
		t.Fatalf("capture should retain unresolved native frames before symbol upload: %+v", original.Exception)
	}

	otherEvent, err := f.db.DebugSymbolFiles(f.ctx, otherProject.ID)
	if err != nil || len(otherEvent) != 1 {
		t.Fatalf("other project symbols missing: %d %v", len(otherEvent), err)
	}
	reprocessPath := base + f.project.Slug + "/reprocessing/"
	if response := f.request(t, f.client, http.MethodPost, reprocessPath, "", http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusOK); len(response) != 0 {
		t.Fatalf("reprocessing response must be empty, got %q", response)
	}
	stop := jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour)
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		var queued int
		if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued); err != nil {
			stop()
			t.Fatal(err)
		}
		if queued == 0 {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	if !completed {
		t.Fatal("worker did not drain project job with foreign-only symbols")
	}
	afterWrongProject, _, _, _, _, _ := readReceipt()
	if !bytes.Equal(before, afterWrongProject) {
		t.Fatal("reprocessing borrowed symbols from another project")
	}

	uploadSymbol(f.project.Slug)
	for range 2 {
		if response := f.request(t, f.client, http.MethodPost, reprocessPath, "", http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusOK); len(response) != 0 {
			t.Fatalf("reprocessing response must be empty, got %q", response)
		}
	}
	var queued int
	var cursor, through int64
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*),min(cursor_event_id),max(through_event_id) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued, &cursor, &through); err != nil || queued != 1 || cursor != 0 {
		t.Fatalf("repeated requests did not coalesce: rows=%d cursor=%d through=%d err=%v", queued, cursor, through, err)
	}
	stop = jobs.StartNativeReprocessing(f.ctx, f.db, time.Hour)
	deadline = time.Now().Add(5 * time.Second)
	var updated []byte
	for time.Now().Before(deadline) {
		updated, _, _, _, _, _ = readReceipt()
		var result struct {
			Exception struct {
				Values []struct {
					Stacktrace struct {
						Frames []struct {
							Function string `json:"function"`
							Filename string `json:"filename"`
							Line     int    `json:"lineno"`
						} `json:"frames"`
					} `json:"stacktrace"`
				} `json:"values"`
			} `json:"exception"`
		}
		if json.Unmarshal(updated, &result) == nil && len(result.Exception.Values) > 0 && len(result.Exception.Values[0].Stacktrace.Frames) > 0 && result.Exception.Values[0].Stacktrace.Frames[0].Function == "CrashFunction" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	var result struct {
		Release     string `json:"release"`
		Environment string `json:"environment"`
		Extra       struct {
			CrashContext string `json:"crash_context"`
		} `json:"extra"`
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []struct {
						Function string `json:"function"`
						Filename string `json:"filename"`
						Line     int    `json:"lineno"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(updated, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Exception.Values) != 1 || len(result.Exception.Values[0].Stacktrace.Frames) < 2 {
		t.Fatalf("reprocessed stack: %+v", result.Exception)
	}
	frame := result.Exception.Values[0].Stacktrace.Frames[0]
	if frame.Function != "CrashFunction" || frame.Filename != "C:\\src\\crash.cc" || frame.Line != 42 || beforeRelease != "native@1" || beforeEnvironment != "production" || result.Extra.CrashContext != "keep-me" {
		t.Fatalf("reprocessed native event: %+v", result)
	}
	_, afterID, afterTimestamp, afterIssue, afterRelease, afterEnvironment := readReceipt()
	if beforeID != afterID || !beforeTimestamp.Equal(afterTimestamp) || beforeIssue != afterIssue || afterRelease != beforeRelease || afterEnvironment != beforeEnvironment || len(result.Exception.Values[0].Stacktrace.Frames) != len(original.Exception.Values[0].Stacktrace.Frames) {
		t.Fatal("reprocessing changed event identity, timestamp, issue, or frame count")
	}
	var untouched []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`, f.project.ID).Scan(&untouched); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(untouched, unrelatedBefore) {
		t.Fatal("reprocessing changed an unrelated event")
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM project_native_reprocessing WHERE project_id=$1::bigint`, f.project.ID).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("completed job remains queued: %d %v", queued, err)
	}
}

func TestNativeMinidumpEnvelopeAttachment(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	const debugID = "5A9832E5287241C1838ED98914E9B7FF1"
	const eventID = "ca2e63929e354414adf9be81211c98aa"
	pdb := nativepdb.WithSourceLines(false).Data
	digest := sha1.Sum(pdb)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "test_app.pdb", "pdb", debugID, "x86_64", "test_app.pdb", hex.EncodeToString(digest[:]), nil, nil, pdb); err != nil {
		t.Fatal(err)
	}
	dump := buildNativeModuleMinidump(t, `C:\test\test_app.pdb`, debugID, 0x1015)
	makeEvent := func(id, platform string) []byte {
		t.Helper()
		payload := map[string]any{
			"event_id": id, "platform": platform, "level": "fatal", "message": "SDK native crash",
			"timestamp": "2026-10-23T10:11:12Z", "tags": map[string]string{"build": "demo-7"},
			"contexts": map[string]any{"device": map[string]string{"arch": "x86_64"}, "app": map[string]string{"version": "7"}},
			"exception": map[string]any{"values": []any{map[string]any{
				"type": "SIGSEGV", "value": "captured by sentry-native", "mechanism": map[string]any{"type": "signal", "handled": false},
				"data": map[string]string{"signal": "SIGSEGV"},
			}}},
			"debug_meta": map[string]any{"sdk_field": "preserved", "images": []any{
				map[string]any{"type": "symbolic", "image_addr": "0x140000000", "debug_id": debugID, "code_file": "client-stale.pdb"},
				map[string]any{"type": "symbolic", "image_addr": "0x1234", "debug_id": "CLIENTONLY1", "code_file": "client-only.dll"},
			}},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	type attachedDump struct {
		attachmentType string
		filename       string
		data           []byte
	}
	makeEnvelope := func(id string, event []byte, attachments ...attachedDump) []byte {
		t.Helper()
		var wire bytes.Buffer
		header, err := json.Marshal(map[string]string{"event_id": id})
		if err != nil {
			t.Fatal(err)
		}
		wire.Write(header)
		wire.WriteByte('\n')
		writeItem := func(itemHeader map[string]any, payload []byte) {
			t.Helper()
			itemHeader["length"] = len(payload)
			header, err := json.Marshal(itemHeader)
			if err != nil {
				t.Fatal(err)
			}
			wire.Write(header)
			wire.WriteByte('\n')
			wire.Write(payload)
			wire.WriteByte('\n')
		}
		if event != nil {
			writeItem(map[string]any{"type": "event"}, event)
		}
		for _, attachment := range attachments {
			itemHeader := map[string]any{"type": "attachment", "filename": attachment.filename}
			if attachment.attachmentType != "" {
				itemHeader["attachment_type"] = attachment.attachmentType
			}
			writeItem(itemHeader, attachment.data)
		}
		return wire.Bytes()
	}
	post := func(wire []byte) (int, []byte) {
		t.Helper()
		endpoint := f.server.URL + "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + f.project.PublicKey
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, endpoint, bytes.NewReader(wire))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-sentry-envelope")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, body
	}
	wire := makeEnvelope(eventID, makeEvent(eventID, "native"), attachedDump{attachmentType: "event.minidump", filename: "minidump.dmp", data: dump})
	status, response := post(wire)
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response, &accepted); err != nil || status != http.StatusOK || accepted.ID != eventID {
		t.Fatalf("native SDK envelope response=%d body=%s err=%v", status, response, err)
	}
	rows, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var stored json.RawMessage
	for _, row := range rows {
		var identity struct {
			EventID string `json:"event_id"`
		}
		if err := json.Unmarshal(row.Data, &identity); err != nil {
			t.Fatal(err)
		}
		if identity.EventID == eventID {
			stored = row.Data
			break
		}
	}
	if len(stored) == 0 {
		t.Fatalf("native SDK event %s was not stored", eventID)
	}
	var got struct {
		EventID   string            `json:"event_id"`
		Platform  string            `json:"platform"`
		Level     string            `json:"level"`
		Message   string            `json:"message"`
		Timestamp string            `json:"timestamp"`
		Tags      map[string]string `json:"tags"`
		Contexts  struct {
			App struct {
				Version string `json:"version"`
			} `json:"app"`
		} `json:"contexts"`
		Exception struct {
			Values []struct {
				Type       string            `json:"type"`
				Value      string            `json:"value"`
				Mechanism  map[string]any    `json:"mechanism"`
				Data       map[string]string `json:"data"`
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
			SDKField string `json:"sdk_field"`
			Images   []struct {
				ImageAddr string `json:"image_addr"`
				DebugID   string `json:"debug_id"`
				CodeFile  string `json:"code_file"`
			} `json:"images"`
		} `json:"debug_meta"`
	}
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatal(err)
	}
	if got.EventID != eventID || got.Platform != "native" || got.Level != "fatal" || got.Message != "SDK native crash" || got.Timestamp != "2026-10-23T10:11:12Z" || got.Tags["build"] != "demo-7" {
		t.Fatalf("SDK event fields were not preserved: %+v", got)
	}
	if got.Contexts.App.Version != "7" {
		t.Fatalf("SDK contexts were not preserved: %+v", got.Contexts)
	}
	if len(got.Exception.Values) != 1 {
		t.Fatalf("native SDK exceptions=%+v", got.Exception.Values)
	}
	exception := got.Exception.Values[0]
	if exception.Type != "SIGSEGV" || exception.Value != "captured by sentry-native" || exception.Mechanism["type"] != "signal" || exception.Data["signal"] != "SIGSEGV" {
		t.Fatalf("SDK exception identity/context changed: %+v", exception)
	}
	if len(exception.Stacktrace.Frames) < 1 || exception.Stacktrace.Frames[0].Function != "CrashFunction" || exception.Stacktrace.Frames[0].Filename != "C:\\src\\crash.cc" || exception.Stacktrace.Frames[0].Line != 42 {
		t.Fatalf("native SDK stack was not symbolicated: %+v", exception.Stacktrace.Frames)
	}
	if got.DebugMeta.SDKField != "preserved" || len(got.DebugMeta.Images) != 2 || got.DebugMeta.Images[0].DebugID != debugID || got.DebugMeta.Images[0].ImageAddr != "0x140000000" || got.DebugMeta.Images[0].CodeFile != `C:\test\test_app.pdb` || got.DebugMeta.Images[1].CodeFile != "client-only.dll" {
		t.Fatalf("Minidump debug images were not merged authoritatively: %+v", got.DebugMeta)
	}
	var retainedDumps int
	if err := f.sql.QueryRow(f.ctx, "SELECT count(*) FROM attachments WHERE project_id::text=$1 AND event_id=$2", f.project.ID, eventID).Scan(&retainedDumps); err != nil || retainedDumps != 0 {
		t.Fatalf("native SDK Minidump attachments retained=%d err=%v", retainedDumps, err)
	}

	badCases := []struct {
		name, id, platform string
		event              bool
		attachments        []attachedDump
	}{
		{name: "duplicate dumps", id: "ba2e63929e354414adf9be81211c98aa", platform: "native", event: true, attachments: []attachedDump{{attachmentType: "event.minidump", filename: "one.dmp", data: dump}, {attachmentType: "event.minidump", filename: "two.dmp", data: dump}}},
		{name: "missing event", id: "bb2e63929e354414adf9be81211c98aa", attachments: []attachedDump{{attachmentType: "event.minidump", filename: "missing.dmp", data: dump}}},
		{name: "non-native event", id: "bc2e63929e354414adf9be81211c98aa", platform: "go", event: true, attachments: []attachedDump{{attachmentType: "event.minidump", filename: "go.dmp", data: dump}}},
		{name: "malformed dump", id: "bd2e63929e354414adf9be81211c98aa", platform: "native", event: true, attachments: []attachedDump{{attachmentType: "event.minidump", filename: "invalid.dmp", data: []byte("not a minidump")}}},
	}
	for _, test := range badCases {
		t.Run(test.name, func(t *testing.T) {
			var parent []byte
			if test.event {
				parent = makeEvent(test.id, test.platform)
			}
			status, body := post(makeEnvelope(test.id, parent, test.attachments...))
			if status != http.StatusBadRequest {
				t.Fatalf("invalid native envelope status=%d body=%s", status, body)
			}
			rows, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				var identity struct {
					EventID string `json:"event_id"`
				}
				if err := json.Unmarshal(row.Data, &identity); err != nil {
					t.Fatal(err)
				}
				if identity.EventID == test.id {
					t.Fatalf("invalid native event %s was committed", test.id)
				}
			}
			var attachmentRows int
			if err := f.sql.QueryRow(f.ctx, "SELECT count(*) FROM attachments WHERE project_id::text=$1 AND event_id=$2", f.project.ID, test.id).Scan(&attachmentRows); err != nil || attachmentRows != 0 {
				t.Fatalf("invalid native attachment rows=%d err=%v", attachmentRows, err)
			}
		})
	}

	ordinaryID := "be2e63929e354414adf9be81211c98aa"
	status, body := post(makeEnvelope(ordinaryID, makeEvent(ordinaryID, "native"), attachedDump{filename: "unmarked.dmp", data: dump}))
	if status != http.StatusOK {
		t.Fatalf("unmarked .dmp attachment status=%d body=%s", status, body)
	}
	var ordinaryRows int
	if err := f.sql.QueryRow(f.ctx, "SELECT count(*) FROM attachments WHERE project_id::text=$1 AND event_id=$2 AND data=$3", f.project.ID, ordinaryID, dump).Scan(&ordinaryRows); err != nil || ordinaryRows != 1 {
		t.Fatalf("unmarked .dmp attachment rows=%d err=%v", ordinaryRows, err)
	}
}

func TestNativeRISCV64MinidumpIngestion(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	dump := buildRISCV64NativeMinidump(t, "C:\\test\\riscv64.exe", nativeTestDebugID, 0x1010)
	body, contentType := minidumpForm(t, dump, map[string]string{"sentry[release]": "riscv64@1"})
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
		t.Fatalf("RISC-V64 minidump response=%d %+v %v", res.StatusCode, accepted, err)
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
	if err != nil || len(events) == 0 {
		t.Fatalf("stored RISC-V64 minidump event: %d %v", len(events), err)
	}
	var payload struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []nativeFrame `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Exception.Values) != 1 || len(payload.Exception.Values[0].Stacktrace.Frames) != 2 {
		t.Fatalf("stored RISC-V64 frames: %+v", payload.Exception)
	}
}

func TestNativePDBSymbolicationUsesEmbeddedDebugID(t *testing.T) {
	const embeddedID = "5A9832E5287241C1838ED98914E9B7FF1"
	for _, test := range []struct {
		name       string
		uploadedID string
		wantSymbol bool
	}{
		{name: "matching identity", uploadedID: embeddedID, wantSymbol: true},
		{name: "mismatched uploaded identity", uploadedID: "5A9832E5287241C1838ED98914E9B7FF2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			pdb := nativepdb.WithSourceLines(false).Data
			digest := sha1.Sum(pdb)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "crash.pdb", "pdb", test.uploadedID, "x86_64", "crash.exe", hex.EncodeToString(digest[:]), nil, nil, pdb); err != nil {
				t.Fatal(err)
			}

			minidump := buildNativeModuleMinidump(t, "C:\\test\\crash.exe", embeddedID, 0x1015)
			body, contentType := minidumpForm(t, minidump, nil)
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
				t.Fatalf("PDB minidump response=%d %+v %v", res.StatusCode, accepted, err)
			}

			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
			if err != nil || len(events) == 0 {
				t.Fatalf("stored PDB minidump event: %d %v", len(events), err)
			}
			var payload struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []nativeFrame `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(events[0].Data, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 || len(payload.Exception.Values[0].Stacktrace.Frames) == 0 {
				t.Fatalf("PDB minidump frames: %+v", payload.Exception)
			}
			frame := payload.Exception.Values[0].Stacktrace.Frames[0]
			gotSymbol := frame.Function == "CrashFunction"
			if gotSymbol != test.wantSymbol {
				t.Fatalf("PDB symbolication=%+v, want symbol=%t", frame, test.wantSymbol)
			}
			if test.wantSymbol && (frame.Filename != `C:\src\crash.cc` || frame.Line != 42) {
				t.Fatalf("PDB source line=%+v, want C:\\src\\crash.cc:42", frame)
			}
			if !test.wantSymbol && (frame.Filename != "" || frame.Line != 0) {
				t.Fatalf("mismatched PDB identity returned source line: %+v", frame)
			}
		})
	}
}

func TestNativePDBInlineFramesReachMinidumpEvent(t *testing.T) {
	const embeddedID = "5A9832E5287241C1838ED98914E9B7FF1"
	f := newResolutionHTTPFixture(t)
	pdb := nativepdb.WithInlineFrames().Data
	digest := sha1.Sum(pdb)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "crash-inline.pdb", "pdb", embeddedID, "x86_64", "crash.exe", hex.EncodeToString(digest[:]), nil, nil, pdb); err != nil {
		t.Fatal(err)
	}

	minidump := buildNativeModuleMinidump(t, "C:\\test\\crash.exe", embeddedID, 0x1015)
	body, contentType := minidumpForm(t, minidump, nil)
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
		t.Fatalf("inline PDB minidump response=%d %+v %v", res.StatusCode, accepted, err)
	}

	events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
	if err != nil || len(events) == 0 {
		t.Fatalf("stored inline PDB event: %d %v", len(events), err)
	}
	var payload struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []struct {
						Function    string `json:"function"`
						Filename    string `json:"filename"`
						Line        int    `json:"lineno"`
						Instruction string `json:"instruction_addr"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Exception.Values) != 1 {
		t.Fatalf("inline PDB exception values=%+v", payload.Exception.Values)
	}
	frames := payload.Exception.Values[0].Stacktrace.Frames
	want := []nativeFrame{
		{Function: "DeepInline", Filename: `C:\include\deep.hpp`, Line: 300},
		{Function: "InnerInline", Filename: `C:\include\inner.hpp`, Line: 200},
		{Function: "OuterInline", Filename: `C:\include\inline.hpp`, Line: 100},
		{Function: "CrashFunction", Filename: `C:\src\crash.cc`, Line: 42},
	}
	if len(frames) != len(want) {
		t.Fatalf("inline PDB event frames=%+v, want %+v", frames, want)
	}
	for i, expected := range want {
		frame := frames[i]
		if frame.Function != expected.Function || frame.Filename != expected.Filename || frame.Line != expected.Line || frame.Instruction != "0x140001015" {
			t.Fatalf("inline PDB frame[%d]=%+v, want %+v at 0x140001015", i, frame, expected)
		}
	}
}

func TestNativeSymCacheSymbolicationUsesEmbeddedDebugID(t *testing.T) {
	const embeddedID = "00112233445566778899AABBCCDDEEFF1"
	for _, test := range []struct {
		name       string
		moduleID   string
		wantSymbol bool
	}{
		{name: "matching module identity", moduleID: embeddedID, wantSymbol: true},
		{name: "mismatched module identity", moduleID: "00112233445566778899AABBCCDDEEFF2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			cache := nativesymcache.Build(9, 0).Data
			digest := sha1.Sum(cache)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "symbols.symcache", "symcache", embeddedID, "x86_64", "crash.exe", hex.EncodeToString(digest[:]), nil, nil, cache); err != nil {
				t.Fatal(err)
			}

			minidump := buildNativeModuleMinidump(t, "C:\\test\\renamed.exe", test.moduleID, 0x1015)
			body, contentType := minidumpForm(t, minidump, nil)
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
				t.Fatalf("SymCache minidump response=%d %+v %v", res.StatusCode, accepted, err)
			}

			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
			if err != nil || len(events) == 0 {
				t.Fatalf("stored SymCache minidump event: %d %v", len(events), err)
			}
			var payload struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []nativeFrame `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(events[0].Data, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 || len(payload.Exception.Values[0].Stacktrace.Frames) == 0 {
				t.Fatalf("SymCache minidump frames: %+v", payload.Exception)
			}
			frame := payload.Exception.Values[0].Stacktrace.Frames[0]
			gotSymbol := frame.Function == "CrashFunction"
			if gotSymbol != test.wantSymbol {
				t.Fatalf("SymCache symbolication=%+v, want symbol=%t", frame, test.wantSymbol)
			}
			if test.wantSymbol && (frame.Filename != `C:\project\src\crash.c` || frame.Line != 42) {
				t.Fatalf("SymCache source line=%+v, want C:\\project\\src\\crash.c:42", frame)
			}
			if !test.wantSymbol && (frame.Filename != "" || frame.Line != 0) {
				t.Fatalf("mismatched SymCache identity returned source line: %+v", frame)
			}
		})
	}
}

const nativeTestDebugID = "11223344-5566-7788-aabb-ccddeeff00111"

type nativeFrame struct {
	Function string `json:"function"`
	Filename string `json:"filename"`
	Line     int    `json:"lineno"`
}

func buildNativeModuleMinidump(t *testing.T, moduleName, debugID string, functionOffset uint64, stackData ...[]byte) []byte {
	t.Helper()
	const moduleBase = uint64(0x0000000140000000)
	crashAddress := moduleBase + functionOffset
	context := make([]byte, 1232)
	binary.LittleEndian.PutUint64(context[152:], 0x200000)
	binary.LittleEndian.PutUint64(context[160:], 0x200000)
	binary.LittleEndian.PutUint64(context[248:], crashAddress)
	stack := make([]byte, 0x100)
	if len(stackData) > 0 {
		copy(stack, stackData[0])
	}
	modules := make([]byte, 4+108)
	binary.LittleEndian.PutUint32(modules, 1)
	binary.LittleEndian.PutUint64(modules[4:], moduleBase)
	binary.LittleEndian.PutUint32(modules[12:], 0x10000)
	binary.LittleEndian.PutUint32(modules[4+76:], 24)
	threads := make([]byte, 4+48)
	binary.LittleEndian.PutUint32(threads, 1)
	binary.LittleEndian.PutUint32(threads[4:], 7)
	exception := make([]byte, 32)
	binary.LittleEndian.PutUint32(exception, 7)
	binary.LittleEndian.PutUint32(exception[8:], 0xC0000005)
	binary.LittleEndian.PutUint64(exception[24:], crashAddress)
	system := make([]byte, 2)
	binary.LittleEndian.PutUint16(system, 9)
	streams := [][]byte{system, threads, modules, exception}
	buf := make([]byte, 32+len(streams)*12)
	appendData := func(value []byte) uint32 {
		rva := uint32(len(buf))
		buf = append(buf, value...)
		return rva
	}
	encodeUTF16 := func(value string) []byte {
		out := make([]byte, 4+len(value)*2)
		binary.LittleEndian.PutUint32(out, uint32(len(value)*2))
		for i, character := range value {
			binary.LittleEndian.PutUint16(out[4+i*2:], uint16(character))
		}
		return out
	}
	stackRVA := appendData(stack)
	contextRVA := appendData(context)
	binary.LittleEndian.PutUint32(modules[4+20:], appendData(encodeUTF16(moduleName)))
	codeView := make([]byte, 24)
	copy(codeView, "RSDS")
	if err := setCodeViewDebugID(codeView, debugID); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(modules[4+80:], appendData(codeView))
	binary.LittleEndian.PutUint64(threads[28:], 0x200000)
	binary.LittleEndian.PutUint32(threads[36:], uint32(len(stack)))
	binary.LittleEndian.PutUint32(threads[40:], stackRVA)
	binary.LittleEndian.PutUint32(threads[44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(threads[48:], contextRVA)
	types := []uint32{7, 3, 4, 6}
	locations := make([]struct{ typ, size, rva uint32 }, 0, len(streams))
	for i, stream := range streams {
		locations = append(locations, struct{ typ, size, rva uint32 }{types[i], uint32(len(stream)), appendData(stream)})
	}
	copy(buf, "MDMP")
	binary.LittleEndian.PutUint16(buf[4:], 0xa793)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(streams)))
	binary.LittleEndian.PutUint32(buf[12:], 32)
	for i, location := range locations {
		offset := 32 + i*12
		binary.LittleEndian.PutUint32(buf[offset:], location.typ)
		binary.LittleEndian.PutUint32(buf[offset+4:], location.size)
		binary.LittleEndian.PutUint32(buf[offset+8:], location.rva)
	}
	return buf
}

func buildRISCV64NativeMinidump(t *testing.T, moduleName, debugID string, functionOffset uint64) []byte {
	t.Helper()
	data := buildNativeModuleMinidump(t, moduleName, debugID, functionOffset)
	systemRVA := nativeTestMinidumpStreamRVA(data, 7)
	threadRVA := nativeTestMinidumpStreamRVA(data, 3)
	contextRVA := binary.LittleEndian.Uint32(data[threadRVA+48:])
	binary.LittleEndian.PutUint16(data[systemRVA:], 0x8006)
	binary.LittleEndian.PutUint32(data[threadRVA+44:], 528)
	context := data[contextRVA : contextRVA+528]
	binary.LittleEndian.PutUint32(context, 0x08000003)
	binary.LittleEndian.PutUint32(context[4:], 1)
	binary.LittleEndian.PutUint64(context[8:], 0x0000000140000000+functionOffset)
	setRegister := func(register uint32, value uint64) {
		if register == 0 || register > 31 {
			return
		}
		offset := 16 + (register-1)*8
		binary.LittleEndian.PutUint64(context[offset:], value)
	}
	setRegister(1, 0x0000000140000000+functionOffset+4)
	setRegister(2, 0x200000)
	setRegister(8, 0)
	return data
}

func nativeTestMinidumpStreamRVA(data []byte, streamType uint32) uint32 {
	count := binary.LittleEndian.Uint32(data[8:])
	directory := binary.LittleEndian.Uint32(data[12:])
	for i := uint32(0); i < count; i++ {
		entry := directory + i*12
		if binary.LittleEndian.Uint32(data[entry:]) == streamType {
			return binary.LittleEndian.Uint32(data[entry+8:])
		}
	}
	return 0
}

func setCodeViewDebugID(codeView []byte, debugID string) error {
	debugID = strings.ToUpper(strings.NewReplacer("-", "", "{", "", "}", "").Replace(debugID))
	if len(debugID) < 33 || len(debugID) > 40 {
		return fmt.Errorf("invalid fixture debug ID %q", debugID)
	}
	fields := []struct {
		start, end int
		bits       int
	}{
		{0, 8, 32}, {8, 12, 16}, {12, 16, 16}, {16, 32, 64}, {32, len(debugID), 32},
	}
	values := make([]uint64, len(fields))
	for i, field := range fields {
		value, err := strconv.ParseUint(debugID[field.start:field.end], 16, field.bits)
		if err != nil {
			return err
		}
		values[i] = value
	}
	binary.LittleEndian.PutUint32(codeView[4:], uint32(values[0]))
	binary.LittleEndian.PutUint16(codeView[8:], uint16(values[1]))
	binary.LittleEndian.PutUint16(codeView[10:], uint16(values[2]))
	binary.LittleEndian.PutUint64(codeView[12:], values[3])
	binary.LittleEndian.PutUint32(codeView[20:], uint32(values[4]))
	return nil
}

func ingestNativeObjectFrames(t *testing.T, f *resolutionHTTPFixture, object []byte, objectName, symbolType string, functionOffset uint64, expectedFunction string, stackData ...[]byte) []nativeFrame {
	t.Helper()
	digest := sha1.Sum(object)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, objectName, symbolType, nativeTestDebugID, "x86_64", objectName, hex.EncodeToString(digest[:]), nil, nil, object); err != nil {
		t.Fatal(err)
	}
	buf := buildNativeModuleMinidump(t, "C:\\test\\different.module", nativeTestDebugID, functionOffset, stackData...)
	body, contentType := minidumpForm(t, buf, nil)
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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("native object minidump response=%d", res.StatusCode)
	}
	events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
	if err != nil || len(events) == 0 {
		t.Fatalf("stored native object event: %d %v", len(events), err)
	}
	var payload struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []nativeFrame `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
		DebugMeta struct {
			Images []struct {
				DebugID string `json:"debug_id"`
			} `json:"images"`
		} `json:"debug_meta"`
	}
	found := false
	for _, event := range events {
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Exception.Values) == 1 && len(payload.Exception.Values[0].Stacktrace.Frames) > 0 &&
			payload.Exception.Values[0].Stacktrace.Frames[0].Function == expectedFunction {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("native object exception projection: %+v events=%d", payload.Exception, len(events))
	}
	if len(payload.DebugMeta.Images) != 1 || payload.DebugMeta.Images[0].DebugID != "1122334455667788AABBCCDDEEFF00111" {
		t.Fatalf("native object debug metadata: %+v", payload.DebugMeta)
	}
	return payload.Exception.Values[0].Stacktrace.Frames
}

func ingestNativeObject(t *testing.T, f *resolutionHTTPFixture, object []byte, objectName, symbolType string, functionOffset uint64, expectedFunction string) nativeFrame {
	t.Helper()
	return ingestNativeObjectFrames(t, f, object, objectName, symbolType, functionOffset, expectedFunction)[0]
}

func nativeDebugArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func nativeTARDebugArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for name, content := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg, Format: tar.FormatGNU}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func nativeGzipDebugArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func TestNativeELFObjectSymbolicationByDebugID(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	object, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	foundBase := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Filesz > 0 && (!foundBase || program.Vaddr < imageBase) {
			imageBase, foundBase = program.Vaddr, true
		}
	}
	symbols, err := file.Symbols()
	if err != nil || !foundBase {
		t.Fatalf("ELF fixture metadata: base=%#x %v", imageBase, err)
	}
	var functionAddress uint64
	for _, symbol := range symbols {
		if symbol.Name == "fixture_function" && symbol.Value >= imageBase {
			functionAddress = symbol.Value
			break
		}
	}
	if functionAddress == 0 {
		t.Fatal("ELF fixture lacks fixture_function")
	}
	frame := ingestNativeObject(t, f, object, "uploaded.elf", "elf", functionAddress-imageBase+0xb, "fixture_function")
	if !strings.HasSuffix(frame.Filename, "elf-dwarf-source.c") || frame.Line != 3 {
		t.Fatalf("ELF interior minidump frame: want elf-dwarf-source.c:3, got %+v", frame)
	}
}

func TestNativeMachODSYMObjectSymbolicationByDebugID(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	object, err := os.ReadFile("../native/testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := macho.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	for _, load := range file.Loads {
		if segment, ok := load.(*macho.Segment); ok && segment.Name == "__TEXT" {
			imageBase = segment.Addr
			break
		}
	}
	table, err := file.DWARF()
	if err != nil {
		t.Fatal(err)
	}
	reader := table.Reader()
	var functionOffset uint64
	for functionOffset == 0 {
		entry, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if entry == nil {
			t.Fatal("Mach-O fixture lacks main")
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		name, _ := entry.Val(dwarf.AttrName).(string)
		ranges, rangeErr := table.Ranges(entry)
		if name == "main" && rangeErr == nil && len(ranges) > 0 && ranges[0][0] >= imageBase {
			functionOffset = ranges[0][0] - imageBase
		}
	}
	frame := ingestNativeObject(t, f, object, "uploaded.dsym", "macho", functionOffset, "main")
	if !strings.HasSuffix(frame.Filename, "hello.c") || frame.Line < 1 {
		t.Fatalf("Mach-O dSYM minidump frame: %+v", frame)
	}
}

func TestNativeCFIUnwindWithoutFramePointer(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	object, err := os.ReadFile("../native/testdata/cfi-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	foundBase := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Filesz > 0 && (!foundBase || program.Vaddr < imageBase) {
			imageBase = program.Vaddr
			foundBase = true
		}
	}
	symbols, err := file.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[string]uint64{}
	for _, symbol := range symbols {
		switch symbol.Name {
		case "crash_frame", "middle_frame", "outer_frame":
			addresses[symbol.Name] = symbol.Value
		}
	}
	if !foundBase || imageBase == 0 || len(addresses) != 3 {
		t.Fatalf("CFI fixture metadata: base=%#x functions=%v", imageBase, addresses)
	}
	const moduleBase = uint64(0x0000000140000000)
	stack := make([]byte, 32)
	binary.LittleEndian.PutUint64(stack[0:], moduleBase+addresses["middle_frame"]-imageBase+0xc)
	binary.LittleEndian.PutUint64(stack[8:], moduleBase+addresses["outer_frame"]-imageBase+0xc)
	frames := ingestNativeObjectFrames(t, f, object, "uploaded-cfi.elf", "elf", addresses["crash_frame"]-imageBase, "crash_frame", stack)
	expected := []string{"crash_frame", "middle_frame", "outer_frame"}
	if len(frames) != len(expected) {
		t.Fatalf("CFI frames=%+v", frames)
	}
	for i, name := range expected {
		if frames[i].Function != name || !strings.HasSuffix(frames[i].Filename, "cfi-source.c") || frames[i].Line < 1 {
			t.Fatalf("CFI frame %d=%+v want %s", i, frames[i], name)
		}
	}
}

func TestNativeArchiveObjectSymbolicationByDebugID(t *testing.T) {
	machoObject, err := os.ReadFile("../native/testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	elfObject, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{
		"Example.dSYM/Contents/Resources/DWARF/Example": machoObject,
		"linux/libexample.so":                           elfObject,
	}
	formats := []struct {
		name  string
		build func(*testing.T, map[string][]byte) []byte
	}{
		{name: "zip", build: nativeDebugArchive},
		{name: "tar", build: nativeTARDebugArchive},
		{name: "tar.gz", build: func(t *testing.T, entries map[string][]byte) []byte {
			return nativeGzipDebugArchive(t, nativeTARDebugArchive(t, entries))
		}},
	}
	for _, format := range formats {
		t.Run(format.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			archive := format.build(t, entries)
			frame := ingestNativeObject(t, f, archive, "bundle."+format.name, "dsym", 0xf6a, "main")
			if !strings.HasSuffix(frame.Filename, "hello.c") || frame.Line < 1 {
				t.Fatalf("archive dSYM frame: %+v", frame)
			}
		})
	}
}
