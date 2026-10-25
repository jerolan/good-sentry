ALTER TABLE issue_resolutions
 ADD COLUMN commit_repository text,
 ADD COLUMN commit_id text;

ALTER TABLE issue_resolutions
 DROP CONSTRAINT issue_resolutions_mode_check,
 DROP CONSTRAINT issue_resolutions_check2,
 ADD CONSTRAINT issue_resolutions_mode_check CHECK (mode IN ('release','next_release','commit')),
 ADD CONSTRAINT issue_resolutions_commit_pair CHECK (
    (commit_repository IS NULL) = (commit_id IS NULL)
    AND (commit_repository IS NULL OR
         (char_length(commit_repository) BETWEEN 1 AND 200 AND commit_repository !~ U&'[\0001-\001F\007F-\009F]'))
    AND (commit_id IS NULL OR
         (char_length(commit_id) BETWEEN 1 AND 200 AND commit_id !~ U&'[\0001-\001F\007F-\009F]'))
 ),
 ADD CONSTRAINT issue_resolutions_state_check CHECK (
    (mode='release' AND baseline_id IS NULL AND release_id IS NOT NULL AND next_check_at IS NULL AND commit_id IS NULL)
    OR (mode='next_release' AND baseline_id IS NOT NULL AND commit_id IS NULL
        AND ((release_id IS NULL AND next_check_at IS NOT NULL)
          OR (release_id IS NOT NULL AND next_check_at IS NULL)))
    OR (mode='commit' AND baseline_id IS NULL AND release_id IS NOT NULL AND next_check_at IS NULL AND commit_id IS NOT NULL)
 );

CREATE INDEX release_commits_identity ON release_commits(repository,commit_id,release_id);
