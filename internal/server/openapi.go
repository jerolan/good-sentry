package server

import (
	"encoding/json"
	"html/template"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const goodsentryVersion = "0.0.0-dev"

var openAPIPathParameter = regexp.MustCompile(`\{([^{}]+)\}`)

func (s *Server) registerOpenAPIRoutes(mux *routeMux) {
	routes := append([]routeRegistration(nil), mux.routes...)
	document := buildOpenAPIDocument(routes, strings.TrimRight(s.cfg.Domain, "/"))
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		panic("marshal embedded OpenAPI document: " + err.Error())
	}
	page := openAPIDocsPage{Base: s.cfg.Prefix, Routes: append([]routeRegistration(nil), routes...)}
	sort.Slice(page.Routes, func(i, j int) bool {
		if page.Routes[i].Path == page.Routes[j].Path {
			return page.Routes[i].Method < page.Routes[j].Method
		}
		return page.Routes[i].Path < page.Routes[j].Path
	})
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1.0")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.cfg.Prefix+"/api/docs/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /api/docs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := openAPIDocsTemplate.Execute(w, page); err != nil {
			failure(w, 500, "API documentation could not be rendered")
		}
	})
}

func buildOpenAPIDocument(routes []routeRegistration, base string) map[string]any {
	paths := map[string]map[string]any{}
	for index, route := range routes {
		parameters := []map[string]any{}
		for _, match := range openAPIPathParameter.FindAllStringSubmatch(route.Path, -1) {
			parameters = append(parameters, map[string]any{
				"name": match[1], "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			})
		}
		operation := map[string]any{
			"operationId":                 openAPIOperationID(route.Method, route.Path, index),
			"summary":                     route.Method + " " + route.Path,
			"tags":                        []string{openAPITag(route.Path)},
			"x-goodsentry-authentication": openAPIAuthentication(route.Method, route.Path),
			"responses": map[string]any{
				"200":     map[string]any{"description": "Successful response"},
				"default": map[string]any{"$ref": "#/components/responses/Error"},
			},
		}
		if len(parameters) > 0 {
			operation["parameters"] = parameters
		}
		if paths[route.Path] == nil {
			paths[route.Path] = map[string]any{}
		}
		paths[route.Path][strings.ToLower(route.Method)] = operation
	}
	tags := []map[string]string{
		{"name": "Sentry compatibility", "description": "Sentry- and GlitchTip-compatible management, ingestion and client routes."},
		{"name": "GoodSentry", "description": "Native application API used by the embedded interface."},
		{"name": "Accounts and sign-in", "description": "Setup, sessions, recovery, OpenID Connect and OAuth."},
		{"name": "Operations", "description": "Health, readiness, MCP and operator-facing routes."},
		{"name": "Other", "description": "Other registered HTTP routes."},
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "GoodSentry API",
			"version":     goodsentryVersion,
			"description": "Routes registered by this GoodSentry build. Authentication and capability details are recorded in docs/compatibility.md and docs/configuration.md.",
		},
		"servers":                  []map[string]string{{"url": base, "description": "Configured public origin"}},
		"tags":                     tags,
		"paths":                    paths,
		"x-goodsentry-route-count": len(routes),
		"components": map[string]any{
			"schemas": map[string]any{
				"Error": map[string]any{"type": "object", "required": []string{"detail"}, "properties": map[string]any{"detail": map[string]string{"type": "string"}}},
			},
			"responses": map[string]any{
				"Error": map[string]any{
					"description": "The request was rejected or storage was unavailable",
					"content":     map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/Error"}}},
				},
			},
			"securitySchemes": map[string]any{
				"sessionCookie": map[string]string{"type": "apiKey", "in": "cookie", "name": sessionCookie},
				"bearerAuth":    map[string]string{"type": "http", "scheme": "bearer"},
			},
		},
	}
}

func openAPIOperationID(method, path string, index int) string {
	id := strings.ToLower(method) + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)
	id = strings.Trim(id, "_")
	if id == "" {
		id = "route"
	}
	return id + "_" + strconv.Itoa(index)
}

func openAPITag(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/0/"), strings.HasPrefix(path, "/api/{project}/"):
		return "Sentry compatibility"
	case strings.HasPrefix(path, "/api/v1/"), strings.HasPrefix(path, "/api/"):
		return "GoodSentry"
	case strings.HasPrefix(path, "/oauth/"), strings.HasPrefix(path, "/auth/"), strings.HasPrefix(path, "/account/"), path == "/api/v1/setup", path == "/api/v1/login":
		return "Accounts and sign-in"
	case path == "/healthz", path == "/readyz", path == "/mcp":
		return "Operations"
	default:
		return "Other"
	}
}

func openAPIAuthentication(method, path string) string {
	if openAPIIsPublic(method, path) {
		return "public"
	}
	if strings.HasPrefix(path, "/oauth/") {
		return "OAuth client credentials or a signed-in account, as documented by the route"
	}
	if strings.HasPrefix(path, "/api/0/") {
		return "signed-in account session or scoped Bearer API token; current membership and project grants are enforced"
	}
	if path == "/api/v1/admin/overview" {
		return "signed-in account session in open or required mode; current organization owner or admin role is rechecked"
	}
	if strings.HasPrefix(path, "/api/v1/") {
		return "implicit owner in open mode or signed-in account session; route-specific authorization is rechecked"
	}
	return "route-specific"
}

func openAPIIsPublic(method, path string) bool {
	switch path {
	case "/healthz", "/readyz", "/api/v1/setup", "/api/v1/login", "/api/v1/login/two-factor", "/api/v1/auth/config", "/api/v1/account/password/reset", "/api/v1/account/password/reset/request", "/api/v1/invitations/inspect", "/api/v1/invitations/accept", "/account/emails/confirm", "/api/embed/error-page/", "/api/settings/", "/oauth/register", "/oauth/token", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource":
		return true
	}
	for _, prefix := range []string{"/api/0/accept/", "/account/settings/wizard/", "/.well-known/", "/api/{project}/", "/api/0/organizations/{org}/heartbeat_check/", "/api/v1/public/", "/status/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	if path == "/oauth/login" || path == "/oauth/callback" {
		return true
	}
	return (method == "GET" || method == "DELETE") && (path == "/api/0/wizard/" || path == "/api/0/wizard/{hash}/")
}

type openAPIDocsPage struct {
	Base   string
	Routes []routeRegistration
}

var openAPIDocsTemplate = template.Must(template.New("api-docs").Funcs(template.FuncMap{"authentication": openAPIAuthentication}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>GoodSentry API</title><link rel="stylesheet" href="{{.Base}}/app.css"></head>
<body><main id="main" class="main"><div class="page-header"><div><div class="eyebrow">GoodSentry</div><h1>GoodSentry API</h1><p>Routes registered by this build, with the authentication boundary documented for each family.</p></div><a class="btn button primary" href="{{.Base}}/api/openapi.json">OpenAPI JSON</a></div>
<section class="card panel"><div class="panel-title"><h2>Endpoint matrix</h2><span class="small muted">{{len .Routes}} operations</span></div><div class="table-scroll"><table class="api-docs-table"><thead><tr><th scope="col">Method</th><th scope="col">Path and authentication</th></tr></thead><tbody>{{range .Routes}}<tr><td><code>{{.Method}}</code></td><td><code>{{.Path}}</code><div class="small muted">{{authentication .Method .Path}}</div></td></tr>{{end}}</tbody></table></div></section>
<p class="small muted">Generated at process startup from the actual router registrations. Optional Uptime, MCP, administration and organization-creation routes appear only when enabled. See <a href="{{.Base}}/">the application</a> for the product interface.</p></main></body></html>`))
