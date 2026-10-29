//go:build integration

package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
	"goodsentry/internal/config"
)

type forcedExpirySource struct {
	mu      sync.Mutex
	ctx     context.Context
	config  *oauth2.Config
	current oauth2.TokenSource
}

func (s *forcedExpirySource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.Token()
}

func (s *forcedExpirySource) expire() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, err := s.current.Token()
	if err != nil {
		return err
	}
	expired := *token
	expired.Expiry = time.Now().Add(-time.Hour)
	s.current = s.config.TokenSource(s.ctx, &expired)
	return nil
}

type oauthObservedTransport struct{ t *testing.T }

func (o oauthObservedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(req)
	if err == nil {
		o.t.Logf("OAuth %s %s: %d", req.Method, req.URL.Path, response.StatusCode)
	}
	return response, err
}

func TestOfficialMCPAuthorizationCodeHandler(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	if _, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Excluded OAuth project", "native"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String() + "/good"
	cfg := config.Config{MCP: true, Logs: true, Domain: base, Prefix: "/good", Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
	srv.Config.Handler = New(cfg, f.db)
	srv.Start()
	t.Cleanup(srv.Close)
	f.server = srv
	redirect := base + "/oauth/callback"
	var source *forcedExpirySource
	var clientID string
	allowConsent := true
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		RedirectURL: redirect,
		Client:      &http.Client{Transport: oauthObservedTransport{t}},
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Official MCP OAuth client", RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: "none",
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
		}},
		NewTokenSource: func(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			source = &forcedExpirySource{ctx: ctx, config: cfg, current: cfg.TokenSource(ctx, token)}
			return source, nil
		},
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			if !allowConsent {
				return nil, fmt.Errorf("consent denied")
			}
			u, err := url.Parse(args.URL)
			if err != nil {
				return nil, err
			}
			get, err := f.client.Get(args.URL)
			if err != nil {
				return nil, err
			}
			get.Body.Close()
			if get.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("consent page status %d", get.StatusCode)
			}
			form := u.Query()
			clientID = form.Get("client_id")
			form.Set("csrf", f.csrf)
			form.Set("organization", f.org.Slug)
			form.Set("project", f.project.ID)
			form.Set("decision", "approve")
			client := &http.Client{Jar: f.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, _ := oauthForm(t, client, base+"/oauth/authorize", form)
			if response.StatusCode != http.StatusSeeOther {
				return nil, fmt.Errorf("consent status %d", response.StatusCode)
			}
			callback, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				return nil, err
			}
			return &auth.AuthorizationResult{Code: callback.Query().Get("code"), State: callback.Query().Get("state"), Iss: callback.Query().Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "Official OAuth acceptance", Version: "1"}, nil)
	session, err := client.Connect(f.ctx, &mcp.StreamableClientTransport{Endpoint: base + "/mcp", OAuthHandler: handler, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("official OAuth connection: %v", err)
	}
	defer session.Close()
	page := mcpWorkspaceCall(t, f, session, "list_organizations", map[string]any{})
	if len(page.Data) != 1 {
		t.Fatalf("authorized organizations=%d, want one", len(page.Data))
	}
	projects := mcpWorkspaceCall(t, f, session, "list_projects", map[string]any{"organization": f.org.ID})
	if len(projects.Data) != 1 || projects.Data[0]["id"] != f.project.ID {
		t.Fatalf("selected-project grant: %+v", projects.Data)
	}
	if source == nil || clientID == "" {
		t.Fatal("handler did not construct its token source")
	}
	first, err := source.Token()
	if err != nil || first.RefreshToken == "" {
		t.Fatalf("initial token source: %v", err)
	}
	oldAccess, oldRefresh := first.AccessToken, first.RefreshToken
	if err := source.expire(); err != nil {
		t.Fatal(err)
	}
	page = mcpWorkspaceCall(t, f, session, "list_organizations", map[string]any{})
	if len(page.Data) != 1 {
		t.Fatalf("organizations after refresh=%d", len(page.Data))
	}
	projects = mcpWorkspaceCall(t, f, session, "list_projects", map[string]any{"organization": f.org.ID})
	if len(projects.Data) != 1 || projects.Data[0]["id"] != f.project.ID {
		t.Fatalf("project grant after refresh: %+v", projects.Data)
	}
	rotated, err := source.Token()
	if err != nil || rotated.AccessToken == oldAccess || rotated.RefreshToken == "" || rotated.RefreshToken == oldRefresh {
		t.Fatalf("official refresh did not rotate both credentials: %v", err)
	}
	response, _ := oauthForm(t, f.client, base+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {oldRefresh}})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh replay status=%d", response.StatusCode)
	}
	request, err := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+oldAccess)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old access survived refresh: %d", response.StatusCode)
	}
	tokens, err := f.db.APITokens(f.ctx, f.owner.ID, f.org.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	var activeID string
	for _, token := range tokens.Data {
		if token.Name == "OAuth Official MCP OAuth client" && token.RevokedAt == nil {
			activeID = token.ID
		}
	}
	if activeID == "" {
		t.Fatal("active official OAuth grant missing")
	}
	f.request(t, f.client, http.MethodDelete, "/api/v1/organizations/"+f.org.Slug+"/api-tokens/"+activeID, "", http.Header{"X-CSRF-Token": {f.csrf}}, http.StatusNoContent)
	allowConsent = false
	if _, err := session.ListTools(f.ctx, nil); err == nil {
		t.Fatal("revoked grant remained usable")
	}
}

func TestMCPIssuerMetadataOriginPath(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	for _, tc := range []struct {
		name    string
		prefix  string
		enabled bool
	}{
		{"nested prefix", "/team/good", true},
		{"no prefix", "", true},
		{"disabled", "/team/good", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{MCP: tc.enabled, Domain: "https://issuer.example" + tc.prefix, Prefix: tc.prefix, Secret: strings.Repeat("r", 64), MaxBodyBytes: 1 << 20}
			srv := httptest.NewServer(New(cfg, f.db))
			defer srv.Close()
			path := "/.well-known/oauth-authorization-server" + tc.prefix
			response, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !tc.enabled {
				if response.StatusCode != http.StatusNotFound {
					t.Fatalf("disabled metadata=%d", response.StatusCode)
				}
				return
			}
			if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(data), cfg.Domain+"/oauth/register") {
				t.Fatalf("metadata status=%d cache=%q body=%s", response.StatusCode, response.Header.Get("Cache-Control"), data)
			}
			head, err := http.Head(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			head.Body.Close()
			if head.StatusCode != http.StatusOK || head.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("HEAD status=%d", head.StatusCode)
			}
			if tc.prefix != "" {
				post, err := http.Post(srv.URL+path, "text/plain", nil)
				if err != nil {
					t.Fatal(err)
				}
				post.Body.Close()
				if post.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("POST metadata=%d", post.StatusCode)
				}
				wrong, err := http.Get(srv.URL + path + "/other")
				if err != nil {
					t.Fatal(err)
				}
				wrong.Body.Close()
				if wrong.StatusCode != http.StatusNotFound {
					t.Fatalf("wrong metadata path=%d", wrong.StatusCode)
				}
			}
		})
	}
}
