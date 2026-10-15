//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// This extends the authenticated HTTP fixture to protect upload-to-mapped-event
// behavior. Pure mapping and SQL tests cannot detect missing routes, multipart
// validation or ingestion wiring. No production test seam is needed.
func verifySourceMapWorkflow(t *testing.T, client *http.Client, base, org, project, csrf, ingestion string) {
	t.Helper()
	endpoint := base + "/api/v1/organizations/" + org + "/projects/" + project + "/sourcemaps"
	const sourceMap = `{"version":3,"file":"app.js","sources":["../src/checkout.js"],"names":["submitCheckout"],"mappings":"AAEIA","sourcesContent":["export function submitCheckout() {\n  const total = 42;\n  throw new Error('mapped fixture');\n}"]}`
	fields := map[string]string{"name": "~/assets/app.js.map", "release": "frontend@1", "dist": "web"}
	upload := func(values map[string]string, data string, expected int) map[string]any {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for key, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		file, err := writer.CreateFormFile("file", "app.js.map")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(file, strings.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest("POST", endpoint, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-CSRF-Token", csrf)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != expected {
			t.Fatalf("map upload=%d want=%d body=%+v", res.StatusCode, expected, result)
		}
		return result
	}
	created := upload(fields, sourceMap, 201)
	if created["created"] != true || created["artifact"].(map[string]any)["size"] != float64(len(sourceMap)) {
		t.Fatalf("map receipt=%+v", created)
	}
	if upload(fields, sourceMap, 200)["created"] != false {
		t.Fatal("identical map retry was not idempotent")
	}
	upload(fields, strings.Replace(sourceMap, "total = 42", "total = 43", 1), 409)
	upload(map[string]string{"name": "~/assets/invalid.map", "release": "frontend@1"}, `{"version":3,"mappings":"!"}`, 400)
	upload(map[string]string{"name": "~/assets/invalid-index.map", "release": "frontend@1"}, `{"version":3,"sources":[],"names":[],"mappings":"AAAA"}`, 400)
	upload(map[string]string{"name": "~/assets/app.js.map"}, sourceMap, 400)
	upload(map[string]string{"name": "~/bad.map", "release": "invalid\x00release"}, sourceMap, 400)
	upload(fields, strings.Repeat("a", (1<<20)+1), 413)
	const debugID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	upload(map[string]string{"name": "~/unrelated-name.js.map", "debugId": debugID}, sourceMap, 201)
	for _, tc := range []struct {
		id, release, dist string
		debug, mapped     bool
	}{
		{"000000000000000000000000000000a1", "frontend@1", "web", false, true},
		{"000000000000000000000000000000a2", "frontend@1", "mobile", false, false},
		{"000000000000000000000000000000a3", "frontend@unrelated", "", true, true},
	} {
		label := "HTTP source map " + tc.id
		event := map[string]any{"event_id": tc.id, "release": tc.release, "dist": tc.dist, "exception": map[string]any{"values": []any{map[string]any{"type": "Error", "value": label, "stacktrace": map[string]any{"frames": []any{map[string]any{"abs_path": "https://cdn.example/assets/app.js", "filename": "app.js", "lineno": 1, "colno": 1, "function": "a"}}}}}}}
		if tc.debug {
			event["debug_meta"] = map[string]any{"images": []any{map[string]any{"type": "sourcemap", "code_file": "https://cdn.example/assets/app.js", "debug_id": debugID}}}
			exception := event["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)
			stack := exception["stacktrace"].(map[string]any)
			stack["frames"] = append(stack["frames"].([]any), map[string]any{"abs_path": "https://cdn.example/unrelated-name.js", "filename": "unrelated-name.js", "lineno": 1, "colno": 1})
		}
		payload, _ := json.Marshal(event)
		res, err := http.Post(ingestion, "text/plain", strings.NewReader("{}\n{\"type\":\"event\"}\n"+string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("mapped capture=%d", res.StatusCode)
		}
		read := func(path string) map[string]any {
			t.Helper()
			res, err := client.Get(base + path)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var data map[string]any
			if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 200 {
				t.Fatalf("read mapped event=%d %+v", res.StatusCode, data)
			}
			return data
		}
		list := read("/api/v1/organizations/" + org + "/issues?query=" + url.QueryEscape(label))["data"].([]any)
		if len(list) != 1 {
			t.Fatalf("mapped issue search=%+v", list)
		}
		detail := read("/api/v1/issues/" + list[0].(map[string]any)["id"].(string))
		exception := detail["event"].(map[string]any)["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)
		frame := exception["stacktrace"].(map[string]any)["frames"].([]any)[0].(map[string]any)
		if tc.debug {
			unrelated := exception["stacktrace"].(map[string]any)["frames"].([]any)[1].(map[string]any)
			if unrelated["filename"] != "unrelated-name.js" || unrelated["lineno"] != float64(1) {
				t.Fatalf("debug-only map crossed frame identity: %+v", unrelated)
			}
		}
		if tc.mapped {
			if frame["filename"] != "../src/checkout.js" || frame["lineno"] != float64(3) || frame["colno"] != float64(5) || frame["function"] != "submitCheckout" || frame["context_line"] != "  throw new Error('mapped fixture');" {
				t.Fatalf("mapped frame=%+v", frame)
			}
			original := exception["raw_stacktrace"].(map[string]any)["frames"].([]any)[0].(map[string]any)
			if original["filename"] != "app.js" || original["lineno"] != float64(1) {
				t.Fatalf("lost original stack=%+v", original)
			}
		} else if frame["filename"] != "app.js" || frame["lineno"] != float64(1) {
			t.Fatalf("map crossed distribution=%+v", frame)
		}
	}
}
