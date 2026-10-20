-- Durable, encrypted password-reset outbox and single-use authorization.
CREATE TABLE account_password_resets (
 id text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 recipient text NOT NULL,
 token_hash text NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 token_ciphertext bytea NOT NULL CHECK (octet_length(token_ciphertext) = 92),
 auth_generation bigint NOT NULL CHECK (auth_generation > 0),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 status text NOT NULL CHECK (status IN ('pending','processing','sent','failed','consumed','canceled')),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 20),
 next_attempt timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 sent_at timestamptz,
 last_reason text NOT NULL DEFAULT '' CHECK (char_length(last_reason) <= 64),
 lease_token text NOT NULL DEFAULT '',
 lease_expires_at timestamptz,
 CHECK ((status='processing' AND lease_token<>'' AND lease_expires_at IS NOT NULL)
     OR (status<>'processing' AND lease_token='' AND lease_expires_at IS NULL)),
 CHECK ((consumed_at IS NULL) OR status='consumed')
);
CREATE INDEX account_password_resets_user ON account_password_resets(user_id,created_at DESC);
CREATE INDEX account_password_resets_due ON account_password_resets(next_attempt,created_at,id)
 WHERE status='pending';
CREATE INDEX account_password_resets_lease ON account_password_resets(lease_expires_at,id)
 WHERE status='processing';
