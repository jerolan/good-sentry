package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidLog = errors.New("invalid log payload")

type TransactionGroup struct {
	Transaction   string    `json:"transaction"`
	Op            string    `json:"op"`
	Count         int64     `json:"count"`
	ErrorCount    int64     `json:"errorCount"`
	AvgDuration   float64   `json:"avgDuration"`
	P50           float64   `json:"p50"`
	P95           float64   `json:"p95"`
	LastSeen      time.Time `json:"lastSeen"`
	LatestTraceID string    `json:"latestTraceId"`
}

type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type LogFilter struct {
	Project, Query, Level, Environment, TraceID, Service string
	Window                                               *TimeWindow
	Page                                                 int
}

type Log struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	Timestamp   time.Time       `json:"timestamp"`
	TraceID     string          `json:"traceId"`
	SpanID      string          `json:"spanId"`
	Level       string          `json:"level"`
	Body        string          `json:"body"`
	Service     string          `json:"service"`
	Environment string          `json:"environment"`
	Attributes  json.RawMessage `json:"attributes"`
}

func (s *Store) Transactions(ctx context.Context, userID, orgSlug, projectID string, window *TimeWindow) ([]TransactionGroup, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return nil, err
	}
	args := []any{org.ID, projectID}
	eventRange := "e.timestamp>=now()-interval '24 hours' AND e.timestamp<=now()"
	spanRange := "s.end_timestamp>=now()-interval '24 hours' AND s.end_timestamp<=now()"
	if window != nil {
		args = append(args, window.Start, window.End)
		eventRange = "e.timestamp>=$3::timestamptz AND e.timestamp<$4::timestamptz"
		spanRange = "s.end_timestamp>=$3::timestamptz AND s.end_timestamp<$4::timestamptz"
	}
	rows, err := s.pool.Query(ctx, `WITH telemetry AS (
		SELECT e.transaction_name AS name,COALESCE(e.data#>>'{contexts,trace,op}','') AS op,e.duration,
		(e.level IN ('error','fatal') OR COALESCE(e.data#>>'{contexts,trace,status}','') NOT IN ('','ok','unknown')) AS failed,
		e.timestamp AS timestamp,e.trace_id,e.id,0 AS source
		FROM events e JOIN projects p ON p.id=e.project_id
		WHERE p.organization_id::text=$1 AND ($2='' OR e.project_id::text=$2) AND e.kind='transaction' AND `+eventRange+`
		UNION ALL
		SELECT s.name,s.op,s.duration,s.status='error',s.end_timestamp,s.trace_id,s.id,1
		FROM spans s JOIN projects p ON p.id=s.project_id
		WHERE p.organization_id::text=$1 AND ($2='' OR s.project_id::text=$2) AND s.is_segment AND `+spanRange+`
	)
		SELECT name,op,count(*),count(*) FILTER(WHERE failed),avg(duration),
		percentile_cont(0.5) WITHIN GROUP(ORDER BY duration),percentile_cont(0.95) WITHIN GROUP(ORDER BY duration),
		max(timestamp),(array_agg(trace_id ORDER BY timestamp DESC,source DESC,id DESC))[1]
		FROM telemetry
		GROUP BY name,op ORDER BY count(*) DESC,max(timestamp) DESC,name,op`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TransactionGroup{}
	for rows.Next() {
		var g TransactionGroup
		if err := rows.Scan(&g.Transaction, &g.Op, &g.Count, &g.ErrorCount, &g.AvgDuration, &g.P50, &g.P95, &g.LastSeen, &g.LatestTraceID); err != nil {
			return nil, err
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

func (s *Store) Logs(ctx context.Context, userID, orgSlug string, f LogFilter) ([]Log, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return nil, err
	}
	args := []any{org.ID}
	where := "WHERE p.organization_id::text=$1 "
	for _, filter := range []struct{ value, column string }{
		{f.Project, "l.project_id::text"}, {f.Level, "l.level"},
		{f.Environment, "l.environment"}, {f.TraceID, "l.trace_id"},
		{f.Service, "l.service"},
	} {
		if filter.value != "" {
			args = append(args, filter.value)
			where += fmt.Sprintf("AND %s=$%d ", filter.column, len(args))
		}
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		where += fmt.Sprintf("AND l.body ILIKE $%d ", len(args))
	}
	if f.Window != nil {
		args = append(args, f.Window.Start, f.Window.End)
		where += fmt.Sprintf("AND l.timestamp>=$%d::timestamptz AND l.timestamp<$%d::timestamptz ", len(args)-1, len(args))
	}
	page := int64(f.Page)
	if page < 1 {
		page = 1
	}
	if page > math.MaxInt64/25 {
		return nil, errors.New("log page is outside supported range")
	}
	args = append(args, (page-1)*25)
	rows, err := s.pool.Query(ctx, `SELECT l.id::text,l.project_id::text,l.timestamp,l.trace_id,l.span_id,
		l.level,l.body,l.service,l.environment,l.attributes FROM logs l JOIN projects p ON p.id=l.project_id `+
		where+fmt.Sprintf("ORDER BY l.timestamp DESC,l.id DESC LIMIT 25 OFFSET $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Log{}
	for rows.Next() {
		var l Log
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Timestamp, &l.TraceID, &l.SpanID, &l.Level, &l.Body, &l.Service, &l.Environment, &l.Attributes); err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	return items, rows.Err()
}

type storedLog struct {
	Log
	data json.RawMessage
	hash string
}

type logAttribute struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func (s *Store) IngestLogs(ctx context.Context, projectID string, payload json.RawMessage) (int, error) {
	logs, err := parseLogs(payload)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidLog, err)
	}
	if len(logs) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	count := 0
	for _, l := range logs {
		result, err := tx.Exec(ctx, `INSERT INTO logs(project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(project_id,item_hash) DO NOTHING`,
			projectID, l.Timestamp, l.TraceID, l.SpanID, l.Level, l.Body, l.Service, l.Environment, l.Attributes, l.data, l.hash)
		if err != nil {
			return 0, err
		}
		count += int(result.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

func parseLogs(raw json.RawMessage) ([]storedLog, error) {
	var batch struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &batch); err != nil || batch.Items == nil {
		return nil, errors.New("expected an object containing an items array")
	}
	if len(batch.Items) > 100 {
		return nil, errors.New("log batch exceeds 100 items")
	}
	logs := make([]storedLog, 0, len(batch.Items))
	for i, raw := range batch.Items {
		l, err := parseLog(raw)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func parseLog(raw json.RawMessage) (storedLog, error) {
	var p struct {
		Timestamp  json.RawMessage         `json:"timestamp"`
		TraceID    string                  `json:"trace_id"`
		SpanID     string                  `json:"span_id"`
		Level      string                  `json:"level"`
		Body       *string                 `json:"body"`
		Attributes map[string]logAttribute `json:"attributes"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return storedLog{}, err
	}
	if p.Body == nil || strings.TrimSpace(p.Level) == "" {
		return storedLog{}, errors.New("body must be a string and level must be nonempty")
	}
	stamp, err := logTimestamp(p.Timestamp)
	if err != nil {
		return storedLog{}, err
	}
	l := storedLog{Log: Log{Timestamp: stamp, TraceID: p.TraceID, SpanID: p.SpanID, Level: p.Level, Body: *p.Body}}
	for name, attr := range p.Attributes {
		if err := validateLogAttribute(attr); err != nil {
			return storedLog{}, fmt.Errorf("attribute %q: %w", name, err)
		}
		if attr.Type == "string" {
			var value string
			_ = json.Unmarshal(attr.Value, &value)
			switch name {
			case "service.name":
				l.Service = value
			case "environment":
				l.Environment = value
			}
		}
	}
	if attr, ok := p.Attributes["sentry.environment"]; ok && attr.Type == "string" {
		_ = json.Unmarshal(attr.Value, &l.Environment)
	}
	if p.Attributes == nil {
		p.Attributes = map[string]logAttribute{}
	}
	l.Attributes, _ = json.Marshal(p.Attributes)
	// Canonical maps give retries the same identity across object key order and
	// whitespace changes. UseNumber preserves numeric values without float loss.
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return storedLog{}, err
	}
	l.data, _ = json.Marshal(value)
	digest := sha256.Sum256(l.data)
	l.hash = hex.EncodeToString(digest[:])
	return l, nil
}

func logTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			stamp, err := time.Parse(time.RFC3339Nano, value)
			if err == nil {
				return stamp.UTC(), nil
			}
		}
	}
	var seconds float64
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &seconds) != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < -62135596800 || seconds >= 253402300800 {
		return time.Time{}, errors.New("timestamp must be finite Unix seconds or RFC3339")
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(math.Round(fraction*1e9))).UTC(), nil
}

func validateLogAttribute(attr logAttribute) error {
	if len(attr.Value) == 0 || string(attr.Value) == "null" {
		return errors.New("attribute value is required")
	}
	valid := false
	switch attr.Type {
	case "string":
		var value string
		valid = json.Unmarshal(attr.Value, &value) == nil
	case "boolean":
		var value bool
		valid = json.Unmarshal(attr.Value, &value) == nil
	case "integer":
		_, err := strconv.ParseInt(string(attr.Value), 10, 64)
		valid = err == nil
	case "double":
		var value float64
		valid = json.Unmarshal(attr.Value, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	if !valid {
		return fmt.Errorf("value does not match supported attribute type %q", attr.Type)
	}
	return nil
}
