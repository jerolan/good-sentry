package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

func tracePreview(truncated *bool, fields ...*string) {
	for _, field := range fields {
		count := 0
		for i := range *field {
			if count == 2000 {
				*field = strings.Clone((*field)[:i])
				*truncated = true
				break
			}
			count++
		}
	}
}

func traceMetadata(ctx context.Context, tx pgx.Tx, selected []traceIdentity) (map[traceIdentity]TraceNode, error) {
	result := make(map[traceIdentity]TraceNode, len(selected))
	for source := 0; source < 3; source++ {
		ids := []int64{}
		ordinals := []int32{}
		for _, id := range selected {
			if id.source == source {
				ids = append(ids, id.row)
				ordinals = append(ordinals, int32(id.ordinal))
			}
		}
		if len(ids) == 0 {
			continue
		}
		var query string
		args := []any{ids}
		switch source {
		case 0:
			query = `SELECT e.project_id,e.id,0,left(p.name,2001),left(e.transaction_name,2001),left(e.trace_op,2001),left(e.trace_status,2001),left(e.environment,2001),left(e.release,2001),left(e.trace_span_id,2001),left(e.trace_parent_span_id,2001),'transaction'
		 FROM events e JOIN projects p ON p.id=e.project_id WHERE e.id=ANY($1::bigint[])`
		case 1:
			query = `SELECT e.project_id,e.id,o.ordinal,left(p.name,2001),left(o.description,2001),left(o.op,2001),left(o.status,2001),left(e.environment,2001),left(e.release,2001),left(o.span_id,2001),left(o.parent_span_id,2001),'span'
		 FROM unnest($1::bigint[],$2::integer[]) wanted(event_id,ordinal)
		 JOIN transaction_span_operations o ON o.event_id=wanted.event_id AND o.ordinal=wanted.ordinal
		 JOIN events e ON e.id=o.event_id JOIN projects p ON p.id=e.project_id`
			args = append(args, ordinals)
		case 2:
			query = `SELECT s.project_id,s.id,0,left(p.name,2001),left(s.name,2001),left(s.op,2001),left(s.status,2001),left(s.environment,2001),left(s.release,2001),left(s.span_id,2001),left(s.parent_span_id,2001),CASE WHEN s.is_segment THEN 'segment' ELSE 'span' END
		 FROM spans s JOIN projects p ON p.id=s.project_id WHERE s.id=ANY($1::bigint[])`
		}
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			id := traceIdentity{source: source}
			var n TraceNode
			if err := rows.Scan(&id.project, &id.row, &id.ordinal, &n.ProjectName, &n.Name, &n.Op, &n.Status, &n.Environment, &n.Release, &n.SpanID, &n.ParentSpanID, &n.Kind); err != nil {
				rows.Close()
				return nil, err
			}
			tracePreview(&n.SummaryTruncated, &n.ProjectName, &n.Name, &n.Op, &n.Status, &n.Environment, &n.Release, &n.SpanID, &n.ParentSpanID)
			result[id] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return result, nil
}
