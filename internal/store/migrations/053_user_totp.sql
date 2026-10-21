ALTER TABLE users ADD COLUMN totp_secret bytea;
ALTER TABLE users ADD COLUMN totp_confirmed_at timestamptz;
ALTER TABLE users ADD COLUMN totp_last_step bigint;
CREATE TABLE user_recovery_codes (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 code_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 used_at timestamptz,
 UNIQUE(user_id,code_hash)
);
CREATE INDEX user_recovery_codes_user ON user_recovery_codes(user_id,created_at DESC,id DESC);
CREATE TABLE login_challenges (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 auth_generation bigint NOT NULL CHECK (auth_generation > 0),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE INDEX login_challenges_user ON login_challenges(user_id,created_at DESC);
CREATE INDEX login_challenges_expiry ON login_challenges(expires_at);
