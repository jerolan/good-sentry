package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	required, err := s.db.SetupRequired(r.Context())
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"required": required})
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
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
	s.startSession(w, r, u, 200)
}
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User, status int) {
	value, err := token()
	if err != nil {
		failure(w, 500, "Could not create session")
		return
	}
	csrf, err := token()
	if err != nil {
		failure(w, 500, "Could not create session")
		return
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	if err = s.db.CreateSession(r.Context(), u.ID, s.hash(value), csrf, expires); err != nil {
		report(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: s.cfg.Prefix + "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.Domain, "https://"), Expires: expires, MaxAge: 7 * 24 * 3600})
	s.sendMe(w, r, identity{User: u, CSRF: csrf}, status)
}
func (s *Server) sendMe(w http.ResponseWriter, r *http.Request, i identity, status int) {
	orgs, err := s.db.Organizations(r.Context(), i.User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, status, map[string]any{"user": i.User, "organizations": orgs, "csrfToken": i.CSRF, "features": map[string]bool{"logs": s.cfg.Logs, "uptime": s.cfg.Uptime, "alerts": true, "alertDelivery": s.cfg.AlertDelivery}})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) { s.sendMe(w, r, current(r), 200) }
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookie)
	if err := s.db.RevokeSession(r.Context(), s.hash(cookie.Value)); err != nil {
		report(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: s.cfg.Prefix + "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.Domain, "https://")})
	writeJSON(w, 204, nil)
}
