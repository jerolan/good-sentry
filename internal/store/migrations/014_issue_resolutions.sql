-- Release references are immutable snapshots, deliberately without release FKs.
CREATE TABLE issue_resolutions (
 issue_id bigint PRIMARY KEY REFERENCES issues ON DELETE CASCADE,
 mode text NOT NULL CHECK (mode IN ('release','next_release')),
 comparison text NOT NULL CHECK (comparison IN ('semantic','created')),
 set_at timestamptz NOT NULL,
 baseline_id bigint CHECK (baseline_id > 0),
 baseline_version text,
 baseline_created_at timestamptz,
 baseline_released_at timestamptz,
 release_id bigint CHECK (release_id > 0),
 release_version text,
 release_created_at timestamptz,
 release_released_at timestamptz,
 next_check_at timestamptz,
 CHECK ((baseline_id IS NULL) = (baseline_version IS NULL)
    AND (baseline_id IS NULL) = (baseline_created_at IS NULL)
    AND (baseline_id IS NOT NULL OR baseline_released_at IS NULL)),
 CHECK ((release_id IS NULL) = (release_version IS NULL)
    AND (release_id IS NULL) = (release_created_at IS NULL)
    AND (release_id IS NOT NULL OR release_released_at IS NULL)),
 CHECK ((mode='release' AND baseline_id IS NULL AND release_id IS NOT NULL AND next_check_at IS NULL)
    OR (mode='next_release' AND baseline_id IS NOT NULL
        AND ((release_id IS NULL AND next_check_at IS NOT NULL)
          OR (release_id IS NOT NULL AND next_check_at IS NULL))))
);
CREATE INDEX issue_resolutions_pending ON issue_resolutions(next_check_at,issue_id)
 WHERE mode='next_release' AND release_id IS NULL;
