CREATE TABLE project_environments (
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
 is_hidden boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,name)
);

CREATE INDEX IF NOT EXISTS project_environments_visibility ON project_environments(project_id,is_hidden,name);
CREATE INDEX IF NOT EXISTS logs_project_environment ON logs(project_id,environment) WHERE environment<>'';
CREATE INDEX IF NOT EXISTS spans_project_environment ON spans(project_id,environment) WHERE environment<>'';
CREATE INDEX IF NOT EXISTS monitors_project_environment ON monitors(project_id,environment) WHERE environment<>'';

INSERT INTO project_environments(project_id,name)
SELECT DISTINCT project_id,environment FROM events WHERE environment<>'' AND char_length(environment)<=255
ON CONFLICT DO NOTHING;

INSERT INTO project_environments(project_id,name)
SELECT DISTINCT project_id,environment FROM monitors WHERE environment<>'' AND char_length(environment)<=255
ON CONFLICT DO NOTHING;

INSERT INTO project_environments(project_id,name)
SELECT DISTINCT project_id,environment FROM logs WHERE environment<>'' AND char_length(environment)<=255
ON CONFLICT DO NOTHING;

INSERT INTO project_environments(project_id,name)
SELECT DISTINCT project_id,environment FROM spans WHERE environment<>'' AND char_length(environment)<=255
ON CONFLICT DO NOTHING;
