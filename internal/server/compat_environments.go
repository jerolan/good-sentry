package server

import (
	"net/http"
	"net/url"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleEnvironmentRoutes(mux *routeMux) {
	mux.Handle("GET /api/0/organizations/{org}/environments/{$}", s.compatible("org:read", s.compatibleOrganizationEnvironments))
	mux.Handle("GET /api/0/projects/{org}/{project}/environments/{$}", s.compatible("project:read", s.compatibleProjectEnvironments))
	mux.Handle("GET /api/0/projects/{org}/{project}/environments/{name}/{$}", s.compatible("project:read", s.compatibleProjectEnvironment))
	mux.Handle("PUT /api/0/projects/{org}/{project}/environments/{name}/{$}", s.compatible("project:write", s.compatibleUpdateProjectEnvironment))
}

func compatibleEnvironmentPage(w http.ResponseWriter, r *http.Request) (string, int, int, url.Values, bool) {
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "visibility", "limit", "per_page", "cursor") {
		return "", 0, 0, nil, false
	}
	visibility := q.Get("visibility")
	if visibility == "" {
		visibility = "visible"
	}
	if !store.ValidEnvironmentVisibility(visibility) {
		failure(w, 400, "Visibility must be all, hidden or visible")
		return "", 0, 0, nil, false
	}
	offset, limit, valid := discoveryPage(w, q)
	return visibility, offset, limit, q, valid
}

func (s *Server) compatibleProjectEnvironments(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	visibility, offset, limit, q, valid := compatibleEnvironmentPage(w, r)
	if !valid {
		return
	}
	environments, err := s.db.CompatibleProjectEnvironments(r.Context(), access, r.PathValue("project"), visibility, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	page, next := pageCompatible(environments, 0, limit)
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, page)
}

func (s *Server) compatibleOrganizationEnvironments(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	visibility, offset, limit, q, valid := compatibleEnvironmentPage(w, r)
	if !valid {
		return
	}
	environments, err := s.db.CompatibleOrganizationEnvironments(r.Context(), access, visibility, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	page, next := pageCompatible(environments, 0, limit)
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, page)
}

func (s *Server) compatibleProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	name := r.PathValue("name")
	if !store.ValidCompatibleEnvironmentName(name) {
		failure(w, 400, "Environment name is invalid")
		return
	}
	environment, err := s.db.CompatibleProjectEnvironment(r.Context(), access, r.PathValue("project"), name)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, environment)
}

func (s *Server) compatibleUpdateProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	access, valid := s.projectWriteAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	name := r.PathValue("name")
	if !store.ValidCompatibleEnvironmentName(name) {
		failure(w, 400, "Environment name is invalid")
		return
	}
	var input struct {
		Name     *string `json:"name"`
		IsHidden *bool   `json:"isHidden"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if input.IsHidden == nil || input.Name != nil && *input.Name != name {
		failure(w, 400, "isHidden is required and name cannot rename an environment")
		return
	}
	environment, err := s.db.SetCompatibleProjectEnvironmentVisibility(r.Context(), access, r.PathValue("project"), name, *input.IsHidden)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, environment)
}
