CREATE TABLE artifact_chunks (
    organization_id bigint NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    checksum text NOT NULL CHECK (checksum ~ '^[0-9a-f]{40}$'),
    data bytea NOT NULL,
    size bigint NOT NULL CHECK (size >= 0 AND size = octet_length(data)),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, checksum)
);
CREATE INDEX artifact_chunks_expiry_idx ON artifact_chunks(expires_at);
ALTER TABLE source_maps ADD COLUMN code_files text[] NOT NULL DEFAULT '{}';
