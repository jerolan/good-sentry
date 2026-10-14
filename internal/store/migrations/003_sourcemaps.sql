CREATE TABLE source_maps (
 id bigserial PRIMARY KEY,
 project_id bigint NOT NULL REFERENCES projects ON DELETE CASCADE,
 name text NOT NULL, release text NOT NULL DEFAULT '', dist text NOT NULL DEFAULT '',
 debug_id text NOT NULL DEFAULT '', checksum text NOT NULL,
 size bigint NOT NULL CHECK (size >= 0), data bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (size=octet_length(data)), UNIQUE(project_id,name,release,dist,debug_id)
);
CREATE INDEX source_maps_project_release_dist ON source_maps(project_id,release,dist);
CREATE INDEX source_maps_project_debug_id ON source_maps(project_id,debug_id) WHERE debug_id<>'';
