-- Stable numeric identities for the compatible performance API. Metrics remain
-- projected from retained telemetry; this table owns only group identity.
CREATE TABLE compatible_transaction_group_ids (
 id bigserial PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 transaction text NOT NULL,
 op text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,transaction,op)
);
CREATE INDEX compatible_transaction_group_project ON compatible_transaction_group_ids(project_id,id);
