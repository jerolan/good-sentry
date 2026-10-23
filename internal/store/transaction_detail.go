package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

func (s *Store) TransactionDetail(ctx context.Context, userID, orgSlug, source, id string, page int) (TransactionDetail, error) {
	if source != "event" && source != "span" {
		return TransactionDetail{}, ErrNotFound
	}
	rowID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || rowID <= 0 || strconv.FormatInt(rowID, 10) != id {
		return TransactionDetail{}, ErrNotFound
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) (TransactionDetail, error) {
		orgID, err := read.organizationID(ctx, userID, orgSlug)
		if err != nil {
			return TransactionDetail{}, err
		}
		tx := read.tx
		detail := TransactionDetail{
			Sample: TransactionReference{Source: source, ID: id},
			Page:   max(page, 1), PageSize: 50, Spans: []TransactionDetailSpan{},
		}
		if source == "event" {
			err = tx.QueryRow(ctx, `SELECT e.project_id::text,p.name,e.event_id,e.trace_id,e.transaction_name,
				e.environment,e.release,e.timestamp,e.duration,e.data
				FROM events e JOIN projects p ON p.id=e.project_id
				WHERE p.organization_id=$1 AND e.id=$2 AND e.kind='transaction'`, orgID, rowID).Scan(
				&detail.Sample.ProjectID, &detail.ProjectName, &detail.EventID, &detail.TraceID, &detail.Name,
				&detail.Environment, &detail.Release, &detail.EndTimestamp, &detail.Duration, &detail.Data)
			if err != nil {
				return TransactionDetail{}, dbError(err)
			}
			root := detailObject(detail.Data)
			trace := detailObject(detailObject(root["contexts"])["trace"])
			detail.StartTimestamp = detailTimestamp(root["start_timestamp"])
			detail.SpanID = detailString(trace, "span_id")
			detail.ParentSpanID = detailString(trace, "parent_span_id")
			detail.Op, detail.Status = detailString(trace, "op"), detailString(trace, "status")
			detail.SpanScope = "transaction"
			var children []json.RawMessage
			if json.Unmarshal(root["spans"], &children) == nil {
				detail.SpanCount = int64(len(children))
				first := int64(detail.Page-1) * int64(detail.PageSize)
				for i := first; i < min(first+int64(detail.PageSize), detail.SpanCount); i++ {
					detail.Spans = append(detail.Spans, legacyDetailSpan(children[i], i+1))
				}
				detail.HasMore = first+int64(len(detail.Spans)) < detail.SpanCount
			}
		} else {
			if err := read.Load(ctx, spanArchiveSelector{OrganizationID: orgID, SpanRowID: rowID}); err != nil {
				return TransactionDetail{}, err
			}
			var start time.Time
			err = tx.QueryRow(ctx, `SELECT s.project_id::text,p.name,s.trace_id,s.span_id,s.parent_span_id,s.name,
				s.op,s.status,s.environment,s.release,s.start_timestamp,s.end_timestamp,s.duration,s.data
				FROM spans s JOIN projects p ON p.id=s.project_id
				WHERE p.organization_id=$1 AND s.id=$2 AND s.is_segment`, orgID, rowID).Scan(
				&detail.Sample.ProjectID, &detail.ProjectName, &detail.TraceID, &detail.SpanID, &detail.ParentSpanID,
				&detail.Name, &detail.Op, &detail.Status, &detail.Environment, &detail.Release, &start,
				&detail.EndTimestamp, &detail.Duration, &detail.Data)
			if err != nil {
				return TransactionDetail{}, dbError(err)
			}
			projectID, err := strconv.ParseInt(detail.Sample.ProjectID, 10, 64)
			if err != nil {
				return TransactionDetail{}, err
			}
			if err := read.Load(ctx, spanArchiveSelector{ProjectID: projectID, TraceID: detail.TraceID}); err != nil {
				return TransactionDetail{}, err
			}
			start = start.UTC()
			detail.StartTimestamp, detail.SpanScope = &start, "projectTrace"
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM spans WHERE project_id=$1 AND trace_id=$2`, detail.Sample.ProjectID, detail.TraceID).Scan(&detail.SpanCount); err != nil {
				return TransactionDetail{}, err
			}
			offset := int64(detail.Page-1) * int64(detail.PageSize)
			rows, err := tx.Query(ctx, `SELECT id::text,span_id,parent_span_id,name,op,status,start_timestamp,end_timestamp,duration,data
				FROM spans WHERE project_id=$1 AND trace_id=$2
				ORDER BY start_timestamp,spans.id LIMIT 50 OFFSET $3`, detail.Sample.ProjectID, detail.TraceID, offset)
			if err != nil {
				return TransactionDetail{}, err
			}
			for rows.Next() {
				var span TransactionDetailSpan
				var start, end time.Time
				var duration float64
				if err := rows.Scan(&span.ID, &span.SpanID, &span.ParentSpanID, &span.Name, &span.Op, &span.Status, &start, &end, &duration, &span.Data); err != nil {
					rows.Close()
					return TransactionDetail{}, err
				}
				start, end = start.UTC(), end.UTC()
				span.StartTimestamp, span.EndTimestamp, span.Duration = &start, &end, &duration
				detail.Spans = append(detail.Spans, span)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return TransactionDetail{}, err
			}
			detail.HasMore = offset+int64(len(detail.Spans)) < detail.SpanCount
		}
		detail.EndTimestamp = detail.EndTimestamp.UTC()
		return detail, nil
	})
}

func legacyDetailSpan(raw json.RawMessage, ordinal int64) TransactionDetailSpan {
	fields := detailObject(raw)
	span := TransactionDetailSpan{
		ID: strconv.FormatInt(ordinal, 10), Data: raw,
		SpanID: detailString(fields, "span_id"), ParentSpanID: detailString(fields, "parent_span_id"),
		Op: detailString(fields, "op"), Status: detailString(fields, "status"),
		StartTimestamp: detailTimestamp(fields["start_timestamp"]), EndTimestamp: detailTimestamp(fields["timestamp"]),
	}
	var description *string
	if json.Unmarshal(fields["description"], &description) == nil && description != nil {
		span.Name = *description
	} else {
		span.Name = detailString(fields, "name")
	}
	if span.StartTimestamp != nil && span.EndTimestamp != nil && !span.EndTimestamp.Before(*span.StartTimestamp) {
		duration := float64(span.EndTimestamp.Unix()-span.StartTimestamp.Unix())*1000 + float64(span.EndTimestamp.Nanosecond()-span.StartTimestamp.Nanosecond())/1e6
		span.Duration = &duration
	}
	return span
}

func detailObject(raw json.RawMessage) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	return fields
}

func detailString(fields map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return value
}

func detailTimestamp(raw json.RawMessage) *time.Time {
	stamp, err := logTimestamp(raw)
	if err != nil || stamp.Year() < 1 || stamp.Year() > 9999 {
		return nil
	}
	stamp = stamp.UTC().Truncate(time.Microsecond)
	return &stamp
}
