export function createProjectKeysUI({ state, node, request, button, field, date, number, copyValue, formError, notify }) {
  const dialogs = new Set();
  const pageSize = 25;
  const canManage = () => ['owner', 'manager', 'admin'].includes(state.organization?.role);
  const keysPath = project => `/api/0/projects/${encodeURIComponent(state.organization.slug)}/${encodeURIComponent(project.slug)}/keys/`;

  function closeDialogs() {
    for (const dialog of dialogs) {
      if (dialog.open) dialog.close();
      dialog.remove();
    }
    dialogs.clear();
  }

  function linkedCursor(link, relation) {
    const entry = link?.split(/,\s*(?=<)/).find(value => new RegExp(`rel="${relation}"`).test(value));
    const href = entry?.match(/<([^>]+)>/)?.[1];
    if (!href) return null;
    try { return new URL(href, location.href).searchParams.get('cursor'); }
    catch (_) { return null; }
  }

  function keyDSN(project, key) {
    try {
      const value = new URL(project.dsn, location.href);
      value.username = key.public || key.dsn?.public || '';
      value.password = '';
      return value.toString();
    } catch (_) {
      return project.dsn || '';
    }
  }

  function rateLimitText(key) {
    if (!key.rateLimit) return 'No limit';
    return `${number(key.rateLimit.count)} events per ${number(key.rateLimit.window)} seconds`;
  }

  function open(project) {
    const organization = state.organization.slug;
    const path = keysPath(project);
    const dialogID = `project-keys-${project.id}-${Date.now()}`;
    const dialog = node('dialog', { class: 'project-keys-dialog', 'aria-labelledby': dialogID });
    const header = node('header', {}, node('h2', { id: dialogID }, `SDK keys for ${project.name}`), button('Close', () => dialog.close(), 'small'));
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const success = node('section', { class: 'sdk-key-created', hidden: true, 'aria-label': 'New SDK key' });
    const content = node('div', { class: 'sdk-key-content' });
    const label = field('Key label', 'text', { required: true, maxlength: 200, autocomplete: 'off' });
    const window = field('Rate limit window (seconds)', 'number', { min: 1, max: 604800, step: 1, inputmode: 'numeric' });
    const count = field('Rate limit count', 'number', { min: 1, max: 1000000000, step: 1, inputmode: 'numeric' });
    const editorTitle = node('h3', {}, 'Create SDK key');
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Create SDK key');
    const cancelEdit = button('Cancel edit', () => resetEditor(), 'small');
    cancelEdit.hidden = true;
    const form = node('form', { class: 'sdk-key-form form-stack' }, editorTitle,
      node('div', { class: 'sdk-key-fields' }, label.wrap, window.wrap, count.wrap),
      node('p', { class: 'small muted' }, 'Leave both rate-limit fields empty for no limit. Existing Sentry SDKs use the DSN shown for each key.'),
      error, node('div', { class: 'actions' }, cancelEdit, submit));
    const list = node('div', { class: 'sdk-key-list', 'aria-live': 'polite' });
    const pagination = node('div', { class: 'pagination sdk-key-pagination' });
    let currentCursor = '';
    let page = 1;
    let nextCursor = null;
    let previousCursor = null;
    let loadSequence = 0;
    let editing = null;
    let pending = null;

    const active = () => dialog.open && dialog.isConnected && state.organization?.slug === organization;
    const rateFromFields = () => {
      const rawWindow = window.input.value.trim();
      const rawCount = count.input.value.trim();
      if (!rawWindow && !rawCount) return null;
      if (!rawWindow || !rawCount) throw new Error('Enter both rate-limit values, or leave both empty.');
      const seconds = Number(rawWindow);
      const events = Number(rawCount);
      if (!Number.isSafeInteger(seconds) || seconds < 1 || seconds > 604800) throw new Error('Rate limit window must be an integer from 1 to 604800 seconds.');
      if (!Number.isSafeInteger(events) || events < 1 || events > 1000000000) throw new Error('Rate limit count must be an integer from 1 to 1000000000.');
      return { window: seconds, count: events };
    };

    function resetEditor(key = null) {
      editing = key;
      error.textContent = '';
      editorTitle.textContent = key ? 'Edit SDK key' : 'Create SDK key';
      submit.textContent = key ? 'Save SDK key' : 'Create SDK key';
      cancelEdit.hidden = !key;
      label.input.value = key?.label || key?.name || '';
      window.input.value = key?.rateLimit?.window ?? '';
      count.input.value = key?.rateLimit?.count ?? '';
    }

    function confirmation() {
      if (!pending) return null;
      const { key, action } = pending;
      const actionName = action === 'delete' ? 'delete' : action === 'activate' ? 'activate' : 'deactivate';
      const confirmLabel = action === 'delete' ? 'Confirm delete key' : action === 'activate' ? 'Confirm activation' : 'Confirm deactivation';
      const section = node('section', { class: 'sdk-key-confirmation', role: 'group', 'aria-label': `Confirm ${actionName}` },
        node('p', {}, action === 'delete'
          ? `Delete ${key.label} permanently? This cannot be undone.`
          : `${action === 'activate' ? 'Activate' : 'Deactivate'} ${key.label}? ${action === 'activate' ? 'Ingestion will resume for this key.' : 'New events sent with this key will be rejected.'}`),
        node('div', { class: 'actions' }, button(confirmLabel, async event => {
          const confirm = event.currentTarget;
          confirm.disabled = true;
          try {
            const url = `${path}${encodeURIComponent(key.id)}/`;
            if (action === 'delete') await request(url, { method: 'DELETE' });
            else await request(url, { method: 'PUT', body: { label: key.label || key.name || '', rateLimit: key.rateLimit, isActive: action === 'activate' } });
            if (!active()) return;
            pending = null;
            if (action === 'delete') {
              notify('SDK key deleted.');
              if (editing?.id === key.id) resetEditor();
            } else notify(`SDK key ${action === 'activate' ? 'activated' : 'deactivated'}.`);
            await loadPage(currentCursor, page);
          } catch (failure) {
            if (active()) { formError(error, failure); confirm.disabled = false; }
          }
        }, action === 'delete' ? 'danger small' : 'primary small'), button('Cancel', () => { pending = null; drawRows(lastKeys); }, 'small')));
      return section;
    }

    let lastKeys = [];
    function drawRows(keys) {
      lastKeys = keys;
      const columns = ['Key', 'DSN', 'Rate limit', 'Created', 'Status'];
      if (canManage()) columns.push('Actions');
      const rows = keys.map(key => {
        const dsn = keyDSN(project, key);
        const actions = canManage() ? node('div', { class: 'actions sdk-key-actions' },
          button('Edit', () => { pending = null; resetEditor(key); label.input.focus(); }, 'small'),
          button(key.isActive ? 'Deactivate' : 'Activate', () => { pending = { key, action: key.isActive ? 'deactivate' : 'activate' }; drawRows(lastKeys); }, 'small'),
          button('Delete', () => { pending = { key, action: 'delete' }; drawRows(lastKeys); }, 'danger small')) : null;
        return node('tr', {},
          node('td', { class: 'sdk-key-name' }, node('strong', {}, key.label || key.name || 'Unnamed key'), node('div', { class: 'muted small mono' }, key.id)),
          node('td', { class: 'sdk-key-dsn' }, node('code', {}, dsn), button('Copy DSN', () => copyValue(dsn, 'DSN'), 'small')),
          node('td', {}, rateLimitText(key)),
          node('td', {}, node('time', { datetime: key.dateCreated }, date(key.dateCreated))),
          node('td', {}, node('span', { class: `badge badge-sm ${key.isActive ? 'resolved' : 'ignored'}` }, key.isActive ? 'Active' : 'Inactive')),
          canManage() ? node('td', {}, actions) : null);
      });
      const table = node('div', { class: 'table-scroll sdk-key-table-scroll' }, node('table', { class: 'sdk-key-table' },
        node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))),
        node('tbody', {}, rows)));
      const confirmationPanel = confirmation();
      list.replaceChildren(keys.length ? table : node('div', { class: 'empty sdk-key-empty' }, node('span', { class: 'empty-icon', 'aria-hidden': true }, '◎'), node('h3', {}, 'No SDK keys yet'), node('p', {}, 'Create a key to configure your existing Sentry SDK.')),
        confirmationPanel);
      const pageItems = keys.length === 1 ? '1 key' : `${number(keys.length)} keys`;
      pagination.replaceChildren(node('span', { class: 'small muted' }, `${pageItems} · Page ${page}`),
        node('div', { class: 'actions' },
          button('Previous', () => { if (previousCursor !== null) void loadPage(previousCursor, Math.max(1, page - 1)); }, 'small'),
          button('Next', () => { if (nextCursor !== null) void loadPage(nextCursor, page + 1); }, 'small')));
      pagination.querySelector('button:first-of-type').disabled = previousCursor === null;
      pagination.querySelector('button:last-of-type').disabled = nextCursor === null;
    }

    async function loadPage(cursor, pageNumber) {
      const sequence = ++loadSequence;
      currentCursor = cursor;
      page = pageNumber;
      nextCursor = null;
      previousCursor = null;
      list.replaceChildren(node('div', { class: 'panel-body muted', role: 'status', 'aria-busy': 'true' }, 'Loading SDK keys…'));
      pagination.replaceChildren();
      try {
        const query = new URLSearchParams({ limit: String(pageSize) });
        if (cursor) query.set('cursor', cursor);
        const result = await request(`${path}?${query}`, { withLinks: true });
        if (!active() || sequence !== loadSequence) return;
        nextCursor = linkedCursor(result.link, 'next');
        previousCursor = linkedCursor(result.link, 'previous');
        drawRows(result.data || []);
      } catch (failure) {
        if (!active() || sequence !== loadSequence) return;
        list.replaceChildren(node('div', { class: 'error-state', role: 'alert' }, node('h3', {}, 'Could not load SDK keys'), node('p', {}, failure.message), button('Try again', () => { void loadPage(currentCursor, page); }, 'small')));
      }
    }

    form.addEventListener('submit', async event => {
      event.preventDefault();
      if (!active()) return;
      error.textContent = '';
      let rateLimit;
      try { rateLimit = rateFromFields(); }
      catch (failure) { formError(error, failure); return; }
      submit.disabled = true;
      const selected = editing;
      const body = { label: label.input.value, rateLimit };
      try {
        const result = await request(selected ? `${path}${encodeURIComponent(selected.id)}/` : path, {
          method: selected ? 'PUT' : 'POST', body,
        });
        if (!active()) return;
        resetEditor();
        if (!selected && result) {
          const dsn = keyDSN(project, result);
          success.replaceChildren(node('h3', {}, 'New SDK key'), node('p', { class: 'small muted' }, 'Copy this DSN into your existing Sentry SDK configuration.'),
            node('div', { class: 'sdk-key-created-value' }, node('code', {}, dsn), button('Copy DSN', () => copyValue(dsn, 'DSN'), 'small')));
          success.hidden = false;
          success.scrollIntoView({ block: 'nearest' });
        }
        pending = null;
        notify(selected ? 'SDK key updated.' : 'SDK key created.');
        await loadPage(currentCursor, page);
      } catch (failure) {
        if (active()) formError(error, failure);
      } finally { submit.disabled = false; }
    });

    dialog.append(header, success, content);
    content.append(list, pagination);
    if (canManage()) content.append(form);
    dialog.addEventListener('close', () => { dialogs.delete(dialog); dialog.remove(); });
    dialogs.add(dialog);
    document.body.append(dialog);
    dialog.showModal();
    void loadPage('', 1);
    if (canManage()) label.input.focus(); else header.querySelector('button')?.focus();
  }

  return { open, closeDialogs };
}
