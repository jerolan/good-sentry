package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"goodsentry/internal/otlp"
)

type LogDetail struct {
	Log
	Data            json.RawMessage `json:"data"`
	TimestampSource string          `json:"timestampSource,omitempty"`
	ReceivedAt      *time.Time      `json:"receivedAt,omitempty"`
	TraceFlags      *uint32         `json:"traceFlags,omitempty"`
}

// IngestOTLP commits every occurrence in a fully Prepare-admitted batch. It
// reads the batch without mutation, so concurrent retries remain independent.
func (s *Store) IngestOTLP(ctx context.Context, projectID string, batch *otlp.Batch) (int, error) {
	if batch == nil {
		return 0, otlp.ErrMalformed
	}
	if len(batch.Records) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var batchID int64
	if err := tx.QueryRow(ctx, `INSERT INTO otlp_log_batches(project_id,received_at,payload) VALUES($1,$2,$3) RETURNING id`, projectID, batch.ReceivedAt, batch.Payload).Scan(&batchID); err != nil {
		return 0, err
	}
	for index, record := range batch.Records {
		// An OTLP record has no mandatory identity. Domain-separated batch/ordinal
		// identity preserves identical records and replays under the SDK unique key.
		digest := sha256.Sum256([]byte(fmt.Sprintf("goodsentry-otlp-log-v1\x00%d\x00%d", batchID, index)))
		result, err := tx.Exec(ctx, `INSERT INTO logs(project_id,timestamp,trace_id,span_id,level,body,service,environment,attributes,data,item_hash,otlp_batch_id,otlp_record_index)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,'{}','{}',$9,$10,$11)`, projectID, record.Timestamp, record.TraceID, record.SpanID, record.Level, record.Body, record.Service, record.Environment, hex.EncodeToString(digest[:]), batchID, index)
		if err != nil {
			return 0, err
		}
		if result.RowsAffected() != 1 {
			return 0, errors.New("OTLP log insert did not create one occurrence")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(batch.Records), nil
}

// Log authorizes membership and row/project containment in the same statement
// that retrieves canonical data. The byte-capacity branch returns no oversized
// OTLP payload to the application, including after a deployment limit reduction.
func (s *Store) Log(ctx context.Context, userID, orgSlug, logID string, limits otlp.Limits) (LogDetail, error) {
	return s.readLog(ctx, ManagementAccess{UserID: userID, AllProjects: true}, orgSlug, logID, limits)
}

func (s *Store) readLog(ctx context.Context, access ManagementAccess, orgSlug, logID string, limits otlp.Limits) (LogDetail, error) {
	var detail LogDetail
	var native bool
	var payload []byte
	var payloadBytes int64
	var receivedAt time.Time
	var index int
	err := s.pool.QueryRow(ctx, `SELECT l.id::text,l.project_id::text,l.timestamp,l.trace_id,l.span_id,
  l.level,l.body,l.service,l.environment,l.attributes,l.data,l.otlp_batch_id IS NOT NULL,
  CASE WHEN octet_length(b.payload)<=$4 THEN b.payload END,
  COALESCE(octet_length(b.payload),0),COALESCE(b.received_at,l.timestamp),COALESCE(l.otlp_record_index,-1)
  FROM logs l JOIN projects p ON p.id=l.project_id
  JOIN organizations o ON o.id=p.organization_id
  JOIN memberships m ON m.organization_id=o.id AND m.user_id::text=$1
  LEFT JOIN otlp_log_batches b ON b.project_id=l.project_id AND b.id=l.otlp_batch_id
  WHERE (($5='' AND o.slug=$2) OR ($5<>'' AND o.id::text=$5)) AND l.id::text=$3
  AND ($6 OR p.id=ANY($7::bigint[]))`, access.UserID, orgSlug, logID, limits.MaxBytes, access.OrganizationID, access.AllProjects, access.ProjectIDs).Scan(
		&detail.ID, &detail.ProjectID, &detail.Timestamp, &detail.TraceID, &detail.SpanID,
		&detail.Level, &detail.Body, &detail.Service, &detail.Environment, &detail.Attributes, &detail.Data, &native,
		&payload, &payloadBytes, &receivedAt, &index)
	if err != nil {
		return LogDetail{}, dbError(err)
	}
	detail.Source = "sentry"
	if !native {
		return detail, nil
	}
	if payloadBytes > limits.MaxBytes {
		return LogDetail{}, otlp.ErrTooLarge
	}
	selected, err := otlp.Select(payload, index, receivedAt, limits)
	if err != nil {
		return LogDetail{}, err
	}
	detail.Source = "otlp"
	detail.Attributes = nil
	detail.Data = selected.Data
	detail.TimestampSource = selected.TimestampSource
	detail.ReceivedAt = &selected.ReceivedAt
	detail.TraceFlags = &selected.TraceFlags
	return detail, nil
}
