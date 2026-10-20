export function createPeopleUI({ state, node, request, button, field, pageHeader, empty, formError, notify }) {
  let selectedTeam = null;

  function memberLabel(member) {
    return member.name || member.email || member.id;
  }

  function teamPath(org, id = null) {
    const base = `/api/v1/organizations/${encodeURIComponent(org)}/teams`;
    return id ? `${base}/${encodeURIComponent(id)}` : base;
  }

  return {
    async render(main, _params, alive) {
      const org = state.organization.slug;
      const canManage = ['owner', 'admin'].includes(state.organization.role);
      const operationError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });

      if (!canManage) {
        main.replaceChildren(pageHeader('People and teams', 'Organization owners and admins manage members and teams.'),
          node('section', { class: 'card panel' }, empty('Permission required', 'Ask an organization owner or admin to manage people and teams.')));
        return;
      }

      async function load() {
        main.replaceChildren(pageHeader('People and teams', 'Manage roles, organization membership and project teams.', button('Refresh', () => load())));
        main.append(node('div', { class: 'card panel panel-body muted', role: 'status' }, 'Loading people and teams…'));
        try {
          const [membersResult, teamsResult, invitationsResult] = await Promise.all([
            request(`/api/v1/organizations/${encodeURIComponent(org)}/members`),
            request(teamPath(org)),
            request(`/api/v1/organizations/${encodeURIComponent(org)}/invitations`),
          ]);
          if (!alive()) return;
          let teamDetail = null;
          if (selectedTeam) {
            try {
              teamDetail = await request(teamPath(org, selectedTeam));
            } catch (error) {
              if (error.status !== 404) throw error;
              selectedTeam = null;
            }
          }
          if (!alive()) return;
          draw(membersResult.data || [], teamsResult.data || [], invitationsResult.data || [], teamDetail);
        } catch (error) {
          if (alive()) {
            const failure = node('div', { class: 'card panel panel-body form-stack', role: 'alert' }, node('p', {}, error.message), button('Try again', () => load()));
            if (main.lastElementChild?.getAttribute('role') === 'status') main.lastElementChild.replaceWith(failure);
            else main.append(failure);
          }
        }
      }

      function draw(members, teams, invitations, teamDetail) {
        operationError.textContent = '';
        const memberRows = members.map(member => {
          const roleOptions = state.organization.role === 'owner' ? ['owner', 'admin', 'member'] : ['admin', 'member'];
          const protectedOwner = state.organization.role !== 'owner' && member.role === 'owner';
          if (protectedOwner) {
            return node('tr', {}, node('td', {}, node('strong', {}, memberLabel(member)), node('div', { class: 'small muted' }, member.email)), node('td', {}, member.role), node('td', { class: 'small muted' }, 'Only an owner can change this owner.'));
          }
          const role = node('select', { class: 'select', 'aria-label': `Role for ${memberLabel(member)}` }, roleOptions.map(value => node('option', { value }, value)));
          role.value = member.role;
          role.disabled = protectedOwner;
          const save = button('Save role', async () => {
            save.disabled = true;
            try {
              await request(`/api/v1/organizations/${encodeURIComponent(org)}/members/${encodeURIComponent(member.id)}`, { method: 'PATCH', body: { role: role.value } });
              notify(`Updated ${memberLabel(member)}.`);
              await load();
            } catch (error) { formError(operationError, error); save.disabled = false; }
          }, 'small');
          save.disabled = protectedOwner || member.role === role.value;
          role.addEventListener('change', () => { save.disabled = protectedOwner || member.role === role.value; });
          const actions = node('div', { class: 'actions' }, role, save);
          actions.append(button('Remove', () => {
            const warning = node('div', { class: 'form-stack' }, node('p', {}, `Remove ${memberLabel(member)} from this organization? Their organization tokens, assignments and team memberships will be removed.`), operationError);
            const confirm = button('Confirm removal', async () => {
              confirm.disabled = true;
              try {
                await request(`/api/v1/organizations/${encodeURIComponent(org)}/members/${encodeURIComponent(member.id)}`, { method: 'DELETE' });
                notify(`${memberLabel(member)} was removed.`);
                await load();
              } catch (error) { formError(operationError, error); confirm.disabled = false; }
            }, 'danger small');
            warning.append(node('div', { class: 'actions' }, confirm, button('Cancel', () => load(), 'small')));
            row.replaceChildren(warning);
          }, 'danger small'));
          const row = node('tr', {}, node('td', {}, node('strong', {}, memberLabel(member)), node('div', { class: 'small muted' }, member.email)), node('td', {}, member.role), node('td', {}, actions));
          return row;
        });
        const membersPanel = node('section', { class: 'card panel' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Members'), node('span', { class: 'muted small' }, `${members.length} current`)),
          memberRows.length ? node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, ['Member', 'Current role', 'Manage'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, memberRows))) : node('p', { class: 'panel-body muted' }, 'No organization members.'));

        const inviteEmail = field('Email', 'email', { required: true, maxlength: 254, autocomplete: 'off' });
        const inviteRoles = state.organization.role === 'owner' ? ['member', 'admin', 'owner'] : ['member', 'admin'];
        const inviteRole = node('select', { class: 'select', 'aria-label': 'Invitation role' }, inviteRoles.map(role => node('option', { value: role }, role)));
        const inviteError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
        const inviteSubmit = node('button', { type: 'submit', class: 'btn button primary' }, 'Send invitation');
        const inviteForm = node('form', { class: 'people-invite-form' }, node('div', { class: 'people-inline-form' }, inviteEmail.wrap, inviteRole, inviteSubmit), inviteError);
        inviteForm.addEventListener('submit', async event => {
          event.preventDefault();
          inviteSubmit.disabled = true;
          try {
            await request(`/api/v1/organizations/${encodeURIComponent(org)}/invitations`, { method: 'POST', body: { email: inviteEmail.input.value, role: inviteRole.value } });
            notify('Invitation queued.');
            await load();
          } catch (error) { formError(inviteError, error); inviteSubmit.disabled = false; }
        });
        const invitationRows = invitations.map(invitation => node('tr', {}, node('td', {}, node('strong', {}, invitation.email), node('div', { class: 'small muted' }, `Expires ${new Date(invitation.expiresAt).toLocaleString()}`)), node('td', {}, invitation.role), node('td', {}, invitation.status),
          node('td', {}, button('Cancel', async () => {
            try {
              await request(`/api/v1/organizations/${encodeURIComponent(org)}/invitations/${encodeURIComponent(invitation.id)}`, { method: 'DELETE' });
              notify('Invitation canceled.');
              await load();
            } catch (error) { formError(operationError, error); }
          }, 'small danger'))));
        const invitesPanel = node('section', { class: 'card panel' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Invitations'), node('span', { class: 'muted small' }, 'Encrypted, single-use links')), node('div', { class: 'panel-body form-stack' }, inviteForm,
          invitationRows.length ? node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, ['Email', 'Role', 'Status', 'Manage'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, invitationRows))) : node('p', { class: 'muted' }, 'No active invitations.')));

        const input = field('Team name', 'text', { required: true, maxlength: 120, autocomplete: 'off' });
        const createError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
        const create = node('button', { type: 'submit', class: 'btn button primary' }, 'Create team');
        const createForm = node('form', { class: 'form-stack people-create' }, input.wrap, createError, create);
        createForm.addEventListener('submit', async event => {
          event.preventDefault();
          create.disabled = true;
          try {
            const team = await request(teamPath(org), { method: 'POST', body: { name: input.input.value } });
            selectedTeam = team.id;
            notify(`${team.name} was created.`);
            await load();
          } catch (error) { formError(createError, error); create.disabled = false; }
        });

        const cards = teams.map(team => {
          const card = node('article', { class: 'card panel people-team' }, node('div', { class: 'panel-title' },
            node('div', {}, node('h3', {}, team.name), node('p', { class: 'small muted mono' }, team.slug)),
            node('div', { class: 'actions' }, node('span', { class: 'badge badge-sm' }, `${team.memberCount} members`), node('span', { class: 'badge badge-sm' }, `${team.projectCount} projects`), button('Open', () => { selectedTeam = team.id; load(); }, 'small'))));
          if (teamDetail?.id === team.id) card.append(teamEditor(team, teamDetail, members));
          return card;
        });
        const teamsPanel = node('section', { class: 'form-stack' }, node('div', { class: 'card panel panel-body' }, node('h2', {}, 'Create a team'), createForm),
          ...(cards.length ? cards : [node('div', { class: 'card panel' }, empty('No teams yet', 'Create a team to group projects and alert recipients.'))]));

        main.replaceChildren(pageHeader('People and teams', 'Manage roles, organization membership and project teams.', button('Refresh', () => load())), operationError, membersPanel, invitesPanel, teamsPanel);
      }

      function teamEditor(team, detail, members) {
        const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
        const rename = field('Team name', 'text', { required: true, maxlength: 120, value: team.name });
        const saveName = button('Save name', async () => {
          saveName.disabled = true;
          try {
            await request(teamPath(org, team.id), { method: 'PATCH', body: { name: rename.input.value } });
            notify('Team renamed.');
            await load();
          } catch (failure) { formError(error, failure); saveName.disabled = false; }
        }, 'small');
        const memberIDs = new Set(detail.members.map(member => member.id));
        const available = members.filter(member => !memberIDs.has(member.id));
        const add = node('select', { class: 'select', 'aria-label': 'Member to add' }, available.map(member => node('option', { value: member.id }, `${memberLabel(member)} · ${member.email}`)));
        const addButton = button('Add member', async () => {
          if (!add.value) return;
          addButton.disabled = true;
          try {
            await request(`${teamPath(org, team.id)}/members/${encodeURIComponent(add.value)}`, { method: 'POST' });
            notify('Team member added.');
            await load();
          } catch (failure) { formError(error, failure); addButton.disabled = false; }
        }, 'small');
        addButton.disabled = available.length === 0;
        const memberRows = detail.members.map(member => node('tr', {}, node('td', {}, node('strong', {}, memberLabel(member)), node('div', { class: 'small muted' }, member.email)),
          node('td', {}, button('Remove', async () => {
            try {
              await request(`${teamPath(org, team.id)}/members/${encodeURIComponent(member.id)}`, { method: 'DELETE' });
              notify('Team member removed.');
              await load();
            } catch (failure) { formError(error, failure); }
          }, 'small danger'))));
        const editor = node('div', { class: 'form-stack people-team-detail' });
        const deleteTeam = button('Delete team', () => {
          const warning = node('div', { class: 'form-stack' }, node('p', {}, 'Delete this team? Project associations are removed; project data is kept.'), error);
          warning.append(node('div', { class: 'actions' }, button('Confirm deletion', async () => {
            try {
              await request(teamPath(org, team.id), { method: 'DELETE' });
              selectedTeam = null;
              notify('Team deleted.');
              await load();
            } catch (failure) { formError(error, failure); }
          }, 'danger small'), button('Cancel', () => load(), 'small')));
          editor.replaceChildren(warning);
        }, 'danger small');
        deleteTeam.disabled = team.slug === 'default';
        editor.append(
          node('div', { class: 'people-team-header' }, node('div', { class: 'people-inline-form' }, rename.wrap, saveName), deleteTeam), error,
          node('div', { class: 'people-add' }, available.length ? node('div', { class: 'people-inline-form' }, add, addButton) : node('p', { class: 'small muted' }, 'Every organization member is already in this team.')),
          memberRows.length ? node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, ['Team member', 'Manage'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, memberRows))) : node('p', { class: 'muted' }, 'This team has no members.'));
        return editor;
      }

      await load();
    },
  };
}
