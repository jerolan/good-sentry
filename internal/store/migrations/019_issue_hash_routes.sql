-- Active signature routing requires the current historical groups to be valid.
DO $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM issues i JOIN issues parent ON parent.id=i.merged_into
  WHERE parent.project_id<>i.project_id OR parent.merged_into IS NOT NULL OR parent.id>=i.id
 ) THEN
  RAISE EXCEPTION 'issue hash routes require same-project roots older than their direct members';
 END IF;
 IF EXISTS (
  SELECT 1 FROM issues i WHERE i.merged_into IS NOT NULL AND
   (i.status<>'unresolved' OR i.resolved_at IS NOT NULL OR i.regressed_at IS NOT NULL
    OR EXISTS(SELECT 1 FROM issue_ignores g WHERE g.issue_id=i.id)
    OR EXISTS(SELECT 1 FROM issue_resolutions r WHERE r.issue_id=i.id))
 ) THEN
  RAISE EXCEPTION 'issue hash route aliases must not have active triage state';
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
  RAISE EXCEPTION 'issue hash route events have inconsistent current or original ownership';
 END IF;
END $$;

CREATE TABLE issue_hash_routes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 fingerprint text NOT NULL,
 origin_issue_id bigint,
 CONSTRAINT issue_hash_routes_project_fingerprint_key UNIQUE(project_id,fingerprint),
 CONSTRAINT issue_hash_routes_origin_fkey FOREIGN KEY(project_id,origin_issue_id)
  REFERENCES issues(project_id,id)
);
CREATE INDEX issue_hash_routes_origin ON issue_hash_routes(project_id,origin_issue_id,id)
 WHERE origin_issue_id IS NOT NULL;
INSERT INTO issue_hash_routes(project_id,fingerprint,origin_issue_id)
 SELECT project_id,fingerprint,id FROM issues;
ALTER TABLE issues DROP CONSTRAINT issues_project_id_fingerprint_key;
CREATE INDEX issues_project_fingerprint ON issues(project_id,fingerprint);
