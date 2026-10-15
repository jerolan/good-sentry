//go:build integration

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/otlp"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func TestSDKLogDetailPreservesNativeShape(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	const payload = `{"items":[{"timestamp":1791981000.25,"level":"info","body":"legacy detail","attributes":{"attempt":{"type":"integer","value":9223372036854775807}},"extra":{"retained":true}}]}`
	if n, err := db.IngestLogs(ctx, project.ID, json.RawMessage(payload)); err != nil || n != 1 {
		t.Fatalf("legacy SDK setup: count=%d err=%v", n, err)
	}
	rows, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("legacy SDK listing: count=%d err=%v", len(rows), err)
	}
	raw, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var listed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if _, exists := listed["source"]; exists || len(listed["attributes"]) == 0 {
		t.Fatalf("legacy listing JSON changed: %s", raw)
	}
	if !bytes.Contains(listed["attributes"], []byte("9223372036854775807")) {
		t.Fatal("legacy listing lost the stored integer digits")
	}
	detail, err := db.Log(ctx, user.ID, org.Slug, rows[0].ID, otlp.Limits{MaxBytes: 65536, MaxNodes: 10000, MaxRecords: 100})
	if err != nil {
		t.Fatalf("authorized SDK detail is missing after successful legacy ingestion/listing: %v", err)
	}
	if detail.Source != "sentry" || detail.ID != rows[0].ID || detail.Body != rows[0].Body {
		t.Fatalf("legacy detail identity/source: %+v", detail)
	}
	var stored json.RawMessage
	if err := db.pool.QueryRow(ctx, "SELECT data FROM logs WHERE id::text=$1", rows[0].ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(detail.Data) != string(stored) {
		t.Fatal("SDK detail must expose the actual canonical stored SDK data")
	}
	raw, err = json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]json.RawMessage
	if err := json.Unmarshal(raw, &public); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"timestampSource", "receivedAt", "traceFlags"} {
		if _, exists := public[key]; exists {
			t.Fatalf("SDK detail fabricated OTLP metadata %q", key)
		}
	}
}

func TestOTLPLogListingAndSelectedDurableDetail(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	limits := otlp.Limits{MaxBytes: 65536, MaxNodes: 10000, MaxRecords: 100}
	received := time.Date(2026, 10, 15, 18, 19, 20, 987654321, time.FixedZone("receiver", -6*3600))
	base := time.Date(2026, 10, 15, 0, 0, 0, 123456789, time.UTC)
	str := func(value string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
	}
	attribute := func(key, value string) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: key, Value: str(value)}
	}
	records := make([]*logspb.LogRecord, 26)
	for i := range records {
		records[i] = &logspb.LogRecord{TimeUnixNano: uint64(base.Add(time.Duration(i/2) * time.Second).UnixNano()), SeverityNumber: 9, Body: str(fmt.Sprintf("otlp read %02d", i))}
	}
	records[0].SeverityNumber = 0
	records[0].Body = str("otlp read 00\x00selected")
	records[0].TraceId = bytes.Repeat([]byte{0xab}, 16)
	records[0].Attributes = []*commonpb.KeyValue{{Key: "exact", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 9223372036854775807}}}}
	data := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{
		{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{attribute("service.name", "api\x00read"), attribute("deployment.environment.name", "production")}}, SchemaUrl: "resource://selected", ScopeLogs: []*logspb.ScopeLogs{{Scope: &commonpb.InstrumentationScope{Name: "scope\x00selected", Version: "2"}, SchemaUrl: "scope://selected", LogRecords: records}}},
		{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{attribute("service.name", "unselected-resource-sentinel"), attribute("deployment.environment.name", "staging")}}, SchemaUrl: "resource://unselected-sentinel", ScopeLogs: []*logspb.ScopeLogs{{Scope: &commonpb.InstrumentationScope{Name: "unselected-scope-sentinel"}, LogRecords: []*logspb.LogRecord{{TimeUnixNano: uint64(base.Add(13 * time.Second).UnixNano()), SeverityNumber: 9, Body: str("unselected-record-sentinel")}}}}},
	}}
	payload, err := proto.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := otlp.Prepare(bytes.NewReader(payload), otlp.Protobuf, "", limits, received)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := db.IngestOTLP(ctx, project.ID, batch); err != nil || count != 27 {
		t.Fatalf("durable OTLP setup: count=%d err=%v", count, err)
	}
	first, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{Project: project.ID, Page: 1})
	if err != nil || len(first) != 25 {
		t.Fatalf("first OTLP page: count=%d err=%v", len(first), err)
	}
	second, err := db.Logs(ctx, user.ID, org.Slug, LogFilter{Project: project.ID, Page: 2})
	if err != nil || len(second) != 2 {
		t.Fatalf("second OTLP page: count=%d err=%v", len(second), err)
	}
	if first[0].Body != "unselected-record-sentinel" || first[24].Body != "otlp read 02" || second[0].Body != "otlp read 01" || second[1].Body != `otlp read 00\u0000selected` {
		t.Fatal("OTLP records changed the established timestamp/ID ordering or 25-row pages")
	}
	all := append(first, second...)
	ids := make(map[string]bool)
	for i, row := range all {
		if ids[row.ID] {
			t.Fatal("a stored occurrence appeared on both log pages")
		}
		ids[row.ID] = true
		public := otlpReadJSON(t, row)
		if string(public["source"]) != `"otlp"` {
			t.Fatal("OTLP listing did not identify its actual source")
		}
		if _, exists := public["attributes"]; exists {
			t.Fatal("OTLP listing fabricated legacy attributes")
		}
		if i > 0 {
			prior := all[i-1]
			left, leftOK := new(big.Int).SetString(prior.ID, 10)
			right, rightOK := new(big.Int).SetString(row.ID, 10)
			if !leftOK || !rightOK || prior.Timestamp.Before(row.Timestamp) || (prior.Timestamp.Equal(row.Timestamp) && left.Cmp(right) <= 0) {
				t.Fatal("OTLP listing did not retain timestamp DESC, numeric ID DESC ordering")
			}
		}
	}
	window := &TimeWindow{Start: base.Truncate(time.Microsecond), End: base.Add(time.Second).Truncate(time.Microsecond)}
	filter := LogFilter{Project: project.ID, Query: "otlp read 00", Level: "unknown", Environment: "production", Service: `api\u0000read`, TraceID: strings.Repeat("AB", 16), CanonicalTrace: true, Window: window}
	filtered, err := db.Logs(ctx, user.ID, org.Slug, filter)
	if err != nil || len(filtered) != 1 || filtered[0].ID != second[1].ID {
		t.Fatalf("combined native filters did not select the persisted OTLP row: count=%d err=%v", len(filtered), err)
	}
	filter.Window = &TimeWindow{Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)}
	if rows, err := db.Logs(ctx, user.ID, org.Slug, filter); err != nil || len(rows) != 0 {
		t.Fatalf("time filter should exclude the stored row: count=%d err=%v", len(rows), err)
	}
	detail, err := db.Log(ctx, user.ID, org.Slug, filtered[0].ID, limits)
	if err != nil {
		t.Fatalf("selected detail inherited the current list/time filter: %v", err)
	}
	public := otlpReadJSON(t, detail)
	if detail.Source != "otlp" || detail.ID != filtered[0].ID || detail.TimestampSource != "event" || detail.ReceivedAt == nil || !detail.ReceivedAt.Equal(received.UTC().Truncate(time.Microsecond)) || detail.TraceFlags == nil || *detail.TraceFlags != 0 || string(public["traceFlags"]) != "0" {
		t.Fatal("durable OTLP detail lost source, receiver time or present zero trace flags")
	}
	var selected struct {
		Resource struct {
			Attributes []struct {
				Key   string `json:"key"`
				Value struct {
					String string `json:"stringValue"`
				} `json:"value"`
			} `json:"attributes"`
		} `json:"resource"`
		ResourceSchema string `json:"resourceSchemaUrl"`
		Scope          struct {
			Name string `json:"name"`
		} `json:"scope"`
		ScopeSchema string `json:"scopeSchemaUrl"`
		Record      struct {
			Time string `json:"timeUnixNano"`
			Body struct {
				String string `json:"stringValue"`
			} `json:"body"`
			Attributes []struct {
				Key   string `json:"key"`
				Value struct {
					Integer string `json:"intValue"`
				} `json:"value"`
			} `json:"attributes"`
		} `json:"logRecord"`
	}
	if err := json.Unmarshal(detail.Data, &selected); err != nil {
		t.Fatal(err)
	}
	if len(selected.Resource.Attributes) != 2 || selected.Resource.Attributes[0].Value.String != "api\x00read" || selected.ResourceSchema != "resource://selected" || selected.Scope.Name != "scope\x00selected" || selected.ScopeSchema != "scope://selected" || selected.Record.Body.String != "otlp read 00\x00selected" || selected.Record.Time != fmt.Sprint(base.UnixNano()) || len(selected.Record.Attributes) != 1 || selected.Record.Attributes[0].Value.Integer != "9223372036854775807" {
		t.Fatal("PostgreSQL selected detail lost its exact typed record or own contexts")
	}
	if bytes.Contains(detail.Data, []byte("unselected-")) || bytes.Contains(detail.Data, []byte("otlp read 01")) {
		t.Fatal("shared batch detail exposed another record or context")
	}
	reduced := limits
	reduced.MaxBytes = 8
	if _, err := db.Log(ctx, user.ID, org.Slug, filtered[0].ID, reduced); !errors.Is(err, otlp.ErrTooLarge) {
		t.Fatalf("historical detail ignored reduced current capacity: %v", err)
	}
}

func TestLogDetailRechecksMembershipAndProjectContainment(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	member := triageMember(t, ctx, db, org, "otlp-read-member", "member")
	foreign := triageForeignOrganization(t, ctx, db, "otlp-read-foreign")
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')", foreign.ID, owner.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := db.CreateProject(ctx, owner.ID, foreign.Slug, "Foreign log project", "go")
	if err != nil {
		t.Fatal(err)
	}
	limits := otlp.Limits{MaxBytes: 65536, MaxNodes: 10000, MaxRecords: 100}
	localID := otlpReadCapture(t, ctx, db, project.ID, org.Slug, owner.ID, "local batch", limits)
	foreignID := otlpReadCapture(t, ctx, db, foreignProject.ID, foreign.Slug, owner.ID, "foreign batch", limits)
	for _, allowed := range []struct{ slug, id string }{{org.Slug, localID}, {foreign.Slug, foreignID}} {
		if _, err := db.Log(ctx, member.ID, allowed.slug, allowed.id, limits); err != nil {
			t.Fatalf("member positive control in %s: %v", allowed.slug, err)
		}
	}
	for _, denied := range []struct{ slug, id string }{{org.Slug, foreignID}, {foreign.Slug, localID}, {org.Slug, "9223372036854775807"}} {
		if _, err := db.Log(ctx, member.ID, denied.slug, denied.id, limits); !errors.Is(err, ErrNotFound) {
			t.Fatalf("wrong organization/project containment exposed a log: %v", err)
		}
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Log(ctx, member.ID, org.Slug, localID, limits); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked membership retained access to a durable batch: %v", err)
	}
	if _, err := db.Log(ctx, member.ID, foreign.Slug, foreignID, limits); err != nil {
		t.Fatalf("independent current foreign membership was lost: %v", err)
	}
}

func otlpReadCapture(t *testing.T, ctx context.Context, db *Store, projectID, slug, userID, body string, limits otlp.Limits) string {
	t.Helper()
	raw := fmt.Sprintf(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":%q}}]}]}]}`, body)
	batch, err := otlp.Prepare(strings.NewReader(raw), otlp.JSON, "", limits, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if count, err := db.IngestOTLP(ctx, projectID, batch); err != nil || count != 1 {
		t.Fatalf("read-isolation setup: count=%d err=%v", count, err)
	}
	rows, err := db.Logs(ctx, userID, slug, LogFilter{Project: projectID, Query: body})
	if err != nil || len(rows) != 1 {
		t.Fatalf("read-isolation listing: count=%d err=%v", len(rows), err)
	}
	return rows[0].ID
}

func otlpReadJSON(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	return object
}
