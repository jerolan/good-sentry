CREATE TABLE discarded_issue_hashes (
 project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 fingerprint text NOT NULL,
 discarded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,fingerprint)
);
