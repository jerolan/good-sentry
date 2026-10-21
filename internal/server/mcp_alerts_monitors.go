package server

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"goodsentry/internal/notify"
	"goodsentry/internal/store"
	"goodsentry/internal/uptime"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpAlertMonitorInput struct {
	mcpWorkspaceInput
	Project string `json:"project,omitempty"`
}

type mcpAlertSummary struct {
	ID              string      `json:"id"`
	ProjectID       string      `json:"projectId"`
	Name            string      `json:"name"`
	Kind            notify.Kind `json:"kind"`
	Enabled         bool        `json:"enabled"`
	Quantity        int         `json:"quantity"`
	WindowMinutes   int         `json:"windowMinutes"`
	CooldownSeconds int         `json:"cooldownSeconds"`
	Environments    []string    `json:"environments"`
	RecipientTypes  []string    `json:"recipientTypes"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
	LastTriggered   *time.Time  `json:"lastTriggered"`
}

func mcpAlertMonitorSchema(alerts bool) map[string]any {
	schema := mcpWorkspaceSchema(false)
	properties := schema["properties"].(map[string]any)
	properties["organization"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	delete(properties, "query")
	if alerts {
		properties["project"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	}
	schema["required"] = []string{"organization"}
	return schema
}

func alertSummary(rule notify.Rule) mcpAlertSummary {
	types := make([]string, 0, len(rule.Recipients))
	seen := map[string]bool{}
	for _, recipient := range rule.Recipients {
		if !seen[recipient.Type] {
			seen[recipient.Type] = true
			types = append(types, recipient.Type)
		}
	}
	sort.Strings(types)
	return mcpAlertSummary{
		ID: rule.ID, ProjectID: rule.ProjectID, Name: rule.Name, Kind: rule.Kind, Enabled: rule.Enabled,
		Quantity: rule.Quantity, WindowMinutes: rule.WindowMinutes, CooldownSeconds: rule.CooldownSeconds,
		Environments: append([]string{}, rule.Environments...), RecipientTypes: types,
		CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt, LastTriggered: rule.LastTriggered,
	}
}

func mcpAlertMonitorError(err error) error {
	switch {
	case errors.Is(err, store.ErrForbidden):
		return errors.New("Alert configuration requires a current owner or admin")
	case errors.Is(err, store.ErrNotFound):
		return errors.New("Alert, monitor, project or organization not found")
	default:
		return errors.New("Alert or monitor data unavailable")
	}
}

func (s *Server) addMCPAlertMonitorTools(server *mcp.Server, ctx context.Context, grant store.APITokenGrant) {
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "list_alerts", Description: "List sanitized alert configuration for authorized projects. Requires project:read and a current owner or admin role.", InputSchema: mcpAlertMonitorSchema(true), OutputSchema: mcpEventOutputSchema[mcpWorkspaceResult[mcpAlertSummary]](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpAlertMonitorInput) (*mcp.CallToolResult, mcpWorkspaceResult[mcpAlertSummary], error) {
			var result mcpWorkspaceResult[mcpAlertSummary]
			if !slices.Contains(grant.Scopes, "project:read") {
				return nil, result, errors.New("Credential requires project:read")
			}
			if input.Organization == "" || !credentialText(input.Organization, 200) || !credentialText(input.Project, 200) {
				return nil, result, errors.New("Invalid organization or project")
			}
			if err := s.mcpOrganization(ctx, grant, input.Organization); err != nil {
				return nil, result, err
			}
			access := store.ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs, ReadOnly: true}
			rows, err := s.db.ScopedAlerts(ctx, access, input.Project, (input.Page-1)*input.Limit, input.Limit)
			if err != nil {
				return nil, result, mcpAlertMonitorError(err)
			}
			more := len(rows) > input.Limit
			if more {
				rows = rows[:input.Limit]
			}
			data := make([]mcpAlertSummary, 0, len(rows))
			for _, row := range rows {
				data = append(data, alertSummary(row))
			}
			return nil, mcpWorkspaceResult[mcpAlertSummary]{Data: data, Page: input.Page, PageSize: input.Limit, HasMore: more}, nil
		})
	if s.cfg.Uptime {
		mcp.AddTool(server, &mcp.Tool{Name: "list_monitors", Description: "List uptime monitors visible to the current member without heartbeat capability URLs. Requires org:read.", InputSchema: mcpAlertMonitorSchema(false), OutputSchema: mcpEventOutputSchema[mcpWorkspaceResult[uptime.Monitor]](), Annotations: annotations},
			func(_ context.Context, _ *mcp.CallToolRequest, input mcpAlertMonitorInput) (*mcp.CallToolResult, mcpWorkspaceResult[uptime.Monitor], error) {
				var result mcpWorkspaceResult[uptime.Monitor]
				if !slices.Contains(grant.Scopes, "org:read") {
					return nil, result, errors.New("Credential requires org:read")
				}
				if input.Organization == "" || !credentialText(input.Organization, 200) {
					return nil, result, errors.New("Invalid organization")
				}
				if err := s.mcpOrganization(ctx, grant, input.Organization); err != nil {
					return nil, result, err
				}
				access := store.ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs, ReadOnly: true}
				rows, err := s.db.ScopedMonitors(ctx, access, (input.Page-1)*input.Limit, input.Limit)
				if err != nil {
					return nil, result, mcpAlertMonitorError(err)
				}
				return nil, mcpWorkspacePage(rows, input.mcpWorkspaceInput), nil
			})
	}
}
