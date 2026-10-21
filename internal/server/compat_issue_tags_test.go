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
		map[string]any{"region": "eu", "service": "api", "ignored": 7, "empty": ""},
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
	for _, path := range []string{"/api/0/issues/" + issueID + "/tags/", "/api/0/organizations/" + f.org.Slug + "/issues/" + issueID + "/tags/"} {
		var tags []struct {
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
		if err := json.Unmarshal(f.request(t, f.client, "GET", path, "", nil, 200), &tags); err != nil {
			t.Fatal(err)
		}
		if len(tags) != 4 {
			t.Fatalf("%s tags=%+v", path, tags)
		}
		if tags[0].Key != "empty" || tags[1].Key != "region" || tags[1].UniqueValues != 2 || tags[1].TotalValues != 3 {
			t.Fatalf("%s tag order/counts=%+v", path, tags)
		}
		if tags[1].TopValues[0].Value != "eu" || tags[1].TopValues[0].Count != 2 {
			t.Fatalf("%s top values=%+v", path, tags[1].TopValues)
		}
		f.request(t, f.client, "GET", path+"?query=region", "", nil, 400)
	}
}
