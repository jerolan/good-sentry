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

type oidcAuth struct {
	issuer, clientID, clientSecret, label, redirectURL string
	scopes                                             []string
	autoCreate                                         bool

	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

type oidcFlow struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Expires  int64  `json:"expires"`
}

func newOIDCAuth(cfg config.Config) *oidcAuth {
	if cfg.OIDCIssuer == "" {
		return nil
	}
	scopes := cfg.OIDCScopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	label := cfg.OIDCName
	if label == "" {
		label = "Single sign-on"
	}
	return &oidcAuth{
		issuer: cfg.OIDCIssuer, clientID: cfg.OIDCClientID, clientSecret: cfg.OIDCClientSecret,
		label: label, scopes: scopes, autoCreate: cfg.OIDCAutoCreate,
		redirectURL: cfg.Domain + "/auth/oidc/callback",
	}
}

func (a *oidcAuth) client(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return a.oauth, a.verifier, nil
	}
	provider, err := oidc.NewProvider(ctx, a.issuer)
	if err != nil {
		return nil, nil, err
	}
	a.provider = provider
	a.oauth = &oauth2.Config{
		ClientID: a.clientID, ClientSecret: a.clientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: a.redirectURL, Scopes: a.scopes,
	}
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.clientID})
	return a.oauth, a.verifier, nil
}

func (s *Server) registerOIDCRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/config", s.authConfig)
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ctx = oidc.ClientContext(ctx, &http.Client{Timeout: 10 * time.Second})
	config, _, err := s.oidc.client(ctx)
	if err != nil {
		slog.Error("OIDC discovery failed")
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
	flow := oidcFlow{State: state, Nonce: nonce, Verifier: verifier, Expires: time.Now().Add(10 * time.Minute).Unix()}
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

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	flow, ok := s.readOIDCFlow(r)
	s.clearOIDCFlow(w)
	if !ok {
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
	config, verifier, err := s.oidc.client(ctx)
	if err != nil {
		slog.Error("OIDC discovery failed during callback")
		s.oidcRedirectError(w, r)
		return
	}
	exchange, err := config.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		slog.Error("OIDC code exchange failed")
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
		slog.Error("OIDC ID token verification failed")
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
	user, err := s.db.LoginOIDC(r.Context(), store.OIDCIdentity{
		Provider: s.oidc.issuer, Subject: claims.Subject, Email: email, Name: name,
		Password: []byte("oidc-only:" + sentinel), AutoCreate: s.oidc.autoCreate,
	})
	if errors.Is(err, store.ErrNotFound) {
		s.oidcRedirectErrorCode(w, r, "no-account")
		return
	}
	if err != nil {
		slog.Error("OIDC account linking failed")
		s.oidcRedirectError(w, r)
		return
	}
	if _, err = s.establishSession(w, r, user); err != nil {
		slog.Error("OIDC session creation failed")
		s.oidcRedirectError(w, r)
		return
	}
	http.Redirect(w, r, s.cfg.Prefix+"/", http.StatusSeeOther)
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
