package server

import (
	"context"
	"errors"
	"net/url"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"goodsentry/internal/store"
)

type mcpPerformanceInput struct {
	mcpWorkspaceInput
	Project     string  `json:"project,omitempty"`
	Transaction string  `json:"transaction,omitempty"`
	Op          string  `json:"op,omitempty"`
	Start       *string `json:"start,omitempty"`
	End         *string `json:"end,omitempty"`
	Threshold   int     `json:"threshold,omitempty"`
}

func mcpPerformanceSchema(name string) map[string]any {
	schema := mcpWorkspaceSchema(false)
	properties := schema["properties"].(map[string]any)
	delete(properties, "query")
	properties["organization"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	properties["project"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	properties["start"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 64}
	properties["end"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 64}
	if name != "detect_n_plus_one" {
		properties["transaction"] = map[string]any{"type": "string", "maxLength": 2000}
		properties["op"] = map[string]any{"type": "string", "maxLength": 200}
	}
	required := []string{"organization"}
	switch name {
	case "get_transaction_group", "list_transaction_spans", "get_transaction_trend":
		required = append(required, "transaction")
	case "list_span_groups":
		properties["query"] = map[string]any{"type": "string", "maxLength": 2000}
	case "detect_n_plus_one":
		properties["threshold"] = map[string]any{"type": "integer", "minimum": 2, "maximum": 100, "default": 5}
	}
	if name == "list_transaction_spans" || name == "get_transaction_trend" {
		delete(properties, "limit")
	}
	if name == "get_transaction_group" || name == "get_transaction_trend" {
		delete(properties, "page")
	}
	schema["required"] = required
	return schema
}

func mcpPerformanceAuthority(grant store.APITokenGrant) (store.ManagementAccess, error) {
	if !slices.Contains(grant.Scopes, "event:read") && !slices.Contains(grant.Scopes, "event:write") {
		return store.ManagementAccess{}, errors.New("Credential requires event:read or event:write")
	}
	return store.ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs, ReadOnly: true}, nil
}

func mcpPerformanceError(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden):
		return errors.New("Transaction group or project not found")
	case errors.Is(err, store.ErrAmbiguousTransactionGroup):
		return errors.New("Multiple operations match this transaction; provide op")
	case errors.Is(err, store.ErrSpanProjectionPending):
		return errors.New("Historical spans are still being indexed; retry shortly")
	default:
		return errors.New("Performance data unavailable")
	}
}

func (s *Server) mcpOrganizationSlug(ctx context.Context, grant store.APITokenGrant, reference string) (string, error) {
	organizations, err := s.db.CompatibleOrganizations(ctx, grant.User.ID, grant.OrganizationID, "", false, "name", 0, 1)
	if err != nil {
		return "", errors.New("Organization unavailable")
	}
	if len(organizations) != 1 || reference != organizations[0].ID && reference != organizations[0].Slug {
		return "", errors.New("Organization not found")
	}
	return organizations[0].Slug, nil
}

func (s *Server) mcpWindow(input mcpPerformanceInput) (*store.TimeWindow, error) {
	query := url.Values{}
	if input.Start != nil {
		query.Set("start", *input.Start)
	}
	if input.End != nil {
		query.Set("end", *input.End)
	}
	return queryTimeWindow(query)
}

func mcpPerformanceText(input mcpPerformanceInput) bool {
	return credentialText(input.Organization, 200) && credentialText(input.Project, 200) &&
		credentialText(input.Transaction, 2000) && credentialText(input.Op, 200) && credentialText(input.Query, 2000)
}

func (s *Server) addMCPPerformanceTools(server *mcp.Server, ctx context.Context, grant store.APITokenGrant) {
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	common := func(input mcpPerformanceInput) (store.ManagementAccess, string, *store.TimeWindow, error) {
		access, err := mcpPerformanceAuthority(grant)
		if err != nil {
			return access, "", nil, err
		}
		if input.Organization == "" || !mcpPerformanceText(input) {
			return access, "", nil, errors.New("Invalid performance filter")
		}
		slug, err := s.mcpOrganizationSlug(ctx, grant, input.Organization)
		if err != nil {
			return access, "", nil, err
		}
		window, err := s.mcpWindow(input)
		if err != nil {
			return access, "", nil, err
		}
		return access, slug, window, nil
	}
	mcp.AddTool(server, &mcp.Tool{Name: "list_transaction_groups", Description: "List transaction endpoints and operations with count, errors and latency percentiles. Requires event:read or event:write.", InputSchema: mcpPerformanceSchema("list_transaction_groups"), OutputSchema: mcpEventOutputSchema[mcpWorkspaceResult[store.TransactionGroup]](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpPerformanceInput) (*mcp.CallToolResult, mcpWorkspaceResult[store.TransactionGroup], error) {
			var result mcpWorkspaceResult[store.TransactionGroup]
			access, _, window, err := common(input)
			if err != nil {
				return nil, result, err
			}
			rows, err := s.db.ScopedTransactions(ctx, access, store.TransactionFilter{Project: input.Project, Window: window, Offset: (input.Page - 1) * input.Limit, Limit: input.Limit + 1})
			if err != nil {
				return nil, result, mcpPerformanceError(err)
			}
			return nil, mcpWorkspacePage(rows, input.mcpWorkspaceInput), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_transaction_group", Description: "Read one transaction operation group and its latest authorized sample. Requires event:read or event:write.", InputSchema: mcpPerformanceSchema("get_transaction_group"), OutputSchema: mcpEventOutputSchema[store.TransactionGroup](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpPerformanceInput) (*mcp.CallToolResult, store.TransactionGroup, error) {
			var result store.TransactionGroup
			access, _, window, err := common(input)
			if err != nil {
				return nil, result, err
			}
			if input.Transaction == "" {
				return nil, result, errors.New("Transaction is required")
			}
			result, err = s.db.ScopedTransactionGroup(ctx, access, store.TransactionFilter{Project: input.Project, Transaction: input.Transaction, Op: input.Op, Window: window})
			if err != nil {
				return nil, store.TransactionGroup{}, mcpPerformanceError(err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_transaction_spans", Description: "Read child span groups from the latest authorized sample of a transaction operation. Requires event:read or event:write.", InputSchema: mcpPerformanceSchema("list_transaction_spans"), OutputSchema: mcpEventOutputSchema[store.TransactionSpanGroups](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpPerformanceInput) (*mcp.CallToolResult, store.TransactionSpanGroups, error) {
			var result store.TransactionSpanGroups
			access, slug, window, err := common(input)
			if err != nil {
				return nil, result, err
			}
			if input.Transaction == "" {
				return nil, result, errors.New("Transaction is required")
			}
			group, err := s.db.ScopedTransactionGroup(ctx, access, store.TransactionFilter{Project: input.Project, Transaction: input.Transaction, Op: input.Op, Window: window})
			if err != nil {
				return nil, result, mcpPerformanceError(err)
			}
			result, err = s.db.TransactionSpanGroups(ctx, grant.User.ID, slug, group.LatestSample.Source, group.LatestSample.ID, group.LatestSample.ProjectID, window, input.Page)
			if err != nil {
				return nil, store.TransactionSpanGroups{}, mcpPerformanceError(err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_span_groups", Description: "List authorized span operations with trace counts, errors, latency and time. Requires event:read or event:write.", InputSchema: mcpPerformanceSchema("list_span_groups"), OutputSchema: mcpEventOutputSchema[mcpWorkspaceResult[store.SpanGroup]](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpPerformanceInput) (*mcp.CallToolResult, mcpWorkspaceResult[store.SpanGroup], error) {
			var result mcpWorkspaceResult[store.SpanGroup]
			access, _, window, err := common(input)
			if err != nil {
				return nil, result, err
			}
			rows, err := s.db.ScopedSpanGroups(ctx, access, store.SpanGroupFilter{Project: input.Project, Op: input.Op, Query: input.Query, Window: window, Offset: (input.Page - 1) * input.Limit, Limit: input.Limit + 1})
			if err != nil {
				return nil, result, mcpPerformanceError(err)
			}
			return nil, mcpWorkspacePage(rows, input.mcpWorkspaceInput), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "detect_n_plus_one", Description: "Find repeated child span operations within authorized traces. Requires event:read or event:write.", InputSchema: mcpPerformanceSchema("detect_n_plus_one"), OutputSchema: mcpEventOutputSchema[mcpWorkspaceResult[store.NPlusOneFinding]](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpPerformanceInput) (*mcp.CallToolResult, mcpWorkspaceResult[store.NPlusOneFinding], error) {
			var result mcpWorkspaceResult[store.NPlusOneFinding]
			access, _, window, err := common(input)
			if err != nil {
				return nil, result, err
			}
			rows, err := s.db.DetectNPlusOne(ctx, access, store.NPlusOneFilter{Project: input.Project, Window: window, Threshold: input.Threshold, Offset: (input.Page - 1) * input.Limit, Limit: input.Limit + 1})
			if err != nil {
				return nil, result, mcpPerformanceError(err)
			}
			return nil, mcpWorkspacePage(rows, input.mcpWorkspaceInput), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_transaction_trend", Description: "Read daily UTC count, error and latency trend buckets for a transaction operation. Requires event:read or event:write.", InputSchema: mcpPerformanceSchema("get_transaction_trend"), OutputSchema: mcpEventOutputSchema[struct {
		Data []store.TransactionTrendPoint `json:"data"`
	}](), Annotations: annotations},
		func(_ context.Context, _ *mcp.CallToolRequest, input mcpPerformanceInput) (*mcp.CallToolResult, struct {
			Data []store.TransactionTrendPoint `json:"data"`
		}, error) {
			result := struct {
				Data []store.TransactionTrendPoint `json:"data"`
			}{}
			access, _, window, err := common(input)
			if err != nil {
				return nil, result, err
			}
			if input.Transaction == "" {
				return nil, result, errors.New("Transaction is required")
			}
			result.Data, err = s.db.ScopedTransactionTrend(ctx, access, store.TransactionFilter{Project: input.Project, Transaction: input.Transaction, Op: input.Op, Window: window})
			if err != nil {
				return nil, struct {
					Data []store.TransactionTrendPoint `json:"data"`
				}{}, mcpPerformanceError(err)
			}
			return nil, result, nil
		})
}
