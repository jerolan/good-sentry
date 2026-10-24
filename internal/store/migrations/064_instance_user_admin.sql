ALTER TABLE users ADD COLUMN is_instance_admin boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN is_active boolean NOT NULL DEFAULT true;
