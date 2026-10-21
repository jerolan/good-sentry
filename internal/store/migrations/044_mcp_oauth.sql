CREATE TABLE oauth_clients (
 client_id text PRIMARY KEY CHECK (client_id ~ '^gso_[0-9a-f]{32,64}$'),
 client_name text NOT NULL CHECK (char_length(client_name) BETWEEN 1 AND 100),
 redirect_uris text[] NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 5),
 token_endpoint_auth_method text NOT NULL CHECK (token_endpoint_auth_method='none'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE oauth_authorization_codes (
 code_hash text PRIMARY KEY CHECK (code_hash ~ '^[0-9a-f]{64}$'),
 client_id text NOT NULL REFERENCES oauth_clients ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 project_ids bigint[] NOT NULL,
 all_projects boolean NOT NULL,
 scopes text[] NOT NULL CHECK (cardinality(scopes)>0),
 redirect_uri text NOT NULL,
 code_challenge text NOT NULL CHECK (char_length(code_challenge) BETWEEN 43 AND 128),
 auth_generation bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE INDEX oauth_codes_expiry ON oauth_authorization_codes(expires_at);

CREATE TABLE oauth_refresh_tokens (
 secret_hash text PRIMARY KEY CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
 client_id text NOT NULL REFERENCES oauth_clients ON DELETE CASCADE,
 access_token_id bigint NOT NULL REFERENCES api_tokens ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 project_ids bigint[] NOT NULL,
 all_projects boolean NOT NULL,
 scopes text[] NOT NULL CHECK (cardinality(scopes)>0),
 auth_generation bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 rotated_to text
);
CREATE INDEX oauth_refresh_access ON oauth_refresh_tokens(access_token_id);
CREATE INDEX oauth_refresh_expiry ON oauth_refresh_tokens(expires_at);
