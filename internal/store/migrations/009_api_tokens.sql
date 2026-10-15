CREATE TABLE api_tokens (
 id bigserial PRIMARY KEY,
 organization_id bigint NOT NULL,
 user_id bigint NOT NULL,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
 secret_hash text NOT NULL UNIQUE CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
 prefix text NOT NULL CHECK (prefix ~ '^gs_[0-9a-f]{8}$'),
 scopes text[] NOT NULL CHECK (cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read']::text[]),
 all_projects boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 FOREIGN KEY (organization_id,user_id) REFERENCES memberships ON DELETE CASCADE
);
CREATE INDEX api_tokens_organization_date ON api_tokens(organization_id,created_at DESC,id DESC);
CREATE TABLE api_token_projects (
 token_id bigint NOT NULL REFERENCES api_tokens ON DELETE CASCADE,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 PRIMARY KEY (token_id,project_id)
);
