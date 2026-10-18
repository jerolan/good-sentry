-- Preserve existing issue state while assigning its historical error category.
ALTER TABLE issues ADD COLUMN category text NOT NULL DEFAULT 'error'
 CHECK(category IN ('error','feedback'));
ALTER TABLE issues ADD CONSTRAINT issues_project_category_id_key UNIQUE(project_id,category,id);
ALTER TABLE issues DROP CONSTRAINT issues_group_root_fkey;
ALTER TABLE issues ADD CONSTRAINT issues_group_root_fkey FOREIGN KEY(project_id,category,merged_into)
 REFERENCES issues(project_id,category,id) ON DELETE SET NULL (merged_into);

CREATE TABLE feedback_events (
 event_receipt_id bigint PRIMARY KEY,
 project_id bigint NOT NULL,
 event_id text NOT NULL CHECK(event_id ~ '^[0-9a-f]{32}$'),
 associated_event_id text CHECK(associated_event_id ~ '^[0-9a-f]{32}$'),
 associated_event_receipt_id bigint,
 association_pending boolean NOT NULL DEFAULT false,
 CHECK(associated_event_id IS NULL OR associated_event_id<>event_id),
 CHECK(associated_event_receipt_id IS NULL OR associated_event_id IS NOT NULL),
 CHECK(NOT association_pending OR (associated_event_id IS NOT NULL AND associated_event_receipt_id IS NULL)),
 FOREIGN KEY(event_receipt_id,project_id,event_id) REFERENCES events(id,project_id,event_id) ON DELETE CASCADE,
 FOREIGN KEY(associated_event_receipt_id,project_id,associated_event_id)
  REFERENCES events(id,project_id,event_id) ON DELETE SET NULL (associated_event_receipt_id)
);
CREATE INDEX feedback_events_pending ON feedback_events(project_id,associated_event_id,event_receipt_id)
 WHERE association_pending;
