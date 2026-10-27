package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// CompatibleLogMaxLimit bounds one compatible log page.
	CompatibleLogMaxLimit = 200
	// CompatibleLogResourceLimit bounds one resource-name response.
	CompatibleLogResourceLimit = 200
	// CompatibleLogStatsHours bounds the statistics window.
	CompatibleLogStatsHours = 90 * 24
)

// CompatibleLogCursor is the keyset position of a compatible log page. A zero
// value starts at the newest retained log.
type CompatibleLogCursor struct {
	Timestamp time.Time
	ID        int64
}

func (c CompatibleLogCursor) Empty() bool { return c.Timestamp.IsZero() && c.ID == 0 }

// CompatibleLogFilter carries the published query surface for retained logs.
type CompatibleLogFilter struct {
	ProjectIDs  []int64
	Levels      []string
	Service     string
	Environment string
	Host        string
	TraceID     string
	Query       string
	Window      *TimeWindow
	Cursor      CompatibleLogCursor
	Limit       int
	// Services and Environments carry the statistics filters, which accept
	// repeated values where the list filters accept one.
	Services     []string
	Environments []string
}

// CompatibleLog is one retained log in the published GlitchTip shape.
type CompatibleLog struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	Timestamp   time.Time       `json:"timestamp"`
	Level       string          `json:"level"`
	Body        string          `json:"body"`
	Service     string          `json:"service"`
	Environment string          `json:"environment"`
	Host        string          `json:"host"`
	TraceID     string          `json:"traceId"`
	SpanID      string          `json:"spanId"`
	Attributes  json.RawMessage `json:"attributes,omitempty"`
	Source      string          `json:"source,omitempty"`
}

// CompatibleLogResource is one distinct service, environment or host name.
type CompatibleLogResource struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	LastSeen time.Time `json:"lastSeen"`
}

// CompatibleLogStat is one hourly bucket for one level.
type CompatibleLogStat struct {
	Interval time.Time
	Level    string
	Count    int64
}

// StoredCompatibleLevel maps a published level spelling to the retained value.
// GlitchTip publishes "warning" while OpenTelemetry severity maps to "warn".
func StoredCompatibleLevel(level string) string {
	if level == "warning" {
		return "warn"
	}
	return level
}

// StoredCompatibleLevels returns every retained spelling that satisfies one
// published level, because SDK logs may store either warning spelling.
func StoredCompatibleLevels(level string) []string {
	if level == "warn" || level == "warning" {
		return []string{"warning", "warn"}
	}
	return []string{level}
}

// PublishedCompatibleLevel maps a retained value to the published spelling.
func PublishedCompatibleLevel(level string) string {
	if level == "warn" {
		return "warning"
	}
	return level
}

const compatibleLogColumns = `l.id::text,l.project_id::text,l.timestamp,l.level,l.body,l.service,
	l.environment,l.host,l.trace_id,l.span_id,l.attributes,l.otlp_batch_id IS NOT NULL`

func scanCompatibleLog(row interface{ Scan(...any) error }) (CompatibleLog, error) {
	var l CompatibleLog
	var native bool
	if err := row.Scan(&l.ID, &l.ProjectID, &l.Timestamp, &l.Level, &l.Body, &l.Service, &l.Environment, &l.Host,
		&l.TraceID, &l.SpanID, &l.Attributes, &native); err != nil {
		return CompatibleLog{}, dbError(err)
	}
	l.Level = PublishedCompatibleLevel(l.Level)
	if native {
		l.Source = "otlp"
		l.Attributes = nil
	}
	return l, nil
}

// compatibleLogScope resolves the authorized project filter and returns the SQL
// fragment with its arguments. Requested projects must all be inside the
// membership and token grant, otherwise the caller sees no rows.
func (s *Store) compatibleLogScope(ctx context.Context, access ManagementAccess, projectIDs []int64) (string, []any, error) {
	args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs}
	filter := ""
	if len(projectIDs) > 0 {
		selected, err := scopedProjectIDs(ctx, s, access, projectIDs)
		if err != nil {
			return "", nil, err
		}
		args = append(args, selected)
		filter = fmt.Sprintf(" AND l.project_id=ANY($%d::bigint[])", len(args))
	}
	return "WHERE p.organization_id::text=$1 AND m.user_id::text=$2 AND ($3 OR p.id=ANY($4::bigint[]))" + filter + " ", args, nil
}

// CompatibleLogs returns one keyset page of retained logs.
func (s *Store) CompatibleLogs(ctx context.Context, access ManagementAccess, f CompatibleLogFilter) ([]CompatibleLog, error) {
	if f.Limit < 1 || f.Limit > CompatibleLogMaxLimit || len(f.Levels) > 8 {
		return nil, ErrInvalidEventFilter
	}
	where, args, err := s.compatibleLogScope(ctx, access, f.ProjectIDs)
	if err != nil {
		return nil, err
	}
	if len(f.Levels) > 0 {
		levels := make([]string, 0, len(f.Levels)*2)
		for _, level := range f.Levels {
			levels = append(levels, StoredCompatibleLevels(level)...)
		}
		args = append(args, levels)
		where += fmt.Sprintf("AND l.level=ANY($%d::text[]) ", len(args))
	}
	for _, equality := range []struct{ value, column string }{
		{f.Service, "l.service"}, {f.Environment, "l.environment"}, {f.Host, "l.host"},
	} {
		if equality.value != "" {
			args = append(args, equality.value)
			where += fmt.Sprintf("AND %s=$%d ", equality.column, len(args))
		}
	}
	if f.TraceID != "" {
		args = append(args, f.TraceID)
		where += fmt.Sprintf("AND l.trace_id=$%d ", len(args))
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		where += fmt.Sprintf("AND l.body ILIKE $%d ", len(args))
	}
	if f.Window != nil {
		args = append(args, f.Window.Start, f.Window.End)
		where += fmt.Sprintf("AND l.timestamp>=$%d::timestamptz AND l.timestamp<$%d::timestamptz ", len(args)-1, len(args))
	}
	if !f.Cursor.Empty() {
		args = append(args, f.Cursor.Timestamp, f.Cursor.ID)
		where += fmt.Sprintf("AND (l.timestamp,l.id)<($%d::timestamptz,$%d::bigint) ", len(args)-1, len(args))
	}
	args = append(args, f.Limit)
	rows, err := s.pool.Query(ctx, "SELECT "+compatibleLogColumns+` FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
		JOIN memberships m ON m.organization_id=p.organization_id `+where+
		fmt.Sprintf("ORDER BY l.timestamp DESC,l.id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CompatibleLog{}
	for rows.Next() {
		item, err := scanCompatibleLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CompatibleLog reads one retained log inside the authorized scope.
func (s *Store) CompatibleLog(ctx context.Context, access ManagementAccess, logID string) (CompatibleLog, error) {
	where, args, err := s.compatibleLogScope(ctx, access, nil)
	if err != nil {
		return CompatibleLog{}, err
	}
	args = append(args, logID)
	return scanCompatibleLog(s.pool.QueryRow(ctx, "SELECT "+compatibleLogColumns+` FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
		JOIN memberships m ON m.organization_id=p.organization_id `+where+
		fmt.Sprintf("AND l.id=$%d::bigint", len(args)), args...))
}

// CompatibleLogResources lists distinct resource names ordered by last use.
func (s *Store) CompatibleLogResources(ctx context.Context, access ManagementAccess, resourceType string) ([]CompatibleLogResource, error) {
	switch resourceType {
	case "", "service", "environment", "host":
	default:
		return nil, ErrInvalidEventFilter
	}
	where, args, err := s.compatibleLogScope(ctx, access, nil)
	if err != nil {
		return nil, err
	}
	args = append(args, resourceType, CompatibleLogResourceLimit)
	resourceArg, limitArg := len(args)-1, len(args)
	rows, err := s.pool.Query(ctx, `WITH resources AS (
	SELECT l.service AS name,'service' AS type,l.timestamp FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
		JOIN memberships m ON m.organization_id=p.organization_id `+where+`AND l.service<>''
	UNION ALL
	SELECT l.environment,'environment',l.timestamp FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
		JOIN memberships m ON m.organization_id=p.organization_id `+where+`AND l.environment<>''
	UNION ALL
	SELECT l.host,'host',l.timestamp FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
		JOIN memberships m ON m.organization_id=p.organization_id `+where+`AND l.host<>''
	) SELECT name,type,max(timestamp) FROM resources WHERE ($`+fmt.Sprintf("%d", resourceArg)+`::text='' OR type=$`+fmt.Sprintf("%d", resourceArg)+`::text)
	GROUP BY name,type ORDER BY max(timestamp) DESC,name LIMIT $`+fmt.Sprintf("%d", limitArg), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CompatibleLogResource{}
	for rows.Next() {
		var item CompatibleLogResource
		if err := rows.Scan(&item.Name, &item.Type, &item.LastSeen); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CompatibleLogStats returns hourly counts per level for the requested window.
func (s *Store) CompatibleLogStats(ctx context.Context, access ManagementAccess, f CompatibleLogFilter) ([]CompatibleLogStat, error) {
	if f.Window == nil {
		return nil, errors.New("a statistics window is required")
	}
	hours := int(f.Window.End.Sub(f.Window.Start).Hours())
	if hours < 1 || hours > CompatibleLogStatsHours {
		return nil, ErrInvalidEventFilter
	}
	where, args, err := s.compatibleLogScope(ctx, access, f.ProjectIDs)
	if err != nil {
		return nil, err
	}
	if len(f.Levels) > 0 {
		levels := make([]string, 0, len(f.Levels)*2)
		for _, level := range f.Levels {
			levels = append(levels, StoredCompatibleLevels(level)...)
		}
		args = append(args, levels)
		where += fmt.Sprintf("AND l.level=ANY($%d::text[]) ", len(args))
	}
	for _, equality := range []struct {
		values []string
		column string
	}{
		{f.Services, "l.service"}, {f.Environments, "l.environment"},
	} {
		if len(equality.values) > 0 {
			args = append(args, equality.values)
			where += fmt.Sprintf("AND %s=ANY($%d::text[]) ", equality.column, len(args))
		}
	}
	args = append(args, f.Window.Start, f.Window.End)
	rows, err := s.pool.Query(ctx, `SELECT date_trunc('hour',l.timestamp) AS bucket,l.level,count(*)
	FROM logs_with_archive l JOIN projects p ON p.id=l.project_id
	JOIN memberships m ON m.organization_id=p.organization_id `+where+
		fmt.Sprintf("AND l.timestamp>=$%d::timestamptz AND l.timestamp<$%d::timestamptz ", len(args)-1, len(args))+
		"GROUP BY bucket,l.level ORDER BY bucket,l.level", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CompatibleLogStat{}
	for rows.Next() {
		var item CompatibleLogStat
		if err := rows.Scan(&item.Interval, &item.Level, &item.Count); err != nil {
			return nil, err
		}
		item.Level = PublishedCompatibleLevel(item.Level)
		items = append(items, item)
	}
	return items, rows.Err()
}

// CompatibleLogCursorFrom builds the next-page cursor from one page.
func CompatibleLogCursorFrom(page []CompatibleLog) (CompatibleLogCursor, bool) {
	if len(page) == 0 {
		return CompatibleLogCursor{}, false
	}
	last := page[len(page)-1]
	id, err := parsePositiveInt64(last.ID)
	if err != nil {
		return CompatibleLogCursor{}, false
	}
	return CompatibleLogCursor{Timestamp: last.Timestamp, ID: id}, true
}

func parsePositiveInt64(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("empty identifier")
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid identifier")
	}
	return parsed, nil
}
