CREATE TABLE spans (
 id bigserial PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 trace_id text NOT NULL, span_id text NOT NULL, parent_span_id text NOT NULL DEFAULT '',
 name text NOT NULL, status text NOT NULL CHECK (status IN ('ok','error')),
 is_segment boolean NOT NULL,
 start_timestamp timestamptz NOT NULL, end_timestamp timestamptz NOT NULL,
 duration double precision NOT NULL CHECK (duration >= 0),
 op text NOT NULL DEFAULT '', environment text NOT NULL DEFAULT '', release text NOT NULL DEFAULT '',
 attributes jsonb NOT NULL DEFAULT '{}', data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (end_timestamp >= start_timestamp), UNIQUE(project_id,trace_id,span_id)
);
CREATE INDEX spans_project_start ON spans(project_id,start_timestamp);
CREATE INDEX spans_project_trace ON spans(project_id,trace_id,start_timestamp);
CREATE INDEX spans_end_time ON spans(end_timestamp);
CREATE INDEX spans_project_segment_time ON spans(project_id,end_timestamp DESC) WHERE is_segment;
