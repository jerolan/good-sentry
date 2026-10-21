CREATE TABLE status_pages (
 id bigserial PRIMARY KEY,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
 slug text NOT NULL UNIQUE CHECK (char_length(slug) BETWEEN 1 AND 200),
 is_public boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX status_pages_organization ON status_pages(organization_id,created_at DESC,id DESC);
CREATE TABLE status_page_monitors (
 status_page_id bigint NOT NULL REFERENCES status_pages ON DELETE CASCADE,
 monitor_id bigint NOT NULL REFERENCES monitors ON DELETE CASCADE,
 PRIMARY KEY(status_page_id,monitor_id)
);
CREATE INDEX status_page_monitors_monitor ON status_page_monitors(monitor_id);
