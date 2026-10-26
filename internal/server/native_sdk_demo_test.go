//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/native"
	"goodsentry/internal/store"
)

func TestNativeSDKDemoCrashSymbolicates(t *testing.T) {
	binaryPath := os.Getenv("GOODSENTRY_NATIVE_DEMO_BINARY")
	if binaryPath == "" {
		t.Skip("native SDK demo is built only by its dedicated integration workflow")
	}
	handlerPath := os.Getenv("GOODSENTRY_NATIVE_DEMO_HANDLER")
	if handlerPath == "" {
		t.Fatal("GOODSENTRY_NATIVE_DEMO_HANDLER is required when the demo binary is configured")
	}

	f := newResolutionHTTPFixture(t)
	accessToken := issueManagementToken(t, f, f.org.Slug, "Native SDK demo", []string{"project:releases"}, []string{f.project.ID})
	executable, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	debugObjects := native.ParseDebugObjects(filepath.Base(binaryPath), executable)
	if len(debugObjects) != 1 || debugObjects[0].DebugID == "" {
		t.Fatalf("demo ELF must contain one parseable Debug ID, got %+v", debugObjects)
	}

	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, err := form.CreateFormFile("file", filepath.Base(binaryPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(executable); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"type": "elf", "cpu_name": "x86_64", "object_name": filepath.Base(binaryPath),
	} {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	uploadURL := f.server.URL + "/good/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/files/dsyms/"
	uploadRequest, err := http.NewRequestWithContext(f.ctx, http.MethodPost, uploadURL, &upload)
	if err != nil {
		t.Fatal(err)
	}
	uploadRequest.Header.Set("Content-Type", form.FormDataContentType())
	uploadRequest.Header.Set("Authorization", "Bearer "+accessToken)
	uploadResponse, err := http.DefaultClient.Do(uploadRequest)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, readErr := io.ReadAll(uploadResponse.Body)
	_ = uploadResponse.Body.Close()
	if readErr != nil || uploadResponse.StatusCode != http.StatusOK {
		t.Fatalf("upload the demo ELF: status=%d body=%s read error=%v", uploadResponse.StatusCode, uploadBody, readErr)
	}

	serverURL, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	databasePath, err := filepath.Abs(filepath.Join(t.TempDir(), "sdk-cache"))
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("http://%s@%s/good/%s", f.project.PublicKey, serverURL.Host, f.project.ID)
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binaryPath)
	command.Env = nativeDemoEnvironment(os.Environ(), map[string]string{
		"SENTRY_DSN":              dsn,
		"SENTRY_CRASHPAD_HANDLER": handlerPath,
		"SENTRY_DATABASE_PATH":    databasePath,
	})
	output, crashErr := command.CombinedOutput()
	if ctx.Err() != nil || crashErr == nil || !bytes.Contains(output, []byte("Sending an intentional native crash to GoodSentry.")) {
		t.Fatalf("demo did not reach its intentional native fault: error=%v output=%s", crashErr, output)
	}

	type nativeFrame struct {
		Function string `json:"function"`
		Filename string `json:"filename"`
		Line     int    `json:"lineno"`
	}
	type nativeImage struct {
		CodeFile string `json:"code_file"`
		DebugID  string `json:"debug_id"`
	}
	type nativePayload struct {
		Platform  string `json:"platform"`
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []nativeFrame `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
		DebugMeta struct {
			Images []nativeImage `json:"images"`
		} `json:"debug_meta"`
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	deadline := time.Now().Add(15 * time.Second)
	var eventID string
	var lastFrames []nativeFrame
	var lastImages []nativeImage
	for time.Now().Before(deadline) {
		events, queryErr := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		for _, event := range events {
			var payload nativePayload
			if json.Unmarshal(event.Data, &payload) != nil || payload.Platform != "native" || len(payload.Exception.Values) == 0 {
				continue
			}
			frames := payload.Exception.Values[0].Stacktrace.Frames
			lastFrames = frames
			lastImages = payload.DebugMeta.Images
			for _, frame := range frames {
				if frame.Function == "goodsentry_demo_crash" && filepath.Base(frame.Filename) == "main.c" && frame.Line > 0 {
					imageMatched := false
					for _, image := range payload.DebugMeta.Images {
						if filepath.Base(image.CodeFile) == filepath.Base(binaryPath) && native.DebugKey(image.DebugID) == native.DebugKey(debugObjects[0].DebugID) {
							imageMatched = true
							break
						}
					}
					if !imageMatched {
						t.Fatalf("native crash frame resolved without matching ELF debug metadata: frames=%+v images=%+v", frames, payload.DebugMeta.Images)
					}
					eventID = event.ID
					break
				}
			}
			if eventID != "" {
				break
			}
		}
		if eventID != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if eventID == "" {
		t.Fatalf("Crashpad event did not resolve goodsentry_demo_crash to main.c with a positive line; last frames=%+v images=%+v", lastFrames, lastImages)
	}
	var issueID string
	if err := f.sql.QueryRow(f.ctx, `SELECT i.id::text FROM events e JOIN issues i ON i.id=e.issue_id
		WHERE e.project_id=$1::bigint AND e.event_id=$2`, f.project.ID, eventID).Scan(&issueID); err != nil || issueID == "" {
		t.Fatalf("native SDK event has no issue link: %v", err)
	}

	var attachmentCount, envelopeItemCount int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM attachments WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, eventID).Scan(&attachmentCount); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM envelope_items WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, eventID).Scan(&envelopeItemCount); err != nil {
		t.Fatal(err)
	}
	if attachmentCount != 0 || envelopeItemCount != 0 {
		t.Fatalf("native minidump bytes were retained: attachments=%d envelope items=%d", attachmentCount, envelopeItemCount)
	}
}

func nativeDemoEnvironment(base []string, values map[string]string) []string {
	environment := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[name]; !replaced {
			environment = append(environment, entry)
		}
	}
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment
}
