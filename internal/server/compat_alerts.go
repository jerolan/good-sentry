package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

type compatibleAlertInput struct {
	Name            *string                    `json:"name"`
	Environments    []string                   `json:"environments"`
	AlertRecipients []compatibleAlertRecipient `json:"alertRecipients"`
	TimespanMinutes *int                       `json:"timespanMinutes"`
	Quantity        *int                       `json:"quantity"`
	Uptime          *bool                      `json:"uptime"`
}

type compatibleAlertRecipient struct {
	RecipientType string   `json:"recipientType"`
	URL           *string  `json:"url"`
	TagsToAdd     []string `json:"tagsToAdd"`
	BotEmail      string   `json:"botEmail"`
	APIKey        string   `json:"apiKey"`
	Channel       string   `json:"channel"`
	Topic         string   `json:"topic"`
}

func alertRecipientWire(recipient notify.Recipient) map[string]any {
	recipientType := recipient.Type
	var url any
	if recipientType == "team_email" {
		recipientType = "email"
		url = nil
	} else {
		url = recipient.Destination
	}
	return map[string]any{
		"id": nil, "recipientType": recipientType, "url": url,
		"config": nil, "tagsToAdd": nil,
	}
}

func alertWire(rule notify.Rule) map[string]any {
	id, _ := strconv.ParseInt(rule.ID, 10, 64)
	recipients := make([]map[string]any, 0, len(rule.Recipients))
	for _, recipient := range rule.Recipients {
		recipients = append(recipients, alertRecipientWire(recipient))
	}
	return map[string]any{
		"id": id, "name": rule.Name, "environments": rule.Environments,
		"alertRecipients": recipients, "timespanMinutes": rule.WindowMinutes,
		"quantity": rule.Quantity, "uptime": rule.Kind == notify.Uptime,
	}
}

func compatibleAlertConfig(input compatibleAlertInput, existing *notify.Rule) (notify.Config, error) {
	config := notify.Defaults()
	config.Kind = notify.Frequency
	if input.Uptime != nil && *input.Uptime {
		config.Kind = notify.Uptime
	}
	if existing != nil {
		config.Kind = existing.Kind
		config.Enabled = existing.Enabled
		config.CooldownSeconds = existing.CooldownSeconds
		config.Name = existing.Name
		config.Quantity = existing.Quantity
		config.WindowMinutes = existing.WindowMinutes
		config.Environments = existing.Environments
		config.Recipients = existing.Recipients
		if input.Uptime != nil {
			if *input.Uptime {
				config.Kind = notify.Uptime
			} else {
				config.Kind = notify.Frequency
			}
		}
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		config.Name = strings.TrimSpace(*input.Name)
	}
	if config.Name == "" {
		if config.Kind == notify.Uptime {
			config.Name = "Uptime alert"
		} else {
			config.Name = "Project alert"
		}
	}
	if input.Quantity != nil {
		config.Quantity = *input.Quantity
	}
	if input.TimespanMinutes != nil {
		config.WindowMinutes = *input.TimespanMinutes
	}
	if input.Environments != nil {
		config.Environments = input.Environments
	}
	if input.AlertRecipients != nil {
		config.Recipients = []notify.Recipient{}
		for _, recipient := range input.AlertRecipients {
			if len(recipient.TagsToAdd) > 0 || recipient.BotEmail != "" || recipient.APIKey != "" || recipient.Channel != "" || recipient.Topic != "" && recipient.Topic != "GlitchTip Alerts" {
				return notify.Config{}, errors.New("recipient tags and Zulip bot configuration are not implemented")
			}
			switch recipient.RecipientType {
			case "email":
				if recipient.URL != nil && strings.TrimSpace(*recipient.URL) != "" {
					return notify.Config{}, errors.New("email recipient URLs are not implemented; use project team email")
				}
				config.Recipients = append(config.Recipients, notify.Recipient{Type: "team_email"})
			case "webhook", "slack", "discord", "teams", "googlechat", "feishu", "ntfy", "zulip":
				if recipient.URL == nil || strings.TrimSpace(*recipient.URL) == "" {
					return notify.Config{}, errors.New("webhook recipients require a URL")
				}
				config.Recipients = append(config.Recipients, notify.Recipient{Type: recipient.RecipientType, Destination: strings.TrimSpace(*recipient.URL)})
			default:
				return notify.Config{}, errors.New("unsupported alert recipient type")
			}
		}
	}
	if err := config.Validate(); err != nil {
		return notify.Config{}, err
	}
	return config, nil
}

func (s *Server) registerCompatibleAlertRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/0/projects/{org}/{project}/alerts/{$}", s.compatible("project:read", s.compatibleAlerts))
	mux.Handle("POST /api/0/projects/{org}/{project}/alerts/{$}", s.compatible("project:write", s.compatibleSaveAlert))
	mux.Handle("PUT /api/0/projects/{org}/{project}/alerts/{alert}/{$}", s.compatible("project:write", s.compatibleSaveAlert))
	mux.Handle("DELETE /api/0/projects/{org}/{project}/alerts/{alert}/{$}", s.compatible("project:write", s.compatibleDeleteAlert))
	mux.Handle("POST /api/0/projects/{org}/{project}/alerts/{alert}/test/{$}", s.compatible("project:write", s.compatibleTestAlert))
}

func (s *Server) compatibleAlertProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return "", false
	}
	project, err := s.db.ManagementProject(r.Context(), access, r.PathValue("project"))
	if err != nil {
		report(w, err)
		return "", false
	}
	return project.ID, true
}

func (s *Server) compatibleAlerts(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.compatibleAlertProject(w, r)
	if !ok {
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
	rules, err := s.db.Alerts(r.Context(), current(r).User.ID, r.PathValue("org"), projectID)
	if err != nil {
		report(w, err)
		return
	}
	page, next := pageCompatible(rules, offset, limit)
	result := make([]map[string]any, 0, len(page))
	for _, rule := range page {
		result = append(result, alertWire(rule))
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleSaveAlert(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.compatibleAlertProject(w, r)
	if !ok {
		return
	}
	var input compatibleAlertInput
	if !bodyJSON(w, r, &input) {
		return
	}
	var existing *notify.Rule
	if r.Method == http.MethodPut {
		rule, err := s.db.Alert(r.Context(), current(r).User.ID, r.PathValue("org"), projectID, r.PathValue("alert"))
		if err != nil {
			report(w, err)
			return
		}
		existing = &rule
	}
	config, err := compatibleAlertConfig(input, existing)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	var rule notify.Rule
	status := 201
	if existing != nil {
		rule, err = s.db.UpdateAlert(r.Context(), current(r).User.ID, r.PathValue("org"), projectID, r.PathValue("alert"), config)
		status = 200
	} else {
		rule, err = s.db.CreateAlert(r.Context(), current(r).User.ID, r.PathValue("org"), projectID, config)
	}
	if errors.Is(err, store.ErrConflict) {
		failure(w, 409, "Alert kind cannot change after creation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, status, alertWire(rule))
}

func (s *Server) compatibleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.compatibleAlertProject(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteAlert(r.Context(), current(r).User.ID, r.PathValue("org"), projectID, r.PathValue("alert")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleTestAlert(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.compatibleAlertProject(w, r)
	if !ok {
		return
	}
	rule, err := s.db.Alert(r.Context(), current(r).User.ID, r.PathValue("org"), projectID, r.PathValue("alert"))
	if err != nil {
		report(w, err)
		return
	}
	if _, err = s.db.TestAlert(r.Context(), current(r).User.ID, r.PathValue("org"), projectID, r.PathValue("alert")); err != nil {
		if errors.Is(err, store.ErrConflict) {
			failure(w, 409, "Enable this alert before queuing a test")
			return
		}
		if errors.Is(err, store.ErrNoRecipients) {
			failure(w, 400, "No current project team members match this alert")
			return
		}
		report(w, err)
		return
	}
	result := make([]map[string]any, 0, len(rule.Recipients))
	for _, recipient := range rule.Recipients {
		wire := alertRecipientWire(recipient)
		result = append(result, map[string]any{
			"recipientType": wire["recipientType"], "status": "queued", "message": nil,
		})
	}
	writeJSON(w, 200, result)
}
