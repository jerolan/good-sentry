package server

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodsentry/internal/store"
)

// GlitchTip serializes this endpoint's created field with millisecond
// precision and a trailing Z.
const accountTokenTimeLayout = "2006-01-02T15:04:05.000Z"

type compatibleUserToken struct {
	ID      int64    `json:"id"`
	Label   string   `json:"label"`
	Scopes  []string `json:"scopes"`
	Token   string   `json:"token"`
	Created string   `json:"created"`
}

// compatibleAccount resolves the signed-in account these credential routes
// belong to. Any bearer credential answers Unauthorized, and mutations keep the
// same-origin CSRF requirement.
func (s *Server) compatibleAccount(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	return s.compatibleAccountWithCSRF(w, r, true)
}

// compatibleAccountWithCSRF resolves the signed-in account. The invitation
// acceptance route is token-authoritative and same-origin checked, so it skips
// the CSRF header while retaining the session/account resolution.
func (s *Server) compatibleAccountWithCSRF(w http.ResponseWriter, r *http.Request, requireCSRF bool) (store.User, bool) {
	if len(r.Header.Values("Authorization")) > 0 {
		failure(w, 401, "Unauthorized")
		return store.User{}, false
	}
	var user store.User
	var csrf string
	if s.cfg.AuthenticationDisabled() {
		if s.defaultUserID == "" {
			failure(w, 503, "Implicit owner is unavailable")
			return store.User{}, false
		}
		u, err := s.db.UserByID(r.Context(), s.defaultUserID)
		if err != nil {
			report(w, err)
			return store.User{}, false
		}
		user, csrf = u, s.openCSRF
	} else {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || len(cookie.Value) != 64 {
			failure(w, 401, "Unauthorized")
			return store.User{}, false
		}
		u, value, err := s.db.Session(r.Context(), s.hash(cookie.Value))
		if err != nil {
			failure(w, 401, "Unauthorized")
			return store.User{}, false
		}
		user, csrf = u, value
	}
	if requireCSRF && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		if !s.sameOrigin(r) || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) {
			failure(w, 403, "CSRF check Failed")
			return store.User{}, false
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{User: user, CSRF: csrf}))
	return user, true
}

func compatibleTokenView(t store.UserAPIToken, secret string) compatibleUserToken {
	return compatibleUserToken{
		ID:      t.ID,
		Label:   t.Label,
		Scopes:  store.UserTokenScopeNames(t.ScopesMask),
		Token:   secret,
		Created: t.CreatedAt.UTC().Format(accountTokenTimeLayout),
	}
}

func (s *Server) userAPITokens(w http.ResponseWriter, r *http.Request) {
	user, ok := s.compatibleAccount(w, r)
	if !ok {
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Cannot parse query string")
		return
	}
	limit := 100
	if query.Has("limit") {
		parsed, err := strconv.Atoi(strings.TrimSpace(query.Get("limit")))
		if err != nil {
			validationFailure(w, []any{accountQueryIssue("limit", "int_parsing", "Input should be a valid integer, unable to parse string as an integer")})
			return
		}
		limit = parsed
	}
	if limit < 0 {
		// A negative limit publishes an empty page whose next cursor points at
		// the newest entry.
		newest, _, err := s.db.UserAPITokens(r.Context(), user.ID, 1, nil)
		if err != nil {
			report(w, err)
			return
		}
		next := ""
		if len(newest) > 0 {
			next = encodeAccountTokenCursor(newest[0].CreatedAt, false)
		}
		s.setAccountTokenLinks(w, r, limit, "", next, false, next != "")
		writeJSON(w, 200, []compatibleUserToken{})
		return
	}
	if limit == 0 || limit > 100 {
		limit = 100
	}
	cursor, err := accountTokenCursor(query.Get("cursor"))
	if err != nil {
		validationFailure(w, []any{map[string]any{
			"type": "value_error", "loc": []any{"query", "cursor"},
			"msg": "Value error, Invalid cursor.", "ctx": map[string]any{"error": "Invalid cursor."},
		}})
		return
	}
	tokens, more, err := s.db.UserAPITokens(r.Context(), user.ID, limit, cursor)
	if err != nil {
		report(w, err)
		return
	}
	views := make([]compatibleUserToken, 0, len(tokens))
	for _, t := range tokens {
		secret, err := s.userTokens.open(t.Sealed)
		if err != nil {
			report(w, err)
			return
		}
		views = append(views, compatibleTokenView(t, hex.EncodeToString(secret)))
	}
	previous, next := "", ""
	reverse := cursor != nil && cursor.Reverse
	hasPrevious := (cursor != nil && !reverse) || (reverse && more)
	hasNext := (!reverse && more) || (reverse && cursor != nil)
	if len(tokens) > 0 {
		if hasPrevious {
			previous = encodeAccountTokenCursor(tokens[0].CreatedAt, true)
		}
		if hasNext {
			next = encodeAccountTokenCursor(tokens[len(tokens)-1].CreatedAt, false)
		}
	} else if cursor != nil {
		if hasPrevious {
			previous = encodeAccountTokenCursor(cursor.Boundary, true)
		}
		if hasNext {
			next = encodeAccountTokenCursor(cursor.Boundary, false)
		}
	}
	s.setAccountTokenLinks(w, r, limit, previous, next, hasPrevious, hasNext)
	writeJSON(w, 200, views)
}

func accountTokenCursor(raw string) (*store.UserAPITokenCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(raw)
		if err != nil {
			return nil, err
		}
	}
	values, err := url.ParseQuery(string(decoded))
	if err != nil {
		return nil, err
	}
	boundary, ok := parseCursorTime(values.Get("p"))
	if !ok {
		return nil, errors.New("invalid cursor")
	}
	return &store.UserAPITokenCursor{Boundary: boundary, Reverse: values.Get("r") == "1"}, nil
}

func parseCursorTime(value string) (time.Time, bool) {
	layouts := []string{"2006-01-02 15:04:05.000000-07:00", "2006-01-02 15:04:05.000000-0700", "2006-01-02 15:04:05.000000Z07:00", time.RFC3339Nano, "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func encodeAccountTokenCursor(boundary time.Time, reverse bool) string {
	values := url.Values{"p": {boundary.UTC().Format("2006-01-02 15:04:05.000000-07:00")}}
	if reverse {
		values.Set("r", "1")
	}
	return base64.StdEncoding.EncodeToString([]byte(values.Encode()))
}

// setAccountTokenLinks publishes cursor navigation. Both directions are
// present so clients can see whether a page exists without guessing.
func (s *Server) setAccountTokenLinks(w http.ResponseWriter, r *http.Request, limit int, previous, next string, hasPrevious, hasNext bool) {
	base := r.URL.Path
	if s.cfg.Domain != "" {
		base = strings.TrimSuffix(s.cfg.Domain, "/") + s.cfg.Prefix + r.URL.Path
	}
	link := func(cursor string, rel string, results bool) string {
		values := url.Values{}
		if limit != 100 {
			values.Set("limit", strconv.Itoa(limit))
		}
		if results && cursor != "" {
			values.Set("cursor", cursor)
		}
		target := base
		if len(values) > 0 {
			target += "?" + values.Encode()
		}
		link := fmt.Sprintf("<%s>; rel=\"%s\"; results=\"%t\"", target, rel, results)
		if results && cursor != "" {
			link += fmt.Sprintf("; cursor=\"%s\"", cursor)
		}
		return link
	}
	parts := []string{link(previous, "previous", hasPrevious), link(next, "next", hasNext)}
	w.Header().Set("Link", strings.Join(parts, ", "))
}

// accountBodyIssue mirrors the reference framework's structured 422 entries
// for a request body model. The intermediate payload segment is part of the
// published location.
func accountBodyIssue(field, kind, message string) map[string]any {
	loc := []any{"body", "payload"}
	if field != "" {
		loc = append(loc, field)
	}
	return map[string]any{"type": kind, "loc": loc, "msg": message}
}

func accountQueryIssue(field, kind, message string) map[string]any {
	return map[string]any{"type": kind, "loc": []any{"query", field}, "msg": message}
}

func accountPathIssue(field, kind, message string) map[string]any {
	return map[string]any{"type": kind, "loc": []any{"path", field}, "msg": message}
}

func validationFailure(w http.ResponseWriter, detail []any) {
	writeJSON(w, 422, map[string]any{"detail": detail})
}

type accountTokenInput struct {
	label string
	mask  int64
}

func decodeAccountTokenInput(w http.ResponseWriter, r *http.Request) (accountTokenInput, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		failure(w, 400, "Cannot parse request body")
		return accountTokenInput{}, false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		validationFailure(w, []any{accountBodyIssue("", "missing", "Field required")})
		return accountTokenInput{}, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		failure(w, 400, "Cannot parse request body")
		return accountTokenInput{}, false
	}
	result := accountTokenInput{}
	issues := []any{}
	raw, present := fields["scopes"]
	if !present {
		issues = append(issues, accountBodyIssue("scopes", "missing", "Field required"))
	} else if mask, issue := coerceScopeMask(raw); issue != nil {
		issues = append(issues, issue)
	} else {
		result.mask = mask
	}
	if labelRaw, ok := fields["label"]; ok {
		var label any
		if err := json.Unmarshal(labelRaw, &label); err != nil {
			failure(w, 400, "Cannot parse request body")
			return accountTokenInput{}, false
		} else if label == nil {
			result.label = ""
		} else if text, ok := label.(string); !ok {
			issues = append(issues, accountBodyIssue("label", "string_type", "Input should be a valid string"))
		} else if len([]rune(text)) > 255 {
			issues = append(issues, map[string]any{
				"type": "string_too_long", "loc": []any{"body", "payload", "label"},
				"msg": "String should have at most 255 characters", "ctx": map[string]any{"max_length": 255},
			})
		} else {
			result.label = text
		}
	}
	if len(issues) > 0 {
		validationFailure(w, issues)
		return accountTokenInput{}, false
	}
	return result, true
}

func coerceScopeMask(raw json.RawMessage) (int64, any) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, accountBodyIssue("scopes", "json_invalid", "JSON decode error")
	}
	switch typed := value.(type) {
	case nil:
		return 0, accountBodyIssue("scopes", "int_type", "Input should be a valid integer")
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case float64:
		if typed != float64(int64(typed)) {
			return 0, accountBodyIssue("scopes", "int_from_float", "Input should be a valid integer, got a number with a fractional part")
		}
		return int64(typed), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, accountBodyIssue("scopes", "int_parsing", "Input should be a valid integer, unable to parse string as an integer")
		}
		return parsed, nil
	case []any:
		if len(typed) == 0 {
			return 0, nil
		}
		names := make([]string, len(typed))
		for i, item := range typed {
			name, ok := item.(string)
			if !ok {
				return 0, map[string]any{"scopes": "Invalid scope"}
			}
			names[i] = name
		}
		mask, ok := store.UserTokenMaskChecked(names)
		if !ok {
			return 0, map[string]any{"scopes": "Invalid scope"}
		}
		return mask, nil
	default:
		return 0, accountBodyIssue("scopes", "int_type", "Input should be a valid integer")
	}
}

func (s *Server) createUserAPIToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.compatibleAccount(w, r)
	if !ok {
		return
	}
	input, ok := decodeAccountTokenInput(w, r)
	if !ok {
		return
	}
	secret, err := token()
	if err != nil {
		failure(w, 503, "Cannot create a credential; retry shortly")
		return
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		failure(w, 503, "Cannot create a credential; retry shortly")
		return
	}
	sealed, err := s.userTokens.seal(raw)
	if err != nil {
		failure(w, 503, "Cannot create a credential; retry shortly")
		return
	}
	created, err := s.db.CreateUserAPIToken(r.Context(), user.ID, user.AuthGeneration, input.label, input.mask&0xFFFF, s.hash(secret), sealed)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, compatibleTokenView(created, secret))
}

func (s *Server) deleteUserAPIToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.compatibleAccount(w, r)
	if !ok {
		return
	}
	value := r.PathValue("token_id")
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		validationFailure(w, []any{accountPathIssue("token_id", "int_parsing", "Input should be a valid integer, unable to parse string as an integer")})
		return
	}
	if err := s.db.DeleteUserAPIToken(r.Context(), user.ID, value); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 404, "Not Found")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
