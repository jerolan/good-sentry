-- Manager is an organization role; invitation team assignments keep stable IDs.
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;
ALTER TABLE memberships ADD CONSTRAINT memberships_role_check
 CHECK (role IN ('owner','manager','admin','member'));

ALTER TABLE organization_invitations DROP CONSTRAINT organization_invitations_role_check;
ALTER TABLE organization_invitations ADD CONSTRAINT organization_invitations_role_check
 CHECK (role IN ('owner','manager','admin','member'));

CREATE TABLE organization_invitation_teams (
 invitation_id text NOT NULL REFERENCES organization_invitations(id) ON DELETE CASCADE,
 team_id bigint NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
 PRIMARY KEY (invitation_id,team_id)
);
CREATE INDEX organization_invitation_teams_team ON organization_invitation_teams(team_id,invitation_id);
