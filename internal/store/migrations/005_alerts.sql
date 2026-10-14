ALTER TABLE events ALTER COLUMN created_at SET DEFAULT clock_timestamp();
CREATE INDEX events_alert_receipt ON events(project_id,created_at DESC) WHERE kind<>'transaction';
CREATE INDEX events_alert_environment_receipt ON events(project_id,environment,created_at DESC) WHERE kind<>'transaction';
ALTER TABLE monitors ADD COLUMN environment text NOT NULL DEFAULT '' CHECK (char_length(environment)<=128);

CREATE TABLE alert_rules (
 id bigserial PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
 kind text NOT NULL CHECK (kind IN ('error_frequency','uptime')), enabled boolean NOT NULL,
 quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 1000000),
 window_minutes integer NOT NULL CHECK (window_minutes BETWEEN 1 AND 10080),
 cooldown_seconds integer NOT NULL CHECK (cooldown_seconds BETWEEN 0 AND 604800),
 environments text[] NOT NULL DEFAULT '{}', recipients jsonb NOT NULL CHECK (jsonb_typeof(recipients)='array'),
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, last_triggered timestamptz
);
CREATE INDEX alert_rules_project ON alert_rules(project_id,id);
CREATE TABLE alert_deliveries (
 id text PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 rule_id bigint REFERENCES alert_rules ON DELETE SET NULL, rule_name text NOT NULL,
 trigger_key text NOT NULL, recipient_type text NOT NULL CHECK (recipient_type IN ('email','webhook')),
 destination text NOT NULL, destination_hash text NOT NULL, payload jsonb NOT NULL,
 status text NOT NULL CHECK (status IN ('pending','processing','sent','failed','canceled')),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 20),
 next_attempt timestamptz NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 sent_at timestamptz, last_reason text NOT NULL DEFAULT '' CHECK (char_length(last_reason)<=64),
 last_status_code integer CHECK (last_status_code BETWEEN 100 AND 599),
 lease_token text NOT NULL DEFAULT '', lease_expires_at timestamptz,
 CHECK ((status='processing' AND lease_token<>'' AND lease_expires_at IS NOT NULL)
     OR (status<>'processing' AND lease_token='' AND lease_expires_at IS NULL)),
 UNIQUE(rule_id,trigger_key,recipient_type,destination_hash)
);
CREATE INDEX alert_deliveries_due ON alert_deliveries(next_attempt,created_at,id) WHERE status='pending';
CREATE INDEX alert_deliveries_expired ON alert_deliveries(lease_expires_at,id) WHERE status='processing';
CREATE INDEX alert_deliveries_project_history ON alert_deliveries(project_id,created_at DESC,id DESC);
CREATE INDEX alert_deliveries_rule ON alert_deliveries(rule_id);
CREATE TABLE alert_attempts (
 delivery_id text NOT NULL REFERENCES alert_deliveries ON DELETE CASCADE,
 number integer NOT NULL CHECK (number BETWEEN 1 AND 20),
 started_at timestamptz NOT NULL, completed_at timestamptz,
 status text NOT NULL CHECK (status IN ('processing','delivered','retry','permanent','lease_expired','canceled')),
 reason text NOT NULL DEFAULT '' CHECK (char_length(reason)<=64),
 status_code integer CHECK (status_code BETWEEN 100 AND 599), elapsed_ms bigint CHECK (elapsed_ms>=0),
 PRIMARY KEY(delivery_id,number)
);
