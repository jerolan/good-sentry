-- Conditional mute is additive; existing ignored issues remain indefinite.
CREATE TABLE issue_ignores (
 issue_id bigint PRIMARY KEY REFERENCES issues ON DELETE CASCADE,
 minutes integer CHECK (minutes > 0),
 count integer CHECK (count > 0),
 until timestamptz,
 baseline bigint NOT NULL CHECK (baseline >= 0),
 CHECK (minutes IS NOT NULL OR count IS NOT NULL),
 CHECK ((minutes IS NULL) = (until IS NULL))
);
CREATE INDEX issue_ignores_expiry ON issue_ignores(until,issue_id) WHERE until IS NOT NULL;
