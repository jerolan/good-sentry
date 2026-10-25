CREATE SEQUENCE project_native_reprocessing_generation;

CREATE TABLE project_native_reprocessing (
 project_id bigint PRIMARY KEY REFERENCES projects ON DELETE CASCADE,
 cursor_event_id bigint NOT NULL DEFAULT 0 CHECK (cursor_event_id >= 0),
 through_event_id bigint NOT NULL DEFAULT 0 CHECK (through_event_id >= cursor_event_id),
 generation bigint NOT NULL DEFAULT nextval('project_native_reprocessing_generation') CHECK (generation > 0),
 requested_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX project_native_reprocessing_order ON project_native_reprocessing(requested_at,project_id);

CREATE INDEX events_native_reprocessing ON events(project_id,id)
 WHERE kind='error' AND data->>'platform'='native';
