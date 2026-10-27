package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type CompatibleTransactionFilter struct {
	ProjectIDs      []int64
	Transaction, Op string
	Query, Sort     string
	Window          *TimeWindow
	Offset, Limit   int
}

type CompatibleTransactionGroup struct {
	ID          int64
	ProjectID   int64
	Transaction string
	Op          string
	Method      string
	Count       int64
	ErrorCount  int64
	AvgDuration float64
	P50         float64
	P95         float64
	FirstSeen   time.Time
	LastSeen    time.Time
}

type CompatibleTransactionIdentity struct {
	ProjectID   int64
	Transaction string
	Op          string
	CreatedAt   time.Time
}

func compatibleTransactionOrder(sort string) (string, bool) {
	switch sort {
	case "", "-avg_duration":
		return `average DESC,item_count DESC,project_id,name COLLATE "C",op COLLATE "C"`, true
	case "avg_duration":
		return `average,item_count DESC,project_id,name COLLATE "C",op COLLATE "C"`, true
	case "count":
		return `item_count,average DESC,project_id,name COLLATE "C",op COLLATE "C"`, true
	case "-count":
		return `item_count DESC,average DESC,project_id,name COLLATE "C",op COLLATE "C"`, true
	case "created":
		return `first_seen,project_id,name COLLATE "C",op COLLATE "C"`, true
	case "-created":
		return `last_seen DESC,project_id,name COLLATE "C",op COLLATE "C"`, true
	default:
		return "", false
	}
}

func compatibleTransactionQuery(access ManagementAccess, filter CompatibleTransactionFilter) (string, []any, error) {
	args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs}
	eventRange, spanRange := "TRUE", "TRUE"
	if filter.Window != nil {
		args = append(args, filter.Window.Start, filter.Window.End)
		eventRange = fmt.Sprintf("e.timestamp>=$%d::timestamptz AND e.timestamp<$%d::timestamptz", len(args)-1, len(args))
		spanRange = fmt.Sprintf("s.end_timestamp>=$%d::timestamptz AND s.end_timestamp<$%d::timestamptz", len(args)-1, len(args))
	}
	eventWhere, spanWhere := "", ""
	if len(filter.ProjectIDs) > 0 {
		args = append(args, filter.ProjectIDs)
		eventWhere += fmt.Sprintf(" AND e.project_id=ANY($%d::bigint[])", len(args))
		spanWhere += fmt.Sprintf(" AND s.project_id=ANY($%d::bigint[])", len(args))
	}
	if filter.Transaction != "" {
		args = append(args, filter.Transaction)
		eventWhere += fmt.Sprintf(" AND e.transaction_name=$%d", len(args))
		spanWhere += fmt.Sprintf(" AND s.name=$%d", len(args))
	}
	if filter.Op != "" {
		args = append(args, filter.Op)
		eventWhere += fmt.Sprintf(" AND COALESCE(e.data#>>'{contexts,trace,op}','')=$%d", len(args))
		spanWhere += fmt.Sprintf(" AND s.op=$%d", len(args))
	}
	if filter.Query != "" {
		args = append(args, "%"+filter.Query+"%")
		eventWhere += fmt.Sprintf(" AND (e.transaction_name ILIKE $%d OR COALESCE(e.data#>>'{contexts,trace,op}','') ILIKE $%d)", len(args), len(args))
		spanWhere += fmt.Sprintf(" AND (s.name ILIKE $%d OR s.op ILIKE $%d)", len(args), len(args))
	}
	order, valid := compatibleTransactionOrder(filter.Sort)
	if !valid {
		return "", nil, ErrNotFound
	}
	query := `WITH telemetry AS (
 SELECT e.project_id,e.transaction_name AS name,COALESCE(e.data#>>'{contexts,trace,op}','') AS op,
 COALESCE(e.data#>>'{request,method}','') AS method,e.duration,
 (e.level IN ('error','fatal') OR COALESCE(e.data#>>'{contexts,trace,status}','') NOT IN ('','ok','unknown')) AS failed,
 e.timestamp
 FROM transactions_with_archive e JOIN projects p ON p.id=e.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND e.kind='transaction' AND ` + eventRange + eventWhere + `
 UNION ALL
 SELECT s.project_id,s.name,s.op,
 COALESCE(s.attributes#>>'{http.request.method,value}',s.attributes#>>'{http.method,value}',''),s.duration,s.status='error',s.end_timestamp
 FROM spans s JOIN projects p ON p.id=s.project_id JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[])) AND s.is_segment AND ` + spanRange + spanWhere + `
), grouped AS (
 SELECT project_id,name,op,COALESCE(max(method) FILTER(WHERE method<>''),'') AS method,
 count(*) AS item_count,count(*) FILTER(WHERE failed) AS error_count,avg(duration) AS average,
 percentile_cont(0.5) WITHIN GROUP(ORDER BY duration) AS p50,
 percentile_cont(0.95) WITHIN GROUP(ORDER BY duration) AS p95,
 min(timestamp) AS first_seen,max(timestamp) AS last_seen
 FROM telemetry WHERE char_length(name)<=1024 AND char_length(op)<=255
 GROUP BY project_id,name,op
)
 SELECT project_id::text,name,op,method,item_count,error_count,average,p50,p95,first_seen,last_seen
 FROM grouped ORDER BY ` + order
	return query, args, nil
}

func (s *Store) CompatibleTransactionGroups(ctx context.Context, access ManagementAccess, filter CompatibleTransactionFilter) ([]CompatibleTransactionGroup, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 101 {
		return nil, ErrNotFound
	}
	if len(filter.ProjectIDs) > 0 {
		projects, err := scopedProjectIDs(ctx, s, access, filter.ProjectIDs)
		if err != nil {
			return nil, err
		}
		filter.ProjectIDs = projects
	}
	query, args, err := compatibleTransactionQuery(access, filter)
	if err != nil {
		return nil, err
	}
	args = append(args, filter.Limit, filter.Offset)
	items, err := withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) ([]CompatibleTransactionGroup, error) {
		selector := spanArchiveSelector{ProjectIDs: filter.ProjectIDs, RestrictProjectIDs: len(filter.ProjectIDs) > 0}
		isSegment := true
		selector.IsSegment = &isSegment
		if filter.Window != nil {
			selector.TimeColumn, selector.Start, selector.End = "end_timestamp", &filter.Window.Start, &filter.Window.End
		}
		if err := read.loadAccessSpans(ctx, access, selector); err != nil {
			return nil, err
		}
		rows, err := read.tx.Query(ctx, query+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []CompatibleTransactionGroup{}
		for rows.Next() {
			var item CompatibleTransactionGroup
			var project string
			if err = rows.Scan(&project, &item.Transaction, &item.Op, &item.Method, &item.Count, &item.ErrorCount,
				&item.AvgDuration, &item.P50, &item.P95, &item.FirstSeen, &item.LastSeen); err != nil {
				return nil, err
			}
			item.ProjectID, _ = strconv.ParseInt(project, 10, 64)
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	if err = s.assignCompatibleTransactionGroupIDs(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) assignCompatibleTransactionGroupIDs(ctx context.Context, groups []CompatibleTransactionGroup) error {
	if len(groups) == 0 {
		return nil
	}
	projects := make([]int64, len(groups))
	transactions := make([]string, len(groups))
	ops := make([]string, len(groups))
	for index, group := range groups {
		projects[index], transactions[index], ops[index] = group.ProjectID, group.Transaction, group.Op
	}
	rows, err := s.pool.Query(ctx, `INSERT INTO compatible_transaction_group_ids(project_id,transaction,op)
		SELECT * FROM unnest($1::bigint[],$2::text[],$3::text[])
		ON CONFLICT(project_id,transaction,op) DO UPDATE SET transaction=EXCLUDED.transaction
		RETURNING id::text,project_id::text,transaction,op`, projects, transactions, ops)
	if err != nil {
		return err
	}
	ids := map[string]int64{}
	for rows.Next() {
		var id, project, transaction, op string
		if err = rows.Scan(&id, &project, &transaction, &op); err != nil {
			rows.Close()
			return err
		}
		parsed, _ := strconv.ParseInt(id, 10, 64)
		ids[project+"\x00"+transaction+"\x00"+op] = parsed
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for index := range groups {
		key := strconv.FormatInt(groups[index].ProjectID, 10) + "\x00" + groups[index].Transaction + "\x00" + groups[index].Op
		groups[index].ID = ids[key]
		if groups[index].ID == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (s *Store) CompatibleTransactionGroupIdentity(ctx context.Context, access ManagementAccess, id int64) (CompatibleTransactionIdentity, error) {
	var item CompatibleTransactionIdentity
	var project string
	if id <= 0 {
		return item, ErrNotFound
	}
	err := s.pool.QueryRow(ctx, `SELECT g.project_id::text,g.transaction,g.op,g.created_at
		FROM compatible_transaction_group_ids g JOIN projects p ON p.id=g.project_id
		JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$2::bigint
		WHERE g.id=$1 AND p.organization_id=$3::bigint AND ($4 OR p.id=ANY($5::bigint[]))`,
		id, access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs).
		Scan(&project, &item.Transaction, &item.Op, &item.CreatedAt)
	item.ProjectID, _ = strconv.ParseInt(project, 10, 64)
	return item, dbError(err)
}

func (s *Store) CompatibleTransactionGroup(ctx context.Context, access ManagementAccess, identity CompatibleTransactionIdentity, window *TimeWindow) (CompatibleTransactionGroup, error) {
	groups, err := s.CompatibleTransactionGroups(ctx, access, CompatibleTransactionFilter{
		ProjectIDs: []int64{identity.ProjectID}, Transaction: identity.Transaction, Op: identity.Op, Window: window, Limit: 1,
	})
	if err != nil {
		return CompatibleTransactionGroup{}, err
	}
	if len(groups) == 0 {
		return CompatibleTransactionGroup{}, ErrNotFound
	}
	return groups[0], nil
}

func compatibleTransactionID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

func compatibleTransactionQueryText(value string) bool {
	return utf8ValidNoNUL(value) && len([]rune(value)) <= 2000 && !strings.ContainsRune(value, 0)
}
