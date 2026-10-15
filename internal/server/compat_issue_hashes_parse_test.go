package server

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// Authoring gate: these parsers own wire-only presence, scalar cardinality,
// UUID spelling and body framing. A cap applied before full=true, a malformed
// UUID accepted by SQL, or deduplication before the admission bound is a credible
// regression. Provider tests receive typed inputs and cannot cover those risks.
// No production seam or replay of the Store reset lifecycle is needed.
func TestCompatibleIssueHashListQuery(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		limit       int
		offset      int
		full        bool
	}{
		{"full by default", "", 10, 0, true},
		{"explicit uncapped limit retained in links", "limit=100", 10, 0, true},
		{"small full page", "limit=3&full=1", 3, 0, true},
		{"summary default", "full=false", 100, 0, false},
		{"summary explicit", "full=0&limit=100", 100, 0, false},
		{"forward cursor", "limit=2&full=true&cursor=0%3A4%3A0", 2, 4, true},
		{"backward boundary", "cursor=0%3A100000000%3A1", 10, 100000000, true},
		{"empty cursor follows existing contract", "cursor=", 10, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?"+tc.query, nil)
			w := httptest.NewRecorder()
			paging, ok := compatibleIssueHashListQuery(w, r)
			if !ok || paging.filter.Limit != tc.limit || paging.filter.Offset != tc.offset || paging.full != tc.full {
				t.Fatalf("paging=%+v ok=%t status=%d", paging, ok, w.Code)
			}
			want, err := url.ParseQuery(tc.query)
			if err != nil || !reflect.DeepEqual(paging.query, want) {
				t.Fatalf("link query=%v want=%v error=%v", paging.query, want, err)
			}
		})
	}
	for _, query := range []string{
		"limit=", "limit=0", "limit=01", "limit=-1", "limit=101", "limit=101&full=true", "limit=1&limit=1",
		"full=", "full=True", "full=yes", "full=2", "full=1&full=0",
		"cursor=0:01:0", "cursor=0:-1:0", "cursor=0:100000001:0", "cursor=1:0:0", "cursor=0:0:2", "cursor=0:0", "cursor=0:0:0&cursor=0:0:0",
		"per_page=1", "environment=prod", "id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "full=%zz", "full=%ff", "cursor=%00", "%ff=x", "full=true;limit=1",
	} {
		t.Run("invalid "+query, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, ok := compatibleIssueHashListQuery(w, httptest.NewRequest("GET", "/?"+query, nil))
			if ok || w.Code != 400 {
				t.Fatalf("query=%q ok=%t status=%d body=%s", query, ok, w.Code, w.Body.String())
			}
		})
	}
}

func TestCompatibleIssueHashResetQuery(t *testing.T) {
	const first = "0123456789abcdef0123456789abcdef"
	const second = "fedcba9876543210fedcba9876543210"
	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"one compact", "id=" + first, []string{first}},
		{"canonical and compact duplicate", "id=01234567-89AB-CDEF-0123-456789ABCDEF&id=" + first, []string{first}},
		{"distinct case forms", "id=FEDCBA98-7654-3210-FEDC-BA9876543210&id=0123456789ABCDEF0123456789ABCDEF", []string{second, first}},
		{"supplied upper bound before dedup", strings.TrimSuffix(strings.Repeat("id="+first+"&", 1000), "&"), []string{first}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			got, ok := compatibleIssueHashResetQuery(w, httptest.NewRequest("DELETE", "/?"+tc.query, nil))
			if !ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ids=%v want=%v ok=%t status=%d", got, tc.want, ok, w.Code)
			}
		})
	}
	for _, tc := range []struct{ name, query, body string }{
		{"missing", "", ""}, {"empty", "id=", ""},
		{"too short", "id=0123456789abcdef0123456789abcde", ""},
		{"too long", "id=0123456789abcdef0123456789abcdef0", ""},
		{"nonhex", "id=g123456789abcdef0123456789abcdef", ""},
		{"misplaced hyphens", "id=0123456-789ab-cdef-0123-456789abcdef", ""},
		{"braces", "id=%7B01234567-89ab-cdef-0123-456789abcdef%7D", ""},
		{"urn", "id=urn%3Auuid%3A01234567-89ab-cdef-0123-456789abcdef", ""},
		{"encoding", "id=%zz", ""}, {"utf8", "id=%ff", ""}, {"nul", "id=" + first + "%00", ""},
		{"unknown", "id=" + first + "&query=", ""}, {"pagination", "id=" + first + "&cursor=", ""},
		{"too many supplied", strings.TrimSuffix(strings.Repeat("id="+first+"&", 1001), "&"), ""},
		{"object body", "id=" + first, "{}"}, {"whitespace body", "id=" + first, " "},
		{"newline body", "id=" + first, "\n"}, {"binary body", "id=" + first, string([]byte{0})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, ok := compatibleIssueHashResetQuery(w, httptest.NewRequest("DELETE", "/?"+tc.query, strings.NewReader(tc.body)))
			if ok || w.Code != 400 {
				t.Fatalf("accepted reset query=%q body=%q ok=%t status=%d", tc.query, tc.body, ok, w.Code)
			}
		})
	}
}
