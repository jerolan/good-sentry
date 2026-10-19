package store

import (
	"context"
	"strings"
)

func (s *Store) TraceErrors(ctx context.Context, userID, orgSlug, traceID, projectID string, page int) (TraceErrors, error) {
	tx, orgID, err := s.traceRead(ctx, userID, orgSlug, projectID)
	if err != nil {
		return TraceErrors{}, err
	}
	defer tx.Rollback(ctx)
	result := TraceErrors{Data: []TraceError{}, Page: max(page, 1), PageSize: 25}
	args := []any{orgID, strings.ToLower(traceID), projectID}
	where := ` FROM events e JOIN projects p ON p.id=e.project_id WHERE p.organization_id=$1::bigint AND e.trace_id<>'' AND lower(e.trace_id)=$2 AND ($3='' OR e.project_id=NULLIF($3,'')::bigint) AND e.kind NOT IN ('transaction','feedback')`
	if err := tx.QueryRow(ctx, "SELECT count(*)"+where, args...).Scan(&result.Total); err != nil {
		return TraceErrors{}, err
	}
	offset := int64(result.Page-1) * 25
	args = append(args, offset)
	rows, err := tx.Query(ctx, `SELECT e.id::text,left(e.event_id,2001),COALESCE(e.issue_id::text,''),e.project_id::text,left(p.name,2001),left(e.title,2001),left(e.level,2001),e.timestamp,
	 CASE WHEN jsonb_typeof(e.data#>'{contexts,trace,span_id}')='string' THEN left(e.data#>>'{contexts,trace,span_id}',2001) ELSE '' END`+where+` ORDER BY e.timestamp,e.id LIMIT 25 OFFSET $4`, args...)
	if err != nil {
		return TraceErrors{}, err
	}
	for rows.Next() {
		var e TraceError
		if err := rows.Scan(&e.ID, &e.EventID, &e.IssueID, &e.ProjectID, &e.ProjectName, &e.Title, &e.Level, &e.Timestamp, &e.SpanID); err != nil {
			rows.Close()
			return TraceErrors{}, err
		}
		e.Timestamp = e.Timestamp.UTC()
		tracePreview(&e.SummaryTruncated, &e.EventID, &e.ProjectName, &e.Title, &e.Level, &e.SpanID)
		result.Data = append(result.Data, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return TraceErrors{}, err
	}
	result.HasMore = offset+int64(len(result.Data)) < result.Total
	if err := tx.Commit(ctx); err != nil {
		return TraceErrors{}, err
	}
	return result, nil
}
