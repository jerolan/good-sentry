package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/store"
)

type oauthAuthorize struct {
	ClientID, RedirectURI, ResponseType, State, Resource, Scope, Challenge string
}

type oauthPage struct {
	Base, Next, Error string
}

type oauthProjectView struct {
	ID, Name string
}

type oauthOrganizationView struct {
	Slug, Name string
	Projects   []oauthProjectView
}

type oauthConsentPage struct {
	Base, CSRF, ClientName, Scopes string
	Request                        template.HTML
	Organizations                  []oauthOrganizationView
}

var oauthLoginTemplate = template.Must(template.New("oauth-login").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sign in · GoodSentry</title><link rel="stylesheet" href="{{.Base}}/app.css"></head><body><div class="auth-layout"><section class="auth-intro"><div><div class="brand">GoodSentry</div><h2>Let an approved assistant investigate your telemetry.</h2><p>Sign in to review scopes, organizations and projects before issuing an MCP grant.</p></div><p class="auth-foot">Explicit consent · PKCE · revocable access</p></section><main class="auth-main"><div class="auth-box"><h1>Sign in</h1><p>Authorize an MCP client for GoodSentry.</p>{{if .Error}}<div class="form-error" role="alert">{{.Error}}</div>{{end}}<form class="form-stack" method="post" action="{{.Base}}/oauth/login"><input type="hidden" name="next" value="{{.Next}}"><div class="field"><label for="oauth-email">Email</label><input id="oauth-email" name="email" type="email" autocomplete="username" required></div><div class="field"><label for="oauth-password">Password</label><input id="oauth-password" name="password" type="password" autocomplete="current-password" required></div><button class="button primary" type="submit">Sign in</button></form></div></main></div></body></html>`))

var oauthConsentTemplate = template.Must(template.New("oauth-consent").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Authorize {{.ClientName}} · GoodSentry</title><link rel="stylesheet" href="{{.Base}}/app.css"></head><body><div class="auth-layout"><section class="auth-intro"><div><div class="brand">GoodSentry</div><h2>Review access before {{.ClientName}} can continue.</h2><p>Choose one organization, then grant all projects or a specific subset.</p></div><p class="auth-foot">Requested scopes: {{.Scopes}}</p></section><main class="auth-main"><div class="auth-box"><h1>Authorize {{.ClientName}}</h1><p>Requested scopes: <strong>{{.Scopes}}</strong></p><form class="form-stack" method="post" action="{{.Base}}/oauth/authorize"><input type="hidden" name="csrf" value="{{.CSRF}}">{{.Request}}<fieldset class="token-fieldset"><legend>Organization and projects</legend>{{range .Organizations}}<label class="checkbox-field"><input type="radio" name="organization" value="{{.Slug}}" required> {{.Name}}</label>{{range .Projects}}<label class="checkbox-field"><input type="checkbox" name="project" value="{{.ID}}"> {{.Name}}</label>{{end}}{{end}}</fieldset><label class="checkbox-field"><input type="checkbox" name="all_projects" value="1"> Grant all projects in the selected organization</label><div class="actions"><button class="button primary" type="submit" name="decision" value="approve">Authorize</button><button class="button" type="submit" name="decision" value="deny">Deny</button></div></form></div></main></div></body></html>`))

func (s *Server) oauthBase() string { return strings.TrimRight(s.cfg.Domain, "/") }

func (s *Server) oauthScopes() []string {
	scopes := []string{"org:read", "project:read", "event:read", "event:write"}
	if s.cfg.Logs {
		scopes = append(scopes, "log:read")
	}
	return scopes
}

func (s *Server) registerOAuthRoutes(mux *routeMux) {
	metadata := []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"}
	if !s.cfg.MCP {
		for _, path := range append(metadata, "/oauth/register", "/oauth/authorize", "/oauth/login", "/oauth/token", "/oauth/revoke") {
			mux.HandleFunc(path, http.NotFound)
		}
		return
	}
	for _, path := range metadata {
		mux.HandleFunc("GET "+path, s.oauthMetadata)
	}
	mux.HandleFunc("POST /oauth/register", s.oauthRegister)
	mux.HandleFunc("GET /oauth/authorize", s.oauthAuthorizePage)
	mux.HandleFunc("POST /oauth/authorize", s.oauthAuthorizeDecision)
	mux.HandleFunc("GET /oauth/login", s.oauthLoginPage)
	mux.HandleFunc("POST /oauth/login", s.oauthLoginSubmit)
	mux.HandleFunc("POST /oauth/token", s.oauthToken)
	mux.HandleFunc("POST /oauth/revoke", s.oauthRevoke)
}

func oauthNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func (s *Server) oauthMetadata(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	base := s.oauthBase()
	resource := base + "/mcp"
	if strings.HasSuffix(r.URL.Path, "oauth-protected-resource") || strings.HasSuffix(r.URL.Path, "oauth-protected-resource/mcp") {
		writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "authorization_servers": []string{base}, "bearer_methods_supported": []string{"header"}, "scopes_supported": s.oauthScopes()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": base, "authorization_endpoint": base + "/oauth/authorize", "token_endpoint": base + "/oauth/token",
		"registration_endpoint": base + "/oauth/register", "revocation_endpoint": base + "/oauth/revoke",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported": s.oauthScopes(), "authorization_response_iss_parameter_supported": true,
	})
}

func validOAuthRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || u.Hostname() == "localhost")
}

func oauthParseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	return r.ParseForm() == nil
}

func (s *Server) oauthRegister(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	var raw map[string]json.RawMessage
	decoderInput := struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		ApplicationType         string   `json:"application_type"`
	}{}
	raw, err = issueObject(body, "client_name", "redirect_uris", "token_endpoint_auth_method", "grant_types", "response_types", "application_type")
	if err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	for _, value := range raw {
		if string(value) == "null" {
			oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
			return
		}
	}
	if err := json.Unmarshal(body, &decoderInput); err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if strings.TrimSpace(decoderInput.ClientName) == "" || !credentialText(decoderInput.ClientName, 100) || len(decoderInput.RedirectURIs) == 0 || len(decoderInput.RedirectURIs) > 5 || decoderInput.TokenEndpointAuthMethod != "none" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if _, present := raw["application_type"]; present && decoderInput.ApplicationType != "native" && decoderInput.ApplicationType != "web" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if decoderInput.GrantTypes != nil && (len(decoderInput.GrantTypes) != 2 || !slices.Equal(decoderInput.GrantTypes, []string{"authorization_code", "refresh_token"})) {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if decoderInput.ResponseTypes != nil && (len(decoderInput.ResponseTypes) != 1 || decoderInput.ResponseTypes[0] != "code") {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	seen := map[string]bool{}
	for _, redirect := range decoderInput.RedirectURIs {
		if seen[redirect] || !validOAuthRedirect(redirect) {
			oauthJSONError(w, http.StatusBadRequest, "invalid_redirect_uri")
			return
		}
		seen[redirect] = true
	}
	rawID, err := token()
	if err != nil {
		oauthJSONError(w, http.StatusServiceUnavailable, "server_error")
		return
	}
	id := "gso_" + rawID
	client := store.OAuthClient{ID: id, Name: decoderInput.ClientName, RedirectURIs: decoderInput.RedirectURIs}
	if err := s.db.CreateOAuthClient(r.Context(), client); err != nil {
		oauthJSONError(w, http.StatusServiceUnavailable, "server_error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"client_id": id, "client_name": client.Name, "redirect_uris": client.RedirectURIs, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "client_id_issued_at": time.Now().Unix()})
}

func (s *Server) parseOAuthAuthorize(ctx context.Context, values url.Values) (oauthAuthorize, store.OAuthClient, error) {
	request := oauthAuthorize{ClientID: values.Get("client_id"), RedirectURI: values.Get("redirect_uri"), ResponseType: values.Get("response_type"), State: values.Get("state"), Resource: values.Get("resource"), Scope: values.Get("scope"), Challenge: values.Get("code_challenge")}
	if request.ClientID == "" {
		return request, store.OAuthClient{}, errors.New("invalid_client")
	}
	client, err := s.db.OAuthClient(ctx, request.ClientID)
	if err != nil || !slices.Contains(client.RedirectURIs, request.RedirectURI) {
		return request, store.OAuthClient{}, errors.New("invalid_request")
	}
	return request, client, nil
}

func (s *Server) validateOAuthAuthorize(r oauthAuthorize, values url.Values) string {
	if r.ResponseType != "code" || values.Get("code_challenge_method") != "S256" {
		return "invalid_request"
	}
	if !credentialText(r.State, 512) || r.Resource != "" && r.Resource != s.oauthBase()+"/mcp" {
		return "invalid_request"
	}
	decoded, err := base64.RawURLEncoding.DecodeString(r.Challenge)
	if err != nil || len(decoded) != sha256.Size || len(r.Challenge) < 43 || len(r.Challenge) > 128 {
		return "invalid_request"
	}
	scopes := strings.Fields(r.Scope)
	if len(scopes) == 0 || len(scopes) > len(s.oauthScopes()) {
		return "invalid_scope"
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if !slices.Contains(s.oauthScopes(), scope) || seen[scope] {
			return "invalid_scope"
		}
		seen[scope] = true
	}
	return ""
}

func (s *Server) oauthSession(r *http.Request) (identity, bool) {
	if s.cfg.AuthenticationDisabled() {
		if s.defaultUserID == "" {
			return identity{}, false
		}
		user, err := s.db.UserByID(r.Context(), s.defaultUserID)
		if err != nil {
			return identity{}, false
		}
		return identity{User: user, CSRF: s.openCSRF}, true
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return identity{}, false
	}
	user, csrf, err := s.db.Session(r.Context(), s.hash(cookie.Value))
	if err != nil {
		return identity{}, false
	}
	return identity{User: user, CSRF: csrf}, true
}

func (s *Server) oauthAuthorizePage(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	request, client, err := s.parseOAuthAuthorize(r.Context(), r.URL.Query())
	if err != nil {
		failure(w, http.StatusBadRequest, "Invalid OAuth client or redirect URI")
		return
	}
	if code := s.validateOAuthAuthorize(request, r.URL.Query()); code != "" {
		s.redirectOAuthError(w, request, code)
		return
	}
	session, ok := s.oauthSession(r)
	if !ok {
		next := "/oauth/authorize?" + r.URL.RawQuery
		s.renderOAuthLogin(w, http.StatusOK, next, "")
		return
	}
	required, err := s.requiresTwoFactorSetup(r.Context(), session.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	if required {
		http.Redirect(w, r, s.cfg.Prefix+"/", http.StatusSeeOther)
		return
	}
	orgs, err := s.db.Organizations(r.Context(), session.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	views := make([]oauthOrganizationView, 0, len(orgs))
	for _, org := range orgs {
		projects, err := s.db.Projects(r.Context(), session.User.ID, org.Slug)
		if err != nil {
			report(w, err)
			return
		}
		view := oauthOrganizationView{Slug: org.Slug, Name: org.Name}
		for _, project := range projects {
			view.Projects = append(view.Projects, oauthProjectView{ID: project.ID, Name: project.Name})
		}
		views = append(views, view)
	}
	hidden := `<input type="hidden" name="client_id" value="` + template.HTMLEscapeString(request.ClientID) + `"><input type="hidden" name="redirect_uri" value="` + template.HTMLEscapeString(request.RedirectURI) + `"><input type="hidden" name="response_type" value="code"><input type="hidden" name="state" value="` + template.HTMLEscapeString(request.State) + `"><input type="hidden" name="resource" value="` + template.HTMLEscapeString(request.Resource) + `"><input type="hidden" name="scope" value="` + template.HTMLEscapeString(request.Scope) + `"><input type="hidden" name="code_challenge" value="` + template.HTMLEscapeString(request.Challenge) + `"><input type="hidden" name="code_challenge_method" value="S256">`
	data := oauthConsentPage{Base: s.cfg.Prefix, CSRF: session.CSRF, ClientName: client.Name, Scopes: request.Scope, Request: template.HTML(hidden), Organizations: views}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := oauthConsentTemplate.Execute(w, data); err != nil {
		report(w, err)
	}
}

func (s *Server) oauthAuthorizeDecision(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	if !s.sameOrigin(r) {
		failure(w, http.StatusForbidden, "Invalid origin")
		return
	}
	if !oauthParseForm(w, r) {
		failure(w, http.StatusBadRequest, "Invalid form")
		return
	}
	session, ok := s.oauthSession(r)
	if !ok {
		failure(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	required, err := s.requiresTwoFactorSetup(r.Context(), session.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	if required {
		failure(w, http.StatusForbidden, "Two-factor authentication setup required")
		return
	}
	request, _, err := s.parseOAuthAuthorize(r.Context(), r.PostForm)
	if err != nil {
		failure(w, http.StatusBadRequest, "Invalid OAuth request")
		return
	}
	if code := s.validateOAuthAuthorize(request, r.PostForm); code != "" {
		s.redirectOAuthError(w, request, code)
		return
	}
	if !hmac.Equal([]byte(r.PostForm.Get("csrf")), []byte(session.CSRF)) {
		failure(w, http.StatusForbidden, "Invalid CSRF token")
		return
	}
	if r.PostForm.Get("decision") != "approve" {
		s.redirectOAuthError(w, request, "access_denied")
		return
	}
	org, err := s.db.Organization(r.Context(), session.User.ID, r.PostForm.Get("organization"))
	if err != nil {
		s.redirectOAuthError(w, request, "access_denied")
		return
	}
	var projectIDs []int64
	all := r.PostForm.Get("all_projects") == "1" || r.PostForm.Get("all_projects") == "true"
	if !all {
		for _, raw := range r.PostForm["project"] {
			id, valid := occurrenceNumber(raw, 9223372036854775807)
			if !valid {
				s.redirectOAuthError(w, request, "invalid_request")
				return
			}
			projectIDs = append(projectIDs, id)
		}
		if len(projectIDs) == 0 {
			s.redirectOAuthError(w, request, "invalid_request")
			return
		}
	}
	rawCode, err := token()
	if err != nil {
		failure(w, http.StatusServiceUnavailable, "Could not authorize")
		return
	}
	codeInput := store.OAuthCodeInput{CodeHash: s.hash(rawCode), ClientID: request.ClientID, UserID: session.User.ID,
		AuthGeneration: session.User.AuthGeneration, OrganizationID: org.ID, ProjectIDs: projectIDs, AllProjects: all,
		Scopes: strings.Fields(request.Scope), RedirectURI: request.RedirectURI, Challenge: request.Challenge, ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err := s.db.CreateOAuthCode(r.Context(), codeInput); err != nil {
		s.redirectOAuthError(w, request, "access_denied")
		return
	}
	u, _ := url.Parse(request.RedirectURI)
	q := u.Query()
	q.Set("code", rawCode)
	if request.State != "" {
		q.Set("state", request.State)
	}
	q.Set("iss", s.oauthBase())
	u.RawQuery = q.Encode()
	w.Header().Set("Location", u.String())
	oauthNoStore(w)
	w.WriteHeader(http.StatusSeeOther)
}

func (s *Server) redirectOAuthError(w http.ResponseWriter, request oauthAuthorize, code string) {
	u, _ := url.Parse(request.RedirectURI)
	q := u.Query()
	q.Set("error", code)
	if request.State != "" {
		q.Set("state", request.State)
	}
	q.Set("iss", s.oauthBase())
	u.RawQuery = q.Encode()
	w.Header().Set("Location", u.String())
	oauthNoStore(w)
	w.WriteHeader(http.StatusSeeOther)
}

func safeOAuthNext(next string) bool {
	return strings.HasPrefix(next, "/oauth/authorize?") && !strings.Contains(next, "//") && !strings.ContainsAny(next, "\r\n")
}

func (s *Server) renderOAuthLogin(w http.ResponseWriter, status int, next, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = oauthLoginTemplate.Execute(w, oauthPage{Base: s.cfg.Prefix, Next: next, Error: message})
}

func (s *Server) oauthLoginPage(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	next := r.URL.Query().Get("next")
	if next != "" && !safeOAuthNext(next) {
		failure(w, http.StatusBadRequest, "Invalid return path")
		return
	}
	s.renderOAuthLogin(w, http.StatusOK, next, "")
}

func (s *Server) oauthLoginSubmit(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	if !s.sameOrigin(r) {
		failure(w, http.StatusForbidden, "Invalid origin")
		return
	}
	if !oauthParseForm(w, r) {
		failure(w, http.StatusBadRequest, "Invalid form")
		return
	}
	next := r.PostForm.Get("next")
	if !safeOAuthNext(next) {
		failure(w, http.StatusBadRequest, "Invalid return path")
		return
	}
	email, password := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email"))), r.PostForm.Get("password")
	if len(email) > 254 || len(password) > 72 {
		s.renderOAuthLogin(w, http.StatusUnauthorized, next, "Invalid email or password")
		return
	}
	user, err := s.db.UserByEmail(r.Context(), email)
	if err != nil || bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)) != nil {
		s.renderOAuthLogin(w, http.StatusUnauthorized, next, "Invalid email or password")
		return
	}
	if _, err := s.establishSession(w, r, user); err != nil {
		s.renderOAuthLogin(w, http.StatusUnauthorized, next, "Could not start session")
		return
	}
	http.Redirect(w, r, s.cfg.Prefix+next, http.StatusSeeOther)
}

func oauthJSONError(w http.ResponseWriter, status int, code string) {
	oauthNoStore(w)
	writeJSON(w, status, map[string]string{"error": code})
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !oauthParseForm(w, r) {
		oauthJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	clientID := r.PostForm.Get("client_id")
	if _, err := s.db.OAuthClient(r.Context(), clientID); err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client")
		return
	}
	accessRaw, err := token()
	if err != nil {
		oauthJSONError(w, http.StatusServiceUnavailable, "server_error")
		return
	}
	refreshRaw, err := token()
	if err != nil {
		oauthJSONError(w, http.StatusServiceUnavailable, "server_error")
		return
	}
	access := "gs_" + accessRaw
	refresh := "ort_" + refreshRaw
	now := time.Now()
	var issued store.OAuthIssued
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		code, redirect, verifier := r.PostForm.Get("code"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier")
		if code == "" || redirect == "" || len(verifier) < 43 || len(verifier) > 128 {
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		hash := sha256.Sum256([]byte(verifier))
		if base64.RawURLEncoding.EncodeToString(hash[:]) != s.db.OAuthCodeChallenge(r.Context(), clientID, s.hash(code)) {
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		issued, err = s.db.ExchangeOAuthCode(r.Context(), store.OAuthExchangeInput{CodeHash: s.hash(code), ClientID: clientID, RedirectURI: redirect,
			AccessSecretHash: s.hash(access), RefreshSecretHash: s.hash(refresh), Prefix: access[:11], AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)})
	case "refresh_token":
		old := r.PostForm.Get("refresh_token")
		if old == "" {
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		issued, err = s.db.RotateOAuthRefresh(r.Context(), store.OAuthRotationInput{RefreshSecretHash: s.hash(old), ClientID: clientID,
			AccessSecretHash: s.hash(access), NewRefreshSecretHash: s.hash(refresh), Prefix: access[:11], AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)})
	default:
		oauthJSONError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if err != nil || issued.AccessTokenID == "" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "refresh_token": refresh, "scope": strings.Join(issued.Scopes, " ")})
}

func (s *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	oauthNoStore(w)
	if !oauthParseForm(w, r) {
		oauthJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if client := r.PostForm.Get("client_id"); client != "" {
		_ = s.db.RevokeOAuthArtifact(r.Context(), client, s.hash(r.PostForm.Get("token")))
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
