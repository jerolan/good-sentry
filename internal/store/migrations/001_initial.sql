-- Independently authored GoodSentry schema. Applied once with a verified checksum.
CREATE TABLE users (
 id bigserial PRIMARY KEY, email text NOT NULL UNIQUE, name text NOT NULL,
 password_hash bytea NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE organizations (
 id bigserial PRIMARY KEY, slug text NOT NULL UNIQUE, name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE memberships (
 organization_id bigint REFERENCES organizations ON DELETE CASCADE,
 user_id bigint REFERENCES users ON DELETE CASCADE,
 role text NOT NULL CHECK (role IN ('owner','admin','member')),
 PRIMARY KEY (organization_id,user_id)
);
CREATE TABLE teams (
 id bigserial PRIMARY KEY, organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 slug text NOT NULL, name text NOT NULL, UNIQUE(organization_id,slug)
);
CREATE TABLE team_memberships (
 team_id bigint REFERENCES teams ON DELETE CASCADE, user_id bigint REFERENCES users ON DELETE CASCADE,
 PRIMARY KEY(team_id,user_id)
);
CREATE TABLE projects (
 id bigserial PRIMARY KEY, organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 team_id bigint REFERENCES teams ON DELETE SET NULL,
 slug text NOT NULL, name text NOT NULL, platform text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(organization_id,slug)
);
CREATE TABLE project_keys (
 public_key text PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY, user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 csrf_token text NOT NULL, expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE issues (
 id bigserial PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 fingerprint text NOT NULL, title text NOT NULL, level text NOT NULL, platform text NOT NULL,
 status text NOT NULL DEFAULT 'unresolved' CHECK(status IN ('unresolved','resolved','ignored')),
 count bigint NOT NULL DEFAULT 0, first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL,
 environment text NOT NULL DEFAULT '', release text NOT NULL DEFAULT '',
 resolved_at timestamptz, assignee_id bigint REFERENCES users ON DELETE SET NULL,
 merged_into bigint REFERENCES issues ON DELETE SET NULL, UNIQUE(project_id,fingerprint)
);
CREATE INDEX issues_project_last_seen ON issues(project_id,last_seen DESC);
CREATE TABLE events (
 id bigserial PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 event_id text NOT NULL, issue_id bigint REFERENCES issues ON DELETE CASCADE,
 original_issue_id bigint REFERENCES issues ON DELETE SET NULL,
 kind text NOT NULL, title text NOT NULL, level text NOT NULL, timestamp timestamptz NOT NULL,
 environment text NOT NULL DEFAULT '', release text NOT NULL DEFAULT '',
 trace_id text NOT NULL DEFAULT '', transaction_name text NOT NULL DEFAULT '',
 duration double precision NOT NULL DEFAULT 0, data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(project_id,event_id)
);
CREATE INDEX events_issue_date ON events(issue_id,timestamp DESC);
CREATE INDEX events_project_date ON events(project_id,timestamp DESC);
CREATE INDEX events_trace ON events(project_id,trace_id) WHERE trace_id<>'';
CREATE TABLE comments (
 id bigserial PRIMARY KEY, issue_id bigint NOT NULL REFERENCES issues ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 text text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE issue_activity (
 id bigserial PRIMARY KEY, issue_id bigint NOT NULL REFERENCES issues ON DELETE CASCADE,
 user_id bigint REFERENCES users ON DELETE SET NULL, action text NOT NULL,
 data jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE attachments (
 id text PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 event_id text NOT NULL, filename text NOT NULL, content_type text NOT NULL,
 data bytea NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(project_id,event_id) REFERENCES events(project_id,event_id) ON DELETE CASCADE
);
CREATE TABLE envelope_items (
 id text PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 event_id text NOT NULL DEFAULT '', item_type text NOT NULL, header jsonb NOT NULL,
 data bytea NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE logs (
 id bigserial PRIMARY KEY, project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 timestamp timestamptz NOT NULL, trace_id text NOT NULL DEFAULT '', span_id text NOT NULL DEFAULT '',
 level text NOT NULL, body text NOT NULL, service text NOT NULL DEFAULT '', environment text NOT NULL DEFAULT '',
 attributes jsonb NOT NULL DEFAULT '{}', data jsonb NOT NULL, item_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(project_id,item_hash)
);
CREATE INDEX logs_project_date ON logs(project_id,timestamp DESC);
