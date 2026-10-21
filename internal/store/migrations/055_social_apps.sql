CREATE TABLE social_apps (
 id bigserial PRIMARY KEY,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 provider text NOT NULL CHECK (provider IN ('openid_connect','google')),
 brand text NOT NULL,
 provider_id text NOT NULL UNIQUE,
 name text NOT NULL,
 client_id text NOT NULL,
 client_secret bytea NOT NULL,
 server_url text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX social_apps_organization ON social_apps(organization_id,id);
