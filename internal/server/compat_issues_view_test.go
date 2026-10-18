package server

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/eventview"
	"goodsentry/internal/store"
)

// Authoring gate: this pure boundary owns public issue JSON types and fields,
// sourced from real provider records. Numeric counts, a policy-derived fake
// release, historical-match metadata, or native mutation would break consumers;
// SQL tests cannot observe serialization. It uses no new productive seam.
func TestCompatibleIssueWireProjection(t *testing.T) {
	stamp := time.Date(2026, 10, 15, 8, 0, 0, 0, time.FixedZone("fixture", -6*3600))
	assigned := &store.IssueActor{Type: "team", ID: "7", Name: "Platform", Slug: "platform"}
	raw := json.RawMessage(`{"exception":{"values":[{"type":"CheckoutError","value":"actual latest"}]},"logger":"checkout.service","culprit":"actual file"}`)
	before := bytes.Clone(raw)
	latest := &eventview.Record{ID: strings.Repeat("a", 32), Environment: "production", Kind: "error", Data: raw}
	release := &eventview.ReleaseReference{ID: "88", Version: "app@2", DateCreated: stamp, DateReleased: nil}
	record := store.CompatibleIssueRecord{Issue: store.Issue{ID: "42", Count: 9007199254740993, ShortID: "CHECKOUT-42", Title: strings.Repeat("é", 300), Level: "error", Status: "resolved", FirstSeen: stamp, LastSeen: stamp.Add(time.Hour), Project: store.Project{ID: "5", Slug: "checkout", Name: strings.Repeat("p", 70)}, AssignedTo: assigned, Resolution: &store.IssueResolution{Mode: "next_release", Release: &store.IssueResolutionRelease{Version: "app@2", DateCreated: stamp}}}, OrganizationSlug: "team space", ProjectPlatform: strings.Repeat("界", 70), NumComments: 17, UserCount: 3, IsUnhandled: true, Latest: latest, Stats: map[string][][2]int64{"24h": {{1, 2}}}, FirstRelease: release, LastRelease: release}
	view, err := projectCompatibleIssue(record, "https://good.example.test/prefix", true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["count"] != "9007199254740993" || wire["userCount"] != float64(3) || wire["numComments"] != float64(17) || wire["isUnhandled"] != true || wire["title"] != strings.Repeat("é", 255) || wire["level"] != "error" {
		t.Fatalf("known root summaries lost: %s", encoded)
	}
	if wire["type"] != "error" || wire["metadata"].(map[string]any)["value"] != "actual latest" || wire["logger"] != "checkout.service" || wire["culprit"] != "actual file" {
		t.Fatalf("latest event metadata/logger/culprit lost: %s", encoded)
	}
	project := wire["project"].(map[string]any)
	if project["id"] != "5" || project["slug"] != "checkout" || project["name"] != strings.Repeat("p", 64) || project["platform"] != strings.Repeat("界", 64) || wire["permalink"] != "https://good.example.test/prefix/?issue=42&org=team+space" {
		t.Fatalf("public project reference/link lost: %s", encoded)
	}
	if wire["assignedTo"].(map[string]any)["slug"] != "platform" || wire["userReportCount"] != float64(0) || wire["statusDetails"].(map[string]any)["inRelease"] != "app@2" {
		t.Fatalf("actual actor/selected boundary/detail shape lost: %s", encoded)
	}
	first := wire["firstRelease"].(map[string]any)
	if first["shortVersion"] != "app@2" || first["version"] != "app@2" || first["dateReleased"] != nil || first["dateCreated"] != stamp.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("actual release reference lost: %s", encoded)
	}
	if _, exists := first["id"]; exists {
		t.Fatalf("issue release exposed internal metadata ID: %s", encoded)
	}
	if wire["lifetime"].(map[string]any)["count"] != "9007199254740993" || wire["matchingEventId"] != latest.ID || wire["matchingEventEnvironment"] != "production" {
		t.Fatalf("lifetime/current representative linkage lost: %s", encoded)
	}
	if !bytes.Equal(raw, before) || record.Issue.Title != strings.Repeat("é", 300) || record.Issue.Project.Name != strings.Repeat("p", 70) || record.ProjectPlatform != strings.Repeat("界", 70) {
		t.Fatal("wire projection mutated its source record")
	}
	for _, field := range []string{"isPublic", "isBookmarked", "isSubscribed", "hasSeen", "priority"} {
		if _, exists := wire[field]; exists {
			t.Fatalf("unsupported state fabricated: %s", field)
		}
	}
	record.Filtered = &store.CompatibleIssueMetrics{Count: 0, Stats: map[string][][2]int64{}}
	view, err = projectCompatibleIssue(record, "https://good.example.test/prefix/", false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(view)
	wire = nil
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	filtered := wire["filtered"].(map[string]any)
	if filtered["count"] != "0" || filtered["firstSeen"] != nil || filtered["lastSeen"] != nil || wire["matchingEventId"] != nil || wire["matchingEventEnvironment"] != nil || wire["type"] != "error" {
		t.Fatalf("empty matching subset replaced lifetime representative: %s", encoded)
	}
	if bytes.Contains(encoded, []byte(`"userReportCount"`)) {
		t.Fatalf("list gained detail-only feedback field: %s", encoded)
	}
}

// Category is durable even after receipt expiry; latest-payload inference and
// a missing JSON field would mislabel an empty feedback history. Provider SQL
// cannot verify this public wire type, and no projection seam is required.
func TestCompatibleFeedbackIssueCategoryWithEmptyHistory(t *testing.T) {
	for _, retained := range []bool{false, true} {
		record := store.CompatibleIssueRecord{Issue: store.Issue{ID: "42", Category: "feedback", Title: "Actual feedback title", Count: 0}}
		if retained {
			record.Issue.Count = 1
			record.Latest = &eventview.Record{ID: strings.Repeat("a", 32), Kind: "feedback", Title: record.Issue.Title, Data: json.RawMessage(`{"contexts":{"feedback":{"message":"Actual feedback message"}}}`)}
		}
		view, err := projectCompatibleIssue(record, "https://good.example.test", true)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil || wire["issueCategory"] != "feedback" || wire["type"] != "feedback" || wire["userReportCount"] != float64(0) {
			t.Fatal("feedback wire category/type lost with retained=", retained, string(raw), err)
		}
	}
}

func TestCompatibleIssueWireImplementedPoliciesAndEmptyHistory(t *testing.T) {
	until := time.Date(2026, 10, 15, 8, 0, 0, 0, time.FixedZone("fixture", -6*3600))
	configured, remaining := int32(30), int64(7)
	for _, tc := range []struct {
		name  string
		issue store.Issue
		want  string
	}{
		{"indefinite", store.Issue{Status: "ignored"}, `{}`},
		{"configured count and until", store.Issue{Status: "ignored", Ignore: &store.IssueIgnore{Count: &configured, Remaining: &remaining, Until: &until}}, `{"ignoreCount":30,"ignoreUntil":"2026-10-15T14:00:00Z"}`},
		{"pending next", store.Issue{Status: "resolved", Resolution: &store.IssueResolution{Mode: "next_release", Baseline: &store.IssueResolutionRelease{Version: "app@1"}}}, `{"inNextRelease":true}`},
		{"selected boundary", store.Issue{Status: "resolved", Resolution: &store.IssueResolution{Mode: "release", Release: &store.IssueResolutionRelease{Version: "app@2"}}}, `{"inRelease":"app@2"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := projectCompatibleIssue(store.CompatibleIssueRecord{Issue: tc.issue}, "https://good.test", false)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(view)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatal(err)
			}
			var got, want any
			_ = json.Unmarshal(fields["statusDetails"], &got)
			_ = json.Unmarshal([]byte(tc.want), &want)
			if !reflect.DeepEqual(got, want) || string(fields["type"]) != `"default"` || string(fields["metadata"]) != `{}` || string(fields["stats"]) != `{}` || string(fields["assignedTo"]) != `null` || string(fields["firstRelease"]) != `null` {
				t.Fatalf("policy or empty-history representation: %s", body)
			}
		})
	}
}
