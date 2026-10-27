-- Durable local copy of retained logs. Hot rows remain authoritative until the
-- existing log-retention cutoff; readers union the copy without duplicates.
CREATE TABLE log_archive (
 id bigint PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 timestamp timestamptz NOT NULL,
 trace_id text NOT NULL,
 span_id text NOT NULL,
 level text NOT NULL,
 body text NOT NULL,
 service text NOT NULL,
 environment text NOT NULL,
 attributes jsonb NOT NULL,
 data jsonb NOT NULL,
 item_hash text NOT NULL,
 created_at timestamptz NOT NULL,
 otlp_batch_id bigint,
 otlp_record_index integer,
 host text NOT NULL,
 archived_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX log_archive_project_hash ON log_archive(project_id,item_hash);
CREATE INDEX log_archive_project_date ON log_archive(project_id,timestamp DESC);
CREATE INDEX log_archive_created ON log_archive(created_at,id);

CREATE VIEW logs_with_archive AS
 SELECT l.id,l.project_id,l.timestamp,l.trace_id,l.span_id,l.level,l.body,l.service,l.environment,l.attributes,l.data,l.item_hash,l.created_at,l.otlp_batch_id,l.otlp_record_index,l.host
 FROM logs l
 UNION ALL
 SELECT a.id,a.project_id,a.timestamp,a.trace_id,a.span_id,a.level,a.body,a.service,a.environment,a.attributes,a.data,a.item_hash,a.created_at,a.otlp_batch_id,a.otlp_record_index,a.host
 FROM log_archive a
 WHERE NOT EXISTS (SELECT 1 FROM logs h WHERE h.id=a.id);
