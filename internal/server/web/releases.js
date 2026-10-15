export function createReleasesUI({ state, node, request, navigate, routeLink, button, field, selectField, pageHeader, empty, formError, notify, number, text, errorState, reload }) {
  const dialogs = new Set();
  const manages = () => ['owner', 'admin'].includes(state.organization?.role);
  const org = () => encodeURIComponent(state.organization.slug);
  const compatible = version => `/api/0/organizations/${org()}/releases/${version === undefined ? '' : `${encodeURIComponent(version)}/`}`;
  const native = () => `/api/v1/organizations/${org()}/releases`;
  const utc = value => value ? node('time', { datetime: value }, String(value)) : node('span', { class: 'muted' }, 'Not recorded');
  const status = release => node('div', { class: 'actions' }, node('span', { class: `badge ${release.status === 'archived' ? 'ignored' : ''}` }, release.status), node('span', { class: `badge ${release.dateReleased ? 'resolved' : 'warning'}` }, release.dateReleased ? 'Released' : 'Unreleased'));
  const projectsText = projects => projects.map(project => project.name).join(', ');

  function metadataURL(value) {
    if (!value) return node('span', { class: 'muted' }, 'Not provided');
    let url;
    try { url = new URL(value); } catch (_) { return node('span', {}, value); }
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password
      ? node('a', { href: url.href, target: '_blank', rel: 'noopener noreferrer' }, value) : node('span', {}, value);
  }

  function table(columns, rows) {
    return node('div', { class: 'table-scroll' }, node('table', { class: 'release-table' }, node('thead', {}, node('tr', {}, columns.map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, rows)));
  }

  function pager(result, key, label) {
    const previous = button(`Previous${label ? ` ${label}` : ''}`, () => navigate({ [key]: result.page - 1 }));
    const next = button(`Next${label ? ` ${label}` : ''}`, () => navigate({ [key]: result.page + 1 }));
    previous.disabled = result.page <= 1;
    next.disabled = !result.hasMore;
    return node('div', { class: 'pagination' }, node('span', {}, `${number(result.data.length)} records · Page ${number(result.page)}`), node('div', { class: 'actions' }, previous, next));
  }

  async function render(main, params, isCurrent) {
    document.title = 'Releases · GoodSentry';
    if (!params.has('version')) {
      const query = new URLSearchParams({ page: params.has('page') ? params.get('page') : '1' });
      for (const key of ['project', 'query', 'environment']) if (params.has(key)) query.set(key, params.get(key));
      if (params.has('project')) {
        const selected = state.projects.find(project => project.id === params.get('project'));
        if (selected) query.set('project', selected.slug);
      }
      const result = await request(`${native()}?${query}`);
      if (!isCurrent()) return;
      main.replaceChildren(pageHeader('Releases', 'Versions, manually supplied commit metadata and completed deployments.', node('div', { class: 'actions' }, button('Refresh releases', reload), manages() ? button('Create release', openCreate, 'primary') : null)), filters(params));
      const panel = node('section', { class: 'card panel', 'aria-label': 'Releases' });
      if (!result.data.length) panel.append(empty('No matching releases', 'Try another version, environment or project filter. Captures and source-map uploads also record valid releases.'));
      else panel.append(table(['Version', 'Projects', 'Status', 'Created (UTC)', 'Released (UTC)', 'Commits / deploys'], result.data.map(release => node('tr', {},
        node('td', { class: 'release-identity' }, routeLink(release.version, { version: release.version, commitPage: null, deployPage: null }, { class: 'issue-title issue-link release-version' })),
        node('td', { class: 'release-value' }, projectsText(release.projects)), node('td', {}, status(release)), node('td', {}, utc(release.dateCreated)), node('td', {}, utc(release.dateReleased)),
        node('td', {}, `${number(release.commitCount)} commits · ${number(release.deployCount)} deploys`)))));
      panel.append(pager(result, 'page', '')); main.append(panel); return;
    }
    const version = params.get('version');
    if (!version) throw new Error('This release link is incomplete. Choose a release from the list.');
    const [detail, commits, deploys] = await Promise.allSettled([
      request(compatible(version)), request(`${native()}/${encodeURIComponent(version)}/commits?${new URLSearchParams({ page: params.has('commitPage') ? params.get('commitPage') : '1' })}`),
      request(`${native()}/${encodeURIComponent(version)}/deploys?${new URLSearchParams({ page: params.has('deployPage') ? params.get('deployPage') : '1' })}`),
    ]);
    if (!isCurrent()) return;
    if (detail.status === 'rejected') throw detail.reason;
    const release = detail.value;
    main.replaceChildren(routeLink('← Back to releases', { version: null, commitPage: null, deployPage: null }, { class: 'back-link' }),
      pageHeader(release.version, 'Exact release identity. All times are UTC.', node('div', { class: 'actions' }, button('Refresh release', reload))), overview(release));
    main.querySelector('h1').classList.add('release-version');
    for (const [name, result, renderPanel] of [['Commits', commits, commitPanel], ['Deployments', deploys, deployPanel]]) {
      if (result.status === 'fulfilled') main.append(renderPanel(release, result.value));
      else { const panel = node('section', { class: 'card panel', 'aria-label': name }, node('div', { class: 'panel-title' }, node('h2', {}, name))); errorState(panel, result.reason, reload); main.append(panel); }
    }
  }

  function filters(params) {
    const project = selectField('Release project', [['', 'All projects'], ...state.projects.map(item => [item.id, item.name])], params.get('project') || '');
    const query = field('Search versions', 'search', { value: params.get('query') || '', maxlength: 400 });
    const environment = field('Release environment', 'text', { value: params.get('environment') || '' });
    const form = node('form', { class: 'filters release-filters', 'aria-label': 'Filter releases' }, query.wrap, project.wrap, environment.wrap, node('button', { class: 'btn button', type: 'submit' }, 'Apply release filters'));
    form.addEventListener('submit', event => { event.preventDefault(); navigate({ query: query.input.value || null, project: project.input.value || null, environment: environment.input.value || null, page: null, version: null, commitPage: null, deployPage: null }); });
    return form;
  }

  function overview(release) {
    const panel = node('section', { class: 'card panel', 'aria-label': 'Release context' });
    const actions = node('div', { class: 'actions' });
    if (manages() && release.canWrite) actions.append(button('Edit release', () => openEdit(release)), button('Finalize now (UTC)', () => openFinalize(release)));
    panel.append(node('div', { class: 'panel-title' }, status(release), actions));
    const details = node('dl', { class: 'release-context' });
    for (const [label, value] of [['Created (UTC)', utc(release.dateCreated)], ['Started (UTC)', utc(release.dateStarted)], ['Released (UTC)', utc(release.dateReleased)], ['Ref', text(release.ref)], ['URL', metadataURL(release.url)], ['Recorded metadata', `${number(release.commitCount)} commits · ${number(release.deployCount)} deployments`]]) details.append(node('div', {}, node('dt', {}, label), node('dd', {}, value)));
    panel.append(node('div', { class: 'panel-body' }, details));
    const projects = node('div', { class: 'panel-body release-project-list' }, node('h2', {}, 'Associated projects'));
    for (const project of release.projects) {
      const controls = node('div', { class: 'actions' }, routeLink('Project and source maps', { view: 'projects', project: project.id, sourceMaps: project.id, version: null, commitPage: null, deployPage: null }, { class: 'btn button small' }));
      if (manages() && release.canWrite) controls.append(button('Remove project', () => openRemoval(release, project), 'small'));
      projects.append(node('div', { class: 'release-project' }, node('div', {}, node('strong', {}, project.name), node('div', { class: 'small muted mono' }, project.slug)), controls));
    }
    panel.append(projects);
    if (manages() && release.canWrite) panel.append(node('div', { class: 'panel-body' }, button('Delete release', () => openRemoval(release), 'danger')));
    else panel.append(node('p', { class: 'panel-body muted small' }, 'An owner or admin with access to every associated project can change this release.'));
    return panel;
  }

  function commitPanel(release, result) {
    const panel = node('section', { class: 'card panel', 'aria-label': 'Release commits' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Commits'), manages() && release.canWrite ? button('Replace commit set', () => openCommits(release)) : null));
    panel.append(node('p', { class: 'panel-body muted small' }, 'Manually supplied metadata, in recorded order. Raw previews preserve large integers and extreme exponents as text; they are never submitted by the editor.'));
    if (!result.data.length) panel.append(empty('No commits on this page', result.page > 1 ? 'Choose an earlier commit page.' : 'No commit metadata has been supplied for this release.'));
    else panel.append(table(['Commit / repository', 'Message', 'Author', 'Timestamp (UTC)', 'Metadata'], result.data.map(commit => node('tr', {},
      node('td', { class: 'release-identity' }, node('code', {}, commit.id), node('div', { class: 'small muted' }, text(commit.repository, 'No repository supplied'))),
      node('td', { class: 'release-message' }, commit.message), node('td', { class: 'release-value' }, commit.authorName, node('div', { class: 'small muted' }, commit.authorEmail)), node('td', {}, utc(commit.dateCreated)),
      node('td', {}, node('details', { class: 'release-raw' }, node('summary', {}, 'Inspect metadata'), node('pre', {}, JSON.stringify(commit.data, null, 2))))))));
    panel.append(pager(result, 'commitPage', 'commits')); return panel;
  }

  function deployPanel(release, result) {
    const panel = node('section', { class: 'card panel', 'aria-label': 'Release deployments' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Deployments'), manages() && release.canWrite ? button('Record deployment', () => openDeploy(release)) : null));
    if (!result.data.length) panel.append(empty('No deployments on this page', result.page > 1 ? 'Choose an earlier deployment page.' : 'Record a completed deployment with its environment and projects.'));
    else panel.append(table(['Environment / name', 'Projects', 'Started (UTC)', 'Finished (UTC)', 'Recorded (UTC)'], result.data.map(deploy => node('tr', {},
      node('td', { class: 'release-identity' }, node('strong', {}, deploy.environment), node('div', {}, text(deploy.name, 'Unnamed deployment')), metadataURL(deploy.url)),
      node('td', { class: 'release-value' }, projectsText(deploy.projects)), node('td', {}, utc(deploy.dateStarted)), node('td', {}, utc(deploy.dateFinished)), node('td', {}, utc(deploy.dateCreated))))));
    panel.append(pager(result, 'deployPage', 'deployments')); return panel;
  }

  function projectChoices(projects) {
    const choices = projects.map(project => ({ slug: project.slug, input: node('input', { type: 'checkbox', value: project.id }), name: project.name }));
    const wrap = node('fieldset', { class: 'token-fieldset form-stack release-project-choices' }, node('legend', {}, 'Projects'), choices.map(choice => node('label', { class: 'checkbox-field' }, choice.input, choice.name)));
    return { wrap, selected: () => { const slugs = choices.filter(choice => choice.input.checked).map(choice => choice.slug); if (!slugs.length) throw new Error('Choose at least one project.'); return slugs; } };
  }

  function closeDialogs() {
    for (const dialog of dialogs) {
      if (dialog.open) dialog.close();
      dialog.remove();
    }
    dialogs.clear();
  }

  function openDialog(title, fields, label, save, danger = false) {
    const organization = state.organization.slug;
    const id = `release-dialog-${Date.now()}`;
    const dialog = node('dialog', { class: 'release-dialog', 'aria-labelledby': id });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const submit = node('button', { type: 'submit', class: `btn button ${danger ? 'danger' : 'primary'}` }, label);
    const form = node('form', { class: 'form-stack' }, node('h2', { id }, title), fields, error, node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      const active = () => dialog.open && dialog.isConnected && state.organization?.slug === organization;
      if (!active()) return;
      error.textContent = ''; submit.disabled = true;
      try {
        const after = await save();
        if (!active()) return;
        dialog.close();
        if (typeof after === 'function') after(); else reload();
      }
      catch (failure) { if (active()) formError(error, failure); }
      finally { submit.disabled = false; }
    });
    dialog.append(form); dialog.addEventListener('close', () => { dialogs.delete(dialog); dialog.remove(); }); dialogs.add(dialog); document.body.append(dialog); dialog.showModal();
    form.querySelector('input,select,textarea,button')?.focus();
  }

  function openCreate() {
    const version = field('Release version', 'text', { required: true, maxlength: 510, autocomplete: 'off' });
    version.input.addEventListener('input', () => version.input.setCustomValidity(Array.from(version.input.value).length > 255 ? 'Use at most 255 Unicode characters.' : ''));
    const projects = projectChoices(state.projects);
    openDialog('Create unreleased version', [version.wrap, node('p', { class: 'muted small' }, 'Version identity is immutable. Spaces, case and slashes are preserved exactly. Choose the projects sharing this version.'), projects.wrap], 'Create release', async () => {
      const submittedVersion = version.input.value;
      await request(compatible(), { method: 'POST', body: { version: submittedVersion, projects: projects.selected(), dateReleased: null } });
      return () => { notify('Release saved.'); navigate({ version: submittedVersion, commitPage: null, deployPage: null }); };
    });
  }

  function openEdit(release) {
    const ref = field('Ref', 'text', { value: release.ref || '', maxlength: 4000 });
    const url = field('Metadata URL', 'text', { value: release.url || '', maxlength: 4000 });
    const stateField = selectField('Release status', [['open', 'Open'], ['archived', 'Archived']], release.status);
    openDialog('Edit release metadata', [ref.wrap, url.wrap, stateField.wrap, node('p', { class: 'muted small' }, 'Empty ref or URL clears that metadata. The exact version and project associations are retained.')], 'Save release', () => request(compatible(release.version), { method: 'PUT', body: { ref: ref.input.value || null, url: url.input.value || null, status: stateField.input.value } }));
  }

  function openFinalize(release) {
    openDialog('Finalize release', node('p', {}, 'Record the current UTC instant as this release’s release time. Existing commit and deployment metadata is retained.'), 'Finalize now', () => request(compatible(release.version), { method: 'PUT', body: { dateReleased: new Date().toISOString() } }));
  }

  function openCommits(release) {
    const input = field('Complete commit JSON array', 'textarea', { rows: 10, required: true, spellcheck: 'false' });
    openDialog('Replace commit set', [node('p', { class: 'muted small' }, 'Provide the complete ordered JSON array, newest first. Each record needs an id. This replaces the existing set; [] clears it. Raw previews are never prefilled or resubmitted.'), input.wrap], 'Replace commits', () => {
      let parsed; try { parsed = JSON.parse(input.input.value); } catch (_) { throw new Error('Enter a valid JSON array.'); }
      if (!Array.isArray(parsed)) throw new Error('The complete commit set must be a JSON array.');
      return request(`${compatible(release.version)}commits/`, { method: 'POST', jsonText: input.input.value });
    });
  }

  function openDeploy(release) {
    const environment = field('Deployment environment', 'text', { required: true, maxlength: 128 });
    const name = field('Deployment name', 'text', { maxlength: 400 });
    const url = field('Deployment URL', 'text', { maxlength: 4000 });
    const projects = projectChoices(release.projects);
    openDialog('Record completed deployment', [environment.wrap, name.wrap, url.wrap, projects.wrap, node('p', { class: 'muted small' }, 'This records a completed deployment; it does not run a deployment. The server records its finish time when accepted.')], 'Record deployment', () => request(`${compatible(release.version)}deploys/`, { method: 'POST', body: { environment: environment.input.value, name: name.input.value || null, url: url.input.value || null, projects: projects.selected() } }));
  }

  function openRemoval(release, project) {
    const description = project ? `Remove ${project.name} from this release and delete this project’s release-specific source maps. Other associated projects remain. Removing the last project also deletes the release.` : 'Delete this release from every associated project, including its release-specific source maps, commit metadata and deployment records.';
    const path = project ? `/api/0/projects/${org()}/${encodeURIComponent(project.slug)}/releases/${encodeURIComponent(release.version)}/` : compatible(release.version);
    openDialog(project ? 'Remove project association' : 'Delete release', [node('code', { class: 'release-version' }, release.version), node('p', {}, description), node('p', { class: 'muted small' }, 'Captured events, issues, spans and their preserved stacks remain. A new capture or upload may observe this version again.')], project ? 'Confirm removal' : 'Confirm delete', async () => {
      await request(path, { method: 'DELETE' });
      return () => {
        notify(project ? 'Project association removed.' : 'Release deleted.');
        if (!project || release.projects.length === 1) navigate({ version: null, commitPage: null, deployPage: null }); else reload();
      };
    }, true);
  }

  return { render, closeDialogs };
}
