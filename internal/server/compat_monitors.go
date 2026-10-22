package server

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/store"
	"goodsentry/internal/uptime"
)

func (s *Server) registerCompatibleMonitorRoutes(mux *http.ServeMux) {
	if !s.cfg.Uptime {
		return
	}
	mux.Handle("GET /api/0/organizations/{org}/monitors/{$}", s.compatible("org:read", s.compatibleMonitors))
	mux.Handle("POST /api/0/organizations/{org}/monitors/{$}", s.compatible("org:write", s.compatibleSaveMonitor))
	mux.Handle("GET /api/0/organizations/{org}/monitors/{monitor}/{$}", s.compatible("org:read", s.compatibleMonitor))
	mux.Handle("PUT /api/0/organizations/{org}/monitors/{monitor}/{$}", s.compatible("org:write", s.compatibleSaveMonitor))
	mux.Handle("DELETE /api/0/organizations/{org}/monitors/{monitor}/{$}", s.compatible("org:write", s.compatibleDeleteMonitor))
	mux.Handle("GET /api/0/organizations/{org}/monitors/{monitor}/checks/{$}", s.compatible("org:read", s.compatibleMonitorChecks))
	mux.HandleFunc("POST /api/0/organizations/{org}/heartbeat_check/{endpoint}/{$}", s.compatibleHeartbeatCheck)
}

type compatibleMonitorInput struct {
	Name                  string  `json:"name"`
	URL                   *string `json:"url"`
	MonitorType           string  `json:"monitorType"`
	Project               *string `json:"project"`
	EnvironmentID         *int    `json:"environmentID"`
	ExpectedStatus        *int    `json:"expectedStatus"`
	ExpectedBody          string  `json:"expectedBody"`
	Interval              int     `json:"interval"`
	Timeout               *int    `json:"timeout"`
	ConfirmationThreshold int     `json:"confirmationThreshold"`
}

func compatibleMonitorKind(value string) (uptime.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "ping", "head":
		return uptime.HEAD, true
	case "get":
		return uptime.GET, true
	case "post":
		return uptime.POST, true
	case "tcp port", "tcp":
		return uptime.TCP, true
	case "ssl":
		return uptime.SSL, true
	case "heartbeat":
		return uptime.Heartbeat, true
	default:
		return "", false
	}
}

func compatibleMonitorType(kind uptime.Kind) string {
	switch kind {
	case uptime.HEAD:
		return "Ping"
	case uptime.GET:
		return "GET"
	case uptime.POST:
		return "POST"
	case uptime.TCP:
		return "TCP Port"
	case uptime.SSL:
		return "SSL"
	case uptime.Heartbeat:
		return "Heartbeat"
	default:
		return string(kind)
	}
}

func (s *Server) monitorWire(monitor uptime.Monitor, orgSlug string) map[string]any {
	isUp := any(nil)
	if monitor.Status == uptime.Up {
		isUp = true
	} else if monitor.Status == uptime.Down {
		isUp = false
	}
	organizationID, _ := strconv.ParseInt(monitor.OrganizationID, 10, 64)
	monitorID, _ := strconv.ParseInt(monitor.ID, 10, 64)
	heartbeatEndpoint := monitor.HeartbeatURL
	if monitor.Type == uptime.Heartbeat && monitor.EndpointID != "" {
		heartbeatEndpoint = s.cfg.Domain + "/api/0/organizations/" + url.PathEscape(orgSlug) + "/heartbeat_check/" + monitor.EndpointID + "/"
	}
	return map[string]any{
		"id": monitorID, "name": monitor.Name, "url": monitor.URL,
		"monitorType": compatibleMonitorType(monitor.Type), "projectID": monitor.Project,
		"projectName": monitor.Project, "environmentID": nil, "envName": monitor.Environment,
		"organizationID": organizationID, "isUp": isUp, "lastChange": monitor.LastChange,
		"created": monitor.CreatedAt, "expectedStatus": monitor.ExpectedStatus,
		"expectedBody": monitor.ExpectedBody, "interval": monitor.IntervalSeconds,
		"timeout": monitor.TimeoutSeconds, "confirmationThreshold": monitor.ConfirmationThreshold,
		"endpointId": monitor.EndpointID, "heartbeatEndpoint": heartbeatEndpoint, "checks": []any{},
	}
}

func (s *Server) compatibleHeartbeatCheck(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		failure(w, 400, "Heartbeat check accepts no query parameters")
		return
	}
	if _, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1024)); err != nil {
		failure(w, 413, "Heartbeat body must be at most 1024 bytes")
		return
	}
	check, err := s.db.CompatibleHeartbeat(r.Context(), r.PathValue("org"), r.PathValue("endpoint"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"isUp": check.Up, "reason": nil, "startCheck": check.StartedAt.UTC()})
}

func (s *Server) compatibleMonitors(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.managementAccess(w, r); !ok {
		return
	}
	q, ok := credentialQuery(w, r)
	if !ok || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, ok := discoveryPage(w, q)
	if !ok {
		return
	}
	items, err := s.db.Monitors(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	page, more := pageCompatible(items, offset, limit)
	result := make([]map[string]any, 0, len(page))
	for _, monitor := range page {
		s.monitorURL(&monitor)
		result = append(result, s.monitorWire(monitor, r.PathValue("org")))
	}
	s.discoveryLinks(w, r, q, offset, limit, more)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleMonitor(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.managementAccess(w, r); !ok {
		return
	}
	if _, ok := discoveryNoQuery(w, r); !ok {
		return
	}
	monitor, err := s.db.Monitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"))
	if err != nil {
		report(w, err)
		return
	}
	s.monitorURL(&monitor)
	writeJSON(w, 200, s.monitorWire(monitor, r.PathValue("org")))
}

func (s *Server) compatibleSaveMonitor(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.orgWriteAccess(w, r); !ok {
		return
	}
	var input compatibleMonitorInput
	if !bodyJSON(w, r, &input) {
		return
	}
	kind, ok := compatibleMonitorKind(input.MonitorType)
	if !ok {
		failure(w, 400, "Unsupported monitorType")
		return
	}
	var existing uptime.Monitor
	if r.Method == http.MethodPut {
		var err error
		existing, err = s.db.Monitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"))
		if err != nil {
			report(w, err)
			return
		}
	}
	if input.EnvironmentID != nil {
		failure(w, 400, "Environment IDs are not implemented")
		return
	}
	config := uptime.Defaults()
	config.Name = strings.TrimSpace(input.Name)
	config.Type = kind
	if r.Method == http.MethodPut && input.MonitorType == "" {
		config.Type = existing.Type
	}
	if input.URL != nil {
		config.URL = strings.TrimSpace(*input.URL)
	} else if r.Method == http.MethodPut {
		config.URL = existing.URL
	}
	if input.Project != nil {
		config.Project = strings.TrimSpace(*input.Project)
	}
	if input.Interval != 0 {
		config.IntervalSeconds = input.Interval
	} else if r.Method == http.MethodPut {
		config.IntervalSeconds = existing.IntervalSeconds
	}
	if input.Timeout != nil {
		config.TimeoutSeconds = *input.Timeout
	} else if r.Method == http.MethodPut {
		config.TimeoutSeconds = existing.TimeoutSeconds
	}
	if input.ExpectedStatus != nil {
		config.ExpectedStatus = *input.ExpectedStatus
	} else if r.Method == http.MethodPut {
		config.ExpectedStatus = existing.ExpectedStatus
	}
	config.ExpectedBody = input.ExpectedBody
	if r.Method == http.MethodPut && input.ExpectedBody == "" {
		config.ExpectedBody = existing.ExpectedBody
	}
	if input.ConfirmationThreshold != 0 {
		config.ConfirmationThreshold = input.ConfirmationThreshold
	} else if r.Method == http.MethodPut {
		config.ConfirmationThreshold = existing.ConfirmationThreshold
	}
	config.Environment = ""
	if r.Method == http.MethodPut {
		config.Enabled = existing.Enabled
		config.Environment = existing.Environment
		if input.Project == nil {
			config.Project = existing.Project
		}
	}
	if err := config.Validate(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	var monitor uptime.Monitor
	var err error
	status := 201
	if r.Method == http.MethodPut {
		monitor, err = s.db.UpdateMonitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"), config)
		status = 200
	} else {
		monitor, err = s.db.CreateMonitor(r.Context(), current(r).User.ID, r.PathValue("org"), config)
	}
	if errors.Is(err, store.ErrConflict) {
		failure(w, 409, "Monitor type cannot change after creation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	s.monitorURL(&monitor)
	writeJSON(w, status, s.monitorWire(monitor, r.PathValue("org")))
}

func (s *Server) compatibleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.orgWriteAccess(w, r); !ok {
		return
	}
	if err := s.db.DeleteMonitor(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleMonitorChecks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.managementAccess(w, r); !ok {
		return
	}
	q, ok := credentialQuery(w, r)
	if !ok || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, ok := discoveryPage(w, q)
	if !ok {
		return
	}
	checks, more, err := s.db.MonitorChecksPage(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("monitor"), false, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	result := make([]map[string]any, 0, len(checks))
	for _, check := range checks {
		result = append(result, map[string]any{
			"startCheck": check.StartedAt, "isUp": check.Up, "reason": nil,
			"responseTime": check.ResponseTimeMS,
		})
	}
	s.discoveryLinks(w, r, q, offset, limit, more)
	writeJSON(w, 200, result)
}
