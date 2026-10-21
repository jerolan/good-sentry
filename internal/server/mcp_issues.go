package server

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"goodsentry/internal/eventview"
	"goodsentry/internal/store"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpIssueInput struct {
	mcpWorkspaceInput
	Project     string `json:"project,omitempty"`
	Environment string `json:"environment,omitempty"`
	IssueID     string `json:"issue_id,omitempty"`
	EventID     string `json:"event_id,omitempty"`
	Status      string `json:"status,omitempty"`
}

type mcpEventResult struct {
	Event eventview.Event      `json:"event"`
	Issue *compatibleIssueView `json:"issue"`
}

// Describe the existing wire projection: RawMessage contains JSON values,
// and a nil embedded SiblingIDs omits both navigation properties entirely.
func mcpEventOutputSchema[T any]() *jsonschema.Schema {
	options := &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[json.RawMessage](): {},
	}}
	eventSchema, err := jsonschema.For[eventview.Event](options)
	if err != nil {
		panic(err)
	}
	eventSchema.Required = slices.DeleteFunc(eventSchema.Required, func(name string) bool {
		return name == "previousEventID" || name == "nextEventID"
	})
	options.TypeSchemas[reflect.TypeFor[eventview.Event]()] = eventSchema
	schema, err := jsonschema.For[T](options)
	if err != nil {
		panic(err)
	}
	return schema
}

func mcpIssueSchema(name string) map[string]any {
	text := func() map[string]any { return map[string]any{"type": "string", "minLength": 1, "maxLength": 200} }
	properties := map[string]any{}
	required := []string{"issue_id"}
	if name == "list_issues" {
		schema := mcpWorkspaceSchema(false)
		properties = schema["properties"].(map[string]any)
		properties["organization"], properties["project"], properties["environment"] = text(), text(), text()
		properties["query"] = map[string]any{"type": "string", "maxLength": 2000, "default": "is:unresolved"}
		schema["required"] = []string{"organization"}
		return schema
	}
	if name == "get_event" {
		properties["event_id"] = map[string]any{"type": "string", "minLength": 32, "maxLength": 36}
		properties["project"] = text()
		required = []string{"event_id"}
	} else {
		properties["issue_id"] = map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,18}$"}
	}
	if name == "update_issue" {
		properties["status"] = map[string]any{"type": "string", "enum": []string{"resolved", "unresolved", "ignored"}}
		required = append(required, "status")
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func mcpIssueAuthority(grant store.APITokenGrant, write bool) (store.ManagementAccess, error) {
	writer := slices.Contains(grant.Scopes, "event:write")
	if !writer && (write || !slices.Contains(grant.Scopes, "event:read")) {
		return store.ManagementAccess{}, errors.New("Credential lacks the required issue scope")
	}
	return store.ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs, ReadOnly: !write}, nil
}

func mcpIssueError(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden):
		return errors.New("Issue or event not found")
	case errors.Is(err, store.ErrAmbiguousEvent):
		return errors.New("Event ID is ambiguous; provide a project")
	case errors.Is(err, store.ErrInvalidCompatibleIssueFilter), errors.Is(err, store.ErrInvalidEventFilter), errors.Is(err, store.ErrInvalidCompatibleIssueChange):
		return errors.New("Invalid issue or event reference or query")
	default:
		return errors.New("Issue data unavailable")
	}
}

func (s *Server) mcpOrganization(ctx context.Context, grant store.APITokenGrant, reference string) error {
	organizations, err := s.db.CompatibleOrganizations(ctx, grant.User.ID, grant.OrganizationID, "", false, "name", 0, 1)
	if err != nil {
		return errors.New("Organization unavailable")
	}
	if len(organizations) != 1 || reference != organizations[0].ID && reference != organizations[0].Slug {
		return errors.New("Organization not found")
	}
	return nil
}

func (s *Server) addMCPIssueTools(server *mcp.Server, ctx context.Context, grant store.APITokenGrant) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "list_issues", Description: "Search authorized issues, optionally within a project. Defaults to unresolved issues.", InputSchema: mcpIssueSchema("list_issues"), Annotations: readOnly},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpIssueInput) (*mcp.CallToolResult, mcpWorkspaceResult[compatibleIssueView], error) {
			var result mcpWorkspaceResult[compatibleIssueView]
			access, err := mcpIssueAuthority(grant, false)
			if err != nil {
				return nil, result, err
			}
			if !credentialText(input.Query, 2000) || !credentialText(input.Organization, 200) || !credentialText(input.Project, 200) || !credentialText(input.Environment, 200) {
				return nil, result, errors.New("Invalid query or reference")
			}
			if err := s.mcpOrganization(ctx, grant, input.Organization); err != nil {
				return nil, result, err
			}
			filter := store.CompatibleIssueFilter{Query: input.Query, Sort: "-last_seen", StatsPeriod: "24h", Offset: (input.Page - 1) * input.Limit, Limit: input.Limit}
			if input.Environment != "" {
				filter.Environments = []string{input.Environment}
			}
			rows, err := s.db.CompatibleIssues(ctx, access, input.Project, filter)
			if err != nil {
				return nil, result, mcpIssueError(err)
			}
			more := len(rows) > input.Limit
			if more {
				rows = rows[:input.Limit]
			}
			views := make([]compatibleIssueView, 0, len(rows))
			for _, row := range rows {
				view, err := projectCompatibleIssue(row, s.cfg.Domain, false)
				if err != nil {
					return nil, result, mcpIssueError(err)
				}
				views = append(views, view)
			}
			return nil, mcpWorkspaceResult[compatibleIssueView]{Data: views, Page: input.Page, PageSize: input.Limit, HasMore: more}, nil
		})
	for _, tool := range []struct {
		name, description string
		write             bool
	}{
		{"get_issue", "Read an authorized issue and its current state.", false},
		{"update_issue", "Resolve, unresolve or indefinitely ignore an authorized issue. Resolve and ignore clear conditional policies.", true},
	} {
		write := tool.write
		annotations := readOnly
		if write {
			annotations = &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(bool)}
		}
		mcp.AddTool(server, &mcp.Tool{Name: tool.name, Description: tool.description, InputSchema: mcpIssueSchema(tool.name), Annotations: annotations},
			func(_ context.Context, _ *mcp.CallToolRequest, input mcpIssueInput) (*mcp.CallToolResult, compatibleIssueView, error) {
				var view compatibleIssueView
				access, err := mcpIssueAuthority(grant, write)
				if err != nil {
					return nil, view, err
				}
				if !issueTargetID(input.IssueID) {
					return nil, view, errors.New("Invalid issue ID")
				}
				var row store.CompatibleIssueRecord
				if write {
					change := store.CompatibleIssueChange{Status: input.Status, Policy: &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueClearPolicy}}
					row, err = s.db.UpdateCompatibleIssue(ctx, access, input.IssueID, change)
				} else {
					row, err = s.db.CompatibleIssue(ctx, access, input.IssueID, store.CompatibleIssueDetailFilter{StatsPeriod: "24h"})
				}
				if err != nil {
					return nil, view, mcpIssueError(err)
				}
				view, err = projectCompatibleIssue(row, s.cfg.Domain, true)
				if err != nil {
					return nil, compatibleIssueView{}, mcpIssueError(err)
				}
				return nil, view, nil
			})
	}
	mcp.AddTool(server, &mcp.Tool{Name: "get_latest_event", Description: "Read the latest retained occurrence of an authorized issue, including stack traces and event context.", InputSchema: mcpIssueSchema("get_latest_event"), OutputSchema: mcpEventOutputSchema[eventview.Event](), Annotations: readOnly},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpIssueInput) (*mcp.CallToolResult, eventview.Event, error) {
			var view eventview.Event
			access, err := mcpIssueAuthority(grant, false)
			if err != nil {
				return nil, view, err
			}
			if !issueTargetID(input.IssueID) {
				return nil, view, errors.New("Invalid issue ID")
			}
			row, err := s.db.CompatibleEvent(ctx, access, store.EventIssueTarget(input.IssueID), "latest", nil)
			if err != nil {
				return nil, view, mcpIssueError(err)
			}
			view, err = eventview.Project(row, true)
			if err != nil {
				return nil, eventview.Event{}, mcpIssueError(err)
			}
			return nil, view, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_event", Description: "Read an authorized event and its parent issue in one snapshot. Supply a project when an event ID occurs in multiple projects.", InputSchema: mcpIssueSchema("get_event"), OutputSchema: mcpEventOutputSchema[mcpEventResult](), Annotations: readOnly},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpIssueInput) (*mcp.CallToolResult, mcpEventResult, error) {
			var result mcpEventResult
			access, err := mcpIssueAuthority(grant, false)
			if err != nil {
				return nil, result, err
			}
			if !credentialText(input.Project, 200) {
				return nil, result, errors.New("Invalid project")
			}
			row, err := s.db.CompatibleEventLookup(ctx, access, input.Project, input.EventID)
			if err != nil {
				return nil, result, mcpIssueError(err)
			}
			result.Event, err = eventview.Project(row.Event, true)
			if err == nil && row.Issue != nil {
				view, projectionErr := projectCompatibleIssue(*row.Issue, s.cfg.Domain, true)
				err = projectionErr
				result.Issue = &view
			}
			if err != nil {
				return nil, mcpEventResult{}, mcpIssueError(err)
			}
			return nil, result, nil
		})
}
