package server

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/oauth2"

	"goodsentry/internal/store"
)

const socialOAuth2ResponseMax = 1 << 20

type socialOAuth2Provider struct {
	config *oauth2.Config
	kind   string
	apiURL string
}

func isSocialOAuth2Provider(provider string) bool {
	switch provider {
	case "github", "gitlab", "gitea":
		return true
	default:
		return false
	}
}

func (s *Server) socialOAuth2Provider(app store.SocialApp, secret string) (socialOAuth2Provider, bool) {
	base, err := url.Parse(strings.TrimSpace(app.ServerURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return socialOAuth2Provider{}, false
	}
	base.Host = strings.ToLower(base.Host)
	base.Path = strings.TrimRight(base.Path, "/")
	callback := s.socialCallbackURL(app.ProviderID)
	provider := socialOAuth2Provider{kind: app.Provider}
	switch app.Provider {
	case "github":
		provider.apiURL = "https://api.github.com"
		if !strings.EqualFold(base.Host, "github.com") || base.Path != "" {
			provider.apiURL = base.String() + "/api/v3"
		}
		provider.config = &oauth2.Config{
			ClientID: app.ClientID, ClientSecret: secret, RedirectURL: callback,
			Endpoint: oauth2.Endpoint{
				AuthURL: base.String() + "/login/oauth/authorize", TokenURL: base.String() + "/login/oauth/access_token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes: []string{"read:user", "user:email"},
		}
	case "gitlab":
		provider.apiURL = base.String()
		provider.config = &oauth2.Config{
			ClientID: app.ClientID, ClientSecret: secret, RedirectURL: callback,
			Endpoint: oauth2.Endpoint{
				AuthURL: base.String() + "/oauth/authorize", TokenURL: base.String() + "/oauth/token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes: []string{"read_user"},
		}
	case "gitea":
		provider.apiURL = base.String()
		provider.config = &oauth2.Config{
			ClientID: app.ClientID, ClientSecret: secret, RedirectURL: callback,
			Endpoint: oauth2.Endpoint{
				AuthURL: base.String() + "/login/oauth/authorize", TokenURL: base.String() + "/login/oauth/access_token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes: []string{"read:user"},
		}
	default:
		return socialOAuth2Provider{}, false
	}
	if app.ClientID == "" || secret == "" {
		return socialOAuth2Provider{}, false
	}
	return provider, true
}

func (s *Server) startSocialOAuth2Flow(w http.ResponseWriter, r *http.Request, app store.SocialApp, secret string) {
	provider, ok := s.socialOAuth2Provider(app, secret)
	if !ok {
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
	flow := oidcFlow{State: state, Nonce: nonce, Verifier: verifier, App: app.ProviderID, Expires: time.Now().Add(10 * time.Minute).Unix()}
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
	http.Redirect(w, r, provider.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Server) finishSocialOAuth2Flow(w http.ResponseWriter, r *http.Request, app store.SocialApp, secret string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	flow, ok := s.readOIDCFlow(r)
	s.clearOIDCFlow(w)
	if !ok || flow.App != app.ProviderID || r.URL.Query().Get("error") != "" ||
		!hmac.Equal([]byte(r.URL.Query().Get("state")), []byte(flow.State)) {
		s.oidcRedirectError(w, r)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 8192 {
		s.oidcRedirectError(w, r)
		return
	}
	provider, ok := s.socialOAuth2Provider(app, secret)
	if !ok {
		s.oidcRedirectError(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 12 * time.Second})
	tokenValue, err := provider.config.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		slog.Error("social OAuth2 code exchange failed", "provider", app.Provider, "app", app.ProviderID)
		s.oidcRedirectError(w, r)
		return
	}
	profile, err := fetchSocialOAuth2Profile(ctx, provider, provider.config.Client(ctx, tokenValue))
	if err != nil {
		slog.Error("social OAuth2 profile failed", "provider", app.Provider, "app", app.ProviderID, "error", err)
		s.oidcRedirectError(w, r)
		return
	}
	sentinel, err := token()
	if err != nil {
		s.oidcRedirectError(w, r)
		return
	}
	identity := store.OIDCIdentity{
		Provider: "social:" + app.ProviderID, Subject: profile.subject, Email: profile.email, Name: profile.name,
		Password: []byte("oauth2-only:" + sentinel), AutoCreate: true,
	}
	user, err := s.db.LoginSocialApp(ctx, app.OrganizationID, identity)
	if err != nil {
		slog.Error("social OAuth2 account linking failed", "provider", app.Provider, "app", app.ProviderID)
		s.oidcRedirectError(w, r)
		return
	}
	if _, err = s.establishSession(w, r, user); err != nil {
		slog.Error("social OAuth2 session creation failed", "provider", app.Provider, "app", app.ProviderID)
		s.oidcRedirectError(w, r)
		return
	}
	http.Redirect(w, r, s.cfg.Prefix+"/", http.StatusSeeOther)
}

type socialOAuth2ProfileValue struct {
	subject string
	email   string
	name    string
}

func fetchSocialOAuth2Profile(ctx context.Context, provider socialOAuth2Provider, client *http.Client) (socialOAuth2ProfileValue, error) {
	switch provider.kind {
	case "github":
		var user struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Name  string `json:"name"`
		}
		if err := fetchSocialOAuth2JSON(ctx, client, provider.apiURL+"/user", &user); err != nil {
			return socialOAuth2ProfileValue{}, err
		}
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := fetchSocialOAuth2JSON(ctx, client, provider.apiURL+"/user/emails", &emails); err != nil {
			return socialOAuth2ProfileValue{}, err
		}
		email := ""
		for _, candidate := range emails {
			if candidate.Verified && (candidate.Primary || email == "") {
				email = candidate.Email
			}
		}
		return validSocialOAuth2Profile(user.ID, email, user.Name, user.Login)
	case "gitlab":
		var user struct {
			ID          int64  `json:"id"`
			Username    string `json:"username"`
			Name        string `json:"name"`
			Email       string `json:"email"`
			State       string `json:"state"`
			ConfirmedAt string `json:"confirmed_at"`
		}
		if err := fetchSocialOAuth2JSON(ctx, client, provider.apiURL+"/api/v4/user", &user); err != nil {
			return socialOAuth2ProfileValue{}, err
		}
		if user.State != "active" || user.ConfirmedAt == "" {
			return socialOAuth2ProfileValue{}, errors.New("provider account is not active and confirmed")
		}
		return validSocialOAuth2Profile(user.ID, user.Email, user.Name, user.Username)
	case "gitea":
		var user struct {
			ID       int64  `json:"id"`
			Login    string `json:"login"`
			FullName string `json:"full_name"`
			Email    string `json:"email"`
			Active   bool   `json:"active"`
		}
		if err := fetchSocialOAuth2JSON(ctx, client, provider.apiURL+"/api/v1/user", &user); err != nil {
			return socialOAuth2ProfileValue{}, err
		}
		if !user.Active {
			return socialOAuth2ProfileValue{}, errors.New("provider account is inactive")
		}
		return validSocialOAuth2Profile(user.ID, user.Email, user.FullName, user.Login)
	default:
		return socialOAuth2ProfileValue{}, errors.New("unsupported social OAuth2 provider")
	}
}

func validSocialOAuth2Profile(id int64, email, name, fallback string) (socialOAuth2ProfileValue, error) {
	if id <= 0 {
		return socialOAuth2ProfileValue{}, errors.New("provider returned an invalid identity")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return socialOAuth2ProfileValue{}, errors.New("provider returned no valid verified email")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(fallback)
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	if len(name) > 120 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		name = strings.SplitN(email, "@", 2)[0]
	}
	return socialOAuth2ProfileValue{subject: strconv.FormatInt(id, 10), email: email, name: name}, nil
}

func fetchSocialOAuth2JSON(ctx context.Context, client *http.Client, endpoint string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, socialOAuth2ResponseMax+1))
	if err != nil {
		return err
	}
	if len(data) > socialOAuth2ResponseMax {
		return errors.New("provider response exceeds limit")
	}
	return json.Unmarshal(data, destination)
}
