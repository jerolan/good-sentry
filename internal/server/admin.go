package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"goodsentry/internal/store"
)

type adminInstance struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GoVersion string `json:"goVersion"`
	AuthMode  string `json:"authMode"`
	Database  string `json:"database"`
}

func (s *Server) registerAdminRoutes(mux *routeMux) {
	mux.Handle("GET /api/v1/admin/overview", s.auth(http.HandlerFunc(s.adminOverview)))
	mux.Handle("GET /api/v1/admin/users", s.auth(http.HandlerFunc(s.adminUsers)))
	mux.Handle("PATCH /api/v1/admin/users/{userID}", s.auth(http.HandlerFunc(s.updateAdminUser)))
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	organizations, err := s.db.AdminOrganizations(r.Context(), current(r).User.ID)
	if err != nil {
		report(w, err)
		return
	}
	if len(organizations) == 0 {
		failure(w, http.StatusForbidden, "Organization owner or admin permission required")
		return
	}
	authMode := "required"
	if s.cfg.AuthenticationDisabled() {
		authMode = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instance":      adminInstance{Name: "GoodSentry", Version: goodsentryVersion, GoVersion: runtime.Version(), AuthMode: authMode, Database: "PostgreSQL ready"},
		"organizations": organizations,
		"features": map[string]bool{
			"logs": s.cfg.Logs, "uptime": s.cfg.Uptime, "mcp": s.cfg.MCP, "openapi": s.cfg.OpenAPI, "admin": true,
			"alerts": true, "alertDelivery": s.cfg.AlertDelivery,
		},
		"retention": map[string]int{
			"events": retentionOrDefault(s.cfg.EventRetentionDays, 90), "logs": retentionOrDefault(s.cfg.LogRetentionDays, 90),
			"transactions": retentionOrDefault(s.cfg.TransactionRetentionDays, 90), "uptime": retentionOrDefault(s.cfg.UptimeRetentionDays, 90),
			"files": retentionOrDefault(s.cfg.FileRetentionDays, 90), "releases": retentionOrDefault(s.cfg.ReleaseRetentionDays, 365),
		},
	})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	for key, items := range values {
		if key != "page" && key != "perPage" && key != "query" || len(items) != 1 {
			failure(w, http.StatusBadRequest, "Unsupported or repeated query parameter")
			return
		}
	}
	page, valid := adminPageNumber(values.Get("page"), !values.Has("page"), 1, 1000000)
	if !valid {
		failure(w, http.StatusBadRequest, "page must be a positive decimal integer up to 1000000")
		return
	}
	perPage, valid := adminPageNumber(values.Get("perPage"), !values.Has("perPage"), 25, 100)
	if !valid {
		failure(w, http.StatusBadRequest, "perPage must be a positive decimal integer up to 100")
		return
	}
	query := values.Get("query")
	if len([]rune(query)) > 100 || strings.IndexFunc(query, unicode.IsControl) >= 0 {
		failure(w, http.StatusBadRequest, "query must contain at most 100 characters without controls")
		return
	}
	result, err := s.db.AdminUsers(r.Context(), current(r).User.ID, query, page, perPage)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func adminPageNumber(value string, useFallback bool, fallback, maximum int) (int, bool) {
	if useFallback {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil && parsed > 0 && parsed <= maximum && fmt.Sprint(parsed) == value
}

func (s *Server) updateAdminUser(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalPersonID(w, r.PathValue("userID"))
	if !ok {
		return
	}
	var input struct {
		IsActive        json.RawMessage `json:"isActive"`
		IsInstanceAdmin json.RawMessage `json:"isInstanceAdmin"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	isActive, activeValid := optionalAdminBoolean(input.IsActive)
	isInstanceAdmin, adminValid := optionalAdminBoolean(input.IsInstanceAdmin)
	if !activeValid || !adminValid {
		failure(w, http.StatusBadRequest, "isActive and isInstanceAdmin must be booleans when provided")
		return
	}
	if isActive == nil && isInstanceAdmin == nil {
		failure(w, http.StatusBadRequest, "Provide isActive or isInstanceAdmin")
		return
	}
	if isActive != nil && !*isActive && isInstanceAdmin != nil && *isInstanceAdmin {
		failure(w, http.StatusBadRequest, "Inactive users cannot be instance administrators")
		return
	}
	protectedID := ""
	if s.cfg.AuthenticationDisabled() {
		protectedID = s.defaultUserID
	}
	updated, err := s.db.UpdateAdminUser(r.Context(), current(r).User.ID, id, protectedID, store.AdminUserChange{
		IsActive: isActive, IsInstanceAdmin: isInstanceAdmin,
	})
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func optionalAdminBoolean(raw json.RawMessage) (*bool, bool) {
	if raw == nil {
		return nil, true
	}
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		value := true
		return &value, true
	case "false":
		value := false
		return &value, true
	default:
		return nil, false
	}
}

func retentionOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
