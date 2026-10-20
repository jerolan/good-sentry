ALTER TABLE teams ADD COLUMN created_at timestamptz;
UPDATE teams t SET created_at=o.created_at FROM organizations o WHERE o.id=t.organization_id;
ALTER TABLE teams ALTER COLUMN created_at SET NOT NULL;
ALTER TABLE teams ALTER COLUMN created_at SET DEFAULT clock_timestamp();

ALTER TABLE project_keys ADD COLUMN api_id uuid;
UPDATE project_keys SET api_id=(substr(md5(public_key),1,8)||'-'||substr(md5(public_key),9,4)||'-'||
 substr(md5(public_key),13,4)||'-'||substr(md5(public_key),17,4)||'-'||substr(md5(public_key),21,12))::uuid;
ALTER TABLE project_keys ALTER COLUMN api_id SET NOT NULL;
ALTER TABLE project_keys ALTER COLUMN api_id SET DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX project_keys_api_id ON project_keys(api_id);

ALTER TABLE project_keys ADD COLUMN label text NOT NULL DEFAULT '' CHECK (char_length(label)<=200);
ALTER TABLE project_keys ADD COLUMN updated_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE project_keys ADD COLUMN rate_limit_window integer;
ALTER TABLE project_keys ADD COLUMN rate_limit_count integer;
ALTER TABLE project_keys ADD CONSTRAINT project_keys_rate_limit_pair CHECK (
 (rate_limit_window IS NULL)=(rate_limit_count IS NULL)
 AND (rate_limit_window IS NULL OR (rate_limit_window>0 AND rate_limit_count>0))
);
