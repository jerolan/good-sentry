ALTER TABLE events ADD CONSTRAINT events_report_receipt UNIQUE(id,project_id,event_id);
CREATE TABLE user_reports (
 id bigserial PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 event_id text NOT NULL CHECK(event_id ~ '^[0-9a-f]{32}$'),
 event_receipt_id bigint,
 name text NOT NULL, email text NOT NULL, comments text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(project_id,event_id),
 FOREIGN KEY(event_receipt_id,project_id,event_id) REFERENCES events(id,project_id,event_id) ON DELETE CASCADE
);
CREATE INDEX user_reports_receipt ON user_reports(event_receipt_id);
CREATE INDEX user_reports_pending ON user_reports(created_at,id) WHERE event_receipt_id IS NULL;
