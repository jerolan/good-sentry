CREATE UNIQUE INDEX IF NOT EXISTS users_lower_email_unique ON users(lower(email));

CREATE TABLE user_emails (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 email text NOT NULL CHECK (char_length(email) BETWEEN 1 AND 254),
 is_verified boolean NOT NULL DEFAULT false,
 verification_hash text,
 verification_ciphertext bytea,
 verification_expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_emails_lower_email ON user_emails(lower(email));
CREATE INDEX user_emails_user ON user_emails(user_id,is_verified,email);
