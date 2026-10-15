package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidIssueFilter = errors.New("invalid issue filter")

type IssueSearch struct {
	Text  string
	Terms []IssueSearchTerm
}

type IssueSearchTerm struct {
	Field   string
	Key     string
	Negated bool
	Values  []IssueSearchValue
}

type IssueSearchValue struct {
	Value    string
	Wildcard bool
}

func ParseIssueSearch(query string) (IssueSearch, error) {
	if len(query) > 4096 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return IssueSearch{}, fmt.Errorf("%w: query must be valid UTF-8 without NUL and at most 4096 bytes", ErrInvalidIssueFilter)
	}
	p := issueSearchParser{query: query}
	var result IssueSearch
	var text []string
	for p.skipSpace(); p.pos < len(query); p.skipSpace() {
		if query[p.pos] == '"' {
			value, err := p.quoted()
			if err != nil {
				return IssueSearch{}, err
			}
			if !p.boundary() {
				return IssueSearch{}, p.invalid("expected whitespace after quoted text")
			}
			if value != "" {
				text = append(text, value)
			}
			continue
		}
		term, free, err := p.termOrText()
		if err != nil {
			return IssueSearch{}, err
		}
		if term == nil {
			text = append(text, free)
			continue
		}
		if len(result.Terms) == 20 {
			return IssueSearch{}, p.invalid("query supports at most 20 filter terms")
		}
		result.Terms = append(result.Terms, *term)
	}
	result.Text = strings.Join(text, " ")
	return result, nil
}

type issueSearchParser struct {
	query string
	pos   int
}

func (p *issueSearchParser) invalid(message string) error {
	return fmt.Errorf("%w: %s at byte %d", ErrInvalidIssueFilter, message, p.pos+1)
}

func (p *issueSearchParser) space() bool {
	if p.pos == len(p.query) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(p.query[p.pos:])
	return unicode.IsSpace(r)
}

func (p *issueSearchParser) advance() {
	_, size := utf8.DecodeRuneInString(p.query[p.pos:])
	p.pos += size
}

func (p *issueSearchParser) skipSpace() {
	for p.space() {
		p.advance()
	}
}

func (p *issueSearchParser) boundary() bool {
	return p.pos == len(p.query) || p.space()
}

func (p *issueSearchParser) termOrText() (*IssueSearchTerm, string, error) {
	start := p.pos
	negated := p.query[p.pos] == '!'
	if negated {
		p.pos++
	}
	explicit := strings.HasPrefix(p.query[p.pos:], "tags[")
	var key string
	if explicit {
		var err error
		key, err = p.tagKey()
		if err != nil {
			return nil, "", err
		}
		if p.pos == len(p.query) || p.query[p.pos] != ':' {
			return nil, "", p.invalid("expected ':' after explicit tag key")
		}
	} else {
		keyStart := p.pos
		for p.pos < len(p.query) && !p.space() && p.query[p.pos] != ':' {
			if p.query[p.pos] == '"' {
				return nil, "", p.invalid("quote a complete free-text token or filter value")
			}
			p.advance()
		}
		if p.pos == len(p.query) || p.query[p.pos] != ':' {
			return nil, p.query[start:p.pos], nil
		}
		key = p.query[keyStart:p.pos]
	}
	p.pos++ // The colon is present for both bare and explicit tag terms.
	if p.boundary() {
		if negated || explicit {
			return nil, "", p.invalid("filter requires a value")
		}
		return nil, p.query[start:p.pos], nil
	}
	if key == "" || utf8.RuneCountInString(key) > 128 || (!explicit && strings.ContainsAny(key, "[]")) {
		return nil, "", p.invalid("tag key must contain 1 to 128 characters; quote bracketed keys")
	}
	term := IssueSearchTerm{Field: "tag", Key: key, Negated: negated}
	if !explicit {
		switch key {
		case "is", "status":
			term.Field, term.Key = "status", ""
		case "environment", "release", "level":
			term.Field, term.Key = key, ""
		}
	}
	if p.query[p.pos] == '[' {
		values, err := p.list()
		if err != nil {
			return nil, "", err
		}
		term.Values = values
	} else {
		value, err := p.value(false)
		if err != nil {
			return nil, "", err
		}
		term.Values = []IssueSearchValue{value}
	}
	if !p.boundary() {
		return nil, "", p.invalid("expected whitespace after filter value")
	}
	if term.Field == "status" {
		for _, value := range term.Values {
			if value.Wildcard || (value.Value != "unresolved" && value.Value != "resolved" && value.Value != "ignored") {
				return nil, "", p.invalid("status must be unresolved, resolved or ignored")
			}
		}
	}
	return &term, "", nil
}

func (p *issueSearchParser) tagKey() (string, error) {
	p.pos += len("tags[")
	if p.pos == len(p.query) {
		return "", p.invalid("unterminated explicit tag key")
	}
	var key string
	if p.query[p.pos] == '"' {
		var err error
		key, err = p.quoted()
		if err != nil {
			return "", err
		}
	} else {
		start := p.pos
		for p.pos < len(p.query) && p.query[p.pos] != ']' {
			if p.space() || strings.ContainsRune("[\"", rune(p.query[p.pos])) {
				return "", p.invalid("quote tag keys containing whitespace or brackets")
			}
			p.advance()
		}
		key = p.query[start:p.pos]
	}
	if p.pos == len(p.query) || p.query[p.pos] != ']' {
		return "", p.invalid("unterminated explicit tag key")
	}
	p.pos++
	if key == "" || utf8.RuneCountInString(key) > 128 {
		return "", p.invalid("tag key must contain 1 to 128 characters")
	}
	return key, nil
}

func (p *issueSearchParser) quoted() (string, error) {
	start := p.pos
	p.pos++
	for p.pos < len(p.query) {
		switch p.query[p.pos] {
		case '\\':
			p.pos += 2
		case '"':
			p.pos++
			var value string
			if err := json.Unmarshal([]byte(p.query[start:p.pos]), &value); err != nil {
				return "", p.invalid("invalid JSON-quoted string")
			}
			if strings.ContainsRune(value, 0) {
				return "", p.invalid("quoted strings must not contain NUL")
			}
			return value, nil
		default:
			p.pos++
		}
	}
	return "", p.invalid("unterminated quoted string")
}

func (p *issueSearchParser) value(inList bool) (IssueSearchValue, error) {
	if p.pos == len(p.query) {
		return IssueSearchValue{}, p.invalid("filter requires a value")
	}
	if p.query[p.pos] == '"' {
		value, err := p.quoted()
		return IssueSearchValue{Value: value}, err
	}
	start := p.pos
	for p.pos < len(p.query) && !p.space() {
		char := p.query[p.pos]
		if inList && (char == ',' || char == ']') {
			break
		}
		if char == '[' || char == ']' || char == '"' {
			return IssueSearchValue{}, p.invalid("quote filter values containing brackets or quotes")
		}
		p.advance()
	}
	if p.pos == start {
		return IssueSearchValue{}, p.invalid("filter requires a value")
	}
	value := p.query[start:p.pos]
	return IssueSearchValue{Value: value, Wildcard: strings.Contains(value, "*")}, nil
}

func (p *issueSearchParser) list() ([]IssueSearchValue, error) {
	p.pos++
	var values []IssueSearchValue
	for {
		p.skipSpace()
		if len(values) == 20 {
			return nil, p.invalid("filter supports at most 20 values")
		}
		value, err := p.value(true)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		p.skipSpace()
		if p.pos == len(p.query) {
			return nil, p.invalid("unterminated filter list")
		}
		switch p.query[p.pos] {
		case ']':
			p.pos++
			return values, nil
		case ',':
			p.pos++
		default:
			return nil, p.invalid("expected ',' or ']' in filter list")
		}
	}
}
