-- Refuse ambiguous legacy groupings before enforcing the native flat-star model.
DO $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM issues i JOIN issues parent ON parent.id=i.merged_into
  WHERE parent.project_id<>i.project_id OR parent.merged_into IS NOT NULL OR parent.id>=i.id
 ) THEN
  RAISE EXCEPTION 'issue groups require same-project roots older than their direct members';
 END IF;
 IF EXISTS (
  SELECT 1 FROM issues i WHERE i.merged_into IS NOT NULL AND
   (i.status<>'unresolved' OR i.resolved_at IS NOT NULL OR i.regressed_at IS NOT NULL
    OR EXISTS(SELECT 1 FROM issue_ignores g WHERE g.issue_id=i.id)
    OR EXISTS(SELECT 1 FROM issue_resolutions r WHERE r.issue_id=i.id))
 ) THEN
  RAISE EXCEPTION 'issue group aliases must not have active triage state';
 END IF;
 IF EXISTS (
  SELECT 1 FROM events e
  LEFT JOIN issues owner ON owner.id=e.issue_id
  LEFT JOIN issues origin ON origin.id=e.original_issue_id
  WHERE (e.kind='transaction' AND (e.issue_id IS NOT NULL OR e.original_issue_id IS NOT NULL))
     OR (e.issue_id IS NOT NULL AND (owner.project_id<>e.project_id OR owner.merged_into IS NOT NULL))
     OR (e.original_issue_id IS NOT NULL AND
         (origin.project_id<>e.project_id OR e.issue_id IS NULL OR COALESCE(origin.merged_into,origin.id)<>e.issue_id))
 ) THEN
  RAISE EXCEPTION 'issue events have inconsistent current or original ownership';
 END IF;
END $$;

ALTER TABLE issues ADD CONSTRAINT issues_project_id_id_key UNIQUE(project_id,id);
ALTER TABLE issues DROP CONSTRAINT issues_merged_into_fkey;
ALTER TABLE issues ADD CONSTRAINT issues_group_root_fkey FOREIGN KEY(project_id,merged_into)
 REFERENCES issues(project_id,id) ON DELETE SET NULL (merged_into);
ALTER TABLE issues ADD CONSTRAINT issues_group_older_root CHECK(merged_into<id);
CREATE INDEX issues_group_members ON issues(merged_into,id) WHERE merged_into IS NOT NULL;
CREATE INDEX events_origin_date ON events(original_issue_id,timestamp DESC,id DESC) WHERE original_issue_id IS NOT NULL;
