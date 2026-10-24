ALTER TABLE issue_ignores ADD COLUMN user_count integer CHECK (user_count > 0);
ALTER TABLE issue_ignores DROP CONSTRAINT issue_ignores_check;
ALTER TABLE issue_ignores ADD CONSTRAINT issue_ignores_condition_present
 CHECK (minutes IS NOT NULL OR count IS NOT NULL OR user_count IS NOT NULL);

CREATE TABLE issue_ignore_users (
 issue_id bigint NOT NULL REFERENCES issues ON DELETE CASCADE,
 identity_digest bytea NOT NULL CHECK (octet_length(identity_digest)=32),
 baseline boolean NOT NULL,
 PRIMARY KEY(issue_id,identity_digest)
);
CREATE INDEX issue_ignore_users_additional ON issue_ignore_users(issue_id) WHERE NOT baseline;
