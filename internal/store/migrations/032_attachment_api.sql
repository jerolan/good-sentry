-- Keep native content identities intact; compatible API IDs are stable opaque
-- decimal strings. Historical checksums are calculated when selected, without
-- rewriting stored bytes or requiring a PostgreSQL extension.
ALTER TABLE attachments ADD COLUMN api_id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE attachments ADD CONSTRAINT attachments_api_id_unique UNIQUE (api_id);
ALTER TABLE attachments ADD COLUMN attachment_type text NOT NULL DEFAULT 'event.attachment';
ALTER TABLE attachments ADD COLUMN checksum_sha1 text NOT NULL DEFAULT '';
CREATE INDEX attachments_event_api ON attachments(project_id,event_id,api_id);
