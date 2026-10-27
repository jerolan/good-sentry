-- S3-compatible log archive catalog. log_archive is a materialized read cache;
-- the immutable object plus this catalog are restored after a restart.
CREATE TABLE log_archive_objects (
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 id bigint NOT NULL,
 object_key text NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,id),
 UNIQUE(object_key)
);
CREATE INDEX log_archive_objects_created ON log_archive_objects(created_at,project_id,id);
