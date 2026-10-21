ALTER TABLE users ADD COLUMN options jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE users ADD COLUMN notification_subscribe_by_default boolean NOT NULL DEFAULT true;
