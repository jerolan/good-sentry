-- S3-compatible transaction archive catalog. transaction_archive is the local
-- read cache; immutable objects plus this catalog restore after restart.
CREATE TABLE transaction_archive_objects (
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 id bigint NOT NULL,
 object_key text NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,id),
 UNIQUE(object_key)
);
CREATE INDEX transaction_archive_objects_created ON transaction_archive_objects(created_at,project_id,id);
