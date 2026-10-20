-- Invitation outbox and encrypted single-use organization access tokens.
CREATE TABLE organization_invitations (
 id text PRIMARY KEY,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 email text NOT NULL,
 role text NOT NULL CHECK (role IN ('owner','admin','member')),
 inviter_user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 token_hash text NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 token_ciphertext bytea NOT NULL CHECK (octet_length(token_ciphertext) = 92),
 expires_at timestamptz NOT NULL,
 accepted_at timestamptz,
 status text NOT NULL CHECK (status IN ('pending','processing','sent','failed','accepted','canceled')),
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
 CHECK ((accepted_at IS NULL) OR status='accepted')
);
CREATE UNIQUE INDEX organization_invitations_active_email
 ON organization_invitations(organization_id,lower(email))
 WHERE status IN ('pending','processing','sent');
CREATE INDEX organization_invitations_due ON organization_invitations(next_attempt,created_at,id)
 WHERE status='pending';
CREATE INDEX organization_invitations_lease ON organization_invitations(lease_expires_at,id)
 WHERE status='processing';
