ALTER TABLE issue_ignores ADD COLUMN count_window_minutes integer CHECK (count_window_minutes BETWEEN 1 AND 10080);
ALTER TABLE issue_ignores DROP CONSTRAINT issue_ignores_condition_present;
ALTER TABLE issue_ignores ADD CONSTRAINT issue_ignores_condition_present
 CHECK (minutes IS NOT NULL OR count IS NOT NULL OR user_count IS NOT NULL);
ALTER TABLE issue_ignores ADD CONSTRAINT issue_ignores_window_requires_count
 CHECK (count_window_minutes IS NULL OR count IS NOT NULL);

CREATE TABLE issue_ignore_count_receipts (
 issue_id bigint NOT NULL REFERENCES issue_ignores(issue_id) ON DELETE CASCADE,
 received_at timestamptz NOT NULL
);
CREATE INDEX issue_ignore_count_receipts_window ON issue_ignore_count_receipts(issue_id,received_at);
