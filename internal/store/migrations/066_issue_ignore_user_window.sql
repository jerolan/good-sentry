ALTER TABLE issue_ignores ADD COLUMN user_window_minutes integer CHECK (user_window_minutes BETWEEN 1 AND 10080);
ALTER TABLE issue_ignores ADD CONSTRAINT issue_ignores_user_window_requires_user_count
 CHECK (user_window_minutes IS NULL OR user_count IS NOT NULL);

ALTER TABLE issue_ignore_users ADD COLUMN received_at timestamptz;
UPDATE issue_ignore_users SET received_at=clock_timestamp() WHERE NOT baseline;
ALTER TABLE issue_ignore_users ADD CONSTRAINT issue_ignore_users_additional_receipt_required
 CHECK (baseline OR received_at IS NOT NULL);

DROP INDEX issue_ignore_users_additional;
CREATE INDEX issue_ignore_users_additional ON issue_ignore_users(issue_id,received_at) WHERE NOT baseline;
