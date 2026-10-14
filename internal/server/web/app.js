(() => {
  'use strict';

  const app = document.getElementById('app');
  const notifications = document.getElementById('notifications');
  const configuredBase = document.querySelector('meta[name="goodsentry-base"]')?.content || '';
  const base = configuredBase === '__GOODSENTRY_BASE__' ? '' : configuredBase.replace(/\/$/, '');
  const pageSize = 25;
  const state = { me: null, organization: null, projects: [], requestVersion: 0 };
  const commentDrafts = new Map();
  let fieldNumber = 0;
  let notificationTimer;

  function node(tag, attributes = {}, ...children) {
    const element = document.createElement(tag);
    for (const [name, value] of Object.entries(attributes)) {
      if (value === undefined || value === null || value === false) continue;
      if (name === 'class') element.className = value;
      else if (name.startsWith('on') && typeof value === 'function') element.addEventListener(name.slice(2), value);
      else element.setAttribute(name, value === true ? (name.startsWith('aria-') ? 'true' : '') : String(value));
    }
    for (const child of children.flat(Infinity)) {
      if (child !== undefined && child !== null && child !== false) {
        element.append(child instanceof Node ? child : document.createTextNode(String(child)));
      }
    }
    return element;
  }

  function records(value) {
    return Array.isArray(value) ? value.filter(item => item && typeof item === 'object') : [];
  }

  function text(value, fallback = 'Not provided') {
    if (value === null || value === undefined || value === '') return fallback;
    if (typeof value === 'object') return JSON.stringify(value, null, 2);
    return String(value);
  }

  function date(value, short = false) {
    if (!value) return 'Not provided';
    const parsed = new Date(typeof value === 'number' ? value * 1000 : value);
    if (Number.isNaN(parsed.getTime())) return text(value);
    return new Intl.DateTimeFormat(undefined, short
      ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }
      : { dateStyle: 'medium', timeStyle: 'short' }).format(parsed);
  }

  function number(value) {
    return new Intl.NumberFormat().format(Number(value) || 0);
  }

  function notify(message) {
    clearTimeout(notificationTimer);
    notifications.textContent = message;
    notificationTimer = setTimeout(() => { notifications.textContent = ''; }, 5000);
  }

  async function request(path, options = {}) {
    let response;
    const headers = { Accept: 'application/json', ...options.headers };
    if (options.body !== undefined) headers['Content-Type'] = 'application/json';
    if (options.method && options.method !== 'GET' && state.me?.csrfToken) {
      headers['X-CSRF-Token'] = state.me.csrfToken;
    }
    try {
      response = await fetch(base + path, {
        ...options, credentials: 'same-origin', headers,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
      });
    } catch (_) {
      throw new Error('Cannot reach GoodSentry. Check your connection and try again.');
    }
    if (response.status === 204) return null;
    const data = await response.json().catch(() => null);
    if (!response.ok) {
      if (response.status === 401 && state.me) {
        state.me = null;
        state.requestVersion++;
        renderAuth(false);
        notify('Your session has ended. Sign in to continue.');
      }
      const error = new Error(data?.detail || `The request failed (${response.status}). Try again.`);
      error.status = response.status;
      throw error;
    }
    return data;
  }

  function button(label, click, kind = '') {
    return node('button', { type: 'button', class: `button ${kind}`, onclick: click }, label);
  }

  function field(label, type = 'text', attributes = {}) {
    const id = `field-${++fieldNumber}`;
    const input = node(type === 'textarea' ? 'textarea' : 'input', { id, ...(type === 'textarea' ? {} : { type }), ...attributes });
    return { input, wrap: node('div', { class: 'field' }, node('label', { for: id }, label), input) };
  }

  function selectField(label, options, value, attributes = {}) {
    const id = `field-${++fieldNumber}`;
    const input = node('select', { id, ...attributes }, options.map(([key, name]) => node('option', { value: key }, name)));
    input.value = value;
    return { input, wrap: node('div', { class: 'field' }, node('label', { for: id }, label), input) };
  }

  function formError(target, error) {
    target.textContent = error.message;
    target.focus();
  }

  function routeURL(changes = {}) {
    const url = new URL(location.href);
    for (const [key, value] of Object.entries(changes)) {
      if (value === null || value === undefined || value === '') url.searchParams.delete(key);
      else url.searchParams.set(key, String(value));
    }
    url.hash = '';
    return url.pathname + url.search;
  }

  function navigate(changes) {
    history.pushState(null, '', routeURL(changes));
    loadRoute(true);
  }

  function routeLink(label, changes, attributes = {}) {
    return node('a', {
      href: routeURL(changes), ...attributes,
      onclick: event => {
        if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        navigate(changes);
      },
    }, label);
  }

  function brand() {
    return node('div', { class: 'brand' }, node('span', { class: 'brand-mark', 'aria-hidden': true }, 'G'), 'GoodSentry');
  }

  function renderAuth(setup) {
    document.querySelectorAll('dialog[open]').forEach(dialog => dialog.close());
    state.projects = [];
    commentDrafts.clear();
    document.title = setup ? 'Set up GoodSentry' : 'Sign in · GoodSentry';
    app.setAttribute('aria-busy', 'false');
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Your name', 'text', { name: 'name', autocomplete: 'name', required: true, maxlength: 200 });
    const organization = field('Organization name', 'text', { name: 'organization', autocomplete: 'organization', required: true, maxlength: 200 });
    const email = field('Email', 'email', { name: 'email', autocomplete: 'username', required: true, maxlength: 254 });
    const password = field('Password', 'password', { name: 'password', autocomplete: setup ? 'new-password' : 'current-password', required: true, ...(setup ? { minlength: 12 } : {}) });
    if (setup) password.wrap.append(node('p', { class: 'help' }, 'Use at least 12 characters.'));
    const submit = node('button', { type: 'submit', class: 'button primary' }, setup ? 'Create workspace' : 'Sign in');
    const form = node('form', { class: 'form-stack' }, setup ? [name.wrap, organization.wrap] : [], email.wrap, password.wrap, submit);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = setup ? 'Creating workspace…' : 'Signing in…';
      try {
        state.me = await request(setup ? '/api/v1/setup' : '/api/v1/login', {
          method: 'POST', body: { email: email.input.value, password: password.input.value, ...(setup ? { name: name.input.value, organization: organization.input.value } : {}) },
        });
        await loadRoute(true);
      } catch (failure) {
        formError(error, failure);
      } finally {
        submit.disabled = false;
        submit.textContent = setup ? 'Create workspace' : 'Sign in';
      }
    });
    const intro = node('aside', { class: 'auth-intro' }, brand(),
      node('div', {}, node('h2', {}, 'See the error. Find the cause.'), node('p', {}, 'Your application errors, stack traces and release context in one workspace.')),
      node('div', { class: 'auth-foot' }, 'GoodSentry · Error tracking'));
    const box = node('div', { class: 'auth-box' }, node('div', { class: 'eyebrow' }, setup ? 'First-time setup' : 'Your workspace'),
      node('h1', { tabindex: '-1' }, setup ? 'Welcome to GoodSentry' : 'Sign in to GoodSentry'),
      node('p', {}, setup ? 'Create the first owner account and organization. Then connect your application with its project DSN.' : 'Use your account to view and manage application errors.'), error, form);
    app.replaceChildren(node('div', { class: 'auth-layout' }, intro, node('main', { id: 'main', class: 'auth-main' }, box)));
    (setup ? name : email).input.focus();
  }

  function renderShell(view) {
    const organizations = state.me.organizations || [];
    const org = state.organization;
    const selector = node('select', { class: 'org-select', 'aria-label': 'Organization', onchange: event => {
      navigate({ org: event.target.value, issue: null, project: null, page: null });
    } }, organizations.map(item => node('option', { value: item.slug }, item.name)));
    if (org) selector.value = org.slug;
    const nav = node('nav', { class: 'nav', 'aria-label': 'Main navigation' },
      routeLink([node('span', { class: 'nav-icon', 'aria-hidden': true }, '◉'), 'Issues'], { view: 'issues', issue: null, page: null }, { 'aria-current': view === 'issues' ? 'page' : null }),
      routeLink([node('span', { class: 'nav-icon', 'aria-hidden': true }, '▦'), 'Projects'], { view: 'projects', issue: null, page: null }, { 'aria-current': view === 'projects' ? 'page' : null }));
    const logout = node('button', { type: 'button', onclick: async () => {
      logout.disabled = true;
      try {
        await request('/api/v1/logout', { method: 'POST' });
        state.me = null;
        state.requestVersion++;
        renderAuth(false);
      } catch (error) { notify(error.message); logout.disabled = false; }
    } }, 'Sign out');
    const sidebar = node('aside', { class: 'sidebar' }, brand(),
      node('div', { class: 'org-switch' }, node('p', { class: 'org-label' }, 'Organization'), selector), nav,
      node('div', { class: 'account' }, node('strong', {}, text(state.me.user.name, state.me.user.email)), node('span', { class: 'small' }, state.me.user.email), logout));
    const main = node('main', { id: 'main', class: 'main', tabindex: '-1' });
    app.replaceChildren(node('div', { class: 'app-shell' }, sidebar, node('div', { class: 'content' },
      node('header', { class: 'topbar' }, node('span', {}, node('strong', {}, org?.name || 'Workspace'), ' / ', view === 'projects' ? 'Projects' : 'Issues'), node('span', { class: 'server-label' }, 'Application monitoring')), main)));
    app.setAttribute('aria-busy', 'false');
    return main;
  }

  function loading(target, message) {
    target.replaceChildren(node('div', { class: 'page-header' }, node('h1', {}, message)), node('div', { class: 'panel loading-rows', role: 'status', 'aria-label': message, 'aria-busy': 'true' },
      [1, 2, 3].map(() => node('div', { class: 'loading-row', 'aria-hidden': true }, node('div', { class: 'skeleton wide' }), node('div', { class: 'skeleton' })))));
  }

  function errorState(target, error, retry) {
    target.replaceChildren(node('div', { class: 'error-state', role: 'alert' }, node('h2', {}, 'Could not load this page'), node('p', {}, error.message), button('Try again', retry)));
  }

  function empty(title, description, action) {
    return node('div', { class: 'empty' }, node('span', { class: 'empty-icon', 'aria-hidden': true }, '◎'), node('h2', {}, title), node('p', {}, description), action);
  }

  async function loadRoute(focus = false) {
    if (!state.me) return;
    const version = ++state.requestVersion;
    const params = new URLSearchParams(location.search);
    state.organization = state.me.organizations?.find(item => item.slug === params.get('org')) || state.me.organizations?.[0] || null;
    const view = params.get('view') === 'projects' ? 'projects' : 'issues';
    const main = renderShell(view);
    if (!state.organization) {
      main.append(node('div', { class: 'panel no-org' }, empty('No organization access', 'Your account does not belong to an organization. Contact your workspace owner to request access.')));
      return;
    }
    loading(main, params.get('issue') ? 'Loading issue…' : view === 'projects' ? 'Loading projects…' : 'Loading issues…');
    try {
      const projects = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects`);
      if (version !== state.requestVersion) return;
      state.projects = projects.data || [];
      if (params.get('issue') && view === 'issues') {
        const detail = await request(`/api/v1/issues/${encodeURIComponent(params.get('issue'))}`);
        if (version !== state.requestVersion) return;
        renderIssue(main, detail);
      } else if (view === 'projects') {
        renderProjects(main);
      } else {
        const query = new URLSearchParams();
        for (const name of ['project', 'query', 'environment', 'status', 'sort', 'page']) {
          if (params.get(name)) query.set(name, params.get(name));
        }
        if (!query.has('status')) query.set('status', 'unresolved');
        if (!query.has('sort')) query.set('sort', 'date');
        const issues = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/issues?${query}`);
        if (version !== state.requestVersion) return;
        renderIssues(main, issues, params);
      }
      if (focus) main.querySelector('h1')?.focus();
    } catch (error) {
      if (version === state.requestVersion) errorState(main, error, () => loadRoute(focus));
    }
  }

  function pageHeader(title, description, action) {
    return node('div', { class: 'page-header' }, node('div', {}, node('div', { class: 'eyebrow' }, state.organization.name), node('h1', { tabindex: '-1' }, title), description ? node('p', {}, description) : null), action);
  }

  function badge(value) {
    const known = ['unresolved', 'resolved', 'ignored', 'error', 'fatal', 'warning', 'info', 'debug'];
    return node('span', { class: `badge ${known.includes(value) ? value : ''}` }, text(value, 'Unknown'));
  }

  function renderIssues(main, result, params) {
    document.title = 'Issues · GoodSentry';
    main.replaceChildren(pageHeader('Issues', 'Find, investigate and triage application errors.', node('div', { class: 'actions' }, button('Refresh', () => loadRoute()), button('Create project', openProjectDialog, 'primary'))));
    if (!state.projects.length) {
      main.append(node('section', { class: 'panel' }, empty('Connect your first application', 'Create a project to get a DSN. Set it in your existing Sentry SDK to start receiving errors.', button('Create project', openProjectDialog, 'primary'))));
      return;
    }
    const search = field('Search issues', 'search', { name: 'query', placeholder: 'Message or exception', value: params.get('query') || '' });
    search.wrap.classList.add('search-field');
    const project = selectField('Project', [['', 'All projects'], ...state.projects.map(item => [String(item.id), item.name])], params.get('project') || '');
    const status = selectField('Status', [['unresolved', 'Unresolved'], ['resolved', 'Resolved'], ['ignored', 'Ignored']], params.get('status') || 'unresolved');
    const environment = field('Environment', 'text', { name: 'environment', value: params.get('environment') || '', placeholder: 'All environments' });
    const sort = selectField('Sort by', [['date', 'Last seen'], ['freq', 'Event count']], params.get('sort') || 'date');
    const filters = node('form', { class: 'filters', role: 'search', 'aria-label': 'Filter issues' }, search.wrap, project.wrap, status.wrap, environment.wrap, sort.wrap, node('button', { class: 'button', type: 'submit' }, 'Apply filters'));
    filters.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ query: search.input.value.trim(), project: project.input.value, status: status.input.value, environment: environment.input.value.trim(), sort: sort.input.value, page: null, issue: null });
    });
    main.append(filters);
    const issues = result.data || [];
    const panel = node('section', { class: 'panel', 'aria-label': 'Issues' });
    if (!issues.length) {
      panel.append(empty('No matching issues', 'Errors will appear here when your SDK sends them. Check the selected project, status and environment, or clear your filters.', button('Clear filters', () => navigate({ query: null, project: null, status: null, environment: null, sort: null, page: null }))));
    } else {
      const rows = issues.map(issue => node('tr', {},
        node('td', { class: 'issue-column' }, routeLink(issue.title, { issue: issue.id }, { class: 'issue-link issue-title' }),
          node('div', { class: 'issue-subtitle' }, node('span', { class: 'mono' }, issue.shortId || issue.id), node('span', {}, issue.project?.name || 'Project'), issue.environment ? node('span', {}, issue.environment) : null)),
        node('td', {}, badge(issue.level)), node('td', {}, badge(issue.status)), node('td', { class: 'nowrap' }, number(issue.count)),
        node('td', { class: 'nowrap' }, node('time', { datetime: issue.lastSeen, title: date(issue.lastSeen) }, date(issue.lastSeen, true)))));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, ['Issue', 'Level', 'Status', 'Events', 'Last seen'].map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const total = Number(result.total) || 0;
    const previous = button('Previous', () => navigate({ page: page - 1 }));
    const next = button('Next', () => navigate({ page: page + 1 }));
    previous.disabled = page <= 1;
    next.disabled = page * pageSize >= total;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(total)} ${total === 1 ? 'issue' : 'issues'} · Page ${page}`), node('div', { class: 'actions' }, previous, next)));
    main.append(panel);
  }

  async function copyDSN(value) {
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(value);
      else {
        const copy = node('textarea', { class: 'copy-buffer', 'aria-label': 'DSN to copy', readonly: true }, value);
        document.body.append(copy);
        copy.select();
        const succeeded = document.execCommand('copy');
        copy.remove();
        if (!succeeded) throw new Error('Copy unavailable');
      }
      notify('DSN copied.');
    } catch (_) { notify('Copy unavailable. Select the DSN text and copy it manually.'); }
  }

  function renderProjects(main) {
    document.title = 'Projects · GoodSentry';
    main.replaceChildren(pageHeader('Projects', 'Each application gets its own SDK key and DSN.', button('Create project', openProjectDialog, 'primary')));
    const panel = node('section', { class: 'panel', 'aria-label': 'Projects' });
    if (!state.projects.length) {
      panel.append(empty('Create your first project', 'Choose a name and platform, then copy the project DSN into your Sentry SDK configuration.', button('Create project', openProjectDialog, 'primary')));
    } else {
      const rows = state.projects.map(project => node('tr', {},
        node('td', {}, node('div', { class: 'project-name' }, project.name), node('div', { class: 'muted small mono' }, project.slug)),
        node('td', {}, badge(project.platform)),
        node('td', {}, node('div', { class: 'dsn' }, node('code', {}, project.dsn), button('Copy', () => copyDSN(project.dsn), 'small'))),
        node('td', {}, routeLink('View issues', { view: 'issues', project: project.id, query: null, status: null, environment: null, page: null }, { class: 'button small' }))));
      const columns = ['Project', 'Platform', 'DSN', ''].map((name, index) => node('th', { scope: 'col', ...(index === 3 ? { 'aria-label': 'Actions' } : {}) }, name));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns)), node('tbody', {}, rows))));
      main.append(node('p', { class: 'muted small connection-help' }, 'Use the DSN with your existing Sentry SDK. Keep the SDK configuration in your application; GoodSentry receives events at the project address.'));
    }
    main.append(panel);
  }

  function openProjectDialog() {
    const dialog = node('dialog', { 'aria-labelledby': 'project-dialog-title' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Project name', 'text', { name: 'name', required: true, maxlength: 200, autocomplete: 'off' });
    const platform = selectField('Platform', [['javascript', 'JavaScript / Browser'], ['node', 'Node.js'], ['python', 'Python'], ['go', 'Go'], ['java', 'Java'], ['php', 'PHP'], ['ruby', 'Ruby'], ['csharp', '.NET'], ['other', 'Other']], 'javascript');
    const submit = node('button', { type: 'submit', class: 'button primary' }, 'Create project');
    const close = () => dialog.close();
    const form = node('form', { class: 'form-stack' }, error, name.wrap, platform.wrap,
      node('div', { class: 'dialog-actions' }, button('Cancel', close), submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = 'Creating…';
      try {
        await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects`, { method: 'POST', body: { name: name.input.value.trim(), platform: platform.input.value } });
        dialog.close();
        navigate({ view: 'projects', issue: null, page: null });
        notify('Project created. Copy its DSN to connect your application.');
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = 'Create project'; }
    });
    dialog.append(node('header', {}, node('h2', { id: 'project-dialog-title' }, 'Create project'), button('Close', close, 'small')), form);
    dialog.addEventListener('close', () => dialog.remove());
    document.body.append(dialog);
    dialog.showModal();
    name.input.focus();
  }

  function pairs(value) {
    if (Array.isArray(value)) return value.filter(item => Array.isArray(item) && item.length >= 2);
    if (value && typeof value === 'object') return Object.entries(value);
    return [];
  }

  function keyValues(values) {
    const entries = pairs(values);
    if (!entries.length) return node('p', { class: 'muted small' }, 'None recorded.');
    return node('dl', { class: 'key-values' }, entries.map(([key, value]) => [node('dt', {}, key), node('dd', {}, text(value))]));
  }

  function detailPanel(title, content) {
    return node('section', { class: 'panel' }, node('div', { class: 'panel-title' }, node('h2', {}, title)), content);
  }

  function stackFrame(frame) {
    const location = [frame.filename || frame.abs_path || frame.module || 'Unknown file', frame.lineno, frame.colno].filter(value => value !== undefined && value !== null && value !== '').join(':');
    const row = node('li', { class: 'stack-frame' }, node('div', { class: 'frame-heading' }, node('span', { class: 'frame-function' }, frame.function || '<anonymous>'), frame.in_app ? node('span', { class: 'badge' }, 'App frame') : null), node('div', { class: 'frame-path' }, location));
    if (frame.context_line !== undefined && frame.context_line !== null) {
      const before = Array.isArray(frame.pre_context) ? frame.pre_context : [];
      const after = Array.isArray(frame.post_context) ? frame.post_context : [];
      const currentLine = Number(frame.lineno) || 0;
      const source = node('div', { class: 'source-context', 'aria-label': 'Source context' });
      [...before, frame.context_line, ...after].forEach((line, index) => {
        const lineNumber = currentLine ? currentLine - before.length + index : '';
        source.append(node('div', { class: `source-line ${index === before.length ? 'current' : ''}` }, node('span', { class: 'line-number', 'aria-hidden': true }, lineNumber), node('span', {}, text(line, ''))));
      });
      row.append(source);
    }
    return row;
  }

  function stackTrace(event) {
    const exceptions = records(Array.isArray(event.exception) ? event.exception : event.exception?.values);
    const content = node('div');
    for (const exception of exceptions) {
      content.append(node('div', { class: 'exception-summary' }, node('h3', {}, exception.type || 'Exception'), exception.value ? node('p', {}, exception.value) : null));
      const frames = records(exception.stacktrace?.frames);
      if (frames.length) content.append(node('ol', { class: 'stack-list', 'aria-label': 'Stack frames, most recent first' }, [...frames].reverse().map(stackFrame)));
      else content.append(node('div', { class: 'panel-body muted' }, 'No stack frames were recorded for this exception.'));
    }
    if (!exceptions.length) {
      const frames = records(event.stacktrace?.frames || records(event.threads?.values).find(thread => thread.crashed)?.stacktrace?.frames);
      if (frames.length) content.append(node('ol', { class: 'stack-list', 'aria-label': 'Stack frames, most recent first' }, [...frames].reverse().map(stackFrame)));
      else content.append(node('div', { class: 'panel-body muted' }, 'No stack trace was recorded for this event.'));
    }
    return detailPanel('Stack trace', content);
  }

  function breadcrumbs(event) {
    const entries = records(Array.isArray(event.breadcrumbs) ? event.breadcrumbs : event.breadcrumbs?.values);
    return detailPanel('Breadcrumbs', entries.length ? node('ol', { class: 'breadcrumb-list' }, entries.map(item => node('li', { class: 'breadcrumb' },
      node('div', { class: 'breadcrumb-heading' }, node('span', {}, [item.category, item.type].filter(Boolean).join(' · ') || 'Breadcrumb'), node('time', {}, date(item.timestamp, true))),
      item.message ? node('p', {}, item.message) : null, item.data ? node('pre', { class: 'muted small' }, text(item.data)) : null))) : node('div', { class: 'panel-body muted' }, 'No breadcrumbs were recorded.'));
  }

  function renderIssue(main, detail) {
    const issue = detail.issue;
    const event = detail.event || {};
    document.title = `${issue.title} · GoodSentry`;
    const statusLabel = badge(issue.status);
    const actionError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const statusActions = node('div', { class: 'actions', 'aria-label': 'Issue status' });
    const statusButtons = [];
    for (const [status, label] of [['unresolved', 'Reopen'], ['resolved', 'Resolve'], ['ignored', 'Ignore']]) {
      if (issue.status === status) continue;
      const control = button(label, async () => {
        statusButtons.forEach(item => { item.disabled = true; });
        actionError.textContent = '';
        try {
          const updated = await request(`/api/v1/issues/${encodeURIComponent(issue.id)}`, { method: 'PATCH', body: { status } });
          renderIssue(main, { ...detail, issue: updated });
          main.querySelector('h1')?.focus();
          notify(`Issue ${status === 'unresolved' ? 'reopened' : status}.`);
        } catch (failure) {
          formError(actionError, failure);
          statusButtons.forEach(item => { item.disabled = false; });
        }
      }, status === 'resolved' ? 'primary' : '');
      statusButtons.push(control);
      statusActions.append(control);
    }
    const heading = node('div', { class: 'page-header' }, node('div', {},
      node('div', { class: 'eyebrow' }, issue.shortId || issue.id), node('h1', { class: 'detail-title', tabindex: '-1' }, issue.title),
      node('div', { class: 'detail-meta' }, statusLabel, badge(issue.level), node('span', { class: 'muted' }, issue.project?.name || 'Project'), issue.environment ? node('span', { class: 'badge' }, issue.environment) : null)), statusActions);
    const stats = node('dl', { class: 'stat-list' }, [['Events', number(issue.count)], ['First seen', date(issue.firstSeen, true)], ['Last seen', date(issue.lastSeen, true)]].map(([name, value]) => node('div', { class: 'stat' }, node('dt', {}, name), node('dd', {}, value))));
    const summary = node('section', { class: 'panel' }, stats, node('div', { class: 'panel-body' },
      node('p', { class: 'muted small' }, 'Latest event'), node('p', { class: 'mono' }, text(event.event_id || event.eventID)),
      event.message || event.logentry?.formatted ? node('pre', { class: 'event-message' }, text(event.message || event.logentry.formatted)) : null));
    const left = node('div', {}, summary, stackTrace(event), breadcrumbs(event), commentsPanel(issue, detail.comments || []));
    left.append(node('details', { class: 'panel' }, node('summary', {}, 'Original event JSON'), node('pre', { class: 'event-json' }, JSON.stringify(event, null, 2))));
    const meta = [['Platform', event.platform || issue.platform], ['Environment', event.environment || issue.environment], ['Release', event.release || issue.release], ['SDK', event.sdk ? [event.sdk.name, event.sdk.version].filter(Boolean).join(' ') : null]];
    const right = node('aside', { class: 'detail-aside', 'aria-label': 'Event details' },
      detailPanel('Event details', node('div', { class: 'panel-body' }, keyValues(meta))),
      detailPanel('Tags', node('div', { class: 'panel-body' }, keyValues(event.tags))),
      detailPanel('User', node('div', { class: 'panel-body' }, keyValues(event.user))));
    if (event.contexts && Object.keys(event.contexts).length) right.append(detailPanel('Contexts', node('div', { class: 'panel-body' }, Object.entries(event.contexts).map(([name, context]) => node('section', { class: 'context-block' }, node('h3', {}, name), typeof context === 'object' && context ? keyValues(context) : node('p', {}, text(context)))))));
    if (event.request) right.append(detailPanel('Request', node('div', { class: 'panel-body' }, keyValues(event.request))));
    if (event.extra && Object.keys(event.extra).length) right.append(detailPanel('Extra', node('div', { class: 'panel-body' }, keyValues(event.extra))));
    main.replaceChildren(routeLink('← All issues', { issue: null }, { class: 'back-link' }), heading, actionError, node('div', { class: 'detail-grid' }, left, right));
  }

  function commentsPanel(issue, comments) {
    const list = node('ol', { class: 'comment-list', 'aria-label': 'Comments' });
    const renderComment = comment => node('li', { class: 'comment' }, node('div', { class: 'comment-top' }, node('strong', {}, comment.user?.name || comment.user?.email || 'Member'), node('time', { datetime: comment.dateCreated }, date(comment.dateCreated))), node('p', {}, comment.text));
    list.append(...comments.map(renderComment));
    const emptyMessage = node('p', { class: 'panel-body muted' }, 'No comments yet. Share what you found with your team.');
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const comment = field('Add a comment', 'textarea', { name: 'text', required: true, maxlength: 10000, rows: 3, placeholder: 'What should the team know?' });
    comment.input.value = commentDrafts.get(issue.id) || '';
    comment.input.addEventListener('input', () => commentDrafts.set(issue.id, comment.input.value));
    const submit = node('button', { class: 'button primary', type: 'submit' }, 'Post comment');
    const form = node('form', { class: 'comment-form form-stack' }, error, comment.wrap, node('div', { class: 'actions' }, submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      const value = comment.input.value.trim();
      if (!value) { formError(error, new Error('Enter a comment before posting.')); return; }
      submit.disabled = true;
      submit.textContent = 'Posting…';
      error.textContent = '';
      try {
        const created = await request(`/api/v1/issues/${encodeURIComponent(issue.id)}/comments`, { method: 'POST', body: { text: value } });
        list.append(renderComment(created));
        comments.push(created);
        emptyMessage.remove();
        comment.input.value = '';
        commentDrafts.delete(issue.id);
        notify('Comment posted.');
        comment.input.focus();
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = 'Post comment'; }
    });
    return detailPanel('Comments', node('div', {}, list, comments.length ? null : emptyMessage, form));
  }

  async function initialize() {
    try {
      const setup = await request('/api/v1/setup');
      if (setup.required) { renderAuth(true); return; }
      try { state.me = await request('/api/v1/me'); }
      catch (error) { if (error.status === 401) { renderAuth(false); return; } throw error; }
      await loadRoute();
    } catch (error) {
      app.setAttribute('aria-busy', 'false');
      const main = node('main', { id: 'main', class: 'startup' });
      app.replaceChildren(main);
      errorState(main, error, initialize);
    }
  }

  addEventListener('popstate', () => loadRoute(true));
  initialize();
})();
