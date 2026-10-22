CREATE TABLE user_api_tokens (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 label text NOT NULL DEFAULT '',
 scopes_mask bigint NOT NULL CHECK (scopes_mask >= 0 AND scopes_mask <= 65535),
 secret_hash text NOT NULL UNIQUE CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
 secret bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_api_tokens_owner ON user_api_tokens(user_id,created_at DESC,id DESC);

CREATE TABLE compat_recovery_codes (
 user_id bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,
 code_hashes text[] NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
