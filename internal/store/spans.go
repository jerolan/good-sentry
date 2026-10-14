package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

var ErrInvalidSpan = errors.New("invalid span payload")

type Span struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	TraceID        string          `json:"traceId"`
	SpanID         string          `json:"spanId"`
	ParentSpanID   string          `json:"parentSpanId"`
	Name           string          `json:"name"`
	Status         string          `json:"status"`
	IsSegment      bool            `json:"isSegment"`
	StartTimestamp time.Time       `json:"startTimestamp"`
	EndTimestamp   time.Time       `json:"endTimestamp"`
	Duration       float64         `json:"duration"` // Milliseconds.
	Op             string          `json:"op"`
	Environment    string          `json:"environment"`
	Release        string          `json:"release"`
	Attributes     json.RawMessage `json:"attributes"`
	RawSpan        json.RawMessage `json:"rawSpan"`
}

func (s *Store) IngestSpans(ctx context.Context, projectID string, payload json.RawMessage, expectedCount int) (int, error) {
	spans, err := parseSpans(payload, expectedCount)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidSpan, err)
	}
	// All rows share a trace. A stable acquisition order prevents duplicate
	// batches with reversed item order from taking unique-key locks in a cycle.
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].SpanID < spans[j].SpanID })
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	count := 0
	for _, span := range spans {
		result, err := tx.Exec(ctx, `INSERT INTO spans(project_id,trace_id,span_id,parent_span_id,name,status,is_segment,
			start_timestamp,end_timestamp,duration,op,environment,release,attributes,data)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT(project_id,trace_id,span_id) DO NOTHING`, projectID, span.TraceID, span.SpanID,
			span.ParentSpanID, span.Name, span.Status, span.IsSegment, span.StartTimestamp, span.EndTimestamp,
			span.Duration, span.Op, span.Environment, span.Release, span.Attributes, span.RawSpan)
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

func (s *Store) Spans(ctx context.Context, userID, orgSlug, projectID, traceID string) ([]Span, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id::text,s.project_id::text,s.trace_id,s.span_id,s.parent_span_id,s.name,
		s.status,s.is_segment,s.start_timestamp,s.end_timestamp,s.duration,s.op,s.environment,s.release,s.attributes,s.data
		FROM spans s JOIN projects p ON p.id=s.project_id
		WHERE p.organization_id::text=$1 AND ($2='' OR s.project_id::text=$2) AND ($3='' OR s.trace_id=$3)
		ORDER BY s.start_timestamp,s.id LIMIT 1000`, org.ID, projectID, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Span{}
	for rows.Next() {
		var span Span
		if err := rows.Scan(&span.ID, &span.ProjectID, &span.TraceID, &span.SpanID, &span.ParentSpanID, &span.Name,
			&span.Status, &span.IsSegment, &span.StartTimestamp, &span.EndTimestamp, &span.Duration, &span.Op,
			&span.Environment, &span.Release, &span.Attributes, &span.RawSpan); err != nil {
			return nil, err
		}
		items = append(items, span)
	}
	return items, rows.Err()
}

func parseSpans(raw json.RawMessage, expectedCount int) ([]Span, error) {
	var batch struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &batch); err != nil || len(batch.Items) < 1 || len(batch.Items) > 1000 {
		return nil, errors.New("expected an object containing 1 to 1000 span items")
	}
	if expectedCount != len(batch.Items) {
		return nil, errors.New("item_count does not match span items")
	}
	spans := make([]Span, 0, len(batch.Items))
	for i, raw := range batch.Items {
		span, err := parseSpan(raw)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		if len(spans) != 0 && spans[0].TraceID != span.TraceID {
			return nil, errors.New("all span items must belong to one trace")
		}
		spans = append(spans, span)
	}
	return spans, nil
}

func parseSpan(raw json.RawMessage) (Span, error) {
	var p struct {
		TraceID        string          `json:"trace_id"`
		SpanID         string          `json:"span_id"`
		ParentSpanID   json.RawMessage `json:"parent_span_id"`
		Name           *string         `json:"name"`
		Status         string          `json:"status"`
		IsSegment      *bool           `json:"is_segment"`
		StartTimestamp json.RawMessage `json:"start_timestamp"`
		EndTimestamp   json.RawMessage `json:"end_timestamp"`
		Attributes     json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Span{}, err
	}
	if !lowerHexID(p.TraceID, 32) || !lowerHexID(p.SpanID, 16) {
		return Span{}, errors.New("trace_id and span_id require 32 and 16 lowercase hexadecimal digits")
	}
	span := Span{TraceID: p.TraceID, SpanID: p.SpanID, Status: p.Status, RawSpan: raw}
	if len(p.ParentSpanID) > 0 {
		if json.Unmarshal(p.ParentSpanID, &span.ParentSpanID) != nil || !lowerHexID(span.ParentSpanID, 16) {
			return Span{}, errors.New("parent_span_id requires 16 lowercase hexadecimal digits")
		}
	}
	if p.Name == nil || strings.TrimSpace(*p.Name) == "" || p.IsSegment == nil || (p.Status != "ok" && p.Status != "error") {
		return Span{}, errors.New("span requires a nonempty name, boolean is_segment and status ok or error")
	}
	span.Name, span.IsSegment = *p.Name, *p.IsSegment
	var err error
	span.StartTimestamp, err = spanTimestamp(p.StartTimestamp)
	if err != nil {
		return Span{}, fmt.Errorf("start_timestamp: %w", err)
	}
	span.EndTimestamp, err = spanTimestamp(p.EndTimestamp)
	if err != nil {
		return Span{}, fmt.Errorf("end_timestamp: %w", err)
	}
	if span.EndTimestamp.Before(span.StartTimestamp) {
		return Span{}, errors.New("span ends before it starts")
	}
	span.Duration = float64(span.EndTimestamp.Unix()-span.StartTimestamp.Unix())*1000 + float64(span.EndTimestamp.Nanosecond()-span.StartTimestamp.Nanosecond())/1e6
	span.Attributes = p.Attributes
	if len(span.Attributes) == 0 {
		span.Attributes = json.RawMessage(`{}`)
	}
	var attrs map[string]json.RawMessage
	if json.Unmarshal(span.Attributes, &attrs) != nil || attrs == nil {
		return Span{}, errors.New("attributes must be an object")
	}
	for name, raw := range attrs {
		value, err := spanAttribute(raw)
		if err != nil {
			return Span{}, fmt.Errorf("attribute %q: %w", name, err)
		}
		if value != nil {
			switch name {
			case "sentry.op":
				span.Op = *value
			case "sentry.environment":
				span.Environment = *value
			case "sentry.release":
				span.Release = *value
			}
		}
	}
	return span, nil
}

func lowerHexID(id string, length int) bool {
	if len(id) != length {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func spanTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || raw[0] == '"' {
		return time.Time{}, errors.New("expected numeric Unix seconds")
	}
	return logTimestamp(raw)
}

func spanAttribute(raw json.RawMessage) (*string, error) {
	var attr struct {
		logAttribute
		Unit json.RawMessage `json:"unit"`
	}
	if err := json.Unmarshal(raw, &attr); err != nil {
		return nil, err
	}
	if len(attr.Unit) > 0 {
		var unit *string
		if json.Unmarshal(attr.Unit, &unit) != nil || unit == nil {
			return nil, errors.New("unit must be a string")
		}
	}
	if attr.Type != "array" {
		if err := validateLogAttribute(attr.logAttribute); err != nil {
			return nil, err
		}
		if attr.Type == "string" {
			var value string
			_ = json.Unmarshal(attr.Value, &value)
			return &value, nil
		}
		return nil, nil
	}
	var values []json.RawMessage
	if json.Unmarshal(attr.Value, &values) != nil || values == nil {
		return nil, errors.New("array attribute requires an array value")
	}
	for _, value := range values {
		var scalar any
		if json.Unmarshal(value, &scalar) != nil {
			return nil, errors.New("invalid array attribute element")
		}
		switch v := scalar.(type) {
		case string, bool:
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, errors.New("array attribute numbers must be finite")
			}
		default:
			return nil, errors.New("array attributes contain only scalar values")
		}
	}
	return nil, nil
}
