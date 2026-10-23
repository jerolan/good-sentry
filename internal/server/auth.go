package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/store"
)

const sessionCookie = "goodsentry_session"

func token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (s *Server) hash(value string) string {
	h := hmac.New(sha256.New, []byte(s.cfg.Secret))
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(s.cfg.Domain)
	if err != nil {
		return false
	}
	return origin == u.Scheme+"://"+u.Host
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthenticationDisabled() {
			if s.defaultUserID == "" {
				failure(w, 503, "Implicit owner is unavailable")
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
				if !s.sameOrigin(r) || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.openCSRF)) {
					failure(w, 403, "Invalid CSRF token or origin")
					return
				}
			}
			user, err := s.db.UserByID(r.Context(), s.defaultUserID)
			if err != nil {
				report(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{User: user, CSRF: s.openCSRF})))
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || len(cookie.Value) != 64 {
			failure(w, 401, "Sign in required")
			return
		}
		u, csrf, err := s.db.Session(r.Context(), s.hash(cookie.Value))
		if err != nil {
			if err == store.ErrNotFound {
				failure(w, 401, "Session expired")
			} else {
				report(w, err)
			}
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if !s.sameOrigin(r) || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) {
				failure(w, 403, "Invalid CSRF token or origin")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{User: u, CSRF: csrf})))
	})
}

func (s *Server) setupState(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuthenticationDisabled() {
		writeJSON(w, 200, map[string]bool{"required": false})
		return
	}
	required, err := s.db.SetupRequired(r.Context())
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"required": required})
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuthenticationDisabled() {
		failure(w, 409, "Authentication is disabled; the implicit owner already exists")
		return
	}
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct{ Email, Password, Name, Organization string }
	if !bodyJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	input.Organization = strings.TrimSpace(input.Organization)
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 || len(input.Password) < 12 || len(input.Password) > 72 || input.Name == "" || len(input.Name) > 120 || input.Organization == "" || len(input.Organization) > 120 {
		failure(w, 400, "Provide a valid email, name, organization and password of 12 to 72 bytes")
		return
	}
	password, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		failure(w, 500, "Could not secure account")
		return
	}
	u, _, err := s.db.Bootstrap(r.Context(), input.Email, password, input.Name, input.Organization)
	if err != nil {
		report(w, err)
		return
	}
	s.startSession(w, r, u, 201)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuthenticationDisabled() {
		failure(w, http.StatusConflict, "Authentication is disabled; the implicit owner is active")
		return
	}
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	var input struct{ Email, Password string }
	if !bodyJSON(w, r, &input) {
		return
	}
	if len(input.Email) > 254 || len(input.Password) > 72 {
		failure(w, 401, "Invalid email or password")
		return
	}
	u, err := s.db.UserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(input.Email)))
	if err != nil || bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(input.Password)) != nil {
		failure(w, 401, "Invalid email or password")
		return
	}
	if !s.requireSecondFactor(w, r, u) {
		return
	}
	s.startSession(w, r, u, 200)
}
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User, status int) {
	csrf, err := s.establishSession(w, r, u)
	if err != nil {
		if errors.Is(err, store.ErrCredentialsChanged) {
			failure(w, 401, "Invalid email or password")
		} else {
			report(w, err)
		}
		return
	}
	s.sendMe(w, r, identity{User: u, CSRF: csrf}, status)
}

func (s *Server) establishSession(w http.ResponseWriter, r *http.Request, u store.User) (string, error) {
	value, err := token()
	if err != nil {
		return "", err
	}
	csrf, err := token()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	if err = s.db.CreateSession(r.Context(), u.ID, u.AuthGeneration, s.hash(value), csrf, expires); err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: s.cfg.Prefix + "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.Domain, "https://"), Expires: expires, MaxAge: 7 * 24 * 3600})
	return csrf, nil
}
func (s *Server) sendMe(w http.ResponseWriter, r *http.Request, i identity, status int) {
	orgs, err := s.db.Organizations(r.Context(), i.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, status, map[string]any{"user": i.User, "organizations": orgs, "csrfToken": i.CSRF, "features": map[string]bool{"logs": s.cfg.Logs, "uptime": s.cfg.Uptime, "alerts": true, "alertDelivery": s.cfg.AlertDelivery, "mcp": s.cfg.MCP, "openapi": s.cfg.OpenAPI, "admin": s.cfg.Admin, "oidc": s.oidc != nil, "passwordRecovery": s.mail != nil && s.account != nil, "authRequired": !s.cfg.AuthenticationDisabled()}})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) { s.sendMe(w, r, current(r), 200) }
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookie)
	if cookie != nil {
		if err := s.db.RevokeSession(r.Context(), s.hash(cookie.Value)); err != nil {
			report(w, err)
			return
		}
	}
	s.clearSessionCookie(w)
	writeJSON(w, 204, nil)
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: s.cfg.Prefix + "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.Domain, "https://")})
}
