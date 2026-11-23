CREATE TABLE projects (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    ingest_key text NOT NULL UNIQUE
);

CREATE TABLE issues (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id bigint NOT NULL REFERENCES projects (id),
    fingerprint bytea NOT NULL,
    title text NOT NULL,
    UNIQUE (project_id, fingerprint),
    UNIQUE (project_id, id)
);

CREATE TABLE events (
    project_id bigint NOT NULL,
    issue_id bigint NOT NULL,
    id text NOT NULL,
    message text NOT NULL,
    exception_type text NOT NULL,
    frames jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, id),
    FOREIGN KEY (project_id, issue_id) REFERENCES issues (project_id, id)
);

CREATE INDEX events_issue_received ON events (project_id, issue_id, received_at DESC, id);
