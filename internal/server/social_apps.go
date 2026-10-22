package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/store"
)

const (
	socialAppNameMax   = 40
	socialAppClientMax = 191
	socialAppIssuerMax = 500
	socialGoogleIssuer = "https://accounts.google.com"
)

// compatibleSocialApp is the published organization application shape. Every
// choice runs on the OpenID Connect machinery, so provider is constant and
// brand records the requested preset.
type compatibleSocialApp struct {
	ID          int64   `json:"id"`
	Provider    string  `json:"provider"`
	ProviderID  string  `json:"providerID"`
	Name        string  `json:"name"`
	ClientID    string  `json:"clientID"`
	ServerURL   *string `json:"serverUrl"`
	CallbackURL *string `json:"callbackUrl"`
	LoginURL    string  `json:"loginUrl"`
	Brand       string  `json:"brand"`
}

type socialAppInput struct {
	Name         string  `json:"name"`
	ClientID     string  `json:"clientID"`
	ClientSecret *string `json:"clientSecret"`
	Provider     string  `json:"provider"`
	ServerURL    *string `json:"serverUrl"`
}

func (s *Server) registerSocialAppRoutes(mux *routeMux) {
	root := "/api/0/organizations/{org}/social-apps/"
	mux.Handle("GET "+root+"{$}", s.compatible("org:read", s.compatibleSocialApps))
	mux.Handle("POST "+root+"{$}", s.compatible("org:write", s.compatibleCreateSocialApp))
	mux.Handle("GET "+root+"{app}/{$}", s.compatible("org:read", s.compatibleSocialApp))
	mux.Handle("PUT "+root+"{app}/{$}", s.compatible("org:write", s.compatibleUpdateSocialApp))
	mux.Handle("DELETE "+root+"{app}/{$}", s.compatible("org:write", s.compatibleDeleteSocialApp))
}

func (s *Server) socialAppView(orgSlug string, app store.SocialApp) compatibleSocialApp {
	serverURL, callback := app.ServerURL, s.socialCallbackURL(app.ProviderID)
	view := compatibleSocialApp{
		Provider: "openid_connect", ProviderID: app.ProviderID, Name: app.Name, ClientID: app.ClientID,
		ServerURL: &serverURL, CallbackURL: &callback, LoginURL: s.socialLoginURL(orgSlug), Brand: app.Brand,
	}
	if id, err := parsePositiveInt64Value(app.ID); err == nil {
		view.ID = id
	}
	return view
}

func (s *Server) socialCallbackURL(providerID string) string {
	return strings.TrimRight(s.cfg.Domain, "/") + "/auth/social/" + providerID + "/callback"
}

func (s *Server) socialLoginURL(orgSlug string) string {
	return strings.TrimRight(s.cfg.Domain, "/") + "/login/" + url.PathEscape(orgSlug)
}

func (s *Server) socialStartURL(providerID string) string {
	return strings.TrimRight(s.cfg.Domain, "/") + "/auth/social/" + providerID + "/start"
}

func parsePositiveInt64Value(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("empty identifier")
	}
	var parsed int64
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, errors.New("invalid identifier")
		}
		parsed = parsed*10 + int64(value[index]-'0')
		if parsed <= 0 {
			return 0, errors.New("invalid identifier")
		}
	}
	return parsed, nil
}

func (s *Server) compatibleSocialApps(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	apps, err := s.db.SocialApps(r.Context(), access.OrganizationID)
	if err != nil {
		report(w, err)
		return
	}
	views := make([]compatibleSocialApp, 0, len(apps))
	for _, app := range apps {
		views = append(views, s.socialAppView(r.PathValue("org"), app))
	}
	writeJSON(w, 200, views)
}

func (s *Server) compatibleSocialApp(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	if _, err := parsePositiveInt64Value(r.PathValue("app")); err != nil {
		failure(w, 400, "Application ID must be a positive decimal integer")
		return
	}
	app, err := s.db.SocialApp(r.Context(), access.OrganizationID, r.PathValue("app"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 404, "Not found")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, 200, s.socialAppView(r.PathValue("org"), app))
}

func compatibleSocialText(value string, max int) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl) && utf8.RuneCountInString(value) <= max
}

// compatibleSocialIssuer validates the published https-only server URL rule.
func compatibleSocialIssuer(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > socialAppIssuerMax {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", false
	}
	return strings.TrimRight(trimmed, "/"), true
}

func compatibleSocialBrand(value string) (string, bool) {
	switch value {
	case "", "openid_connect":
		return "openid_connect", true
	case "google":
		return "google", true
	default:
		return "", false
	}
}

func (s *Server) compatibleCreateSocialApp(w http.ResponseWriter, r *http.Request) {
	access, valid := s.orgWriteAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	var input socialAppInput
	if !bodyJSON(w, r, &input) {
		return
	}
	brand, valid := compatibleSocialBrand(input.Provider)
	if !valid {
		failure(w, 400, "Provider must be openid_connect or google")
		return
	}
	if !compatibleSocialText(input.Name, socialAppNameMax) || strings.TrimSpace(input.Name) == "" {
		failure(w, 400, "Name must contain 1 to 40 characters without controls")
		return
	}
	if input.ClientID == "" || len(input.ClientID) > socialAppClientMax || !compatibleSocialText(input.ClientID, socialAppClientMax) {
		failure(w, 400, "Client ID must contain 1 to 191 characters without controls")
		return
	}
	if input.ClientSecret == nil || *input.ClientSecret == "" || len(*input.ClientSecret) > socialAppClientMax || !compatibleSocialText(*input.ClientSecret, socialAppClientMax) {
		failure(w, 400, "Client secret must contain 1 to 191 characters without controls")
		return
	}
	issuer := socialGoogleIssuer
	if input.ServerURL != nil && strings.TrimSpace(*input.ServerURL) != "" {
		var valid bool
		if issuer, valid = compatibleSocialIssuer(*input.ServerURL); !valid {
			failure(w, 400, "Server URL must be an https:// URL")
			return
		}
	} else if brand == "openid_connect" {
		failure(w, 400, "Server URL is required for OpenID Connect applications")
		return
	}
	sealed, err := s.social.seal([]byte(*input.ClientSecret))
	if err != nil {
		report(w, err)
		return
	}
	var app store.SocialApp
	for attempt := 0; attempt < 5; attempt++ {
		providerID, err := socialProviderID(r.PathValue("org"))
		if err != nil {
			report(w, err)
			return
		}
		app, err = s.db.CreateSocialApp(r.Context(), access.OrganizationID, store.SocialAppInput{
			ProviderID: providerID, Provider: "openid_connect", Brand: brand, Name: strings.TrimSpace(input.Name),
			ClientID: input.ClientID, ClientSecret: sealed, ServerURL: issuer,
		})
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		if err != nil {
			report(w, err)
			return
		}
		break
	}
	if app.ID == "" {
		failure(w, 503, "Could not allocate an application identity; retry shortly")
		return
	}
	writeJSON(w, 201, s.socialAppView(r.PathValue("org"), app))
}

func socialProviderID(orgSlug string) (string, error) {
	raw := make([]byte, 3)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	prefix := strings.Trim(orgSlug, "-")
	if len(prefix) > 48 {
		prefix = strings.TrimRight(prefix[:48], "-")
	}
	return prefix + "-" + hex.EncodeToString(raw), nil
}

func (s *Server) compatibleUpdateSocialApp(w http.ResponseWriter, r *http.Request) {
	access, valid := s.orgWriteAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	if _, err := parsePositiveInt64Value(r.PathValue("app")); err != nil {
		failure(w, 400, "Application ID must be a positive decimal integer")
		return
	}
	var input socialAppInput
	if !bodyJSON(w, r, &input) {
		return
	}
	if input.Provider != "" {
		failure(w, 400, "Provider cannot change after creation")
		return
	}
	if !compatibleSocialText(input.Name, socialAppNameMax) || strings.TrimSpace(input.Name) == "" {
		failure(w, 400, "Name must contain 1 to 40 characters without controls")
		return
	}
	if input.ClientID == "" || len(input.ClientID) > socialAppClientMax || !compatibleSocialText(input.ClientID, socialAppClientMax) {
		failure(w, 400, "Client ID must contain 1 to 191 characters without controls")
		return
	}
	var sealed []byte
	if input.ClientSecret != nil && *input.ClientSecret != "" {
		if len(*input.ClientSecret) > socialAppClientMax || !compatibleSocialText(*input.ClientSecret, socialAppClientMax) {
			failure(w, 400, "Client secret must contain at most 191 characters without controls")
			return
		}
		var err error
		if sealed, err = s.social.seal([]byte(*input.ClientSecret)); err != nil {
			report(w, err)
			return
		}
	}
	issuer := ""
	if input.ServerURL != nil && strings.TrimSpace(*input.ServerURL) != "" {
		var ok bool
		if issuer, ok = compatibleSocialIssuer(*input.ServerURL); !ok {
			failure(w, 400, "Server URL must be an https:// URL")
			return
		}
	}
	app, err := s.db.UpdateSocialApp(r.Context(), access.OrganizationID, r.PathValue("app"), strings.TrimSpace(input.Name), input.ClientID, sealed, issuer)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 404, "Not found")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, 200, s.socialAppView(r.PathValue("org"), app))
}

func (s *Server) compatibleDeleteSocialApp(w http.ResponseWriter, r *http.Request) {
	access, valid := s.orgWriteAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	if _, err := parsePositiveInt64Value(r.PathValue("app")); err != nil {
		failure(w, 400, "Application ID must be a positive decimal integer")
		return
	}
	if err := s.db.DeleteSocialApp(r.Context(), access.OrganizationID, r.PathValue("app")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 404, "Not found")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
