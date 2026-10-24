//go:build integration

package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/store"
)

type accountTokenView struct {
	ID      int64    `json:"id"`
	Label   string   `json:"label"`
	Scopes  []string `json:"scopes"`
	Token   string   `json:"token"`
	Created string   `json:"created"`
}

func accountTokenHTTP(t *testing.T, f *resolutionHTTPFixture, client *http.Client, method, path, body string, headers http.Header, want int) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+f.prefix()+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
	}
	return res, raw
}

func (f *resolutionHTTPFixture) prefix() string { return "/good" }

func accountTokenValidation(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var body struct {
		Detail []map[string]any `json:"detail"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("validation body=%s err=%v", raw, err)
	}
	return body.Detail
}

func accountTokenDetail(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("error body=%s err=%v", raw, err)
	}
	return body.Detail
}

func accountTokenLoc(t *testing.T, issue map[string]any) []string {
	t.Helper()
	raw, ok := issue["loc"].([]any)
	if !ok {
		t.Fatalf("issue lacks loc: %+v", issue)
	}
	loc := make([]string, len(raw))
	for i, part := range raw {
		loc[i] = fmt.Sprint(part)
	}
	return loc
}

func accountTokenLink(t *testing.T, header, relation string) (string, bool) {
	t.Helper()
	re := regexp.MustCompile(`rel="` + regexp.QuoteMeta(relation) + `"; results="(true|false)"(?:; cursor="([^"]+)")?`)
	match := re.FindStringSubmatch(header)
	if match == nil {
		t.Fatalf("Link lacks %s relation: %q", relation, header)
	}
	return match[2], match[1] == "true"
}

func createAccountToken(t *testing.T, f *resolutionHTTPFixture, body string) accountTokenView {
	t.Helper()
	_, raw := accountTokenHTTP(t, f, f.client, "POST", "/api/0/api-tokens/", body, http.Header{"X-CSRF-Token": {f.csrf}}, 201)
	var token accountTokenView
	if err := json.Unmarshal(raw, &token); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestCompatibleUserAPITokenHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/api-tokens/", "", nil, 401)
	accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/", "", http.Header{"Authorization": {"Bearer " + strings.Repeat("a", 64)}}, 401)
	accountTokenHTTP(t, f, f.client, "POST", "/api/0/api-tokens/", `{}`, nil, 403)
	accountTokenHTTP(t, f, f.client, "POST", "/api/0/api-tokens/", `{}`, http.Header{"Sec-Fetch-Site": {"cross-site"}, "X-CSRF-Token": {f.csrf}}, 403)

	validation := func(body string, status int) []map[string]any {
		t.Helper()
		_, raw := accountTokenHTTP(t, f, f.client, "POST", "/api/0/api-tokens/", body, http.Header{"X-CSRF-Token": {f.csrf}}, status)
		return accountTokenValidation(t, raw)
	}
	if detail := validation("", 422); len(detail) != 1 || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload") || detail[0]["type"] != "missing" {
		t.Fatalf("empty body validation=%+v", detail)
	}
	_, badJSON := accountTokenHTTP(t, f, f.client, "POST", "/api/0/api-tokens/", `{`, http.Header{"X-CSRF-Token": {f.csrf}}, 400)
	if detail := accountTokenDetail(t, badJSON); detail != "Cannot parse request body" {
		t.Fatalf("bad JSON detail=%q", detail)
	}
	if detail := validation(`{}`, 422); len(detail) != 1 || detail[0]["type"] != "missing" || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload", "scopes") {
		t.Fatalf("missing scopes=%+v", detail)
	}
	for body, kind := range map[string]string{
		`{"scopes":null}`:         "int_type",
		`{"scopes":1.5}`:          "int_from_float",
		`{"scopes":"nope"}`:       "int_parsing",
		`{"scopes":{"a":1}}`:      "int_type",
		`{"scopes":["bad:read"]}`: "",
	} {
		detail := validation(body, 422)
		if kind == "" {
			if len(detail) != 1 || detail[0]["scopes"] != "Invalid scope" {
				t.Fatalf("invalid scope %s=%+v", body, detail)
			}
		} else if len(detail) != 1 || detail[0]["type"] != kind || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload", "scopes") {
			t.Fatalf("validation %s=%+v", body, detail)
		}
	}
	for body, kind := range map[string]string{
		`{"scopes":["member:read"],"label":5}`: "string_type",
	} {
		detail := validation(body, 422)
		if len(detail) != 1 || detail[0]["type"] != kind || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload", "label") {
			t.Fatalf("label validation %s=%+v", body, detail)
		}
	}
	detail := validation(`{"scopes":["member:read"],"label":"`+strings.Repeat("x", 256)+`"}`, 422)
	if len(detail) != 1 || detail[0]["type"] != "string_too_long" || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload", "label") {
		t.Fatalf("long label=%+v", detail)
	}
	if ctx, ok := detail[0]["ctx"].(map[string]any); !ok || ctx["max_length"] != float64(255) {
		t.Fatalf("long label ctx=%+v", detail[0]["ctx"])
	}

	created := createAccountToken(t, f, `{"scopes":["member:read","project:read"],"label":"Probe","unknown":true}`)
	if created.ID <= 0 || created.Label != "Probe" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(created.Token) ||
		!reflectStrings(created.Scopes, "project:read", "member:read") || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`).MatchString(created.Created) {
		t.Fatalf("created=%+v", created)
	}
	_, listedRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/", "", nil, 200)
	var listed []accountTokenView
	if err := json.Unmarshal(listedRaw, &listed); err != nil || len(listed) == 0 || listed[0].ID != created.ID || listed[0].Token != created.Token {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	empty := createAccountToken(t, f, `{"scopes":[]}`)
	if empty.Scopes == nil || len(empty.Scopes) != 0 || empty.Label != "" {
		t.Fatalf("empty scopes=%+v", empty)
	}
	nullLabel := createAccountToken(t, f, `{"scopes":["member:read"],"label":null}`)
	if nullLabel.Label != "" {
		t.Fatalf("null label=%+v", nullLabel)
	}

	_, badPath := accountTokenHTTP(t, f, f.client, "DELETE", "/api/0/api-tokens/not-a-number/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 422)
	if detail := accountTokenValidation(t, badPath); len(detail) != 1 || !reflectStrings(accountTokenLoc(t, detail[0]), "path", "token_id") || detail[0]["type"] != "int_parsing" {
		t.Fatalf("bad path=%+v", detail)
	}
	_, missing := accountTokenHTTP(t, f, f.client, "DELETE", "/api/0/api-tokens/999999999/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 404)
	if detail := accountTokenDetail(t, missing); detail != "Not Found" {
		t.Fatalf("missing delete detail=%q", detail)
	}
	accountTokenHTTP(t, f, f.client, "DELETE", fmt.Sprintf("/api/0/api-tokens/%d/", created.ID), "", http.Header{"X-CSRF-Token": {f.csrf}}, 204)
	accountTokenHTTP(t, f, f.client, "DELETE", fmt.Sprintf("/api/0/api-tokens/%d/", created.ID), "", http.Header{"X-CSRF-Token": {f.csrf}}, 404)
}

func reflectStrings(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestCompatibleUserAPITokenBearerReads(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	reader := createAccountToken(t, f, `{"scopes":["org:read","project:read"]}`)
	headers := http.Header{"Authorization": {"Bearer " + reader.Token}}
	_, organizationsRaw := accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/organizations/", "", headers, 200)
	var organizations []store.CompatibleOrganization
	if err := json.Unmarshal(organizationsRaw, &organizations); err != nil || len(organizations) != 1 || organizations[0].ID != f.org.ID {
		t.Fatalf("organizations=%+v err=%v", organizations, err)
	}
	_, projectsRaw := accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/projects/", "", headers, 200)
	var projects []store.CompatibleProject
	if err := json.Unmarshal(projectsRaw, &projects); err != nil || len(projects) != 1 || projects[0].ID != f.project.ID {
		t.Fatalf("projects=%+v err=%v", projects, err)
	}
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/api-tokens/", "", headers, 401)
	noScopes := createAccountToken(t, f, `{"scopes":[]}`)
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/organizations/", "", http.Header{"Authorization": {"Bearer " + noScopes.Token}}, 403)
	orgOnly := createAccountToken(t, f, `{"scopes":["org:read"]}`)
	projectListing := http.Header{"Authorization": {"Bearer " + orgOnly.Token}}
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/projects/", "", projectListing, 200)
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/keys/", "", projectListing, 403)
	projectOnly := createAccountToken(t, f, `{"scopes":["project:read"]}`)
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/organizations/", "", http.Header{"Authorization": {"Bearer " + projectOnly.Token}}, 403)
	releaseWriter := createAccountToken(t, f, `{"scopes":["project:releases"]}`)
	accountTokenHTTP(t, f, http.DefaultClient, "POST", "/api/0/projects/"+f.org.Slug+"/"+f.project.Slug+"/releases/", `{"version":"account-token@1.0"}`, http.Header{"Authorization": {"Bearer " + releaseWriter.Token}}, 201)

	var secondUserID, secondOrgID string
	var secondGeneration int64
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,name,password_hash) VALUES('foreign-account-token@example.test','Foreign account token',$1) RETURNING id::text,auth_generation`, []byte("hash")).Scan(&secondUserID, &secondGeneration); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-account-token','Foreign account token') RETURNING id::text`).Scan(&secondOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, secondOrgID, secondUserID); err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("b", 64)
	hash := hmac.New(sha256.New, []byte(strings.Repeat("r", 64)))
	hash.Write([]byte(secret))
	if _, err := f.db.CreateUserAPIToken(f.ctx, secondUserID, secondGeneration, "Foreign reader", store.UserTokenMask([]string{"org:read", "project:read"}), hex.EncodeToString(hash.Sum(nil)), []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	foreignHeaders := http.Header{"Authorization": {"Bearer " + secret}}
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/organizations/"+f.org.Slug+"/", "", foreignHeaders, 404)
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/organizations/foreign-account-token/", "", foreignHeaders, 200)
	_, foreignRaw := accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/organizations/", "", foreignHeaders, 200)
	if err := json.Unmarshal(foreignRaw, &organizations); err != nil || len(organizations) != 1 || organizations[0].ID != secondOrgID {
		t.Fatalf("foreign organizations=%+v err=%v", organizations, err)
	}
}

func TestCompatibleUserAPITokenPaginationHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	created := make([]accountTokenView, 3)
	for i := range created {
		created[i] = createAccountToken(t, f, fmt.Sprintf(`{"scopes":["member:read"],"label":"page-%d"}`, i))
		if _, err := f.sql.Exec(f.ctx, "UPDATE user_api_tokens SET created_at=$2 WHERE id=$1", created[i].ID, base.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	res, firstRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=2", "", nil, 200)
	var first []accountTokenView
	if err := json.Unmarshal(firstRaw, &first); err != nil || len(first) != 2 || first[0].Label != "page-0" || first[1].Label != "page-1" {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	if _, ok := accountTokenLink(t, res.Header.Get("Link"), "previous"); ok {
		t.Fatalf("first page has previous link: %q", res.Header.Get("Link"))
	}
	nextCursor, ok := accountTokenLink(t, res.Header.Get("Link"), "next")
	if !ok || nextCursor == "" {
		t.Fatalf("first page next link: %q", res.Header.Get("Link"))
	}
	res, secondRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=2&cursor="+url.QueryEscape(nextCursor), "", nil, 200)
	var second []accountTokenView
	if err := json.Unmarshal(secondRaw, &second); err != nil || len(second) != 1 || second[0].Label != "page-2" {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	previousCursor, ok := accountTokenLink(t, res.Header.Get("Link"), "previous")
	if !ok || previousCursor == "" {
		t.Fatalf("second page previous link: %q", res.Header.Get("Link"))
	}
	if _, ok := accountTokenLink(t, res.Header.Get("Link"), "next"); ok {
		t.Fatalf("second page has next link: %q", res.Header.Get("Link"))
	}
	_, previousRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=2&cursor="+url.QueryEscape(previousCursor), "", nil, 200)
	var previous []accountTokenView
	if err := json.Unmarshal(previousRaw, &previous); err != nil || len(previous) != 2 || previous[0].Label != "page-0" || previous[1].Label != "page-1" {
		t.Fatalf("previous page=%+v err=%v", previous, err)
	}
	_, allRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=0", "", nil, 200)
	var all []accountTokenView
	if err := json.Unmarshal(allRaw, &all); err != nil || len(all) != 3 {
		t.Fatalf("limit zero=%d err=%v", len(all), err)
	}
	_, clampedRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=101", "", nil, 200)
	var clamped []accountTokenView
	if err := json.Unmarshal(clampedRaw, &clamped); err != nil || len(clamped) != 3 {
		t.Fatalf("clamped limit=%d err=%v", len(clamped), err)
	}
	res, emptyRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=-1", "", nil, 200)
	if string(bytes.TrimSpace(emptyRaw)) != "[]" {
		t.Fatalf("negative limit body=%s", emptyRaw)
	}
	if _, ok := accountTokenLink(t, res.Header.Get("Link"), "next"); !ok {
		t.Fatalf("negative limit next link: %q", res.Header.Get("Link"))
	}
	_, badLimit := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?limit=abc", "", nil, 422)
	if detail := accountTokenValidation(t, badLimit); len(detail) != 1 || detail[0]["type"] != "int_parsing" || !reflectStrings(accountTokenLoc(t, detail[0]), "query", "limit") {
		t.Fatalf("bad limit=%+v", detail)
	}
	_, badCursor := accountTokenHTTP(t, f, f.client, "GET", "/api/0/api-tokens/?cursor=nonsense", "", nil, 422)
	if detail := accountTokenValidation(t, badCursor); len(detail) != 1 || detail[0]["type"] != "value_error" || !reflectStrings(accountTokenLoc(t, detail[0]), "query", "cursor") {
		t.Fatalf("bad cursor=%+v", detail)
	}
}

func TestCompatibleRecoveryCodesHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/generate-recovery-codes/", "", nil, 401)
	_, codesRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/generate-recovery-codes/", "", nil, 200)
	var first struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(codesRaw, &first); err != nil || len(first.Codes) != 10 {
		t.Fatalf("codes=%+v err=%v", first, err)
	}
	seen := map[string]bool{}
	for _, code := range first.Codes {
		if len(code) != 8 || !regexp.MustCompile(`^[0-9]{8}$`).MatchString(code) || seen[code] {
			t.Fatalf("invalid recovery code set=%v", first.Codes)
		}
		seen[code] = true
	}
	accountTokenHTTP(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{"code":"`+first.Codes[0]+`"}`, nil, 403)
	if detail := accountTokenValidation(t, mustAccountTokenBody(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", "", http.Header{"X-CSRF-Token": {f.csrf}}, 422)); len(detail) != 1 || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload") {
		t.Fatalf("empty recovery body=%+v", detail)
	}
	if detail := accountTokenValidation(t, mustAccountTokenBody(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{}`, http.Header{"X-CSRF-Token": {f.csrf}}, 422)); len(detail) != 1 || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload", "code") {
		t.Fatalf("missing recovery code=%+v", detail)
	}
	if detail := accountTokenValidation(t, mustAccountTokenBody(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{"code":5}`, http.Header{"X-CSRF-Token": {f.csrf}}, 422)); len(detail) != 1 || detail[0]["type"] != "string_type" || !reflectStrings(accountTokenLoc(t, detail[0]), "body", "payload", "code") {
		t.Fatalf("non-string recovery code=%+v", detail)
	}
	wrong := "00000000"
	if first.Codes[0] == wrong {
		wrong = "11111111"
	}
	if detail := accountTokenDetail(t, mustAccountTokenBody(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{"code":"`+wrong+`"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 400)); detail != "Invalid code" {
		t.Fatalf("wrong code detail=%q", detail)
	}
	accountTokenHTTP(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{"code":"`+first.Codes[1]+`"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 204)
	if detail := accountTokenDetail(t, mustAccountTokenBody(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{"code":"`+first.Codes[2]+`"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 400)); detail != "No recovery codes set, use GET first" {
		t.Fatalf("consumed set detail=%q", detail)
	}
	_, secondRaw := accountTokenHTTP(t, f, f.client, "GET", "/api/0/generate-recovery-codes/", "", nil, 200)
	var second struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(secondRaw, &second); err != nil || len(second.Codes) != 10 {
		t.Fatalf("second codes=%+v err=%v", second, err)
	}
	accountTokenHTTP(t, f, f.client, "POST", "/api/0/generate-recovery-codes/", `{"code":"`+second.Codes[0]+`"}`, http.Header{"X-CSRF-Token": {f.csrf}}, 204)

	bearer := createAccountToken(t, f, `{"scopes":["member:read"]}`)
	bearerHeaders := http.Header{"Authorization": {"Bearer " + bearer.Token}}
	_, bearerCodesRaw := accountTokenHTTP(t, f, http.DefaultClient, "GET", "/api/0/generate-recovery-codes/", "", bearerHeaders, 200)
	var bearerCodes struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(bearerCodesRaw, &bearerCodes); err != nil || len(bearerCodes.Codes) != 10 {
		t.Fatalf("bearer codes=%+v err=%v", bearerCodes, err)
	}
	accountTokenHTTP(t, f, http.DefaultClient, "POST", "/api/0/generate-recovery-codes/", `{"code":"`+bearerCodes.Codes[0]+`"}`, bearerHeaders, 204)
}

func mustAccountTokenBody(t *testing.T, f *resolutionHTTPFixture, client *http.Client, method, path, body string, headers http.Header, want int) []byte {
	t.Helper()
	_, raw := accountTokenHTTP(t, f, client, method, path, body, headers, want)
	return raw
}

func TestOpenModeAccountTokens(t *testing.T) {
	f, srv, cfg := newOpenAuthenticationFixture(t)
	client := &http.Client{}
	request := func(method, path, body string, headers http.Header, want int) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, method, srv.URL+cfg.Prefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for key, values := range headers {
			req.Header[key] = values
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
		}
		return res, raw
	}
	_, meRaw := request("GET", "/api/v1/me", "", nil, 200)
	var me struct {
		User      store.User
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(meRaw, &me); err != nil || me.CSRFToken == "" {
		t.Fatalf("me=%+v err=%v", me, err)
	}
	request("GET", "/api/0/api-tokens/", "", nil, 200)
	request("POST", "/api/0/api-tokens/", `{"scopes":["member:read"]}`, nil, 403)
	_, createdRaw := request("POST", "/api/0/api-tokens/", `{"scopes":["member:read"],"label":"open"}`, http.Header{"X-CSRF-Token": {me.CSRFToken}}, 201)
	var created accountTokenView
	if err := json.Unmarshal(createdRaw, &created); err != nil || created.Label != "open" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	request("GET", "/api/0/api-tokens/", "", http.Header{"Authorization": {"Bearer " + created.Token}}, 401)

	restarted := httptest.NewServer(NewWithAccountMailAndDefaultUser(cfg, f.db, nil, nil, &me.User))
	defer restarted.Close()
	res, err := http.Get(restarted.URL + cfg.Prefix + "/api/0/api-tokens/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(raw), `"label":"open"`) {
		t.Fatalf("restart list=%d body=%s", res.StatusCode, raw)
	}
}
