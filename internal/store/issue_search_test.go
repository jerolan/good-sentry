package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The parser is the grammar boundary: independently written AST expectations
// protect quoting, field selection and limits without involving SQL or HTTP.
func TestIssueSearchGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		want        IssueSearch
	}{
		{"empty", " \t\n", IssueSearch{}},
		{"free message", " TypeError: cannot read user.id ", IssueSearch{Text: "TypeError: cannot read user.id"}},
		{"free quotes preserve literal syntax", `"http://example.test/a?q=x:y" "a  b"`, IssueSearch{Text: "http://example.test/a?q=x:y a  b"}},
		{"free words mixed with terms", `timeout customer:acme "while saving" release:"app@v1" failed`, IssueSearch{
			Text: "timeout while saving failed", Terms: []IssueSearchTerm{
				{Field: "tag", Key: "customer", Values: []IssueSearchValue{{Value: "acme"}}},
				{Field: "release", Values: []IssueSearchValue{{Value: "app@v1"}}},
			},
		}},
		{"reserved fields", `is:unresolved status:[resolved,ignored] environment:production release:v1 level:error env:custom`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "status", Values: []IssueSearchValue{{Value: "unresolved"}}},
			{Field: "status", Values: []IssueSearchValue{{Value: "resolved"}, {Value: "ignored"}}},
			{Field: "environment", Values: []IssueSearchValue{{Value: "production"}}},
			{Field: "release", Values: []IssueSearchValue{{Value: "v1"}}},
			{Field: "level", Values: []IssueSearchValue{{Value: "error"}}},
			{Field: "tag", Key: "env", Values: []IssueSearchValue{{Value: "custom"}}},
		}}},
		{"explicit tags preserve reserved and arbitrary keys", `tags[release]:"v one" tags["arbitrary:key"]:["A", B]`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "tag", Key: "release", Values: []IssueSearchValue{{Value: "v one"}}},
			{Field: "tag", Key: "arbitrary:key", Values: []IssueSearchValue{{Value: "A"}, {Value: "B"}}},
		}}},
		{"negation and per-value wildcard identity", `!browser:[Safari*,"*",Firefox] !status:ignored`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "tag", Key: "browser", Negated: true, Values: []IssueSearchValue{{Value: "Safari*", Wildcard: true}, {Value: "*"}, {Value: "Firefox"}}},
			{Field: "status", Negated: true, Values: []IssueSearchValue{{Value: "ignored"}}},
		}}},
		{"literal SQL metacharacters and bare commas", `path:100%_C:\tmp release:foo,bar`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "tag", Key: "path", Values: []IssueSearchValue{{Value: `100%_C:\tmp`}}},
			{Field: "release", Values: []IssueSearchValue{{Value: "foo,bar"}}},
		}}},
		{"JSON escapes and Unicode identity", `tags["键名\" x"]:"line\n\"value\"\\end"`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "tag", Key: "键名\" x", Values: []IssueSearchValue{{Value: "line\n\"value\"\\end"}}},
		}}},
		{"empty provided literals", `tag:"" environment:"" release:"" level:""`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "tag", Key: "tag", Values: []IssueSearchValue{{Value: ""}}},
			{Field: "environment", Values: []IssueSearchValue{{Value: ""}}},
			{Field: "release", Values: []IssueSearchValue{{Value: ""}}},
			{Field: "level", Values: []IssueSearchValue{{Value: ""}}},
		}}},
		{"no invented aliases or boolean operators", `release:latest foo AND bar OR baz`, IssueSearch{
			Text: "foo AND bar OR baz", Terms: []IssueSearchTerm{{Field: "release", Values: []IssueSearchValue{{Value: "latest"}}}},
		}},
		{"missing bare value remains ordinary message", `customer: Error: foo status:`, IssueSearch{Text: "customer: Error: foo status:"}},
		{"quoted list delimiters are literal", `tag:["a,b","[x]","a:b"]`, IssueSearch{Terms: []IssueSearchTerm{
			{Field: "tag", Key: "tag", Values: []IssueSearchValue{{Value: "a,b"}, {Value: "[x]"}, {Value: "a:b"}}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ParseIssueSearch(tc.query)
			if err != nil || !reflect.DeepEqual(actual, tc.want) {
				t.Fatalf("ParseIssueSearch(%q)=%+v err=%v want=%+v", tc.query, actual, err, tc.want)
			}
		})
	}
}

func TestIssueSearchMalformed(t *testing.T) {
	for _, query := range []string{
		"bad\x00query", string([]byte{0xff}),
		`"unclosed`, `"bad\q"`, `"text"suffix`,
		`!tag:`, `!tag: value`, `tags[key]:`, `tags[key]: value`,
		`:value`, `!is:`, `is:all`, `status:RESOLVED`, `status:*`, `status:""`,
		`tag:[]`, `tag:[a,]`, `tag:[,a]`, `tag:[a,,b]`, `tag:[a b]`, `tag:[a`, `tag:[a]suffix`,
		`tag:"unterminated`, `tag:"a"suffix`, `tag:a"b`, `tag:a[b]`,
		`tags[]:a`, `tags[""]:a`, `tags["key":a`, `tags[key]a`, `tags[key:a`,
		`tag:"\u0000"`, `tags["\u0000"]:value`,
		strings.Repeat("a", 4097), "tag:" + strings.Repeat("界", 1365),
		"tags[" + strings.Repeat("界", 129) + "]:value",
		strings.Repeat("tag:value ", 21),
		"tag:[" + strings.Repeat("value,", 20) + "value]",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := ParseIssueSearch(query)
			if !errors.Is(err, ErrInvalidIssueFilter) || err.Error() == ErrInvalidIssueFilter.Error() {
				t.Fatalf("ParseIssueSearch(%q) must return useful wrapped ErrInvalidIssueFilter, got %v", query, err)
			}
		})
	}
}

func TestIssueSearchExactBounds(t *testing.T) {
	for _, query := range []string{
		strings.Repeat("a", 4096),
		"tags[" + strings.Repeat("界", 128) + "]:value",
		strings.Repeat("tag:value ", 20),
		"tag:[" + strings.Repeat("value,", 19) + "value]",
	} {
		if _, err := ParseIssueSearch(query); err != nil {
			t.Fatalf("valid exact query boundary rejected: %v", err)
		}
	}
}
