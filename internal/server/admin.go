package server

import (
	"net/http"
	"runtime"
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

func retentionOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
