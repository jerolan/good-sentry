ALTER TABLE source_maps ALTER COLUMN created_at SET DEFAULT clock_timestamp();
ALTER TABLE attachments ALTER COLUMN created_at SET DEFAULT clock_timestamp();
ALTER TABLE envelope_items ALTER COLUMN created_at SET DEFAULT clock_timestamp();
CREATE INDEX source_maps_receipt_retention ON source_maps(created_at,id);
CREATE INDEX attachments_receipt_retention ON attachments(created_at,id);
CREATE INDEX envelope_items_receipt_retention ON envelope_items(created_at,id);
