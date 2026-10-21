package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"goodsentry/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerMCPRoutes(mux *http.ServeMux) {
	if !s.cfg.MCP {
		mux.HandleFunc("/mcp", http.NotFound)
		return
	}
	maximum := min(s.cfg.MaxBodyBytes, int64(1<<20))
	if maximum <= 0 {
		maximum = 1 << 20
	}
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		origins := r.Header.Values("Origin")
		if len(origins) > 1 || len(origins) == 1 && origins[0] == "" || !s.sameOrigin(r) {
			failure(w, 403, "Invalid origin")
			return
		}
		values := r.Header.Values("Authorization")
		var parts []string
		if len(values) == 1 {
			parts = strings.Fields(values[0])
		}
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validAPICredential(parts[1]) {
			w.Header().Set("WWW-Authenticate", s.mcpChallenge())
			failure(w, 401, "A valid Bearer credential is required")
			return
		}
		grant, err := s.db.AuthenticateAPIToken(r.Context(), s.hash(parts[1]))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				w.Header().Set("WWW-Authenticate", s.mcpChallenge())
				failure(w, 401, "Credential is invalid, expired or revoked")
			} else {
				failure(w, 503, "Database unavailable")
			}
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			failure(w, 400, "MCP does not accept URL query parameters")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			failure(w, 405, "MCP requires POST")
			return
		}
		// A stateless request never inherits authority from an earlier request.
		// Retain the originating context for legacy transport cancellation too.
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return s.mcpWorkspaceServer(r.Context(), grant)
		}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maximum, PropagateRequestCancellation: true})
		handler.ServeHTTP(mcpNoStoreWriter{w}, r)
	})
}

func (s *Server) mcpChallenge() string {
	challenge := `Bearer resource_metadata="` + s.oauthBase() + `/.well-known/oauth-protected-resource"`
	if scopes := strings.Join(s.oauthScopes(), " "); scopes != "" {
		challenge += `, scope="` + scopes + `"`
	}
	return challenge
}

// The SDK sets transport cache headers itself; enforce the authenticated
// endpoint's storage policy when those headers actually reach the wire.
type mcpNoStoreWriter struct{ http.ResponseWriter }

func (w mcpNoStoreWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(status)
}

func (w mcpNoStoreWriter) Write(body []byte) (int, error) {
	w.Header().Set("Cache-Control", "no-store")
	return w.ResponseWriter.Write(body)
}

func (w mcpNoStoreWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type mcpWorkspaceInput struct {
	Organization string `json:"organization,omitempty"`
	Query        string `json:"query,omitempty"`
	Page         int    `json:"page"`
	Limit        int    `json:"limit"`
}

type mcpWorkspaceResult[T any] struct {
	Data     []T  `json:"data"`
	Page     int  `json:"page"`
	PageSize int  `json:"pageSize"`
	HasMore  bool `json:"hasMore"`
}

func mcpWorkspacePage[T any](rows []T, input mcpWorkspaceInput) mcpWorkspaceResult[T] {
	more := len(rows) > input.Limit
	if more {
		rows = rows[:input.Limit]
	}
	return mcpWorkspaceResult[T]{Data: rows, Page: input.Page, PageSize: input.Limit, HasMore: more}
}

func mcpWorkspaceSchema(projects bool) map[string]any {
	properties := map[string]any{
		"query": map[string]any{"type": "string", "maxLength": 200},
		"page":  map[string]any{"type": "integer", "minimum": 1, "maximum": 1000000, "default": 1},
		"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 25},
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if projects {
		properties["organization"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "description": "Organization ID or slug"}
		schema["required"] = []string{"organization"}
	}
	return schema
}

func (s *Server) mcpWorkspaceServer(httpContext context.Context, grant store.APITokenGrant) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "GoodSentry", Version: "0.0.0-dev"}, nil)
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_organizations", Description: "List organizations authorized by this credential. Supports query and pagination.", InputSchema: mcpWorkspaceSchema(false), Annotations: annotations,
	}, func(_ context.Context, _ *mcp.CallToolRequest, input mcpWorkspaceInput) (*mcp.CallToolResult, mcpWorkspaceResult[store.CompatibleOrganization], error) {
		var result mcpWorkspaceResult[store.CompatibleOrganization]
		if !slices.Contains(grant.Scopes, "org:read") {
			return nil, result, errors.New("Credential requires org:read")
		}
		if !credentialText(input.Query, 200) {
			return nil, result, errors.New("Invalid query")
		}
		rows, err := s.db.CompatibleOrganizations(httpContext, grant.User.ID, grant.OrganizationID, input.Query, false, "name", (input.Page-1)*input.Limit, input.Limit)
		if err != nil {
			return nil, result, errors.New("Organizations unavailable")
		}
		return nil, mcpWorkspacePage(rows, input), nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_projects", Description: "List permitted project metadata in an authorized organization. Supports query and pagination.", InputSchema: mcpWorkspaceSchema(true), Annotations: annotations,
	}, func(_ context.Context, _ *mcp.CallToolRequest, input mcpWorkspaceInput) (*mcp.CallToolResult, mcpWorkspaceResult[store.ManagementProject], error) {
		var result mcpWorkspaceResult[store.ManagementProject]
		if !slices.Contains(grant.Scopes, "project:read") {
			return nil, result, errors.New("Credential requires project:read")
		}
		if !credentialText(input.Query, 200) || !credentialText(input.Organization, 200) {
			return nil, result, errors.New("Invalid query or organization")
		}
		if err := s.mcpOrganization(httpContext, grant, input.Organization); err != nil {
			return nil, result, err
		}
		access := store.ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs, ReadOnly: true}
		rows, err := s.db.ManagementProjects(httpContext, access, input.Query, (input.Page-1)*input.Limit, input.Limit)
		if err != nil {
			return nil, result, errors.New("Projects unavailable")
		}
		return nil, mcpWorkspacePage(rows, input), nil
	})
	s.addMCPIssueTools(server, httpContext, grant)
	s.addMCPAlertMonitorTools(server, httpContext, grant)
	s.addMCPPerformanceTools(server, httpContext, grant)
	if s.cfg.Logs {
		s.addMCPLogTools(server, httpContext, grant)
	}
	return server
}
