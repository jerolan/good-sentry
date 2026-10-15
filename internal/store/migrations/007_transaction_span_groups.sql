ALTER TABLE events ADD COLUMN span_projection_version smallint NOT NULL DEFAULT 0;
CREATE INDEX events_span_projection_pending ON events(id)
 WHERE kind='transaction' AND span_projection_version=0;

CREATE TABLE transaction_span_operations (
 event_id bigint NOT NULL REFERENCES events ON DELETE CASCADE,
 ordinal integer NOT NULL CHECK (ordinal>0),
 op text NOT NULL, description text NOT NULL,
 duration double precision CHECK (duration>=0),
 PRIMARY KEY(event_id,ordinal)
);

CREATE INDEX spans_segment_operations ON spans
 (project_id,trace_id,(attributes#>>'{sentry.segment.id,value}'))
 WHERE NOT is_segment AND attributes#>>'{sentry.segment.id,type}'='string'
 AND jsonb_typeof(attributes#>'{sentry.segment.id,value}')='string';
