CREATE TABLE project_key_rate_usage (
 api_id uuid NOT NULL REFERENCES project_keys(api_id) ON DELETE CASCADE,
 window_start timestamptz NOT NULL,
 count bigint NOT NULL CHECK (count>=0),
 PRIMARY KEY(api_id,window_start)
);
CREATE INDEX project_key_rate_usage_window ON project_key_rate_usage(window_start);
