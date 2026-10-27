-- Durable local copy of retained transaction event rows. Span rows have their
-- own archive; this table/view covers the transaction event projection.
CREATE TABLE transaction_archive (
 id bigint PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 event_id text NOT NULL,
 issue_id bigint REFERENCES issues ON DELETE CASCADE,
 original_issue_id bigint,
 kind text NOT NULL,
 title text NOT NULL,
 level text NOT NULL,
 timestamp timestamptz NOT NULL,
 environment text NOT NULL,
 release text NOT NULL,
 trace_id text NOT NULL,
 transaction_name text NOT NULL,
 duration double precision NOT NULL,
 data jsonb NOT NULL,
 created_at timestamptz NOT NULL,
 archived_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(project_id,event_id)
);
CREATE INDEX transaction_archive_project_date ON transaction_archive(project_id,timestamp DESC);
CREATE INDEX transaction_archive_trace ON transaction_archive(project_id,trace_id);
CREATE INDEX transaction_archive_created ON transaction_archive(created_at,id);

CREATE VIEW transactions_with_archive AS
 SELECT e.id,e.project_id,e.event_id,e.issue_id,e.original_issue_id,e.kind,e.title,e.level,e.timestamp,e.environment,e.release,e.trace_id,e.transaction_name,e.duration,e.data,e.created_at
 FROM events e
 UNION ALL
 SELECT a.id,a.project_id,a.event_id,a.issue_id,a.original_issue_id,a.kind,a.title,a.level,a.timestamp,a.environment,a.release,a.trace_id,a.transaction_name,a.duration,a.data,a.created_at
 FROM transaction_archive a
 WHERE NOT EXISTS (SELECT 1 FROM events h WHERE h.id=a.id);
