CREATE TABLE releases (
 id bigserial PRIMARY KEY,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 version text NOT NULL CHECK (char_length(version) BETWEEN 1 AND 255 AND version NOT IN ('.','..') AND version !~ U&'[\0001-\001F\007F-\009F]'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 date_started timestamptz, date_released timestamptz,
 ref text, url text,
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','archived')),
 UNIQUE(organization_id,version)
);
CREATE INDEX releases_organization_date ON releases(organization_id,created_at DESC,id DESC);
CREATE TABLE release_projects (
 release_id bigint NOT NULL REFERENCES releases ON DELETE CASCADE,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 PRIMARY KEY(release_id,project_id)
);
CREATE INDEX release_projects_project ON release_projects(project_id,release_id);
CREATE TABLE release_repositories (
 id bigserial PRIMARY KEY,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), url text,
 UNIQUE(organization_id,name)
);
CREATE TABLE release_commits (
 release_id bigint NOT NULL REFERENCES releases ON DELETE CASCADE,
 ordinal integer NOT NULL CHECK (ordinal>=0),
 repository_id bigint REFERENCES release_repositories ON DELETE SET NULL,
 repository text NOT NULL DEFAULT '', commit_id text NOT NULL,
 message text NOT NULL DEFAULT '', author_name text NOT NULL DEFAULT '', author_email text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL, received_at timestamptz NOT NULL, patch_set jsonb NOT NULL DEFAULT '[]', data json NOT NULL,
 PRIMARY KEY(release_id,ordinal), UNIQUE(release_id,repository,commit_id)
);
CREATE INDEX release_commits_repository ON release_commits(repository_id,release_id) WHERE repository_id IS NOT NULL;
CREATE TABLE release_deploys (
 id bigserial PRIMARY KEY,
 release_id bigint NOT NULL REFERENCES releases ON DELETE CASCADE,
 environment text NOT NULL, name text, url text,
 created_at timestamptz NOT NULL, date_started timestamptz, date_finished timestamptz NOT NULL,
 CHECK (date_started IS NULL OR date_finished>=date_started),
 UNIQUE(id,release_id)
);
CREATE INDEX release_deploys_release_date ON release_deploys(release_id,created_at DESC,id DESC);
CREATE TABLE release_deploy_projects (
 deploy_id bigint NOT NULL,
 release_id bigint NOT NULL,
 project_id bigint NOT NULL,
 PRIMARY KEY(deploy_id,project_id),
 FOREIGN KEY(deploy_id,release_id) REFERENCES release_deploys(id,release_id) ON DELETE CASCADE,
 FOREIGN KEY(release_id,project_id) REFERENCES release_projects(release_id,project_id) ON DELETE CASCADE
);
CREATE INDEX release_deploy_projects_release_project ON release_deploy_projects(release_id,project_id,deploy_id);

ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases']::text[]
);

-- Hold the source tables stable until all identities and associations are copied.
LOCK TABLE events,spans,source_maps IN SHARE MODE;
WITH observed AS (
 SELECT project_id,release,created_at FROM events
 UNION ALL SELECT project_id,release,created_at FROM spans
 UNION ALL SELECT project_id,release,created_at FROM source_maps
)
INSERT INTO releases(organization_id,version,created_at)
SELECT p.organization_id,o.release,min(o.created_at)
FROM observed o JOIN projects p ON p.id=o.project_id
WHERE char_length(o.release) BETWEEN 1 AND 255 AND o.release NOT IN ('.','..')
 AND o.release !~ U&'[\0001-\001F\007F-\009F]'
GROUP BY p.organization_id,o.release;
WITH observed AS (
 SELECT project_id,release FROM events UNION SELECT project_id,release FROM spans UNION SELECT project_id,release FROM source_maps
)
INSERT INTO release_projects(release_id,project_id)
SELECT DISTINCT r.id,p.id FROM observed o JOIN projects p ON p.id=o.project_id
JOIN releases r ON r.organization_id=p.organization_id AND r.version=o.release;
