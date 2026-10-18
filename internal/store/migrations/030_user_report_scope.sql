-- Legacy feedback submission is opt-in; preserve all existing token grants.
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read','event:write','event:admin','project:write']::text[]
);
