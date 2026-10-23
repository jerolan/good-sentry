package store

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) TraceNodeData(ctx context.Context, userID, orgSlug, traceID, nodeID, projectID string) (TraceNodeData, error) {
	parts := strings.Split(nodeID, "-")
	if len(parts) < 2 || len(parts) > 3 || (parts[0] != "event" && parts[0] != "span") || (parts[0] == "span" && len(parts) != 2) {
		return TraceNodeData{}, ErrNotFound
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != parts[1] {
		return TraceNodeData{}, ErrNotFound
	}
	ordinal := int64(0)
	if len(parts) == 3 {
		ordinal, err = strconv.ParseInt(parts[2], 10, 32)
		if err != nil || ordinal < 1 || strconv.FormatInt(ordinal, 10) != parts[2] {
			return TraceNodeData{}, ErrNotFound
		}
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) (TraceNodeData, error) {
		orgID, err := s.traceArchiveScope(ctx, read, userID, orgSlug, projectID)
		if err != nil {
			return TraceNodeData{}, err
		}
		if parts[0] == "span" {
			selector := spanArchiveSelector{OrganizationID: orgID, TraceID: strings.ToLower(traceID)}
			if projectID != "" {
				selector.ProjectID, err = strconv.ParseInt(projectID, 10, 64)
				if err != nil {
					return TraceNodeData{}, err
				}
			}
			if err := read.Load(ctx, selector); err != nil {
				return TraceNodeData{}, err
			}
		}
		return s.traceNodeDataWithRead(ctx, read.tx, orgID, traceID, nodeID, projectID, parts, id, ordinal)
	})
}

func (s *Store) traceNodeDataWithRead(ctx context.Context, tx pgx.Tx, organizationID int64, traceID, nodeID, projectID string, parts []string, id, ordinal int64) (TraceNodeData, error) {
	orgID := strconv.FormatInt(organizationID, 10)
	result := TraceNodeData{ID: nodeID, Source: parts[0], RecordID: parts[1], Ordinal: int(ordinal)}
	args := []any{orgID, strings.ToLower(traceID), projectID, id}
	if parts[0] == "event" {
		var version int
		args = append(args, ordinal)
		err := tx.QueryRow(ctx, `SELECT e.project_id::text,e.span_projection_version,
		 CASE WHEN $5::integer=0 THEN e.data WHEN jsonb_typeof(e.data->'spans')='array' THEN (e.data->'spans')->($5::integer-1) END
		 FROM events e JOIN projects p ON p.id=e.project_id
		 WHERE p.organization_id=$1::bigint AND e.trace_id<>'' AND lower(e.trace_id)=$2 AND ($3='' OR e.project_id=NULLIF($3,'')::bigint) AND e.id=$4 AND e.kind='transaction'`, args...).Scan(&result.ProjectID, &version, &result.Data)
		if err != nil {
			return TraceNodeData{}, dbError(err)
		}
		if result.Data == nil {
			return TraceNodeData{}, ErrNotFound
		}
		if version < 2 {
			return TraceNodeData{}, ErrSpanProjectionPending
		}
	} else {
		err := tx.QueryRow(ctx, `SELECT s.project_id::text,s.data FROM spans s JOIN projects p ON p.id=s.project_id
		 WHERE p.organization_id=$1::bigint AND s.trace_id=$2 AND ($3='' OR s.project_id=NULLIF($3,'')::bigint) AND s.id=$4`, args...).Scan(&result.ProjectID, &result.Data)
		if err != nil {
			return TraceNodeData{}, dbError(err)
		}
	}
	return result, nil
}
