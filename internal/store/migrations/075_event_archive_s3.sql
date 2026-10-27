-- S3-compatible error archive catalog. The local event_archive row is a
-- materialized read cache; the immutable object plus this catalog are the
-- durable source and are restored after a restart.
CREATE TABLE event_archive_objects (
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 event_id text NOT NULL,
 object_key text NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,event_id),
 UNIQUE(object_key)
);
CREATE INDEX event_archive_objects_created ON event_archive_objects(created_at,project_id,event_id);
