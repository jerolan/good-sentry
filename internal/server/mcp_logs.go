package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"goodsentry/internal/otlp"
	"goodsentry/internal/store"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpLogInput struct {
	mcpWorkspaceInput
	Project     string  `json:"project,omitempty"`
	Level       string  `json:"level,omitempty"`
	Environment string  `json:"environment,omitempty"`
	Service     string  `json:"service,omitempty"`
	TraceID     string  `json:"trace_id,omitempty"`
	Start       *string `json:"start,omitempty"`
	End         *string `json:"end,omitempty"`
	LogID       string  `json:"log_id,omitempty"`
}

func mcpLogSchema(list bool) map[string]any {
	if !list {
		return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"log_id"}, "properties": map[string]any{
			"log_id": map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,18}$"},
		}}
	}
	schema := mcpWorkspaceSchema(false)
	properties := schema["properties"].(map[string]any)
	for _, field := range []string{"query", "environment", "service"} {
		properties[field] = map[string]any{"type": "string", "maxLength": 2000}
	}
	for _, field := range []string{"organization", "project"} {
		properties[field] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	}
	for _, field := range []string{"start", "end"} {
		properties[field] = map[string]any{"type": "string", "minLength": 1, "maxLength": 64}
	}
	properties["level"] = map[string]any{"type": "string", "maxLength": 32}
	properties["trace_id"] = map[string]any{"type": "string", "pattern": "^[0-9a-fA-F]{32}$"}
	schema["required"] = []string{"organization"}
	return schema
}

func mcpLogOutputSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): {}}})
	if err != nil {
		panic(err)
	}
	return schema
}

func mcpLogAuthority(grant store.APITokenGrant) (store.ManagementAccess, error) {
	if !slices.Contains(grant.Scopes, "log:read") {
		return store.ManagementAccess{}, errors.New("Credential requires log:read")
	}
	return store.ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs, ReadOnly: true}, nil
}

func mcpLogError(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden):
		return errors.New("Log or project not found")
	case errors.Is(err, store.ErrInvalidLogFilter):
		return errors.New("Invalid log reference or filter")
	case errors.Is(err, otlp.ErrTooLarge):
		return errors.New("Log detail exceeds current capacity")
	default:
		return errors.New("Log data unavailable")
	}
}

func (s *Server) addMCPLogTools(server *mcp.Server, ctx context.Context, grant store.APITokenGrant) {
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "list_logs", Description: "Search retained logs in authorized projects by text, service, level, environment, trace or time. Requires log:read.", InputSchema: mcpLogSchema(true), OutputSchema: mcpLogOutputSchema[mcpWorkspaceResult[store.Log]](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpLogInput) (*mcp.CallToolResult, mcpWorkspaceResult[store.Log], error) {
			var result mcpWorkspaceResult[store.Log]
			access, err := mcpLogAuthority(grant)
			if err != nil {
				return nil, result, err
			}
			if !credentialText(input.Organization, 200) || !credentialText(input.Project, 200) {
				return nil, result, errors.New("Invalid organization or project")
			}
			for _, text := range []string{input.Query, input.Level, input.Environment, input.Service} {
				if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
					return nil, result, errors.New("Log filters must be valid Unicode without NUL")
				}
			}
			query := url.Values{}
			if input.Start != nil {
				query.Set("start", *input.Start)
			}
			if input.End != nil {
				query.Set("end", *input.End)
			}
			window, err := queryTimeWindow(query)
			if err != nil {
				return nil, result, err
			}
			trace := input.TraceID
			if trace != "" {
				var valid bool
				trace, valid = canonicalTraceID(trace)
				if !valid {
					return nil, result, errors.New("Invalid trace ID")
				}
			}
			if err := s.mcpOrganization(ctx, grant, input.Organization); err != nil {
				return nil, result, err
			}
			filter := store.LogFilter{Project: input.Project, Query: input.Query, Level: input.Level, Environment: input.Environment, Service: input.Service, TraceID: trace, Window: window, CanonicalTrace: true}
			rows, err := s.db.ScopedLogs(ctx, access, filter, (input.Page-1)*input.Limit, input.Limit)
			if err != nil {
				return nil, result, mcpLogError(err)
			}
			return nil, mcpWorkspacePage(rows, input.mcpWorkspaceInput), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_log", Description: "Read one authorized retained log, including its canonical selected OpenTelemetry data. Requires log:read.", InputSchema: mcpLogSchema(false), OutputSchema: mcpLogOutputSchema[store.LogDetail](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpLogInput) (*mcp.CallToolResult, store.LogDetail, error) {
			var result store.LogDetail
			access, err := mcpLogAuthority(grant)
			if err != nil {
				return nil, result, err
			}
			if _, valid := occurrenceNumber(input.LogID, math.MaxInt64); !valid {
				return nil, result, errors.New("Invalid log ID")
			}
			result, err = s.db.ScopedLog(ctx, access, input.LogID, s.otlpLimits())
			if err != nil {
				return nil, store.LogDetail{}, mcpLogError(err)
			}
			return nil, result, nil
		})
}
