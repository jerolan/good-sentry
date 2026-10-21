package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

var ErrAmbiguousTransactionGroup = errors.New("transaction group requires an operation")

type TransactionFilter struct {
	Project, Transaction, Op string
	Window                   *TimeWindow
	Offset, Limit            int
}

type SpanGroupFilter struct {
	Project, Op, Query string
	Window             *TimeWindow
	Offset, Limit      int
}

type NPlusOneFilter struct {
	Project           string
	Window            *TimeWindow
	Threshold, Offset int
	Limit             int
}

type SpanGroup struct {
	Op          string    `json:"op"`
	Description string    `json:"description"`
	ProjectIDs  []string  `json:"projectIds"`
	TraceCount  int64     `json:"traceCount"`
	Count       int64     `json:"count"`
	ErrorCount  int64     `json:"errorCount"`
	AvgDuration float64   `json:"avgDuration"`
	P95Duration float64   `json:"p95Duration"`
	TotalTime   float64   `json:"totalTime"`
	LastSeen    time.Time `json:"lastSeen"`
}

type NPlusOneFinding struct {
	ProjectID     string    `json:"projectId"`
	TraceID       string    `json:"traceId"`
	Transaction   string    `json:"transaction"`
	Op            string    `json:"op"`
	ChildOp       string    `json:"childOp"`
	ChildName     string    `json:"childName"`
	Count         int64     `json:"count"`
	AvgDuration   float64   `json:"avgDuration"`
	TotalDuration float64   `json:"totalDuration"`
	FirstSeen     time.Time `json:"firstSeen"`
	LastSeen      time.Time `json:"lastSeen"`
}

type TransactionTrendPoint struct {
	Date        string  `json:"date"`
	Count       int64   `json:"count"`
	ErrorCount  int64   `json:"errorCount"`
	ErrorRate   float64 `json:"errorRate"`
	AvgDuration float64 `json:"avgDuration"`
	P50         float64 `json:"p50"`
	P95         float64 `json:"p95"`
}

func scopedProjectID(ctx context.Context, s *Store, access ManagementAccess, project string) (int64, error) {
	if project == "" {
		return 0, nil
	}
	p, err := s.ManagementProject(ctx, access, project)
	if err != nil {
		return 0, err
	}
	return referenceID(p.ID), nil
}

func scopedRange(args []any, column string, window *TimeWindow, defaults string) ([]any, string) {
	if window == nil {
		return args, fmt.Sprintf("%s>=now()-%s AND %s<=now()", column, defaults, column)
	}
	args = append(args, window.Start, window.End)
	return args, fmt.Sprintf("%s>=$%d::timestamptz AND %s<$%d::timestamptz", column, len(args)-1, column, len(args))
}

// ScopedTransactions preserves the native event/segment union and ordering while
// applying all authorization predicates to both sides before aggregation.
func (s *Store) ScopedTransactions(ctx context.Context, access ManagementAccess, filter TransactionFilter) ([]TransactionGroup, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 101 {
		return nil, ErrNotFound
	}
	projectID, err := scopedProjectID(ctx, s, access, filter.Project)
	if err != nil {
		return nil, err
	}
	args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs}
	eventRange, spanRange := "", ""
	args, eventRange = scopedRange(args, "e.timestamp", filter.Window, "interval '24 hours'")
	args, spanRange = scopedRange(args, "s.end_timestamp", filter.Window, "interval '24 hours'")
	projectFilter := func(alias string) string {
		if projectID == 0 {
			return ""
		}
		args = append(args, projectID)
		return fmt.Sprintf(" AND %s.project_id=$%d::bigint", alias, len(args))
	}
	eventProject := projectFilter("e")
	spanProject := projectFilter("s")
	if filter.Transaction != "" {
		args = append(args, filter.Transaction)
		eventProject += fmt.Sprintf(" AND e.transaction_name=$%d", len(args))
		spanProject += fmt.Sprintf(" AND s.name=$%d", len(args))
	}
	if filter.Op != "" {
		args = append(args, filter.Op)
		eventProject += fmt.Sprintf(" AND COALESCE(e.data#>>'{contexts,trace,op}','')=$%d", len(args))
		spanProject += fmt.Sprintf(" AND s.op=$%d", len(args))
	}
	args = append(args, filter.Limit, filter.Offset)
	query := `WITH telemetry AS (
 SELECT e.transaction_name AS name,COALESCE(e.data#>>'{contexts,trace,op}','') AS op,e.duration,
 (e.level IN ('error','fatal') OR COALESCE(e.data#>>'{contexts,trace,status}','') NOT IN ('','ok','unknown')) AS failed,
 e.timestamp AS timestamp,e.trace_id,e.id,0 AS source,e.project_id
 FROM events e JOIN projects p ON p.id=e.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND e.kind='transaction' AND ` + eventRange + eventProject + `
 UNION ALL
 SELECT s.name,s.op,s.duration,s.status='error',s.end_timestamp,s.trace_id,s.id,1,s.project_id
 FROM spans s JOIN projects p ON p.id=s.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND s.is_segment AND ` + spanRange + spanProject + `
)
 SELECT name,op,count(*),count(*) FILTER(WHERE failed),avg(duration),
 percentile_cont(0.5) WITHIN GROUP(ORDER BY duration),percentile_cont(0.95) WITHIN GROUP(ORDER BY duration),
 max(timestamp),(array_agg(trace_id ORDER BY timestamp DESC,source DESC,id DESC))[1],
 (array_agg(source ORDER BY timestamp DESC,source DESC,id DESC))[1],
 (array_agg(id ORDER BY timestamp DESC,source DESC,id DESC))[1]::text,
 (array_agg(project_id ORDER BY timestamp DESC,source DESC,id DESC))[1]::text
 FROM telemetry GROUP BY name,op ORDER BY count(*) DESC,max(timestamp) DESC,name,op
 LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TransactionGroup{}
	for rows.Next() {
		var g TransactionGroup
		var source int
		if err := rows.Scan(&g.Transaction, &g.Op, &g.Count, &g.ErrorCount, &g.AvgDuration, &g.P50, &g.P95, &g.LastSeen, &g.LatestTraceID, &source, &g.LatestSample.ID, &g.LatestSample.ProjectID); err != nil {
			return nil, err
		}
		g.LatestSample.Source = "event"
		if source == 1 {
			g.LatestSample.Source = "span"
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

func (s *Store) ScopedTransactionGroup(ctx context.Context, access ManagementAccess, filter TransactionFilter) (TransactionGroup, error) {
	filter.Offset, filter.Limit = 0, 2
	groups, err := s.ScopedTransactions(ctx, access, filter)
	if err != nil {
		return TransactionGroup{}, err
	}
	if len(groups) == 0 {
		return TransactionGroup{}, ErrNotFound
	}
	if len(groups) > 1 {
		return TransactionGroup{}, ErrAmbiguousTransactionGroup
	}
	return groups[0], nil
}

func (s *Store) ScopedSpanGroups(ctx context.Context, access ManagementAccess, filter SpanGroupFilter) ([]SpanGroup, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 101 || !utf8ValidNoNUL(filter.Query) {
		return nil, ErrNotFound
	}
	projectID, err := scopedProjectID(ctx, s, access, filter.Project)
	if err != nil {
		return nil, err
	}
	args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs}
	args, legacyWindow := scopedRange(args, "o.start_timestamp", filter.Window, "interval '24 hours'")
	args, spanWindow := scopedRange(args, "s.start_timestamp", filter.Window, "interval '24 hours'")
	legacyWhere, spanWhere := "", ""
	if projectID != 0 {
		args = append(args, projectID)
		legacyWhere += fmt.Sprintf(" AND e.project_id=$%d::bigint", len(args))
		spanWhere += fmt.Sprintf(" AND s.project_id=$%d::bigint", len(args))
	}
	if filter.Op != "" {
		args = append(args, filter.Op)
		legacyWhere += fmt.Sprintf(" AND o.op=$%d", len(args))
		spanWhere += fmt.Sprintf(" AND s.op=$%d", len(args))
	}
	if filter.Query != "" {
		args = append(args, "%"+filter.Query+"%")
		legacyWhere += fmt.Sprintf(" AND o.description ILIKE $%d", len(args))
		spanWhere += fmt.Sprintf(" AND s.name ILIKE $%d", len(args))
	}
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.pool.Query(ctx, `WITH derived AS (
 SELECT o.op,o.description AS name,e.project_id,e.trace_id,o.status,o.duration,o.start_timestamp,o.end_timestamp
 FROM transaction_span_operations o JOIN events e ON e.id=o.event_id JOIN projects p ON p.id=e.project_id
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND `+legacyWindow+legacyWhere+`
 UNION ALL
 SELECT s.op,s.name,s.project_id,s.trace_id,s.status,s.duration,s.start_timestamp,s.end_timestamp
 FROM spans s JOIN projects p ON p.id=s.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND NOT s.is_segment AND `+spanWindow+spanWhere+`
)
 SELECT op,name,array_agg(DISTINCT project_id::text ORDER BY project_id::text),
 count(DISTINCT trace_id),count(*),count(*) FILTER(WHERE status='error'),avg(duration),
 percentile_cont(0.95) WITHIN GROUP(ORDER BY duration),sum(duration),max(end_timestamp)
 FROM derived GROUP BY op,name ORDER BY sum(duration) DESC,count(*) DESC,op COLLATE "C",name COLLATE "C"
 LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SpanGroup{}
	for rows.Next() {
		var item SpanGroup
		if err := rows.Scan(&item.Op, &item.Description, &item.ProjectIDs, &item.TraceCount, &item.Count, &item.ErrorCount, &item.AvgDuration, &item.P95Duration, &item.TotalTime, &item.LastSeen); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DetectNPlusOne(ctx context.Context, access ManagementAccess, filter NPlusOneFilter) ([]NPlusOneFinding, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 101 || filter.Threshold < 2 || filter.Threshold > 100 {
		return nil, ErrNotFound
	}
	projectID, err := scopedProjectID(ctx, s, access, filter.Project)
	if err != nil {
		return nil, err
	}
	args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs}
	args, legacyWindow := scopedRange(args, "o.start_timestamp", filter.Window, "interval '24 hours'")
	args, spanWindow := scopedRange(args, "s.start_timestamp", filter.Window, "interval '24 hours'")
	legacyWhere, spanWhere := "", ""
	if projectID != 0 {
		args = append(args, projectID)
		legacyWhere += fmt.Sprintf(" AND e.project_id=$%d::bigint", len(args))
		spanWhere += fmt.Sprintf(" AND s.project_id=$%d::bigint", len(args))
	}
	args = append(args, filter.Threshold, filter.Limit, filter.Offset)
	threshold, limit, offset := len(args)-2, len(args)-1, len(args)
	rows, err := s.pool.Query(ctx, `WITH children AS (
 SELECT e.project_id,e.trace_id,e.transaction_name AS root_name,COALESCE(e.data#>>'{contexts,trace,op}','') AS root_op,
 o.op,o.description AS name,o.duration,o.start_timestamp,o.end_timestamp
 FROM transaction_span_operations o JOIN events e ON e.id=o.event_id JOIN projects p ON p.id=e.project_id
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND `+legacyWindow+legacyWhere+`
 UNION ALL
 SELECT s.project_id,s.trace_id,COALESCE(root.name,''),COALESCE(root.op,''),s.op,s.name,s.duration,s.start_timestamp,s.end_timestamp
 FROM spans s JOIN projects p ON p.id=s.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 LEFT JOIN LATERAL (SELECT rs.name,rs.op FROM spans rs WHERE rs.project_id=s.project_id AND rs.trace_id=s.trace_id AND rs.is_segment ORDER BY rs.id LIMIT 1) root ON true
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND NOT s.is_segment AND `+spanWindow+spanWhere+`
)
 SELECT project_id::text,trace_id,root_name,root_op,op,name,count(*),avg(duration),sum(duration),min(start_timestamp),max(end_timestamp)
 FROM children GROUP BY project_id,trace_id,root_name,root_op,op,name HAVING count(*)>=$`+fmt.Sprint(threshold)+`
 ORDER BY count(*) DESC,sum(duration) DESC,project_id DESC,trace_id DESC,op COLLATE "C",name COLLATE "C"
 LIMIT $`+fmt.Sprint(limit)+` OFFSET $`+fmt.Sprint(offset), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NPlusOneFinding{}
	for rows.Next() {
		var item NPlusOneFinding
		if err := rows.Scan(&item.ProjectID, &item.TraceID, &item.Transaction, &item.Op, &item.ChildOp, &item.ChildName, &item.Count, &item.AvgDuration, &item.TotalDuration, &item.FirstSeen, &item.LastSeen); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ScopedTransactionTrend(ctx context.Context, access ManagementAccess, filter TransactionFilter) ([]TransactionTrendPoint, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || filter.Transaction == "" {
		return nil, ErrNotFound
	}
	projectID, err := scopedProjectID(ctx, s, access, filter.Project)
	if err != nil {
		return nil, err
	}
	args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs}
	eventRange, spanRange := "", ""
	args, eventRange = scopedRange(args, "e.timestamp", filter.Window, "interval '14 days'")
	args, spanRange = scopedRange(args, "s.end_timestamp", filter.Window, "interval '14 days'")
	args = append(args, filter.Transaction)
	name := len(args)
	eventFilter := fmt.Sprintf(" AND e.transaction_name=$%d", name)
	spanFilter := fmt.Sprintf(" AND s.name=$%d", name)
	if filter.Op != "" {
		args = append(args, filter.Op)
		eventFilter += fmt.Sprintf(" AND COALESCE(e.data#>>'{contexts,trace,op}','')=$%d", len(args))
		spanFilter += fmt.Sprintf(" AND s.op=$%d", len(args))
	}
	if projectID != 0 {
		args = append(args, projectID)
		eventFilter += fmt.Sprintf(" AND e.project_id=$%d::bigint", len(args))
		spanFilter += fmt.Sprintf(" AND s.project_id=$%d::bigint", len(args))
	}
	rows, err := s.pool.Query(ctx, `WITH telemetry AS (
 SELECT e.timestamp,e.duration,(e.level IN ('error','fatal') OR COALESCE(e.data#>>'{contexts,trace,status}','') NOT IN ('','ok','unknown')) AS failed
 FROM events e JOIN projects p ON p.id=e.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND e.kind='transaction' AND `+eventRange+eventFilter+`
 UNION ALL
 SELECT s.end_timestamp,s.duration,s.status='error'
 FROM spans s JOIN projects p ON p.id=s.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND s.is_segment AND `+spanRange+spanFilter+`
)
 SELECT to_char(date_trunc('day',timestamp AT TIME ZONE 'UTC'),'YYYY-MM-DD'),count(*),count(*) FILTER(WHERE failed),
 100.0*count(*) FILTER(WHERE failed)/count(*),avg(duration),
 percentile_cont(0.5) WITHIN GROUP(ORDER BY duration),percentile_cont(0.95) WITHIN GROUP(ORDER BY duration)
 FROM telemetry GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TransactionTrendPoint{}
	for rows.Next() {
		var item TransactionTrendPoint
		if err := rows.Scan(&item.Date, &item.Count, &item.ErrorCount, &item.ErrorRate, &item.AvgDuration, &item.P50, &item.P95); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func utf8ValidNoNUL(value string) bool {
	return utf8.ValidString(value) && !containsNUL(value)
}

func containsNUL(value string) bool {
	for _, r := range value {
		if r == 0 {
			return true
		}
	}
	return false
}
