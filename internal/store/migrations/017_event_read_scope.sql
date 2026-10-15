-- Existing credentials retain exactly their current scopes and defaults.
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read']::text[]
);
