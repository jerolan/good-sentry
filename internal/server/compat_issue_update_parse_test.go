package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"goodsentry/internal/store"
)

// Authoring gate: parsing owns JSON presence and normalization into typed
// intent. Losing an explicit clear, accepting fractional bounds, or merging
// conflicting selectors would change requests before Store can validate them.
// These cases do not replay effective-status, identity, or transaction tests.
// The parser is called by the real handler and needs no test-only seam.
func TestCompatibleIssueChangeParsing(t *testing.T) {
	integer := func(value int32) *int32 { return &value }
	clear := &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueClearPolicy}
	clearResolution := &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueClearResolutionPolicy}
	next := &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueNextReleasePolicy}
	latest := &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueLatestReleasePolicy}
	assignment := func(value string) *store.CompatibleIssueAssignment {
		return &store.CompatibleIssueAssignment{Reference: value}
	}
	for _, tc := range []struct {
		name string
		body string
		want store.CompatibleIssueChange
	}{
		{"omitted", `{}`, store.CompatibleIssueChange{}},
		{"null status", `{"status":null}`, store.CompatibleIssueChange{}},
		{"null assignment", `{"assignedTo":null}`, store.CompatibleIssueChange{Assignment: assignment("")}},
		{"empty assignment", `{"assignedTo":""}`, store.CompatibleIssueChange{Assignment: assignment("")}},
		{"raw assignment reference", `{"assignedTo":" Owner@Example.test "}`, store.CompatibleIssueChange{Assignment: assignment(" Owner@Example.test ")}},
		{"null policy", `{"statusDetails":null}`, store.CompatibleIssueChange{Policy: clear}},
		{"empty policy", `{"statusDetails":{}}`, store.CompatibleIssueChange{Policy: clear}},
		{"null release selector", `{"statusDetails":{"inRelease":null}}`, store.CompatibleIssueChange{Policy: clear}},
		{"null next selector", `{"statusDetails":{"inNextRelease":null}}`, store.CompatibleIssueChange{Policy: clear}},
		{"both null selectors", `{"statusDetails":{"inRelease":null,"inNextRelease":null}}`, store.CompatibleIssueChange{Policy: clear}},
		{"empty release selector", `{"statusDetails":{"inRelease":""}}`, store.CompatibleIssueChange{Policy: clearResolution}},
		{"false next selector", `{"statusDetails":{"inNextRelease":false}}`, store.CompatibleIssueChange{Policy: clearResolution}},
		{"next selector", `{"statusDetails":{"inNextRelease":true}}`, store.CompatibleIssueChange{Policy: next}},
		{"next with empty release", `{"statusDetails":{"inNextRelease":true,"inRelease":""}}`, store.CompatibleIssueChange{Policy: next}},
		{"latest selector", `{"statusDetails":{"inRelease":"latest"}}`, store.CompatibleIssueChange{Policy: latest}},
		{"exact release", `{"statusDetails":{"inRelease":" aplicación@2.0 "}}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueReleasePolicy, Version: " aplicación@2.0 "}}},
		{"release with false next", `{"statusDetails":{"inRelease":"v2","inNextRelease":false}}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueReleasePolicy, Version: "v2"}}},
		{"case sensitive latest", `{"statusDetails":{"inRelease":"Latest"}}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueReleasePolicy, Version: "Latest"}}},
		{"top ignore", `{"ignoreDuration":3,"ignoreCount":4}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: integer(3), Count: integer(4)}}},
		{"nested ignore", `{"statusDetails":{"ignoreDuration":3,"ignoreCount":4}}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: integer(3), Count: integer(4)}}},
		{"equal copies", `{"ignoreCount":7,"statusDetails":{"ignoreCount":7,"ignoreDuration":8}}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: integer(8), Count: integer(7)}}},
		{"zero disables each bound", `{"ignoreCount":0,"statusDetails":{"ignoreCount":0,"ignoreDuration":0}}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy}}},
		{"one disabled bound", `{"ignoreDuration":0,"ignoreCount":2147483647}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Count: integer(2147483647)}}},
		{"null container with top ignore", `{"statusDetails":null,"ignoreDuration":2}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: integer(2)}}},
		{"empty container with top zero", `{"statusDetails":{},"ignoreCount":0}`, store.CompatibleIssueChange{Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields, err := issueObject([]byte(tc.body), "status", "assignedTo", "statusDetails", "ignoreDuration", "ignoreCount")
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseCompatibleIssueChange(fields)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("request=%s got=%+v want=%+v error=%v", tc.body, got, tc.want, err)
			}
		})
	}
	for _, status := range []string{"unresolved", "resolved", "ignored", "muted", "resolvedInNextRelease"} {
		t.Run("status "+status, func(t *testing.T) {
			raw, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseCompatibleIssueChange(map[string]json.RawMessage{"status": raw})
			if err != nil || got.Status != status || got.Assignment != nil || got.Policy != nil {
				t.Fatalf("status intent=%+v error=%v", got, err)
			}
		})
	}
}

func TestCompatibleIssueChangeRejectsAmbiguousInput(t *testing.T) {
	for _, body := range []string{
		`{"status":""}`, `{"status":"archived"}`, `{"status":false}`, `{"status":1}`,
		`{"assignedTo":1}`, `{"assignedTo":{}}`, `{"assignedTo":[]}`,
		`{"statusDetails":[]}`, `{"statusDetails":false}`, `{"statusDetails":""}`,
		`{"statusDetails":{"inRelease":1}}`, `{"statusDetails":{"inNextRelease":1}}`,
		`{"statusDetails":{"inRelease":"v2","inNextRelease":true}}`,
		`{"statusDetails":{"ignoreWindow":1}}`, `{"statusDetails":{"inCommit":{}}}`,
		`{"ignoreDuration":null}`, `{"ignoreDuration":-1}`, `{"ignoreDuration":2147483648}`,
		`{"ignoreCount":1.5}`, `{"ignoreCount":1e2}`, `{"ignoreCount":"1"}`, `{"ignoreCount":true}`,
		`{"statusDetails":{"ignoreCount":null}}`, `{"statusDetails":{"ignoreDuration":1.0}}`,
		`{"ignoreCount":1,"statusDetails":{"ignoreCount":2}}`,
		`{"ignoreDuration":0,"statusDetails":{"ignoreDuration":1}}`,
		`{"ignoreDuration":0,"statusDetails":{"inRelease":null}}`,
		`{"ignoreCount":0,"statusDetails":{"inNextRelease":false}}`,
		`{"statusDetails":{"ignoreCount":0,"inRelease":""}}`,
		`{"statusDetails":{"inNextRelease":null,"ignoreDuration":1}}`,
		`{"statusDetails":{"inNextRelease":true,"inNextRelease":false}}`,
		`{"statusDetails":{"inRelease":".."}}`, `{"statusDetails":{"inRelease":"v\u0000x"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			fields, err := issueObject([]byte(body), "status", "assignedTo", "statusDetails", "ignoreDuration", "ignoreCount")
			if err == nil {
				_, err = parseCompatibleIssueChange(fields)
			}
			if err == nil {
				t.Fatalf("accepted invalid issue change %s", body)
			}
		})
	}
}
