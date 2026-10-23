ALTER TABLE span_archive_objects
 ADD COLUMN index_complete boolean NOT NULL DEFAULT false,
 ADD COLUMN state text NOT NULL DEFAULT 'ready' CHECK (state IN ('ready','deleting'));

CREATE TABLE span_archive_rows (
 archive_object_id bigint NOT NULL REFERENCES span_archive_objects ON DELETE CASCADE,
 span_row_id bigint NOT NULL,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 trace_id text NOT NULL,
 span_id text NOT NULL,
 start_timestamp timestamptz NOT NULL,
 end_timestamp timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 is_segment boolean NOT NULL,
 environment text NOT NULL,
 release text NOT NULL,
 PRIMARY KEY(project_id,trace_id,span_id),
 UNIQUE(span_row_id)
);

CREATE INDEX span_archive_rows_object ON span_archive_rows(archive_object_id);
CREATE INDEX span_archive_rows_project_object ON span_archive_rows(project_id,archive_object_id);
CREATE INDEX span_archive_rows_project_trace ON span_archive_rows(project_id,trace_id,archive_object_id);
CREATE INDEX span_archive_rows_project_start ON span_archive_rows(project_id,start_timestamp) INCLUDE (archive_object_id);
CREATE INDEX span_archive_rows_project_end ON span_archive_rows(project_id,end_timestamp) INCLUDE (archive_object_id);
CREATE INDEX span_archive_rows_project_environment ON span_archive_rows(project_id,environment) INCLUDE (archive_object_id);
CREATE INDEX span_archive_rows_project_release_environment ON span_archive_rows(project_id,release,environment) INCLUDE (archive_object_id);
