//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/otlp"
	"goodsentry/internal/store"
)

// HTTP owns session admission, route validation and the public selected DTO.
// Store tests own SQL containment and the complete typed serialization matrix.
func TestLogDetailHTTPRequiresSessionAndCanonicalInput(t *testing.T) {
	f := newLogDetailHTTPFixture(t)
	id := logDetailSDKRow(t, f, f.project.ID, "native session control")
	path := "/api/v1/organizations/" + f.org.Slug + "/logs/" + id
	issued := logDetailHTTPObject(t, f.request(t, f.client, "POST", "/api/v1/organizations/"+f.org.Slug+"/api-tokens", `{"name":"Native detail separation","scopes":["org:read","event:read","project:read"],"projectIds":[],"expiresInDays":1}`, http.Header{"X-Csrf-Token": {f.csrf}}, http.StatusCreated))
	var bearer string
	if err := json.Unmarshal(issued["token"], &bearer); err != nil || bearer == "" {
		t.Fatalf("active Bearer setup: %v", err)
	}
	bearerHeaders := http.Header{"Authorization": {"Bearer " + bearer}}
	// Prove this credential is active before rejecting it at the session route.
	f.request(t, &http.Client{}, "GET", "/api/0/organizations/"+f.org.Slug+"/projects/", "", bearerHeaders, http.StatusOK)
	for _, tc := range []struct {
		name    string
		headers http.Header
	}{
		{"anonymous", nil},
		{"bearer is not a session", bearerHeaders},
		{"SDK key is not a session", http.Header{"X-Goodsentry-Key": {f.project.PublicKey}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.request(t, &http.Client{}, "GET", path, "", tc.headers, http.StatusUnauthorized)
		})
	}
	// A real login cookie is the positive control for every rejection below.
	f.request(t, f.client, "GET", path, "", nil, http.StatusOK)
	for _, id := range []string{"0", "01", "+1", "-1", "9223372036854775808", "not-a-number"} {
		t.Run("invalid id "+id, func(t *testing.T) {
			f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/logs/"+id, "", nil, http.StatusBadRequest)
		})
	}
	for _, query := range []string{"?page=1", "?unrecognized", "?bad=%ZZ"} {
		t.Run("query "+query, func(t *testing.T) {
			f.request(t, f.client, "GET", path+query, "", nil, http.StatusBadRequest)
		})
	}
	f.request(t, f.client, "GET", "/api/v1/organizations/"+f.org.Slug+"/logs/9223372036854775807", "", nil, http.StatusNotFound)
	// No originating list time filter is required to retrieve an old row.
	f.request(t, f.client, "GET", path, "", nil, http.StatusOK)
}

func TestLogDetailHTTPReturnsDirectSDKAndSelectedOTLPData(t *testing.T) {
	f := newLogDetailHTTPFixture(t)
	id := logDetailSDKRow(t, f, f.project.ID, "SDK direct DTO")
	path := "/api/v1/organizations/" + f.org.Slug + "/logs/"
	fields := logDetailHTTPObject(t, f.request(t, f.client, "GET", path+id, "", nil, http.StatusOK))
	if string(fields["id"]) != `"`+id+`"` || string(fields["source"]) != `"sentry"` || string(fields["body"]) != `"SDK direct DTO"` {
		t.Fatal("SDK detail must expose the row directly, including its source")
	}
	for _, name := range []string{"timestampSource", "receivedAt", "traceFlags"} {
		if _, exists := fields[name]; exists {
			t.Fatalf("SDK detail fabricated OTLP metadata %q", name)
		}
	}
	var stored json.RawMessage
	if err := f.sql.QueryRow(f.ctx, "SELECT data FROM logs WHERE id::text=$1", id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(logDetailHTTPValue(t, fields["data"]), logDetailHTTPValue(t, stored)) {
		t.Fatal("HTTP SDK data differs from the stored canonical JSON")
	}

	const input = `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"selected service"}}]},"schemaUrl":"resource://selected","scopeLogs":[{"scope":{"name":"selected scope","version":"1"},"schemaUrl":"scope://selected","logRecords":[{"timeUnixNano":"1791981000123456789","body":{"bytesValue":"AAE="},"attributes":[{"key":"exact","value":{"intValue":"9223372036854775807"}}]},{"body":{"stringValue":"sibling-record-must-not-leak"}}]}]},{"resource":{"attributes":[{"key":"secret","value":{"stringValue":"sibling-resource-must-not-leak"}}]},"scopeLogs":[{"scope":{"name":"sibling-scope-must-not-leak"},"logRecords":[{"body":{"stringValue":"third record"}}]}]}]}`
	received := time.Date(2026, 10, 15, 10, 11, 12, 123456000, time.UTC)
	batch, err := otlp.Prepare(strings.NewReader(input), otlp.JSON, "", otlp.Limits{MaxBytes: 1 << 20, MaxRecords: 1000, MaxNodes: 100000}, received)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := f.db.IngestOTLP(f.ctx, f.project.ID, batch); err != nil || count != 3 {
		t.Fatalf("OTLP setup: count=%d err=%v", count, err)
	}
	if err := f.sql.QueryRow(f.ctx, "SELECT id::text FROM logs WHERE project_id::text=$1 AND otlp_record_index=0", f.project.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	raw := f.request(t, f.client, "GET", path+id, "", nil, http.StatusOK)
	fields = logDetailHTTPObject(t, raw)
	if string(fields["id"]) != `"`+id+`"` || string(fields["source"]) != `"otlp"` || string(fields["timestampSource"]) != `"event"` || string(fields["traceFlags"]) != "0" {
		t.Fatal("OTLP detail lost direct identity/source or required metadata, including zero flags")
	}
	var wireReceived time.Time
	if err := json.Unmarshal(fields["receivedAt"], &wireReceived); err != nil || !wireReceived.Equal(received) {
		t.Fatalf("receiver timestamp: %v", err)
	}
	if _, exists := fields["attributes"]; exists {
		t.Fatal("OTLP detail fabricated legacy flattened attributes")
	}
	data := logDetailHTTPObject(t, fields["data"])
	if len(data) != 5 || string(data["resourceSchemaUrl"]) != `"resource://selected"` || string(data["scopeSchemaUrl"]) != `"scope://selected"` {
		t.Fatal("OTLP data must contain only the selected resource/scope/record and their schema URLs")
	}
	for _, name := range []string{"resource", "scope", "logRecord"} {
		if len(data[name]) == 0 || string(data[name]) == "null" {
			t.Fatalf("selected %s was omitted", name)
		}
	}
	record := logDetailHTTPObject(t, data["logRecord"])
	if string(record["timeUnixNano"]) != `"1791981000123456789"` || !bytes.Contains(record["attributes"], []byte(`"9223372036854775807"`)) || string(logDetailHTTPObject(t, record["body"])["bytesValue"]) != `"AAE="` {
		t.Fatal("HTTP encoding changed the selected record's integer digits, timestamp or bytes")
	}
	for _, sentinel := range []string{"sibling-record-must-not-leak", "sibling-resource-must-not-leak", "sibling-scope-must-not-leak"} {
		if bytes.Contains(raw, []byte(sentinel)) {
			t.Fatal("HTTP detail exposed an unselected record or context")
		}
	}

	largeInput := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"` + strings.Repeat("L", 2048) + `"}}]}]}]}`
	large, err := otlp.Prepare(strings.NewReader(largeInput), otlp.JSON, "", otlp.Limits{MaxBytes: 1 << 20, MaxRecords: 1000, MaxNodes: 100000}, received)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := f.db.IngestOTLP(f.ctx, f.project.ID, large); err != nil || count != 1 {
		t.Fatalf("historical capacity setup: count=%d err=%v", count, err)
	}
	if err := f.sql.QueryRow(f.ctx, "SELECT id::text FROM logs WHERE project_id::text=$1 AND body=$2", f.project.ID, strings.Repeat("L", 2048)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// The same authenticated row first succeeds, then maps a lower deployment
	// capacity to413 instead of returning truncated data or an internal error.
	f.request(t, f.client, "GET", path+id, "", nil, http.StatusOK)
	logDetailHTTPServer(t, f, true, 1024)
	limited := logDetailHTTPObject(t, f.request(t, f.client, "GET", path+id, "", nil, http.StatusRequestEntityTooLarge))
	if _, present := limited["data"]; present {
		t.Fatal("capacity rejection returned a truncated full-data object")
	}
}

func TestLogDetailHTTPUsesCurrentMembershipAndFeatureFlag(t *testing.T) {
	f := newLogDetailHTTPFixture(t)
	id := logDetailSDKRow(t, f, f.project.ID, "owned detail")
	var foreign store.Organization
	if err := f.sql.QueryRow(f.ctx, "INSERT INTO organizations(slug,name) VALUES('foreign-log-detail','Foreign logs') RETURNING id::text,slug,name").Scan(&foreign.ID, &foreign.Slug, &foreign.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreign.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	project, err := f.db.CreateProject(f.ctx, f.owner.ID, foreign.Slug, "Foreign log project", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, "UPDATE memberships SET role='member' WHERE organization_id=$1 AND user_id=$2", foreign.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignID := logDetailSDKRow(t, f, project.ID, "foreign detail")
	ownedPath := "/api/v1/organizations/" + f.org.Slug + "/logs/"
	foreignPath := "/api/v1/organizations/" + foreign.Slug + "/logs/"
	f.request(t, f.client, "GET", ownedPath+id, "", nil, http.StatusOK)
	f.request(t, f.client, "GET", foreignPath+foreignID, "", nil, http.StatusOK)
	// Membership of both organizations must not let a row cross URL scope.
	f.request(t, f.client, "GET", ownedPath+foreignID, "", nil, http.StatusNotFound)
	f.request(t, f.client, "GET", foreignPath+id, "", nil, http.StatusNotFound)
	if _, err := f.sql.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "GET", ownedPath+id, "", nil, http.StatusNotFound)
	f.request(t, f.client, "GET", foreignPath+foreignID, "", nil, http.StatusOK)
	logDetailHTTPServer(t, f, false, 1<<20)
	f.request(t, f.client, "GET", foreignPath+foreignID, "", nil, http.StatusNotFound)
}

func newLogDetailHTTPFixture(t *testing.T) *resolutionHTTPFixture {
	t.Helper()
	f := newResolutionHTTPFixture(t)
	logDetailHTTPServer(t, f, true, 1<<20)
	return f
}

func logDetailHTTPServer(t *testing.T, f *resolutionHTTPFixture, logs bool, maxBytes int64) {
	t.Helper()
	f.server.Close()
	f.server = httptest.NewServer(New(config.Config{Domain: "https://public.example.test/good", Prefix: "/good", Secret: strings.Repeat("r", 64), Logs: logs, MaxBodyBytes: maxBytes, OTLPMaxRecords: 1000, OTLPMaxNodes: 100000}, f.db))
	t.Cleanup(f.server.Close)
}

func logDetailSDKRow(t *testing.T, f *resolutionHTTPFixture, projectID, body string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"items": []any{map[string]any{"timestamp": 1791981000.25, "level": "info", "body": body, "attributes": map[string]any{"attempt": map[string]any{"type": "integer", "value": int64(9223372036854775807)}}, "extra": map[string]bool{"retained": true}}}})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := f.db.IngestLogs(f.ctx, projectID, payload); err != nil || count != 1 {
		t.Fatalf("SDK log setup: count=%d err=%v", count, err)
	}
	var id string
	if err := f.sql.QueryRow(f.ctx, "SELECT id::text FROM logs WHERE project_id::text=$1 AND body=$2", projectID, body).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func logDetailHTTPObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		t.Fatalf("expected a JSON object: %v", err)
	}
	return fields
}

func logDetailHTTPValue(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
