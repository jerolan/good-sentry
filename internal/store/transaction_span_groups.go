package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrSpanProjectionPending = errors.New("transaction span projection pending")

type TransactionSpanGroups struct {
	Sample           TransactionReference   `json:"sample"`
	Transaction      string                 `json:"transaction"`
	Op               string                 `json:"op"`
	TransactionCount int64                  `json:"transactionCount"`
	SpanCount        int64                  `json:"spanCount"`
	TimedSpanCount   int64                  `json:"timedSpanCount"`
	GroupCount       int64                  `json:"groupCount"`
	Data             []TransactionSpanGroup `json:"data"`
	Page             int                    `json:"page"`
	PageSize         int                    `json:"pageSize"`
	HasMore          bool                   `json:"hasMore"`
}

type TransactionSpanGroup struct {
	Op          string   `json:"op"`
	Description string   `json:"description"`
	Count       int64    `json:"count"`
	TimedCount  int64    `json:"timedCount"`
	AvgDuration *float64 `json:"avgDuration"`
	P95Duration *float64 `json:"p95Duration"`
	TotalTime   *float64 `json:"totalTime"`
}

func (s *Store) ProjectTransactionSpans(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	var data json.RawMessage
	err = tx.QueryRow(ctx, `SELECT id,data FROM events
		WHERE kind='transaction' AND span_projection_version<2
		ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := projectTransactionSpans(ctx, tx, id, data); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) TransactionSpanGroups(ctx context.Context, userID, orgSlug, source, id, projectID string, window *TimeWindow, page int) (TransactionSpanGroups, error) {
	if source != "event" && source != "span" {
		return TransactionSpanGroups{}, ErrNotFound
	}
	rowID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || rowID <= 0 || strconv.FormatInt(rowID, 10) != id {
		return TransactionSpanGroups{}, ErrNotFound
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) (TransactionSpanGroups, error) {
		return s.transactionSpanGroupsWithRead(ctx, read, userID, orgSlug, source, id, rowID, projectID, window, page)
	})
}

func (s *Store) transactionSpanGroupsWithRead(ctx context.Context, read *spanArchiveRead, userID, orgSlug, source, id string, rowID int64, projectID string, window *TimeWindow, page int) (TransactionSpanGroups, error) {
	orgID, err := read.organizationID(ctx, userID, orgSlug)
	if err != nil {
		return TransactionSpanGroups{}, err
	}
	tx := read.tx
	result := TransactionSpanGroups{
		Sample: TransactionReference{Source: source, ID: id},
		Page:   max(page, 1), PageSize: 50, Data: []TransactionSpanGroup{},
	}
	if source == "event" {
		err = tx.QueryRow(ctx, `SELECT e.project_id::text,e.transaction_name,COALESCE(e.data#>>'{contexts,trace,op}','')
			FROM events e JOIN projects p ON p.id=e.project_id
			WHERE p.organization_id=$1::bigint AND e.id=$2 AND e.kind='transaction'`, orgID, rowID).Scan(
			&result.Sample.ProjectID, &result.Transaction, &result.Op)
	} else {
		if err := read.Load(ctx, spanArchiveSelector{OrganizationID: orgID, SpanRowID: rowID}); err != nil {
			return TransactionSpanGroups{}, err
		}
		err = tx.QueryRow(ctx, `SELECT s.project_id::text,s.name,s.op
			FROM spans s JOIN projects p ON p.id=s.project_id
			WHERE p.organization_id=$1::bigint AND s.id=$2 AND s.is_segment`, orgID, rowID).Scan(
			&result.Sample.ProjectID, &result.Transaction, &result.Op)
	}
	if err != nil {
		return TransactionSpanGroups{}, dbError(err)
	}
	args := []any{orgID, result.Transaction, result.Op}
	eventFilter := "e.timestamp>=now()-interval '24 hours' AND e.timestamp<=now()"
	spanFilter := "s.end_timestamp>=now()-interval '24 hours' AND s.end_timestamp<=now()"
	if window != nil {
		args = append(args, window.Start, window.End)
		eventFilter = "e.timestamp>=$4::timestamptz AND e.timestamp<$5::timestamptz"
		spanFilter = "s.end_timestamp>=$4::timestamptz AND s.end_timestamp<$5::timestamptz"
	}
	if projectID != "" {
		args = append(args, projectID)
		eventFilter += fmt.Sprintf(" AND e.project_id=$%d::bigint", len(args))
		spanFilter += fmt.Sprintf(" AND s.project_id=$%d::bigint", len(args))
	}
	start, end, err := read.windowBounds(ctx, window, 24*time.Hour)
	if err != nil {
		return TransactionSpanGroups{}, err
	}
	isSegment := true
	selector := spanArchiveSelector{OrganizationID: orgID, TimeColumn: "end_timestamp", Start: &start, End: &end, IsSegment: &isSegment}
	if projectID != "" {
		selectedProject, parseErr := strconv.ParseInt(projectID, 10, 64)
		if parseErr != nil {
			return TransactionSpanGroups{}, parseErr
		}
		if selectedProject > 0 {
			selector.ProjectID = selectedProject
		} else {
			selector.RestrictProjectIDs = true
		}
	}
	if err := read.Load(ctx, selector); err != nil {
		return TransactionSpanGroups{}, err
	}
	roots := `WITH legacy_roots AS (
		SELECT e.id,e.span_projection_version FROM events e JOIN projects p ON p.id=e.project_id
		WHERE p.organization_id=$1::bigint AND e.kind='transaction' AND e.transaction_name=$2
		AND COALESCE(e.data#>>'{contexts,trace,op}','')=$3 AND ` + eventFilter + `
	), segment_roots AS (
		SELECT s.project_id,s.trace_id,s.span_id FROM spans s JOIN projects p ON p.id=s.project_id
		WHERE p.organization_id=$1::bigint AND s.is_segment AND s.name=$2 AND s.op=$3 AND ` + spanFilter + `
	)`
	var pending bool
	if err := tx.QueryRow(ctx, roots+` SELECT
		(SELECT count(*) FROM legacy_roots)+(SELECT count(*) FROM segment_roots),
		EXISTS(SELECT 1 FROM legacy_roots WHERE span_projection_version=0)`, args...).Scan(&result.TransactionCount, &pending); err != nil {
		return TransactionSpanGroups{}, err
	}
	if pending {
		return TransactionSpanGroups{}, ErrSpanProjectionPending
	}
	seedQuery := `SELECT s.project_id,s.trace_id FROM spans s JOIN projects p ON p.id=s.project_id
		WHERE p.organization_id=$1::bigint AND s.is_segment AND s.name=$2 AND s.op=$3 AND ` + spanFilter
	if err := read.AddTraceSeeds(ctx, seedQuery, args...); err != nil {
		return TransactionSpanGroups{}, err
	}
	if err := read.LoadSeededTraces(ctx); err != nil {
		return TransactionSpanGroups{}, err
	}
	offset := int64(result.Page-1) * int64(result.PageSize)
	args = append(args, offset)
	rows, err := tx.Query(ctx, roots+`, children AS (
		SELECT o.op,o.description,o.duration FROM legacy_roots r JOIN transaction_span_operations o ON o.event_id=r.id
		UNION ALL
		SELECT c.op,c.name,c.duration FROM segment_roots r JOIN spans c
		ON c.project_id=r.project_id AND c.trace_id=r.trace_id AND c.attributes#>>'{sentry.segment.id,value}'=r.span_id
		WHERE NOT c.is_segment AND c.attributes#>>'{sentry.segment.id,type}'='string'
		AND jsonb_typeof(c.attributes#>'{sentry.segment.id,value}')='string'
	), groups AS (
		SELECT op,description,count(*) AS item_count,count(duration) AS timed_count,avg(duration) AS average,
		percentile_cont(0.95) WITHIN GROUP(ORDER BY duration) AS p95,sum(duration) AS total
		FROM children GROUP BY op,description
	), totals AS (
		SELECT count(*) AS group_count,COALESCE(sum(item_count),0)::bigint AS span_count,
		COALESCE(sum(timed_count),0)::bigint AS timed_count FROM groups
	)
	SELECT t.group_count,t.span_count,t.timed_count,g.op,g.description,g.item_count,g.timed_count,g.average,g.p95,g.total
	FROM totals t LEFT JOIN LATERAL (
		SELECT * FROM groups ORDER BY total DESC NULLS LAST,item_count DESC,op COLLATE "C",description COLLATE "C"
		LIMIT 50 OFFSET $`+strconv.Itoa(len(args))+`
	) g ON true
	ORDER BY g.total DESC NULLS LAST,g.item_count DESC,g.op COLLATE "C",g.description COLLATE "C"`, args...)
	if err != nil {
		return TransactionSpanGroups{}, err
	}
	for rows.Next() {
		var op, description *string
		var count, timedCount *int64
		var average, p95, total *float64
		if err := rows.Scan(&result.GroupCount, &result.SpanCount, &result.TimedSpanCount, &op, &description, &count, &timedCount, &average, &p95, &total); err != nil {
			rows.Close()
			return TransactionSpanGroups{}, err
		}
		if op != nil {
			result.Data = append(result.Data, TransactionSpanGroup{Op: *op, Description: *description, Count: *count, TimedCount: *timedCount, AvgDuration: average, P95Duration: p95, TotalTime: total})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return TransactionSpanGroups{}, err
	}
	result.HasMore = offset+int64(len(result.Data)) < result.GroupCount
	return result, nil
}

func projectTransactionSpans(ctx context.Context, tx pgx.Tx, eventID int64, data json.RawMessage) error {
	// Replace the derived rows inside this transaction. Version-one statistics
	// stay visible until all version-two metadata and its marker commit together.
	if _, err := tx.Exec(ctx, "DELETE FROM transaction_span_operations WHERE event_id=$1", eventID); err != nil {
		return err
	}
	var children []json.RawMessage
	root := detailObject(data)
	_ = json.Unmarshal(root["spans"], &children)
	if len(children) != 0 {
		_, err := tx.CopyFrom(ctx, pgx.Identifier{"transaction_span_operations"}, []string{"event_id", "ordinal", "op", "description", "duration", "span_id", "parent_span_id", "status", "start_timestamp", "end_timestamp"},
			pgx.CopyFromSlice(len(children), func(i int) ([]any, error) {
				span := legacyDetailSpan(children[i], int64(i+1))
				return []any{eventID, i + 1, span.Op, span.Name, span.Duration, span.SpanID, span.ParentSpanID, span.Status, span.StartTimestamp, span.EndTimestamp}, nil
			}))
		if err != nil {
			return err
		}
	}
	trace := detailObject(detailObject(root["contexts"])["trace"])
	_, err := tx.Exec(ctx, `UPDATE events SET span_projection_version=2,trace_span_id=$2,trace_parent_span_id=$3,
		trace_op=$4,trace_status=$5,trace_start_timestamp=$6 WHERE id=$1`, eventID,
		detailString(trace, "span_id"), detailString(trace, "parent_span_id"), detailString(trace, "op"), detailString(trace, "status"), detailTimestamp(root["start_timestamp"]))
	return err
}
