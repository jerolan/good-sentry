CREATE UNIQUE INDEX IF NOT EXISTS projects_organization_id_id ON projects(organization_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS teams_organization_id_id ON teams(organization_id,id);

CREATE TABLE project_teams (
 organization_id bigint NOT NULL,
 project_id bigint NOT NULL,
 team_id bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,team_id),
 FOREIGN KEY(organization_id,project_id) REFERENCES projects(organization_id,id) ON DELETE CASCADE,
 FOREIGN KEY(organization_id,team_id) REFERENCES teams(organization_id,id) ON DELETE CASCADE
);

CREATE INDEX project_teams_team_id ON project_teams(organization_id,team_id,project_id);

INSERT INTO project_teams(organization_id,project_id,team_id,created_at)
SELECT p.organization_id,p.id,p.team_id,p.created_at
FROM projects p
JOIN teams t ON t.id=p.team_id AND t.organization_id=p.organization_id
WHERE p.team_id IS NOT NULL;
