-- Additive native assignment/history migration; retain every legacy value.
ALTER TABLE teams ADD CONSTRAINT teams_organization_id_id_key UNIQUE(organization_id,id);
CREATE TABLE issue_assignments (
 issue_id bigint PRIMARY KEY REFERENCES issues ON DELETE CASCADE,
 organization_id bigint NOT NULL REFERENCES organizations ON DELETE CASCADE,
 user_id bigint,
 team_id bigint,
 CHECK ((user_id IS NULL) <> (team_id IS NULL)),
 FOREIGN KEY(organization_id,user_id) REFERENCES memberships(organization_id,user_id) ON DELETE CASCADE,
 FOREIGN KEY(organization_id,team_id) REFERENCES teams(organization_id,id) ON DELETE CASCADE
);
INSERT INTO issue_assignments(issue_id,organization_id,user_id)
 SELECT i.id,p.organization_id,i.assignee_id FROM issues i JOIN projects p ON p.id=i.project_id
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=i.assignee_id;
ALTER TABLE issues ADD COLUMN regressed_at timestamptz;
ALTER TABLE comments ADD COLUMN updated_at timestamptz;
ALTER TABLE comments ALTER COLUMN created_at SET DEFAULT clock_timestamp();
ALTER TABLE issue_activity ALTER COLUMN created_at SET DEFAULT clock_timestamp();
CREATE INDEX comments_issue_history ON comments(issue_id,id DESC);
CREATE INDEX issue_activity_history ON issue_activity(issue_id,id DESC);
