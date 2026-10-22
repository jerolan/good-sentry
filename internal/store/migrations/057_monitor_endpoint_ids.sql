ALTER TABLE monitors ADD COLUMN endpoint_id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX monitors_endpoint_id ON monitors(endpoint_id);
