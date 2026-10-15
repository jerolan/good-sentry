//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/config"
)

// Authoring gate: HTTP owns collecting byte-framed dependent items and continuing
// independent log/span ingestion after the parent's Ingest call. Returning200
// early or treating those independent types as opaque loses observable data.
// Existing standalone HTTP tests and typed CaptureItems Store tests cannot reach
// this mixed decoder-to-query path. Reuse an isolated real fixture, without a
// production seam. These are hand-built protocol fixtures, not SDK execution.
func TestCaptureEnvelopeKeepsDependentBytesAndIndependentTelemetry(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	f.server.Close()
	f.server = httptest.NewServer(New(config.Config{
		Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20, Logs: true,
	}, f.db))
	t.Cleanup(f.server.Close)
	decode := func(raw []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("response JSON: %v body=%s", err, raw)
		}
	}
	project := func(name string) (string, string) {
		t.Helper()
		var created struct{ ID, DSN string }
		decode(f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/projects", resolutionJSON(t, map[string]string{"name": name, "platform": "go"}), http.Header{"X-CSRF-Token": {f.csrf}}, 201), &created)
		dsn, err := url.Parse(created.DSN)
		if err != nil || dsn.User == nil || dsn.User.Username() == "" || dsn.Path != "/good/"+created.ID {
			t.Fatalf("issued DSN=%q error=%v", created.DSN, err)
		}
		return created.ID, dsn.User.Username()
	}
	projectID, key := project("Mixed envelope capture")
	otherProject, _ := project("Mixed envelope isolation")
	endpoint := "/api/" + projectID + "/envelope/?sentry_version=7&sentry_key=" + url.QueryEscape(key)
	type item struct {
		header map[string]any
		data   []byte
	}
	envelope := func(eventID string, items []item) []byte {
		t.Helper()
		var wire bytes.Buffer
		wire.WriteString(resolutionJSON(t, map[string]string{"event_id": eventID}))
		wire.WriteByte('\n')
		for _, part := range items {
			header := make(map[string]any, len(part.header)+1)
			for key, value := range part.header {
				header[key] = value
			}
			header["length"] = len(part.data)
			wire.WriteString(resolutionJSON(t, header))
			wire.WriteByte('\n')
			wire.Write(part.data)
			wire.WriteByte('\n')
		}
		return wire.Bytes()
	}
	type attachment struct {
		ID, Filename, ContentType string
		Data                      []byte
	}
	type opaque struct {
		ID, Type string
		Header   json.RawMessage
		Data     []byte
	}
	// There is currently no public attachment read/download endpoint. Observe
	// these bytes only in this fixture's isolated schema; do not claim download
	// coverage. Event/log/span reads below go through their real HTTP routes.
	children := func(eventID string) ([]attachment, []opaque) {
		t.Helper()
		rows, err := f.sql.Query(f.ctx, "SELECT id,filename,content_type,data FROM attachments WHERE project_id::text=$1 AND event_id=$2 ORDER BY filename", projectID, eventID)
		if err != nil {
			t.Fatal(err)
		}
		attachments := []attachment{}
		for rows.Next() {
			var current attachment
			if err := rows.Scan(&current.ID, &current.Filename, &current.ContentType, &current.Data); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			attachments = append(attachments, current)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		rows, err = f.sql.Query(f.ctx, "SELECT id,item_type,header,data FROM envelope_items WHERE project_id::text=$1 AND event_id=$2 ORDER BY id", projectID, eventID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		items := []opaque{}
		for rows.Next() {
			var current opaque
			if err := rows.Scan(&current.ID, &current.Type, &current.Header, &current.Data); err != nil {
				t.Fatal(err)
			}
			items = append(items, current)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return attachments, items
	}
	type telemetry struct {
		ID, ProjectID, TraceID, SpanID, Body, Name string
	}
	readTelemetry := func(projectID, traceID string) ([]telemetry, []telemetry) {
		t.Helper()
		var logs, spans struct{ Data []telemetry }
		query := "?project=" + projectID + "&trace=" + traceID
		decode(f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/logs"+query, "", nil, 200), &logs)
		decode(f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/spans"+query, "", nil, 200), &spans)
		return logs.Data, spans.Data
	}
	// Both orders matter: dependent items may precede their parent, while an
	// early return after the event would silently skip later independent items.
	for index, tc := range []struct {
		name, invalid       string
		order               []string
		status, logs, spans int
	}{
		{"independent items after parent", "", []string{"event", "attachment", "opaque", "empty", "log", "span"}, 200, 1, 1},
		{"parent after independent items", "", []string{"span", "attachment", "log", "opaque", "event", "empty"}, 200, 1, 1},
		{"invalid typed log retains its independent status", "log", []string{"event", "attachment", "opaque", "empty", "span", "log"}, 400, 0, 1},
		{"invalid span header retains its independent status", "span", []string{"event", "attachment", "opaque", "empty", "log", "span"}, 415, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventID := fmt.Sprintf("%032x", 100+index)
			traceID := fmt.Sprintf("%032x", 200+index)
			spanID := fmt.Sprintf("%016x", 300+index)
			title := fmt.Sprintf("Mixed parent %d", index)
			stamp := time.Now().Add(-time.Minute).Unix()
			parent := []byte(resolutionJSON(t, map[string]any{"event_id": eventID, "message": title, "fingerprint": []string{title}, "extra": map[string]any{"kept": []any{1, "raw parent", true}}}))
			binary := []byte{'a', 0, '\n', 0xff, '\r', '\n', 0xc3, 0xa9, 'z'}
			opaqueBytes := []byte{0, '\n', 'x', 0xfe, '\n'}
			logPayload := []byte(fmt.Sprintf(`{"items":[{"timestamp":%d,"trace_id":%q,"span_id":%q,"level":"info","body":"mixed independent log","attributes":{"attempt":{"type":"integer","value":3}}}]}`, stamp, traceID, spanID))
			if tc.invalid == "log" {
				logPayload = bytes.Replace(logPayload, []byte(`"value":3`), []byte(`"value":"three"`), 1)
			}
			spanPayload := []byte(fmt.Sprintf(`{"version":2,"items":[{"trace_id":%q,"span_id":%q,"name":"mixed independent span","status":"ok","is_segment":true,"start_timestamp":%d,"end_timestamp":%d}]}`, traceID, spanID, stamp, stamp+1))
			spanContentType := "application/vnd.sentry.items.span.v2+json"
			if tc.invalid == "span" {
				spanContentType = "application/x-unrecognized-span"
			}
			opaqueHeader := map[string]any{"type": "future_payload", "length": len(opaqueBytes), "vendor": map[string]any{"enabled": true, "revision": 2}, "encoding_hint": "binary"}
			parts := map[string]item{
				"event":      {map[string]any{"type": "event"}, parent},
				"attachment": {map[string]any{"type": "attachment", "filename": `..\logs\capture-é.bin`}, binary},
				"empty":      {map[string]any{"type": "attachment", "filename": "/", "content_type": "application/x-empty"}, []byte{}},
				"opaque":     {opaqueHeader, opaqueBytes},
				"log":        {map[string]any{"type": "log", "content_type": "application/vnd.sentry.items.log+json", "item_count": 1}, logPayload},
				"span":       {map[string]any{"type": "span", "content_type": spanContentType, "item_count": 1}, spanPayload},
			}
			ordered := make([]item, 0, len(tc.order))
			for _, kind := range tc.order {
				ordered = append(ordered, parts[kind])
			}
			wire := envelope(eventID, ordered)
			// An authentic key for another path must not admit any part of this
			// envelope. The correct path then processes the exact same bytes.
			f.request(t, http.DefaultClient, "POST", strings.Replace(endpoint, "/api/"+projectID+"/", "/api/"+otherProject+"/", 1), string(wire), nil, 403)
			var beforeAttachments []attachment
			var beforeOpaque []opaque
			var beforeLogs, beforeSpans []telemetry
			attempts := 1
			if tc.status == 200 {
				attempts = 2
			}
			for attempt := 0; attempt < attempts; attempt++ {
				raw := f.request(t, http.DefaultClient, "POST", endpoint, string(wire), nil, tc.status)
				if tc.status == 200 {
					var ack struct {
						ID               string
						UnsupportedItems []string
					}
					decode(raw, &ack)
					if ack.ID != eventID || !reflect.DeepEqual(ack.UnsupportedItems, []string{"future_payload"}) {
						t.Fatalf("mixed acknowledgment=%+v", ack)
					}
				}
				var issues struct{ Data []struct{ ID string } }
				decode(f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/issues?project="+projectID+"&query="+url.QueryEscape(title), "", nil, 200), &issues)
				if len(issues.Data) != 1 {
					t.Fatalf("normalized parent missing after status%d: %+v", tc.status, issues)
				}
				var detail struct {
					Issue  struct{ Count int64 }
					Event  json.RawMessage
					Events []struct{ EventID string }
				}
				decode(f.request(t, f.client, "GET", "/api/v1/issues/"+issues.Data[0].ID, "", nil, 200), &detail)
				var gotParent, wantParent any
				decode(detail.Event, &gotParent)
				decode(parent, &wantParent)
				if detail.Issue.Count != 1 || len(detail.Events) != 1 || detail.Events[0].EventID != eventID || !reflect.DeepEqual(gotParent, wantParent) {
					t.Fatalf("retained parent/count differs: %+v raw=%s", detail, detail.Event)
				}
				attachments, items := children(eventID)
				if len(attachments) != 2 || attachments[0].ID == "" || attachments[0].Filename != "attachment" || attachments[0].ContentType != "application/x-empty" || len(attachments[0].Data) != 0 || attachments[1].ID == "" || attachments[1].Filename != "capture-é.bin" || attachments[1].ContentType != "application/octet-stream" || !bytes.Equal(attachments[1].Data, binary) {
					t.Fatalf("HTTP dependent attachment metadata/bytes=%+v", attachments)
				}
				if len(items) != 1 || items[0].ID == "" || items[0].Type != "future_payload" || !bytes.Equal(items[0].Data, opaqueBytes) {
					t.Fatalf("only unknown data should be opaque: %+v", items)
				}
				var gotHeader, wantHeader any
				decode(items[0].Header, &gotHeader)
				decode([]byte(resolutionJSON(t, opaqueHeader)), &wantHeader)
				if !reflect.DeepEqual(gotHeader, wantHeader) {
					t.Fatalf("unknown item header changed: got=%v want=%v", gotHeader, wantHeader)
				}
				logs, spans := readTelemetry(projectID, traceID)
				if len(logs) != tc.logs || len(spans) != tc.spans {
					t.Fatalf("independent telemetry after status%d: logs=%+v spans=%+v", tc.status, logs, spans)
				}
				for _, log := range logs {
					if log.ID == "" || log.ProjectID != projectID || log.TraceID != traceID || log.SpanID != spanID || log.Body != "mixed independent log" {
						t.Fatalf("independent log fields=%+v", log)
					}
				}
				for _, span := range spans {
					if span.ID == "" || span.ProjectID != projectID || span.TraceID != traceID || span.SpanID != spanID || span.Name != "mixed independent span" {
						t.Fatalf("independent span fields=%+v", span)
					}
				}
				if attempt == 0 {
					beforeAttachments, beforeOpaque, beforeLogs, beforeSpans = attachments, items, logs, spans
				} else if !reflect.DeepEqual(beforeAttachments, attachments) || !reflect.DeepEqual(beforeOpaque, items) || !reflect.DeepEqual(beforeLogs, logs) || !reflect.DeepEqual(beforeSpans, spans) {
					t.Fatal("retry changed dependent IDs/bytes or duplicated independent telemetry")
				}
			}
			if logs, spans := readTelemetry(otherProject, traceID); len(logs) != 0 || len(spans) != 0 {
				t.Fatalf("mixed envelope escaped its project: logs=%+v spans=%+v", logs, spans)
			}
		})
	}
}
