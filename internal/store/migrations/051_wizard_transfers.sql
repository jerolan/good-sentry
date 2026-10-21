-- Sentry wizard handoff: encrypted one-day source-map credential exposed only
-- through a short-lived, unguessable transfer hash.
CREATE TABLE wizard_transfers (
 token_hash text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 user_id bigint NOT NULL,
 organization_id bigint NOT NULL,
 api_token_id bigint NOT NULL UNIQUE REFERENCES api_tokens ON DELETE CASCADE,
 token_ciphertext bytea NOT NULL CHECK (octet_length(token_ciphertext) BETWEEN 40 AND 256),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 CHECK (expires_at > created_at),
 FOREIGN KEY (organization_id,user_id) REFERENCES memberships(organization_id,user_id) ON DELETE CASCADE
);
CREATE INDEX wizard_transfers_expiry ON wizard_transfers(expires_at);
