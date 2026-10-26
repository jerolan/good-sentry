//go:build integration

package server

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

// This boundary owns issued credentials, exact routes, JSON/multipart admission
// and CLI response shapes. Storage tests own quota, expiry and concurrency.
func TestArtifactUploadsHTTPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	schema := fmt.Sprintf("goodsentry_artifact_http_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	cfg := config.Config{
		Domain: "https://public.example.test/sentry", Prefix: "/sentry", Secret: strings.Repeat("a", 64),
		MaxBodyBytes: 1 << 20, ArtifactChunkTTLHours: 24, ArtifactStagingMaxBytes: 256 << 20, ArtifactMaxMapRecords: 1000,
	}
	srv := httptest.NewServer(New(cfg, db))
	defer srv.Close()
	type response struct {
		code int
		raw  []byte
	}
	requestAt := func(serverURL string, client *http.Client, method, path string, body []byte, headers http.Header) response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, serverURL+cfg.Prefix+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for name, values := range headers {
			req.Header[name] = values
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
		return response{res.StatusCode, raw}
	}
	request := func(client *http.Client, method, path string, body []byte, headers http.Header) response {
		return requestAt(srv.URL, client, method, path, body, headers)
	}
	want := func(t *testing.T, label string, r response, code int) response {
		t.Helper()
		if r.code != code {
			t.Fatalf("%s=%d want=%d body=%s", label, r.code, code, r.raw)
		}
		return r
	}
	decode := func(t *testing.T, r response, target any) {
		t.Helper()
		if err := json.Unmarshal(r.raw, target); err != nil {
			t.Fatalf("response JSON: %v body=%s", err, r.raw)
		}
	}
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	// This must fail on an absent route before fixture authentication can hide it.
	want(t, "anonymous chunk negotiation", request(http.DefaultClient, "GET", "/api/0/organizations/artifacts-http/chunk-upload/", nil, nil), 401)
	const password = "artifact-http-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, org, err := db.Bootstrap(ctx, "owner@artifacts.test", hash, "Owner", "Artifacts HTTP")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"admin", "member", "manager"} {
		var id string
		if err := fixture.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text", role+"@artifacts.test", hash, role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3)", org.ID, id, role); err != nil {
			t.Fatal(err)
		}
	}
	projects := make([]store.Project, 2)
	for i := range projects {
		projects[i], err = db.CreateProject(ctx, owner.ID, org.Slug, fmt.Sprintf("Artifact project %d", i), "javascript")
		if err != nil {
			t.Fatal(err)
		}
	}
	var foreignOrgID string
	if err := fixture.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('artifact-foreign','Foreign') RETURNING id::text").Scan(&foreignOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrgID, owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := db.CreateProject(ctx, owner.ID, "artifact-foreign", "Foreign artifact project", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	type actor struct {
		client *http.Client
		csrf   string
	}
	login := func(email string) actor {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		var result struct{ CSRFToken string }
		decode(t, want(t, "login", request(client, "POST", "/api/v1/login", encode(map[string]string{"email": email, "password": password}), nil), 200), &result)
		if result.CSRFToken == "" {
			t.Fatal("login omitted CSRF token")
		}
		return actor{client, result.CSRFToken}
	}
	ownerActor, adminActor, memberActor := login(owner.Email), login("admin@artifacts.test"), login("member@artifacts.test")
	managerActor := login("manager@artifacts.test")
	bearer := func(secret string) http.Header { return http.Header{"Authorization": {"Bearer " + secret}} }
	csrf := func(who actor) http.Header { return http.Header{"X-CSRF-Token": {who.csrf}} }
	issue := func(scopes, selected []string) string {
		t.Helper()
		body := map[string]any{"name": "Artifact HTTP credential", "scopes": scopes}
		if selected != nil {
			body["projectIds"] = selected
		}
		var result struct{ Token string }
		decode(t, want(t, "issue actual credential", request(ownerActor.client, "POST", "/api/v1/organizations/"+org.Slug+"/api-tokens", encode(body), csrf(ownerActor)), 201), &result)
		if !strings.HasPrefix(result.Token, "gs_") {
			t.Fatal("issuance omitted credential")
		}
		return result.Token
	}
	writer := issue([]string{"project:releases"}, nil)
	reader := issue([]string{"org:read", "project:read"}, nil)
	restricted := issue([]string{"project:releases"}, []string{projects[0].ID})
	chunksPath := "/api/0/organizations/" + org.Slug + "/chunk-upload/"
	assemblyPath := "/api/0/organizations/" + org.Slug + "/artifactbundle/assemble/"
	call := func(method, path string, body []byte, secret string) response {
		return request(http.DefaultClient, method, path, body, bearer(secret))
	}
	t.Run("authorization and exact negotiation", func(t *testing.T) {
		type negotiation struct {
			URL                                      string
			HashAlgorithm                            string
			ChunkSize, ChunksPerRequest              int64
			MaxRequestSize, MaxFileSize, Concurrency int64
			Compression, Accept                      []string
		}
		var actual negotiation
		decode(t, want(t, "writer negotiation control", call("GET", chunksPath, nil, writer), 200), &actual)
		if actual.URL != cfg.Domain+chunksPath || actual.HashAlgorithm != "sha1" || actual.ChunkSize != 524288 || actual.ChunksPerRequest != 1 || actual.MaxRequestSize != cfg.MaxBodyBytes || actual.MaxFileSize != 64<<20 || actual.Concurrency != 2 || !reflect.DeepEqual(actual.Compression, []string{"gzip"}) || !reflect.DeepEqual(actual.Accept, []string{"debug_files", "artifact_bundles"}) {
			t.Fatalf("negotiation=%+v", actual)
		}
		want(t, "owner session negotiation", request(ownerActor.client, "GET", chunksPath, nil, nil), 200)
		want(t, "admin session negotiation", request(adminActor.client, "GET", chunksPath, nil, nil), 200)
		want(t, "manager session negotiation", request(managerActor.client, "GET", chunksPath, nil, nil), 403)
		want(t, "read credential negotiation", call("GET", chunksPath, nil, reader), 403)
		if projects[0].PublicKey != "" {
			want(t, "SDK key negotiation", request(http.DefaultClient, "GET", chunksPath, nil, bearer(projects[0].PublicKey)), 401)
		}
		want(t, "member negotiation", request(memberActor.client, "GET", chunksPath, nil, nil), 403)
		want(t, "read credential negotiation", call("GET", chunksPath, nil, reader), 403)
		want(t, "DSN credential negotiation", call("GET", chunksPath, nil, projects[0].PublicKey), 401)
		want(t, "malformed Bearer takes precedence over owner cookie", request(ownerActor.client, "GET", chunksPath, nil, http.Header{"Authorization": {"Bearer invalid"}}), 401)
		want(t, "duplicate Authorization headers", request(ownerActor.client, "GET", chunksPath, nil, http.Header{"Authorization": {"Bearer " + writer, "Bearer " + writer}}), 401)
		want(t, "foreign organization session control", request(ownerActor.client, "GET", "/api/0/organizations/artifact-foreign/chunk-upload/", nil, nil), 200)
		want(t, "credential organization boundary", call("GET", "/api/0/organizations/artifact-foreign/chunk-upload/", nil, writer), 404)
		want(t, "unregistered chunk descendant", call("GET", chunksPath+"unregistered/", nil, writer), 404)
		// Unknown POSTs retain the existing public method-rejection fallback.
		want(t, "unregistered assembly descendant", call("POST", assemblyPath+"unregistered/", []byte(`{}`), writer), 405)
		want(t, "no chunk download route", call("GET", chunksPath+strings.Repeat("a", 40)+"/", nil, writer), 404)
	})
	digest := func(data []byte) string {
		sum := sha1.Sum(data)
		return hex.EncodeToString(sum[:])
	}
	type part struct {
		field, filename string
		data            []byte
	}
	multipartBody := func(parts []part) ([]byte, string) {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for _, item := range parts {
			header := textproto.MIMEHeader{}
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, item.field, item.filename))
			header.Set("Content-Type", "application/octet-stream")
			w, err := mw.CreatePart(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(item.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		return body.Bytes(), mw.FormDataContentType()
	}
	uploadAt := func(serverURL string, client *http.Client, parts []part, auth http.Header) response {
		t.Helper()
		body, contentType := multipartBody(parts)
		headers := auth.Clone()
		if headers == nil {
			headers = make(http.Header)
		}
		headers.Set("Content-Type", contentType)
		return requestAt(serverURL, client, "POST", chunksPath, body, headers)
	}
	upload := func(parts []part) response { return uploadAt(srv.URL, http.DefaultClient, parts, bearer(writer)) }
	assembly := func(checksum string, chunks, refs []string, extra map[string]any) []byte {
		body := map[string]any{"checksum": checksum, "chunks": chunks, "projects": refs}
		for name, value := range extra {
			body[name] = value
		}
		return encode(body)
	}
	state := func(t *testing.T, label string, r response, expected string, missing []string) {
		t.Helper()
		var result struct {
			State         string
			MissingChunks []string
			Detail        string
		}
		decode(t, want(t, label, r, 200), &result)
		if result.State != expected || result.MissingChunks == nil || !reflect.DeepEqual(result.MissingChunks, missing) || (expected == "error" && (result.Detail == "" || len(result.Detail) > 2000)) {
			t.Fatalf("%s=%+v want state=%s missing=%v", label, result, expected, missing)
		}
	}
	empty := []string{}
	projectRefs := []string{projects[0].Slug}
	t.Run("multipart boundaries and session controls", func(t *testing.T) {
		data := []byte("raw HTTP artifact chunk")
		valid := part{"file", strings.ToUpper(digest(data)), data}
		want(t, "raw chunk control", upload([]part{valid}), 200)
		var receipt map[string]any
		decode(t, want(t, "identical raw chunk retry", upload([]part{valid}), 200), &receipt)
		if len(receipt) != 0 {
			t.Fatalf("chunk response is not empty JSON: %+v", receipt)
		}
		want(t, "session chunk without CSRF", uploadAt(srv.URL, ownerActor.client, []part{valid}, nil), 403)
		want(t, "session chunk CSRF control", uploadAt(srv.URL, ownerActor.client, []part{valid}, csrf(ownerActor)), 200)
		crossSite := csrf(ownerActor)
		crossSite.Set("Origin", "https://untrusted.example")
		want(t, "session chunk cross-origin", uploadAt(srv.URL, ownerActor.client, []part{valid}, crossSite), 403)
		want(t, "member chunk upload", uploadAt(srv.URL, memberActor.client, []part{valid}, csrf(memberActor)), 403)
		want(t, "manager chunk upload", uploadAt(srv.URL, managerActor.client, []part{valid}, csrf(managerActor)), 403)
		want(t, "reader chunk upload", uploadAt(srv.URL, http.DefaultClient, []part{valid}, bearer(reader)), 403)
		want(t, "manager assembly", request(managerActor.client, "POST", assemblyPath, assembly(digest(data), []string{digest(data)}, projectRefs, nil), csrf(managerActor)), 403)
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		if _, err := gz.Write([]byte("gzip HTTP artifact chunk")); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		want(t, "gzip chunk control", upload([]part{{"file_gzip", digest([]byte("gzip HTTP artifact chunk")), compressed.Bytes()}}), 200)
		for _, tc := range []struct {
			name  string
			parts []part
			code  int
		}{
			{"empty multipart", nil, 400},
			{"empty chunk", []part{{"file", digest(nil), nil}}, 400},
			{"unknown field", []part{{"unexpected", digest(data), data}}, 400},
			{"missing filename", []part{{"file", "", data}}, 400},
			{"non-hex filename", []part{{"file", strings.Repeat("z", 40), data}}, 400},
			{"normalized traversal filename", []part{{"file", "../" + digest(data), data}}, 400},
			{"wrong chunk checksum", []part{{"file", strings.Repeat("0", 40), data}}, 400},
			{"malformed gzip", []part{{"file_gzip", digest(data), data}}, 400},
			{"too many parts", []part{valid, valid}, 413},
			{"raw part size", []part{{"file", digest(bytes.Repeat([]byte("p"), 524289)), bytes.Repeat([]byte("p"), 524289)}}, 413},
			{"HTTP body size", []part{{"file", digest(bytes.Repeat([]byte("b"), 1<<20)), bytes.Repeat([]byte("b"), 1<<20)}}, 413},
		} {
			t.Run(tc.name, func(t *testing.T) { want(t, tc.name, upload(tc.parts), tc.code) })
		}
		compressed.Reset()
		gz = gzip.NewWriter(&compressed)
		large := bytes.Repeat([]byte("x"), 524289)
		if _, err := gz.Write(large); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		want(t, "gzip expansion exceeds negotiated part size", upload([]part{{"file_gzip", digest(large), compressed.Bytes()}}), 413)
		largeCfg := cfg
		largeCfg.MaxBodyBytes = 4 << 20
		largeServer := httptest.NewServer(New(largeCfg, db))
		defer largeServer.Close()
		first := []byte("must not survive malformed second HTTP chunk")
		want(t, "batch rejects malformed second checksum", uploadAt(largeServer.URL, http.DefaultClient, []part{{"file", digest(first), first}, {"file", strings.Repeat("0", 40), []byte("bad second chunk")}}, bearer(writer)), 400)
		state(t, "rejected multipart batch made no first-chunk write", call("POST", assemblyPath, assembly(digest(first), []string{digest(first)}, projectRefs, nil), writer), "not_found", []string{digest(first)})
	})
	t.Run("strict assembly and credential coverage", func(t *testing.T) {
		missingA, missingB := digest([]byte("missing first chunk")), digest([]byte("missing second chunk"))
		valid := assembly(strings.Repeat("a", 40), []string{missingA, missingB, missingA}, projectRefs, nil)
		state(t, "missing chunks are distinct and ordered", call("POST", assemblyPath, valid, restricted), "not_found", []string{missingA, missingB})
		want(t, "session assembly without CSRF", request(ownerActor.client, "POST", assemblyPath, valid, nil), 403)
		state(t, "session assembly CSRF control", request(ownerActor.client, "POST", assemblyPath, valid, csrf(ownerActor)), "not_found", []string{missingA, missingB})
		want(t, "member assembly", request(memberActor.client, "POST", assemblyPath, valid, csrf(memberActor)), 403)
		want(t, "reader assembly", call("POST", assemblyPath, valid, reader), 403)
		want(t, "hidden project assembly", call("POST", assemblyPath, assembly(strings.Repeat("a", 40), []string{missingA}, []string{projects[1].Slug}, nil), restricted), 404)
		want(t, "mixed authorized and hidden projects", call("POST", assemblyPath, assembly(strings.Repeat("a", 40), []string{missingA}, []string{projects[0].Slug, projects[1].Slug}, nil), restricted), 404)
		want(t, "wrong organization project", call("POST", assemblyPath, assembly(strings.Repeat("a", 40), []string{missingA}, []string{foreignProject.Slug}, nil), writer), 404)
		for _, tc := range []struct{ name, body string }{
			{"null object", `null`},
			{"empty object", `{}`},
			{"unknown field", string(assembly(strings.Repeat("a", 40), []string{missingA}, projectRefs, map[string]any{"unexpected": true}))},
			{"null checksum", `{"checksum":null,"chunks":["` + missingA + `"],"projects":["` + projects[0].Slug + `"]}`},
			{"bad checksum", string(assembly("not-a-digest", []string{missingA}, projectRefs, nil))},
			{"empty chunks", string(assembly(strings.Repeat("a", 40), empty, projectRefs, nil))},
			{"null chunks", string(assembly(strings.Repeat("a", 40), nil, projectRefs, nil))},
			{"invalid chunk digest", string(assembly(strings.Repeat("a", 40), []string{"no"}, projectRefs, nil))},
			{"empty projects", string(assembly(strings.Repeat("a", 40), []string{missingA}, empty, nil))},
			{"null projects", string(assembly(strings.Repeat("a", 40), []string{missingA}, nil, nil))},
			{"invalid version", string(assembly(strings.Repeat("a", 40), []string{missingA}, projectRefs, map[string]any{"version": "."}))},
			{"dist bytes", string(assembly(strings.Repeat("a", 40), []string{missingA}, projectRefs, map[string]any{"dist": strings.Repeat("界", 67)}))},
			{"dist NUL", string(assembly(strings.Repeat("a", 40), []string{missingA}, projectRefs, map[string]any{"dist": "web\x00"}))},
			{"second JSON value", string(valid) + `{}`},
		} {
			t.Run(tc.name, func(t *testing.T) { want(t, tc.name, call("POST", assemblyPath, []byte(tc.body), writer), 400) })
		}
		tooMany := make([]string, 1001)
		for i := range tooMany {
			tooMany[i] = missingA
		}
		want(t, "chunk reference count", call("POST", assemblyPath, assembly(strings.Repeat("a", 40), tooMany, projectRefs, nil), writer), 400)
		for i := range tooMany {
			tooMany[i] = projects[0].Slug
		}
		want(t, "project reference count", call("POST", assemblyPath, assembly(strings.Repeat("a", 40), []string{missingA}, tooMany, nil), writer), 400)
		state(t, "nullable artifact selectors", call("POST", assemblyPath, assembly(strings.Repeat("a", 40), []string{missingA}, projectRefs, map[string]any{"version": nil, "dist": nil}), writer), "not_found", []string{missingA})
	})
	t.Run("complete bundle commits only authorized maps", func(t *testing.T) {
		bundle := artifactHTTPBundle(t, "throw new Error('HTTP artifact');")
		cut := len(bundle) / 2
		pieces := [][]byte{bundle[:cut], bundle[cut:]}
		chunkIDs := []string{digest(pieces[0]), digest(pieces[1])}
		version := " CLI/版本 @1 "
		extra := map[string]any{"version": version, "dist": "web"}
		body := assembly(digest(bundle), chunkIDs, []string{projects[0].Slug, projects[1].Slug}, extra)
		for _, piece := range pieces {
			want(t, "stage valid bundle chunk", upload([]part{{"file", digest(piece), piece}}), 200)
		}
		maps := func(project store.Project) []store.SourceMapFile {
			t.Helper()
			var result struct{ Data []store.SourceMapFile }
			decode(t, want(t, "list actual map metadata", request(ownerActor.client, "GET", "/api/v1/organizations/"+org.Slug+"/projects/"+project.ID+"/sourcemaps", nil, nil), 200), &result)
			return result.Data
		}
		want(t, "complete bundle restricted scope fails atomically", call("POST", assemblyPath, body, restricted), 404)
		if len(maps(projects[0])) != 0 || len(maps(projects[1])) != 0 {
			t.Fatal("failed scoped assembly persisted a partial map set")
		}
		state(t, "full checksum does not match staged bytes", call("POST", assemblyPath, assembly(strings.Repeat("0", 40), chunkIDs, projectRefs, extra), writer), "error", empty)
		badZIP := []byte("SYSB\x02\x00\x00\x00not a zip archive")
		want(t, "stage invalid ZIP control", upload([]part{{"file", digest(badZIP), badZIP}}), 200)
		state(t, "valid chunk checksum does not bypass ZIP validation", call("POST", assemblyPath, assembly(digest(badZIP), []string{digest(badZIP)}, projectRefs, extra), writer), "error", empty)
		expanded := artifactHTTPBundle(t, strings.Repeat("x", 1<<20))
		want(t, "stage compressed large bundle control", upload([]part{{"file", digest(expanded), expanded}}), 200)
		want(t, "expanded bundle uses HTTP resource status", call("POST", assemblyPath, assembly(digest(expanded), []string{digest(expanded)}, projectRefs, extra), writer), 413)
		if len(maps(projects[0])) != 0 || len(maps(projects[1])) != 0 {
			t.Fatal("bundle errors persisted map metadata")
		}
		state(t, "complete multi-project assembly", call("POST", assemblyPath, body, writer), "ok", empty)
		before := maps(projects[0])
		other := maps(projects[1])
		if len(before) != 1 || len(other) != 1 || before[0].Name != "~/assets/http.js.map" || before[0].Release != version || before[0].Dist != "web" || before[0].Checksum == "" || before[0].DateCreated.IsZero() {
			t.Fatalf("actual committed maps=%+v other=%+v", before, other)
		}
		state(t, "identical immediate assembly retry", call("POST", assemblyPath, body, writer), "ok", empty)
		if !reflect.DeepEqual(before, maps(projects[0])) || !reflect.DeepEqual(other, maps(projects[1])) {
			t.Fatal("identical HTTP assembly changed map identity or receipt")
		}
		want(t, "artifact selector does not create release metadata", call("GET", "/api/0/organizations/"+org.Slug+"/releases/"+url.PathEscape(version)+"/", nil, writer), 404)
	})
	t.Run("manifest metadata agreement and debug id requirement", func(t *testing.T) {
		matched := artifactHTTPBundleMeta(t, "manifest release", "manifest-rel", "m1", "")
		want(t, "stage manifest bundle", upload([]part{{"file", digest(matched), matched}}), 200)
		state(t, "manifest metadata matches", call("POST", assemblyPath, assembly(digest(matched), []string{digest(matched)}, projectRefs, map[string]any{"version": "manifest-rel", "dist": "m1"}), writer), "ok", empty)
		state(t, "manifest release disagreement", call("POST", assemblyPath, assembly(digest(matched), []string{digest(matched)}, projectRefs, map[string]any{"version": "other-rel", "dist": "m1"}), writer), "error", empty)
		bare := artifactHTTPBundleMeta(t, "bare map", "", "", "")
		want(t, "stage bare bundle", upload([]part{{"file", digest(bare), bare}}), 200)
		state(t, "map without debug id needs a release", call("POST", assemblyPath, assembly(digest(bare), []string{digest(bare)}, projectRefs, nil), writer), "error", empty)
	})
}

// Produce the wire fixture independently with standard ZIP/JSON writers. The
// archive prefix and manifest URLs are explicit external artifact contracts.
func artifactHTTPBundle(t *testing.T, source string) []byte {
	t.Helper()
	var output bytes.Buffer
	output.Write([]byte{'S', 'Y', 'S', 'B', 2, 0, 0, 0})
	zw := zip.NewWriter(&output)
	sourceMap, err := json.Marshal(map[string]any{"version": 3, "file": "http.js", "sources": []string{"../src/http.js"}, "sourcesContent": []string{source}, "names": []string{}, "mappings": "AAAA"})
	if err != nil {
		t.Fatal(err)
	}
	files := []struct{ name, data string }{
		{"manifest.json", `{"files":{"maps/http.map":{"type":"source_map","url":"~/assets/http.js.map"},"js/http.js":{"type":"minified_source","url":"~/assets/http.js","headers":{"sourcemap":"http.js.map"}}}}`},
		{"maps/http.map", string(sourceMap)},
		{"js/http.js", `throw new Error("minified");`},
	}
	for _, file := range files {
		member, err := zw.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(member, file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
func artifactHTTPBundleMeta(t *testing.T, source, manifestRelease, manifestDist, mapDebugID string) []byte {
	t.Helper()
	var output bytes.Buffer
	output.Write([]byte{'S', 'Y', 'S', 'B', 2, 0, 0, 0})
	zw := zip.NewWriter(&output)
	mapBody := map[string]any{"version": 3, "file": "http.js", "sources": []string{"../src/http.js"}, "sourcesContent": []string{source}, "names": []string{}, "mappings": "AAAA"}
	if mapDebugID != "" {
		mapBody["debug_id"] = mapDebugID
	}
	sourceMap, err := json.Marshal(mapBody)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"files": map[string]any{
		"maps/http.map": map[string]any{"type": "source_map", "url": "~/assets/http.js.map"},
		"js/http.js":    map[string]any{"type": "minified_source", "url": "~/assets/http.js", "headers": map[string]string{"sourcemap": "http.js.map"}},
	}}
	if manifestRelease != "" {
		manifest["release"] = manifestRelease
	}
	if manifestDist != "" {
		manifest["dist"] = manifestDist
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct{ name, data string }{
		{"manifest.json", string(encodedManifest)},
		{"maps/http.map", string(sourceMap)},
		{"js/http.js", `throw new Error("minified");`},
	} {
		member, err := zw.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(member, file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
