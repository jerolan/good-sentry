-- Deletion is opt-in and does not widen any existing credential.
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read','event:write','event:admin']::text[]
);
CREATE INDEX envelope_items_project_event ON envelope_items(project_id,event_id);
