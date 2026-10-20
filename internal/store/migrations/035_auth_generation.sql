-- Add a durable credential version without rewriting existing accounts.
ALTER TABLE users ADD COLUMN auth_generation bigint NOT NULL DEFAULT 1
 CHECK (auth_generation > 0);
