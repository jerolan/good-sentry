CREATE TABLE user_identities (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 provider text NOT NULL,
 subject text NOT NULL,
 email text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 last_login_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(provider,subject),
 UNIQUE(user_id,provider)
);
CREATE INDEX user_identities_email ON user_identities(lower(email));
