CREATE TABLE debug_files (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 512),
 symbol_type text,
 debug_id text,
 cpu_name text NOT NULL DEFAULT 'unknown',
 object_name text NOT NULL,
 size bigint NOT NULL CHECK (size >= 0),
 sha1 text NOT NULL CHECK (sha1 ~ '^[0-9a-f]{40}$'),
 headers jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(headers)='object'),
 data jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data)='object'),
 content bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(project_id,sha1)
);
CREATE INDEX debug_files_project_created ON debug_files(project_id,created_at DESC,id);
