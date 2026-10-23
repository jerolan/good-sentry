//go:build integration

package server

import (
	"encoding/json"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func TestCompatibleIssueTagsAggregation(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	issueID := ""
	for i, tags := range []any{
		map[string]any{"region": "eu", "service": "api", "ignored": 7, "empty": "", "build+channel": "stable"},
		[]any{[]string{"region", "us"}, []string{"region", "eu"}, []string{"tier", "web"}, []string{"bad"}},
	} {
		raw, _ := json.Marshal(map[string]any{
			"event_id": []string{"10101010101010101010101010101010", "20202020202020202020202020202020"}[i],
			"message":  "Tag aggregation", "fingerprint": []string{"tag-aggregation"}, "timestamp": time.Now().UTC(), "tags": tags,
		})
		normalized, err := event.Normalize(raw, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := f.db.Ingest(f.ctx, f.project.ID, normalized)
		if err != nil {
			t.Fatal(err)
		}
		issueID = id
	}
	type issueTag struct {
		Key          string `json:"key"`
		Name         string `json:"name"`
		UniqueValues int64  `json:"uniqueValues"`
		TotalValues  int64  `json:"totalValues"`
		TopValues    []struct {
			Key   string `json:"key"`
			Name  string `json:"name"`
			Value string `json:"value"`
			Count int64  `json:"count"`
		} `json:"topValues"`
	}
	decodeTags := func(path string) []issueTag {
		t.Helper()
		var tags []issueTag
		if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &tags); err != nil {
			t.Fatal(err)
		}
		return tags
	}
	for _, path := range []string{"/api/0/issues/" + issueID + "/tags/", "/api/0/organizations/" + f.org.Slug + "/issues/" + issueID + "/tags/"} {
		tags := decodeTags(path)
		if len(tags) != 5 {
			t.Fatalf("%s tags=%+v", path, tags)
		}
		if tags[0].Key != "build+channel" || tags[1].Key != "empty" || tags[2].Key != "region" || tags[2].UniqueValues != 2 || tags[2].TotalValues != 3 {
			t.Fatalf("%s tag order/counts=%+v", path, tags)
		}
		if tags[2].TopValues[0].Value != "eu" || tags[2].TopValues[0].Count != 2 {
			t.Fatalf("%s top values=%+v", path, tags[2].TopValues)
		}
		filtered := decodeTags(path + "?key=region")
		if len(filtered) != 1 || filtered[0].Key != "region" || filtered[0].TotalValues != 3 {
			t.Fatalf("%s exact key filter=%+v", path, filtered)
		}
		filtered = decodeTags(path + "?key=Region")
		if len(filtered) != 0 {
			t.Fatalf("%s case-sensitive key filter=%+v", path, filtered)
		}
		filtered = decodeTags(path + "?key=missing")
		if len(filtered) != 0 {
			t.Fatalf("%s unmatched key filter=%+v", path, filtered)
		}
		filtered = decodeTags(path + "?key=build%2Bchannel")
		if len(filtered) != 1 || filtered[0].Key != "build+channel" {
			t.Fatalf("%s decoded key filter=%+v", path, filtered)
		}
		filtered = decodeTags(path + "?key=")
		if len(filtered) != 0 {
			t.Fatalf("%s empty key filter=%+v", path, filtered)
		}
		f.request(t, f.client, "GET", path+"?query=region", "", nil, 400)
		f.request(t, f.client, "GET", path+"?key=region&key=service", "", nil, 400)
		f.request(t, f.client, "GET", path+"?key=%zz", "", nil, 400)
	}
}
