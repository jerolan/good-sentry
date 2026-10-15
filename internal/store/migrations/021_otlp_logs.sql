CREATE TABLE otlp_log_batches (
 id bigserial PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 received_at timestamptz NOT NULL,
 payload bytea NOT NULL,
 UNIQUE(project_id,id)
);

ALTER TABLE logs
 ADD COLUMN otlp_batch_id bigint,
 ADD COLUMN otlp_record_index integer,
 ADD CONSTRAINT logs_otlp_reference CHECK (
  (otlp_batch_id IS NULL AND otlp_record_index IS NULL) OR
  (otlp_batch_id IS NOT NULL AND otlp_record_index IS NOT NULL AND otlp_record_index>=0)
 ),
 ADD CONSTRAINT logs_otlp_project_batch FOREIGN KEY(project_id,otlp_batch_id)
 REFERENCES otlp_log_batches(project_id,id) ON DELETE CASCADE;

CREATE UNIQUE INDEX logs_otlp_batch_record ON logs(otlp_batch_id,otlp_record_index)
 WHERE otlp_batch_id IS NOT NULL;
