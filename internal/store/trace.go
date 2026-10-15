package store

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) traceRead(ctx context.Context, userID, orgSlug, projectID string) (pgx.Tx, string, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return nil, "", err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", err
	}
	if projectID != "" {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE organization_id=$1::bigint AND id=$2::bigint)", org.ID, projectID).Scan(&exists); err != nil || !exists {
			_ = tx.Rollback(ctx)
			if err == nil {
				err = ErrNotFound
			}
			return nil, "", err
		}
	}
	return tx, org.ID, nil
}

func (s *Store) Trace(ctx context.Context, userID, orgSlug, traceID, projectID string, page int, logsEnabled bool) (TraceDetail, error) {
	tx, orgID, err := s.traceRead(ctx, userID, orgSlug, projectID)
	if err != nil {
		return TraceDetail{}, err
	}
	defer tx.Rollback(ctx)
	traceID = strings.ToLower(traceID)
	args := []any{orgID, traceID, projectID}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM events e JOIN projects p ON p.id=e.project_id
	 WHERE p.organization_id=$1::bigint AND e.trace_id<>'' AND lower(e.trace_id)=$2 AND ($3='' OR e.project_id=NULLIF($3,'')::bigint)
	 AND e.kind='transaction' AND e.span_projection_version<2)`, args...).Scan(&pending); err != nil {
		return TraceDetail{}, err
	}
	if pending {
		return TraceDetail{}, ErrSpanProjectionPending
	}
	rows, err := tx.Query(ctx, `WITH nodes AS (
	 SELECT e.project_id,0 AS source,e.id AS row_id,0 AS ordinal,e.trace_span_id AS span_id,e.trace_parent_span_id AS parent_span_id,e.trace_start_timestamp AS start_at,e.timestamp AS end_at
	 FROM events e JOIN projects p ON p.id=e.project_id WHERE p.organization_id=$1::bigint AND e.trace_id<>'' AND lower(e.trace_id)=$2 AND ($3='' OR e.project_id=NULLIF($3,'')::bigint) AND e.kind='transaction'
	 UNION ALL
	 SELECT e.project_id,1,e.id,o.ordinal,o.span_id,o.parent_span_id,o.start_timestamp,o.end_timestamp
	 FROM events e JOIN projects p ON p.id=e.project_id JOIN transaction_span_operations o ON o.event_id=e.id
	 WHERE p.organization_id=$1::bigint AND e.trace_id<>'' AND lower(e.trace_id)=$2 AND ($3='' OR e.project_id=NULLIF($3,'')::bigint) AND e.kind='transaction'
	 UNION ALL
	 SELECT s.project_id,2,s.id,0,s.span_id,s.parent_span_id,s.start_timestamp,s.end_timestamp
	 FROM spans s JOIN projects p ON p.id=s.project_id WHERE p.organization_id=$1::bigint AND s.trace_id=$2 AND ($3='' OR s.project_id=NULLIF($3,'')::bigint)
	)
	 SELECT project_id,source,row_id,ordinal,
	 CASE WHEN span_id ~ '^[0-9a-fA-F]{16}$' THEN lower(span_id) ELSE '' END,span_id<>'',
	 CASE WHEN parent_span_id ~ '^[0-9a-fA-F]{16}$' THEN lower(parent_span_id) ELSE '' END,parent_span_id<>'',start_at,end_at FROM nodes`, args...)
	if err != nil {
		return TraceDetail{}, err
	}
	nodes := []traceIndexNode{}
	projects := map[int64]struct{}{}
	for rows.Next() {
		var n traceIndexNode
		if err := rows.Scan(&n.project, &n.source, &n.row, &n.ordinal, &n.span, &n.spanPresent, &n.parent, &n.parentPresent, &n.start, &n.end); err != nil {
			rows.Close()
			return TraceDetail{}, err
		}
		n.start, n.end = traceInstant(n.start), traceInstant(n.end)
		nodes = append(nodes, n)
		projects[n.project] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return TraceDetail{}, err
	}
	result := TraceDetail{TraceID: traceID, ProjectID: projectID, NodeCount: int64(len(nodes)), LogsEnabled: logsEnabled, Page: max(page, 1), PageSize: 50, Data: []TraceNode{}}
	result.ErrorCount, err = traceRelatedProjects(ctx, tx, "events", args, projects)
	if err != nil {
		return TraceDetail{}, err
	}
	if logsEnabled {
		result.LogCount, err = traceRelatedProjects(ctx, tx, "logs", args, projects)
		if err != nil {
			return TraceDetail{}, err
		}
	}
	if len(nodes) == 0 && result.ErrorCount == 0 && result.LogCount == 0 {
		return TraceDetail{}, ErrNotFound
	}
	result.ProjectCount = int64(len(projects))
	order := traceForest(nodes, &result)
	first := int64(result.Page-1) * 50
	last := min(first+50, int64(len(order)))
	selected := []traceIdentity{}
	for i := first; i < last; i++ {
		selected = append(selected, nodes[order[i]].traceIdentity)
	}
	metadata, err := traceMetadata(ctx, tx, selected)
	if err != nil {
		return TraceDetail{}, err
	}
	for i := first; i < last; i++ {
		n := &nodes[order[i]]
		item := metadata[n.traceIdentity]
		item.ID = n.nodeID()
		item.RecordID = strconv.FormatInt(n.row, 10)
		item.Ordinal = n.ordinal
		item.ProjectID = strconv.FormatInt(n.project, 10)
		item.Source = "event"
		if n.source == 2 {
			item.Source = "span"
		}
		item.SpanIDStatus = "valid"
		if n.span == "" {
			item.SpanIDStatus = "missing"
			if n.spanPresent {
				item.SpanIDStatus = "invalid"
			}
		}
		item.ParentStatus = n.parentStatus
		item.Depth = n.depth
		item.ChildCount = len(n.children)
		if n.parentIndex >= 0 {
			parent := nodes[n.parentIndex]
			item.ParentID = parent.nodeID()
			item.ParentPage = parent.position/50 + 1
		}
		item.StartTimestamp, item.EndTimestamp = n.start, n.end
		if n.start != nil && n.end != nil && !n.end.Before(*n.start) {
			duration := traceMilliseconds(*n.start, *n.end)
			item.Duration = &duration
		}
		at := n.start
		if at == nil {
			at = n.end
		}
		if at != nil {
			offset := traceMilliseconds(*result.StartTimestamp, *at)
			item.Offset = &offset
		}
		result.Data = append(result.Data, item)
	}
	result.HasMore = last < int64(len(order))
	if err := tx.Commit(ctx); err != nil {
		return TraceDetail{}, err
	}
	return result, nil
}

func traceRelatedProjects(ctx context.Context, tx pgx.Tx, table string, args []any, projects map[int64]struct{}) (int64, error) {
	filter := ""
	if table == "events" {
		filter = " AND x.kind<>'transaction'"
	}
	rows, err := tx.Query(ctx, `SELECT x.project_id,count(*) FROM `+table+` x JOIN projects p ON p.id=x.project_id
	 WHERE p.organization_id=$1::bigint AND x.trace_id<>'' AND lower(x.trace_id)=$2 AND ($3='' OR x.project_id=NULLIF($3,'')::bigint)`+filter+` GROUP BY x.project_id`, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var project, count int64
		if err := rows.Scan(&project, &count); err != nil {
			return 0, err
		}
		projects[project] = struct{}{}
		total += count
	}
	return total, rows.Err()
}
