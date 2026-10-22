package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

const compatibleDiscoveryVersion = "0.0.0-dev"

var compatibleReadScopes = []string{"org:read", "org:write", "project:read", "project:releases", "project:write", "event:read", "event:write", "event:admin"}

func (s *Server) registerCompatibleDiscoveryRoutes(mux *routeMux) {
	mux.Handle("GET /api/0/{$}", s.compatibleAny(compatibleReadScopes, s.compatibleAPIRoot))
	mux.Handle("GET /api/0/users/{$}", s.compatibleAny(compatibleReadScopes, s.compatibleUsers))
	mux.Handle("GET /api/0/users/{user}/{$}", s.compatibleAny(compatibleReadScopes, s.compatibleUser))
	mux.Handle("PUT /api/0/users/{user}/{$}", s.auth(http.HandlerFunc(s.compatibleUpdateUser)))
	mux.Handle("DELETE /api/0/users/{user}/{$}", s.auth(http.HandlerFunc(s.compatibleDeleteUser)))
	mux.Handle("GET /api/0/users/{user}/emails/{$}", s.auth(http.HandlerFunc(s.compatibleUserEmails)))
	mux.Handle("POST /api/0/users/{user}/emails/{$}", s.auth(http.HandlerFunc(s.compatibleCreateUserEmail)))
	mux.Handle("PUT /api/0/users/{user}/emails/{$}", s.auth(http.HandlerFunc(s.compatibleSetPrimaryUserEmail)))
	mux.Handle("DELETE /api/0/users/{user}/emails/{$}", s.auth(http.HandlerFunc(s.compatibleDeleteUserEmail)))
	mux.Handle("POST /api/0/users/{user}/emails/confirm/{$}", s.auth(http.HandlerFunc(s.compatibleConfirmUserEmail)))
	mux.Handle("GET /api/0/users/{user}/notifications/{$}", s.auth(http.HandlerFunc(s.compatibleUserNotifications)))
	mux.Handle("PUT /api/0/users/{user}/notifications/{$}", s.auth(http.HandlerFunc(s.compatibleUpdateUserNotifications)))
	mux.Handle("GET /api/0/users/{user}/notifications/alerts/{$}", s.auth(http.HandlerFunc(s.compatibleUserNotifications)))
	mux.Handle("PUT /api/0/users/{user}/notifications/alerts/{$}", s.auth(http.HandlerFunc(s.compatibleUpdateUserNotifications)))
	mux.Handle("GET /api/0/organizations/{$}", s.compatible("org:read", s.compatibleOrganizations))
	mux.Handle("GET /api/0/projects/{$}", s.compatibleAny([]string{"org:read", "project:read"}, s.compatibleAllProjects))
	mux.Handle("GET /api/0/instance-license/{$}", s.compatibleAny(compatibleReadScopes, s.compatibleInstanceLicense))
	mux.Handle("GET /api/0/instance-license/support-link/{$}", s.compatibleAny(compatibleReadScopes, s.compatibleSupportLink))
	mux.HandleFunc("GET /api/settings/", s.compatibleSettings)
}

func (s *Server) compatibleAPIRoot(w http.ResponseWriter, r *http.Request) {
	user, err := s.db.CompatibleUser(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, struct {
		Version string               `json:"version"`
		User    store.CompatibleUser `json:"user"`
		Auth    any                  `json:"auth"`
	}{Version: "0", User: user})
}

func (s *Server) compatibleUsers(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	user, err := s.db.CompatibleUser(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, []store.CompatibleUser{user})
}

func (s *Server) compatibleUser(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	reference := r.PathValue("user")
	currentID := current(r).User.ID
	if reference != "me" && reference != currentID {
		failure(w, 404, "Not found")
		return
	}
	user, err := s.db.CompatibleUser(r.Context(), currentID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, user)
}

func (s *Server) compatibleOrganizations(w http.ResponseWriter, r *http.Request) {
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor", "query", "owner", "sortBy") {
		return
	}
	query := q.Get("query")
	if !credentialText(query, 200) {
		failure(w, 400, "Query must contain at most 200 characters without controls")
		return
	}
	owner := false
	if q.Has("owner") {
		value, err := strconv.ParseBool(q.Get("owner"))
		if err != nil {
			failure(w, 400, "Owner must be true or false")
			return
		}
		owner = value
	}
	sortBy := "name"
	if q.Has("sortBy") {
		sortBy = q.Get("sortBy")
		switch sortBy {
		case "name", "-name", "slug", "-slug", "dateCreated", "-dateCreated":
		default:
			failure(w, 400, "Unsupported organization sort")
			return
		}
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	organizationID := ""
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok {
		organizationID = grant.OrganizationID
	}
	organizations, err := s.db.CompatibleOrganizations(r.Context(), current(r).User.ID, organizationID, query, owner, sortBy, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	next := len(organizations) > limit
	if next {
		organizations = organizations[:limit]
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, organizations)
}

func (s *Server) compatibleAllProjects(w http.ResponseWriter, r *http.Request) {
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	organizationID, allProjects, projectIDs := "", true, []int64(nil)
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok {
		organizationID, allProjects, projectIDs = grant.OrganizationID, grant.AllProjects, grant.ProjectIDs
	}
	projects, err := s.db.CompatibleProjectsAcrossOrganizations(r.Context(), current(r).User.ID, organizationID, allProjects, projectIDs, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	next := len(projects) > limit
	if next {
		projects = projects[:limit]
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, projects)
}

func (s *Server) compatibleInstanceLicense(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	writeJSON(w, 200, map[string]string{"billingEmail": ""})
}

func (s *Server) compatibleSupportLink(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	writeJSON(w, 200, map[string]string{"url": "https://github.com/jerolan/good-sentry/issues"})
}

func (s *Server) compatibleSettings(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/settings/")
	if rest != "" {
		if !strings.HasSuffix(rest, "/") || strings.Contains(strings.TrimSuffix(rest, "/"), "/") || strings.TrimSuffix(rest, "/") == "" {
			failure(w, 404, "Not found")
			return
		}
		writeJSON(w, 200, map[string]any{"socialApps": []any{}})
		return
	}
	features := []string{"alerts", "api-tokens", "events", "releases", "transactions"}
	if s.cfg.Logs {
		features = append(features, "logs")
	}
	if s.cfg.Uptime {
		features = append(features, "uptime")
	}
	environment := "development"
	if strings.HasPrefix(s.cfg.Domain, "https://") {
		environment = "production"
	}
	writeJSON(w, 200, map[string]any{
		"socialApps": []any{}, "billingEnabled": false, "iPaidForGlitchTip": false,
		"enableUserRegistration": false, "enableSocialAppsUserRegistration": false, "enableOrganizationCreation": false,
		"stripePublicKey": nil, "plausibleUrl": nil, "plausibleDomain": nil, "chatwootWebsiteToken": nil,
		"sentryDSN": nil, "sentryTracesSampleRate": nil, "environment": environment,
		"version": compatibleDiscoveryVersion, "serverTimeZone": "UTC",
		"glitchtipInstanceName": "GoodSentry", "enabledFeatures": features,
	})
}

func discoveryNoQuery(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	q, valid := credentialQuery(w, r)
	if !valid {
		return nil, false
	}
	return q, discoveryQueryKeys(w, q)
}

func discoveryQueryKeys(w http.ResponseWriter, q url.Values, allowed ...string) bool {
	keys := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		keys[key] = true
	}
	for key, values := range q {
		if !keys[key] || len(values) != 1 {
			failure(w, 400, "Unsupported or repeated query parameter")
			return false
		}
	}
	return true
}

func discoveryPage(w http.ResponseWriter, q url.Values) (int, int, bool) {
	limit := int64(25)
	if q.Has("limit") || q.Has("per_page") {
		value := q.Get("limit")
		if q.Has("per_page") {
			if q.Has("limit") && q.Get("limit") != q.Get("per_page") {
				failure(w, 400, "limit and per_page must agree")
				return 0, 0, false
			}
			value = q.Get("per_page")
		}
		var valid bool
		limit, valid = occurrenceNumber(value, 100)
		if !valid {
			failure(w, 400, "limit must be a positive decimal integer up to 100")
			return 0, 0, false
		}
	}
	offset, valid := readProjectCursor(q.Get("cursor"))
	if !valid {
		failure(w, 400, "Cursor is invalid")
		return 0, 0, false
	}
	return offset, int(limit), true
}

func (s *Server) discoveryLinks(w http.ResponseWriter, r *http.Request, q url.Values, offset, limit int, next bool) {
	links := []string{}
	if offset > 0 {
		links = append(links, s.discoveryLink(r, q, "previous", max(0, offset-limit), limit, true))
	}
	if next {
		nextOffset := offset + limit
		if nextOffset <= maxProjectOffset {
			links = append(links, s.discoveryLink(r, q, "next", nextOffset, limit, true))
		}
	}
	if len(links) > 0 {
		w.Header().Set("Link", strings.Join(links, ", "))
	}
}

func (s *Server) discoveryLink(r *http.Request, q url.Values, relation string, offset, limit int, results bool) string {
	values := q.Clone()
	values.Del("per_page")
	values.Set("limit", strconv.Itoa(limit))
	values.Set("cursor", projectCursor(offset))
	public := strings.TrimRight(s.cfg.Domain, "/") + r.URL.Path + "?" + values.Encode()
	return fmt.Sprintf(`<%s>; rel="%s"; results="%t"; cursor="%s"`, public, relation, results, projectCursor(offset))
}
