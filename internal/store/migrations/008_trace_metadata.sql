ALTER TABLE events
 ADD COLUMN trace_span_id text NOT NULL DEFAULT '',
 ADD COLUMN trace_parent_span_id text NOT NULL DEFAULT '',
 ADD COLUMN trace_op text NOT NULL DEFAULT '',
 ADD COLUMN trace_status text NOT NULL DEFAULT '',
 ADD COLUMN trace_start_timestamp timestamptz;

ALTER TABLE transaction_span_operations
 ADD COLUMN span_id text NOT NULL DEFAULT '',
 ADD COLUMN parent_span_id text NOT NULL DEFAULT '',
 ADD COLUMN status text NOT NULL DEFAULT '',
 ADD COLUMN start_timestamp timestamptz,
 ADD COLUMN end_timestamp timestamptz;

DROP INDEX events_span_projection_pending;
CREATE INDEX events_span_projection_pending ON events(id)
 WHERE kind='transaction' AND span_projection_version<2;

CREATE INDEX events_trace_case ON events(lower(trace_id),project_id)
 WHERE trace_id<>'';
CREATE INDEX logs_trace_case ON logs(lower(trace_id),project_id)
 WHERE trace_id<>'';
CREATE INDEX spans_trace_lookup ON spans(trace_id,project_id);
