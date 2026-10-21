ALTER TABLE logs ADD COLUMN host text NOT NULL DEFAULT '';
CREATE INDEX logs_project_timestamp_id ON logs(project_id,timestamp DESC,id DESC);
