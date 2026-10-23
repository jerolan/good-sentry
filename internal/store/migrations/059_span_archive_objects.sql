CREATE TABLE span_archive_objects (
 id bigserial PRIMARY KEY,
 project_id bigint REFERENCES projects ON DELETE SET NULL,
 backend text NOT NULL CHECK (backend IN ('local','s3')),
 object_key text NOT NULL CHECK (object_key<>''),
 row_count integer NOT NULL CHECK (row_count BETWEEN 1 AND 250),
 object_size_bytes bigint NOT NULL CHECK (object_size_bytes BETWEEN 1 AND 67108864),
 min_created_at timestamptz NOT NULL,
 max_created_at timestamptz NOT NULL CHECK (max_created_at>=min_created_at),
 min_start_timestamp timestamptz NOT NULL,
 max_start_timestamp timestamptz NOT NULL CHECK (max_start_timestamp>=min_start_timestamp),
 min_end_timestamp timestamptz NOT NULL,
 max_end_timestamp timestamptz NOT NULL CHECK (max_end_timestamp>=min_end_timestamp),
 archived_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(backend,object_key)
);
CREATE INDEX span_archive_objects_project_end
 ON span_archive_objects(project_id,min_end_timestamp,max_end_timestamp)
 WHERE project_id IS NOT NULL;
