-- Durable local copy of retained error/message receipts. Read transactions
-- union hot rows with this copy; the hot table remains authoritative until the
-- existing event-retention cutoff, so child rows and counts keep their contracts.
CREATE TABLE event_archive (
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
CREATE INDEX event_archive_issue_date ON event_archive(issue_id,timestamp DESC);
CREATE INDEX event_archive_project_date ON event_archive(project_id,timestamp DESC);
CREATE INDEX event_archive_created ON event_archive(created_at,id);

-- Hot rows win when both copies exist, so enabling the archive never duplicates
-- an event in existing readers. The view exposes exactly the hot table shape.
CREATE VIEW events_with_archive AS
 SELECT e.id,e.project_id,e.event_id,e.issue_id,e.original_issue_id,e.kind,e.title,e.level,e.timestamp,e.environment,e.release,e.trace_id,e.transaction_name,e.duration,e.data,e.created_at
 FROM events e
 UNION ALL
 SELECT a.id,a.project_id,a.event_id,a.issue_id,a.original_issue_id,a.kind,a.title,a.level,a.timestamp,a.environment,a.release,a.trace_id,a.transaction_name,a.duration,a.data,a.created_at
 FROM event_archive a
 WHERE NOT EXISTS (SELECT 1 FROM events h WHERE h.project_id=a.project_id AND h.event_id=a.event_id);
