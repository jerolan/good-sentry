CREATE TABLE monitors (
 id bigserial PRIMARY KEY,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 project_id bigint REFERENCES projects ON DELETE SET NULL,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
 kind text NOT NULL CHECK (kind IN ('HEAD','GET','POST','TCP','heartbeat')),
 url text NOT NULL DEFAULT '',
 interval_seconds integer NOT NULL CHECK (interval_seconds BETWEEN 1 AND 86400),
 timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 60),
 expected_status integer NOT NULL CHECK (expected_status BETWEEN 100 AND 599),
 expected_body text NOT NULL DEFAULT '',
 confirmation_threshold integer NOT NULL CHECK (confirmation_threshold BETWEEN 1 AND 100),
 enabled boolean NOT NULL,
 status text NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown','up','down')),
 consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures BETWEEN 0 AND 100),
 last_check timestamptz, last_change timestamptz,
 next_check timestamptz NOT NULL, created_at timestamptz NOT NULL,
 heartbeat_key text NOT NULL DEFAULT '',
 lease_token text NOT NULL DEFAULT '', lease_started_at timestamptz, lease_expires_at timestamptz,
 CHECK ((kind='heartbeat' AND heartbeat_key<>'') OR (kind<>'heartbeat' AND heartbeat_key='')),
 CHECK ((lease_token='' AND lease_started_at IS NULL AND lease_expires_at IS NULL)
     OR (lease_token<>'' AND lease_started_at IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX monitors_organization ON monitors(organization_id,id);
CREATE INDEX monitors_due ON monitors(next_check,id) WHERE enabled;
CREATE UNIQUE INDEX monitors_heartbeat_key ON monitors(heartbeat_key) WHERE heartbeat_key<>'';
CREATE TABLE monitor_checks (
 id bigserial PRIMARY KEY,
 monitor_id bigint NOT NULL REFERENCES monitors ON DELETE CASCADE,
 started_at timestamptz NOT NULL, completed_at timestamptz NOT NULL,
 up boolean NOT NULL,
 status text NOT NULL CHECK (status IN ('unknown','up','down')),
 previous_status text NOT NULL CHECK (previous_status IN ('unknown','up','down')),
 is_change boolean NOT NULL,
 response_time_ms bigint CHECK (response_time_ms>=0),
 status_code integer CHECK (status_code BETWEEN 100 AND 599),
 reason text NOT NULL CHECK (char_length(reason)<=64),
 CHECK (completed_at>=started_at)
);
CREATE INDEX monitor_checks_history ON monitor_checks(monitor_id,completed_at DESC,id DESC);
CREATE INDEX monitor_checks_changes ON monitor_checks(monitor_id,completed_at DESC,id DESC) WHERE is_change;
