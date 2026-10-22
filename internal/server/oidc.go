package server

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

const oidcFlowCookie = "goodsentry_oidc"

var oidcDefaultScopes = []string{"openid", "profile", "email"}

// oidcClient is one OpenID Connect relying party: the instance-wide provider or
// an organization-managed application.
type oidcClient struct {
	issuer, clientID, clientSecret, redirectURL string
	scopes                                      []string

	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func newOIDCClient(issuer, clientID, clientSecret, redirectURL string, scopes []string) *oidcClient {
	if len(scopes) == 0 {
		scopes = oidcDefaultScopes
	}
	return &oidcClient{issuer: issuer, clientID: clientID, clientSecret: clientSecret, redirectURL: redirectURL, scopes: scopes}
}

func (c *oidcClient) client(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return c.oauth, c.verifier, nil
	}
	provider, err := oidc.NewProvider(ctx, c.issuer)
	if err != nil {
		return nil, nil, err
	}
	c.provider = provider
	c.oauth = &oauth2.Config{
		ClientID: c.clientID, ClientSecret: c.clientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: c.redirectURL, Scopes: c.scopes,
	}
	c.verifier = provider.Verifier(&oidc.Config{ClientID: c.clientID})
	return c.oauth, c.verifier, nil
}

type oidcAuth struct {
	*oidcClient
	label      string
	autoCreate bool
}

type oidcFlow struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	App      string `json:"app,omitempty"`
	Expires  int64  `json:"expires"`
}

func newOIDCAuth(cfg config.Config) *oidcAuth {
	if cfg.OIDCIssuer == "" {
		return nil
	}
	scopes := cfg.OIDCScopes
	if len(scopes) == 0 {
		scopes = oidcDefaultScopes
	}
	label := cfg.OIDCName
	if label == "" {
		label = "Single sign-on"
	}
	return &oidcAuth{
		oidcClient: newOIDCClient(cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.Domain+"/auth/oidc/callback", scopes),
		label:      label,
		autoCreate: cfg.OIDCAutoCreate,
	}
}

func (s *Server) registerOIDCRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/auth/config", s.authConfig)
	mux.HandleFunc("GET /api/v1/organizations/{org}/sso", s.publicOrganizationSSO)
	mux.HandleFunc("GET /login/{org}", s.socialLoginPage)
	mux.HandleFunc("GET /auth/social/{app}/start", s.socialStart)
	mux.HandleFunc("GET /auth/social/{app}/callback", s.socialCallback)
	if s.oidc == nil {
		return
	}
	mux.HandleFunc("GET /auth/oidc/start", s.oidcStart)
	mux.HandleFunc("GET /auth/oidc/callback", s.oidcCallback)
}

func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	response := map[string]any{
		"oidc": map[string]any{"enabled": s.oidc != nil},
	}
	if s.oidc != nil {
		response["oidc"].(map[string]any)["name"] = s.oidc.label
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	s.startOIDCFlow(w, r, s.oidc.oidcClient, "")
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	s.finishOIDCFlow(w, r, s.oidc.oidcClient, "", s.oidc.issuer, s.oidc.autoCreate, "")
}

func (s *Server) startOIDCFlow(w http.ResponseWriter, r *http.Request, client *oidcClient, app string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ctx = oidc.ClientContext(ctx, &http.Client{Timeout: 10 * time.Second})
	config, _, err := client.client(ctx)
	if err != nil {
		slog.Error("OIDC discovery failed", "app", app)
		s.oidcRedirectError(w, r)
		return
	}
	state, err := token()
	if err != nil {
		s.oidcRedirectError(w, r)
		return
	}
	nonce, err := token()
	if err != nil {
		s.oidcRedirectError(w, r)
		return
	}
	verifier := oauth2.GenerateVerifier()
	flow := oidcFlow{State: state, Nonce: nonce, Verifier: verifier, App: app, Expires: time.Now().Add(10 * time.Minute).Unix()}
	raw, err := json.Marshal(flow)
	if err != nil {
		s.oidcRedirectError(w, r)
		return
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name: oidcFlowCookie, Value: payload + "." + s.hash(payload), Path: s.cfg.Prefix + "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.Domain, "https://"),
		MaxAge: 600, Expires: time.Now().Add(10 * time.Minute),
	})
	http.Redirect(w, r, config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Server) finishOIDCFlow(w http.ResponseWriter, r *http.Request, client *oidcClient, app, provider string, autoCreate bool, organizationID string) {
	flow, ok := s.readOIDCFlow(r)
	s.clearOIDCFlow(w)
	if !ok || flow.App != app {
		s.oidcRedirectError(w, r)
		return
	}
	if r.URL.Query().Get("error") != "" || !hmac.Equal([]byte(r.URL.Query().Get("state")), []byte(flow.State)) {
		s.oidcRedirectError(w, r)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 8192 {
		s.oidcRedirectError(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ctx = oidc.ClientContext(ctx, &http.Client{Timeout: 12 * time.Second})
	config, verifier, err := client.client(ctx)
	if err != nil {
		slog.Error("OIDC discovery failed during callback", "app", app)
		s.oidcRedirectError(w, r)
		return
	}
	exchange, err := config.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		slog.Error("OIDC code exchange failed", "app", app)
		s.oidcRedirectError(w, r)
		return
	}
	rawIDToken, ok := exchange.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.oidcRedirectError(w, r)
		return
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil || !hmac.Equal([]byte(idToken.Nonce), []byte(flow.Nonce)) {
		slog.Error("OIDC ID token verification failed", "app", app)
		s.oidcRedirectError(w, r)
		return
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err = idToken.Claims(&claims); err != nil || claims.Subject == "" || claims.Subject != idToken.Subject ||
		!claims.EmailVerified || len(claims.Subject) > 255 || strings.ContainsFunc(claims.Subject, unicode.IsControl) {
		s.oidcRedirectError(w, r)
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		s.oidcRedirectError(w, r)
		return
	}
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	if len(name) > 120 || !utf8.ValidString(name) {
		name = strings.SplitN(email, "@", 2)[0]
	}
	sentinel, err := token()
	if err != nil {
		s.oidcRedirectError(w, r)
		return
	}
	identity := store.OIDCIdentity{
		Provider: provider, Subject: claims.Subject, Email: email, Name: name,
		Password: []byte("oidc-only:" + sentinel), AutoCreate: autoCreate,
	}
	var user store.User
	if organizationID != "" {
		user, err = s.db.LoginSocialApp(r.Context(), organizationID, identity)
	} else {
		user, err = s.db.LoginOIDC(r.Context(), identity)
	}
	if errors.Is(err, store.ErrNotFound) {
		s.oidcRedirectErrorCode(w, r, "no-account")
		return
	}
	if err != nil {
		slog.Error("OIDC account linking failed", "app", app)
		s.oidcRedirectError(w, r)
		return
	}
	if _, err = s.establishSession(w, r, user); err != nil {
		slog.Error("OIDC session creation failed", "app", app)
		s.oidcRedirectError(w, r)
		return
	}
	http.Redirect(w, r, s.cfg.Prefix+"/", http.StatusSeeOther)
}

// socialApp resolves a public application by its stable provider identity.
func (s *Server) socialApp(r *http.Request) (store.SocialApp, *oidcClient, bool) {
	app, err := s.db.SocialAppByProviderID(r.Context(), r.PathValue("app"))
	if err != nil {
		return store.SocialApp{}, nil, false
	}
	secret, err := s.social.open(app.ClientSecret)
	if err != nil {
		slog.Error("social application secret is unreadable", "app", app.ProviderID)
		return store.SocialApp{}, nil, false
	}
	client := newOIDCClient(app.ServerURL, app.ClientID, string(secret), s.socialCallbackURL(app.ProviderID), oidcDefaultScopes)
	return app, client, true
}

func (s *Server) socialStart(w http.ResponseWriter, r *http.Request) {
	app, client, ok := s.socialApp(r)
	if !ok {
		s.oidcRedirectError(w, r)
		return
	}
	s.startOIDCFlow(w, r, client, app.ProviderID)
}

func (s *Server) socialCallback(w http.ResponseWriter, r *http.Request) {
	app, client, ok := s.socialApp(r)
	if !ok {
		s.oidcRedirectError(w, r)
		return
	}
	s.finishOIDCFlow(w, r, client, app.ProviderID, "social:"+app.ProviderID, true, app.OrganizationID)
}

// socialLoginPage sends a visitor to the workspace sign-in view focused on one
// organization's applications.
type publicSSOApp struct {
	Name     string `json:"name"`
	Brand    string `json:"brand"`
	StartURL string `json:"startUrl"`
}

func (s *Server) socialLoginPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("org")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	if _, _, err := s.db.PublicOrganizationApps(r.Context(), slug); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		report(w, err)
		return
	}
	http.Redirect(w, r, s.cfg.Prefix+"/?sso="+url.QueryEscape(slug), http.StatusFound)
}

func (s *Server) publicOrganizationSSO(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		failure(w, 400, "SSO discovery accepts no query parameters")
		return
	}
	organization, apps, err := s.db.PublicOrganizationApps(r.Context(), r.PathValue("org"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 404, "Not found")
			return
		}
		report(w, err)
		return
	}
	views := make([]publicSSOApp, 0, len(apps))
	for _, app := range apps {
		views = append(views, publicSSOApp{Name: app.Name, Brand: app.Brand, StartURL: s.socialStartURL(app.ProviderID)})
	}
	writeJSON(w, 200, map[string]any{
		"organization": map[string]string{"slug": organization.Slug, "name": organization.Name},
		"apps":         views,
	})
}

func (s *Server) readOIDCFlow(r *http.Request) (oidcFlow, bool) {
	cookie, err := r.Cookie(oidcFlowCookie)
	if err != nil {
		return oidcFlow{}, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(s.hash(parts[0]))) {
		return oidcFlow{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oidcFlow{}, false
	}
	var flow oidcFlow
	if json.Unmarshal(raw, &flow) != nil || flow.State == "" || flow.Nonce == "" || flow.Verifier == "" ||
		flow.Expires < time.Now().Unix() || flow.Expires > time.Now().Add(11*time.Minute).Unix() {
		return oidcFlow{}, false
	}
	return flow, true
}

func (s *Server) clearOIDCFlow(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcFlowCookie, Path: s.cfg.Prefix + "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.Domain, "https://"),
	})
}

func (s *Server) oidcRedirectError(w http.ResponseWriter, r *http.Request) {
	s.oidcRedirectErrorCode(w, r, "failed")
}

func (s *Server) oidcRedirectErrorCode(w http.ResponseWriter, r *http.Request, code string) {
	target := s.cfg.Prefix + "/?authError=" + url.QueryEscape(code)
	http.Redirect(w, r, target, http.StatusSeeOther)
}
