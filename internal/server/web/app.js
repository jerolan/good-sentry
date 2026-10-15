(() => {
  'use strict';

  const app = document.getElementById('app');
  const notifications = document.getElementById('notifications');
  const configuredBase = document.querySelector('meta[name="goodsentry-base"]')?.content || '';
  const base = configuredBase === '__GOODSENTRY_BASE__' ? '' : configuredBase.replace(/\/$/, '');
  const pageSize = 25;
  const viewNames = { issues: 'Issues', projects: 'Projects', transactions: 'Transactions', logs: 'Logs', monitors: 'Uptime', alerts: 'Alerts' };
  const state = { me: null, organization: null, projects: [], requestVersion: 0 };
  const commentDrafts = new Map();
  let fieldNumber = 0;
  let notificationTimer;
  let alertsUI;

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

  function parseTelemetryJSON(source) {
    const parsed = JSON.parse(source);
    // Keep large integer digits as display text. Validate first, and leave
    // quoted strings untouched.
    const precise = source.replace(/"(?:\\.|[^"\\])*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g, token => {
      if (!/^-?\d+$/.test(token) || Number.isSafeInteger(Number(token))) return token;
      return JSON.stringify(token);
    });
    return precise === source ? parsed : JSON.parse(precise);
  }

  async function request(path, options = {}) {
    let response;
    const multipart = options.body instanceof FormData;
    const headers = { Accept: 'application/json', ...options.headers };
    if (options.body !== undefined && !multipart) headers['Content-Type'] = 'application/json';
    if (options.method && options.method !== 'GET' && state.me?.csrfToken) {
      headers['X-CSRF-Token'] = state.me.csrfToken;
    }
    try {
      response = await fetch(base + path, {
        ...options, credentials: 'same-origin', headers,
        body: options.body === undefined ? undefined : multipart ? options.body : JSON.stringify(options.body),
      });
    } catch (_) {
      throw new Error('Cannot reach GoodSentry. Check your connection and try again.');
    }
    if (response.status === 204) return null;
    const endpoint = path.split('?')[0];
    const data = await response.text().then(source => endpoint.endsWith('/logs') || /\/transactions\/(event|span)\/[^/]+$/.test(endpoint)
      ? parseTelemetryJSON(source) : JSON.parse(source)).catch(() => null);
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
    return node('button', { type: 'button', class: `btn button ${kind}`, onclick: click }, label);
  }

  function field(label, type = 'text', attributes = {}) {
    const id = `field-${++fieldNumber}`;
    const input = node(type === 'textarea' ? 'textarea' : 'input', { id, class: type === 'textarea' ? 'textarea' : 'input', ...(type === 'textarea' ? {} : { type }), ...attributes });
    return { input, wrap: node('div', { class: 'field' }, node('label', { for: id }, label), input) };
  }

  function selectField(label, options, value, attributes = {}) {
    const id = `field-${++fieldNumber}`;
    const input = node('select', { id, class: 'select', ...attributes }, options.map(([key, name]) => node('option', { value: key }, name)));
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

  function navigationIcon(section) {
    const paths = {
      issues: 'M12 8v4m0 4h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z',
      transactions: 'M4 17l5-6 4 3 7-9m-5 0h5v5',
      logs: 'M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01',
      monitors: 'M2 12h5l3-8 4 16 3-8h5',
      alerts: 'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4',
      projects: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
    };
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    for (const [name, value] of Object.entries({ class: 'nav-icon', viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.7', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true', focusable: 'false' })) svg.setAttribute(name, value);
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', paths[section]);
    svg.append(path);
    return svg;
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
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, setup ? 'Create workspace' : 'Sign in');
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
    const selector = node('select', { class: 'select org-select', 'aria-label': 'Organization', onchange: event => {
      navigate({ org: event.target.value, issue: null, monitor: null, project: null, alertProject: null, rule: null, delivery: null, sampleSource: null, sample: null, page: null, changes: null });
    } }, organizations.map(item => node('option', { value: item.slug }, item.name)));
    if (org) selector.value = org.slug;
    const nav = node('nav', { class: 'nav', 'aria-label': 'Main navigation' },
      ['issues', 'transactions', 'logs', 'monitors', 'alerts', 'projects']
        .filter(section => section !== 'monitors' || state.me.features?.uptime === true)
        .filter(section => section !== 'logs' || state.me.features?.logs !== false)
        .filter(section => section !== 'alerts' || (state.me.features?.alerts === true && ['owner', 'admin'].includes(org?.role)))
        .map(section =>
        routeLink([navigationIcon(section), viewNames[section]],
          { view: section, issue: null, monitor: null, alertProject: null, rule: null, delivery: null, sampleSource: null, sample: null, page: null, changes: null, query: null, status: null, sort: null, environment: null, level: null, trace: null, service: null, window: null, start: null, end: null },
          { 'aria-current': view === section ? 'page' : null })));
    const logout = node('button', { type: 'button', class: 'btn btn-sm', onclick: async () => {
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
      node('header', { class: 'topbar' }, node('span', {}, node('strong', {}, org?.name || 'Workspace'), ' / ', viewNames[view]), node('span', { class: 'server-label' }, 'Application monitoring')), main)));
    app.setAttribute('aria-busy', 'false');
    return main;
  }

  function loading(target, message) {
    target.replaceChildren(node('div', { class: 'page-header' }, node('h1', {}, message)), node('div', { class: 'card panel loading-rows', role: 'status', 'aria-label': message, 'aria-busy': 'true' },
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
    const requestedView = params.get('view');
    const view = Object.prototype.hasOwnProperty.call(viewNames, requestedView) ? requestedView : 'issues';
    const main = renderShell(view);
    if (!state.organization) {
      main.append(node('div', { class: 'card panel no-org' }, empty('No organization access', 'Your account does not belong to an organization. Contact your workspace owner to request access.')));
      return;
    }
    if ((view === 'monitors' && state.me.features?.uptime !== true) || (view === 'logs' && state.me.features?.logs === false)) {
      main.append(pageHeader(viewNames[view], `${viewNames[view]} is disabled on this server.`));
      return;
    }
    loading(main, view === 'issues' && params.get('issue') ? 'Loading issue…' : `Loading ${viewNames[view].toLowerCase()}…`);
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
      } else if (view === 'alerts') {
        const { createAlertsUI } = await import('./alerts.js');
        if (version !== state.requestVersion) return;
        alertsUI ||= createAlertsUI({ state, node, request, navigate, routeLink, button, field, selectField, pageHeader, empty, keyValues, formError, notify, date, number, text, reload: () => loadRoute() });
        await alertsUI.render(main, params, () => version === state.requestVersion);
      } else if (view === 'monitors') {
        const path = monitorPath(params.get('monitor'));
        if (!params.get('monitor')) {
          const monitors = await request(path);
          if (version !== state.requestVersion) return;
          renderMonitors(main, monitors);
        } else {
          const historyQuery = new URLSearchParams({ changes: params.get('changes') === 'false' ? 'false' : 'true', page: String(Math.max(1, parseInt(params.get('page'), 10) || 1)) });
          const historyIsLatest = historyQuery.get('changes') === 'false' && historyQuery.get('page') === '1';
          const [monitor, history, latest] = await Promise.all([request(path), request(`${path}/checks?${historyQuery}`), historyIsLatest ? Promise.resolve(null) : request(`${path}/checks?changes=false&page=1`)]);
          if (version !== state.requestVersion) return;
          renderMonitor(main, monitor, history, (latest || history).data?.[0], params);
        }
      } else if (view === 'transactions' && (params.has('sampleSource') || params.has('sample'))) {
        const source = params.get('sampleSource');
        const id = params.get('sample');
        if (!source || !id) throw new Error('This occurrence link is incomplete. Open a transaction from the Transactions list.');
        const query = new URLSearchParams({ page: params.has('page') ? params.get('page') : '1' });
        const detail = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/transactions/${encodeURIComponent(source)}/${encodeURIComponent(id)}?${query}`);
        if (version !== state.requestVersion) return;
        renderTransactionDetail(main, detail, params);
      } else if (view === 'transactions' || view === 'logs') {
        const query = new URLSearchParams();
        const filters = view === 'transactions' ? ['project', 'start', 'end'] : ['project', 'query', 'level', 'environment', 'trace', 'service', 'start', 'end', 'page'];
        for (const name of filters) {
          if (params.has(name)) query.set(name, params.get(name));
        }
        const result = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/${view}?${query}`);
        if (version !== state.requestVersion) return;
        if (view === 'transactions') renderTransactions(main, result, params);
        else renderLogs(main, result, params);
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
    const variant = ({ warn: 'warning', up: 'resolved', down: 'error', unknown: 'ignored' })[value] || value;
    return node('span', { class: `badge badge-sm ${known.includes(variant) ? variant : ''}` }, text(value, 'Unknown'));
  }

  function renderIssues(main, result, params) {
    document.title = 'Issues · GoodSentry';
    main.replaceChildren(pageHeader('Issues', 'Find, investigate and triage application errors.', node('div', { class: 'actions' }, button('Refresh', () => loadRoute()), button('Create project', openProjectDialog, 'primary'))));
    if (!state.projects.length) {
      main.append(node('section', { class: 'card panel' }, empty('Connect your first application', 'Create a project to get a DSN. Set it in your existing Sentry SDK to start receiving errors.', button('Create project', openProjectDialog, 'primary'))));
      return;
    }
    const search = field('Search issues', 'search', { name: 'query', placeholder: 'Message or exception', value: params.get('query') || '' });
    search.wrap.classList.add('search-field');
    const project = selectField('Project', [['', 'All projects'], ...state.projects.map(item => [String(item.id), item.name])], params.get('project') || '');
    const status = selectField('Status', [['unresolved', 'Unresolved'], ['resolved', 'Resolved'], ['ignored', 'Ignored']], params.get('status') || 'unresolved');
    const environment = field('Environment', 'text', { name: 'environment', value: params.get('environment') || '', placeholder: 'All environments' });
    const sort = selectField('Sort by', [['date', 'Last seen'], ['freq', 'Event count']], params.get('sort') || 'date');
    const filters = node('form', { class: 'filters', role: 'search', 'aria-label': 'Filter issues' }, search.wrap, project.wrap, status.wrap, environment.wrap, sort.wrap, node('button', { class: 'btn button', type: 'submit' }, 'Apply filters'));
    filters.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ query: search.input.value.trim(), project: project.input.value, status: status.input.value, environment: environment.input.value.trim(), sort: sort.input.value, page: null, issue: null });
    });
    main.append(filters);
    const issues = result.data || [];
    const panel = node('section', { class: 'card panel', 'aria-label': 'Issues' });
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

  function projectFilter(params) {
    return selectField('Project', [['', 'All projects'], ...state.projects.map(project => [String(project.id), project.name])], params.get('project') || '');
  }

  function traceReference(traceID, projectID, params) {
    if (!traceID) return node('span', { class: 'muted small' }, 'No trace ID');
    const copy = button('Copy', () => copyValue(traceID, 'Trace ID'), 'small');
    copy.setAttribute('aria-label', 'Copy trace ID');
    return node('div', { class: 'trace-reference' },
      state.me.features?.logs === false ? node('code', {}, traceID) : routeLink(traceID, { view: 'logs', issue: null, sampleSource: null, sample: null, project: projectID || null, trace: traceID, query: null, environment: null, level: null, service: null, status: null, sort: null, page: null, start: params?.get('start') || null, end: params?.get('end') || null, window: params?.get('window') || null }, { class: 'mono', title: 'View logs with this trace ID' }), copy);
  }

  function milliseconds(value) {
    const duration = Number(value);
    return Number.isFinite(duration) ? `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(duration)} ms` : 'Not available';
  }

  function telemetryWindow(params, endpoint) {
    const presets = [['1h', 'Past hour', 3600000], ['24h', 'Past 24 hours', 86400000], ['7d', 'Past 7 days', 604800000], ['30d', 'Past 30 days', 2592000000]];
    const hasRange = params.has('start') || params.has('end');
    const elapsed = Date.parse(params.get('end')) - Date.parse(params.get('start'));
    const preset = presets.find(([key, , duration]) => key === params.get('window') && Math.abs(elapsed - duration) <= 1);
    const selected = hasRange ? preset?.[0] || 'custom' : '';
    const options = [['', endpoint === 'transactions' ? 'Default (last 24 hours)' : 'Default (all time)'], ...presets.map(([key, label]) => [key, label])];
    if (hasRange) options.push(['custom', 'Custom']);
    const control = selectField('Time window', options, selected);
    let changed = false;
    control.input.addEventListener('change', () => { changed = true; });
    function changes() {
      const value = control.input.value;
      if (hasRange && (!changed || value === 'custom')) return { start: params.get('start'), end: params.get('end'), window: preset?.[0] || null };
      if (!value) return { start: null, end: null, window: null };
      const duration = presets.find(([key]) => key === value)[2];
      const end = new Date();
      return { start: new Date(end.getTime() - duration).toISOString(), end: end.toISOString(), window: value };
    }
    function utc(value) {
      const parsed = new Date(value);
      if (!Number.isFinite(parsed.getTime())) return value;
      const fraction = /\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/i.exec(value)?.[1] || '';
      return `${parsed.toISOString().slice(0, 19)}.${fraction.padEnd(6, '0').slice(0, 6)}Z`;
    }
    const summary = node('section', { class: 'time-window-summary', 'aria-label': 'Active time window' },
      node('span', { class: 'badge badge-sm' }, hasRange ? preset?.[1] || 'Custom interval' : endpoint === 'transactions' ? 'Last 24 hours' : 'All time'),
      hasRange ? node('span', { class: 'mono' }, `UTC: ${utc(params.get('start'))} ≤ timestamp < ${utc(params.get('end'))}`)
        : node('span', {}, endpoint === 'transactions' ? 'Default rolling window.' : 'No time restriction.'));
    return { ...control, changes, summary };
  }

  function renderTransactions(main, result, params) {
    document.title = 'Transactions · GoodSentry';
    main.replaceChildren(pageHeader('Transactions', 'Transaction counts and latency in the active time window.', button('Refresh', () => loadRoute())));
    const project = projectFilter(params);
    const window = telemetryWindow(params, 'transactions');
    const form = node('form', { class: 'filters transaction-filters telemetry-filters', 'aria-label': 'Filter transactions' }, project.wrap, window.wrap,
      node('button', { class: 'btn button', type: 'submit' }, 'Apply filter'));
    form.addEventListener('submit', event => { event.preventDefault(); navigate({ project: project.input.value, ...window.changes(), page: null }); });
    main.append(form, window.summary);
    const panel = node('section', { class: 'card panel', 'aria-label': 'Transaction aggregates' });
    const transactions = result.data || [];
    if (!transactions.length) {
      panel.append(empty('No transactions in this time window', 'Transactions appear when your SDK sends performance data. Check the selected project and your SDK sampling configuration.'));
    } else {
      const rows = transactions.map(transaction => node('tr', {},
        node('td', { class: 'transaction-name' }, node('strong', {}, transaction.latestSample
          ? routeLink(text(transaction.transaction, 'Unnamed transaction'), { sampleSource: transaction.latestSample.source, sample: transaction.latestSample.id, page: null }, { class: 'issue-link', title: 'Open the latest transaction occurrence' })
          : text(transaction.transaction, 'Unnamed transaction'))),
        node('td', {}, node('span', { class: 'mono' }, text(transaction.op, 'Not provided'))),
        node('td', { class: 'nowrap' }, number(transaction.count)), node('td', { class: 'nowrap' }, number(transaction.errorCount)),
        node('td', { class: 'nowrap' }, milliseconds(transaction.avgDuration)), node('td', { class: 'nowrap' }, milliseconds(transaction.p50)),
        node('td', { class: 'nowrap' }, milliseconds(transaction.p95)),
        node('td', { class: 'nowrap' }, node('time', { datetime: transaction.lastSeen }, date(transaction.lastSeen, true))),
        node('td', {}, traceReference(transaction.latestTraceId, transaction.latestSample?.projectId || params.get('project'), params))));
      const columns = ['Transaction', 'Operation', 'Events', 'Errors', 'Average', 'p50', 'p95', 'Last seen', 'Latest trace'];
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    main.append(panel);
  }

  function renderTransactionDetail(main, detail, params) {
    document.title = `${text(detail.name, 'Transaction occurrence')} · GoodSentry`;
    const heading = pageHeader(text(detail.name, 'Unnamed transaction'), `${detail.projectName} · ${text(detail.op, 'No operation recorded')}`, button('Refresh', () => loadRoute()));
    heading.classList.add('transaction-heading');
    const occurrence = detailPanel('Occurrence', node('div', { class: 'panel-body' }, keyValues([
      ['Project', detail.projectName], ['Project ID', detail.sample.projectId], ['Operation', detail.op],
      ['Duration', milliseconds(detail.duration)], ['Status', detail.status], ['Environment', detail.environment], ['Release', detail.release],
      ['Start (UTC)', detail.startTimestamp || 'Not available'], ['End (UTC)', detail.endTimestamp || 'Not available'],
    ])));
    const context = detailPanel('Trace context', node('div', { class: 'panel-body form-stack transaction-context' },
      node('div', {}, node('p', { class: 'muted small' }, state.me.features?.logs === false ? 'Trace ID' : 'Trace ID · open to view related logs'), traceReference(detail.traceId, detail.sample.projectId, params)),
      keyValues([
        ['Source', detail.sample.source === 'event' ? 'Legacy transaction' : 'Standalone segment'], ['Record ID', detail.sample.id],
        ['Event ID', detail.eventId], ['Span ID', detail.spanId], ['Parent span ID', detail.parentSpanId],
      ])));
    const originWindow = telemetryWindow(params, 'transactions').summary;
    originWindow.setAttribute('aria-label', 'Originating aggregate window');
    originWindow.prepend(node('strong', {}, 'Aggregate window:'));
    main.replaceChildren(routeLink('← All transactions', { sampleSource: null, sample: null, page: null }, { class: 'back-link' }),
      heading, originWindow, node('div', { class: 'transaction-summary' }, occurrence, context), transactionSpans(detail),
      node('details', { class: 'card panel' }, node('summary', {}, detail.sample.source === 'event' ? 'Original transaction JSON' : 'Original segment JSON'),
        node('pre', { class: 'event-json' }, JSON.stringify(detail.data, null, 2))));
  }

  function transactionSpans(detail) {
    const projectTrace = detail.spanScope === 'projectTrace';
    const panel = detailPanel(projectTrace ? 'Spans in this project and trace' : 'Transaction spans', node('div', { class: 'panel-body muted small' },
      projectTrace
        ? 'Shows all standalone spans in this exact project and trace, including segment roots. Parent IDs describe recorded relationships; rows may belong to other segments. The originating time window does not filter these spans.'
        : 'Shows spans nested in this transaction, in the order sent by the SDK. The originating time window does not filter these spans.'));
    panel.setAttribute('aria-label', 'Occurrence spans');
    const spans = detail.spans || [];
    if (!spans.length) panel.append(empty(detail.page > 1 ? 'No spans on this page' : 'No spans recorded', detail.page > 1 ? 'Choose an earlier page to inspect the recorded spans.' : projectTrace ? 'No standalone spans were found in this project and trace.' : 'This transaction has no nested spans.'));
    else {
      const timestamp = value => value ? node('time', { datetime: value }, value) : node('span', { class: 'muted' }, 'Not available');
      const rows = spans.map(span => node('tr', {},
        node('td', { class: 'transaction-span-name' }, node('strong', {}, text(span.name, 'Unnamed span')),
          node('div', { class: 'mono small' }, text(span.spanId, 'No span ID recorded')),
          node('div', { class: 'muted small' }, `Record: ${span.id}`),
          node('details', { class: 'transaction-span-raw' }, node('summary', {}, 'Raw span JSON'), node('pre', { class: 'event-json' }, JSON.stringify(span.data, null, 2)))),
        node('td', { class: 'mono' }, text(span.op)), node('td', {}, text(span.status)),
        node('td', { class: 'mono nowrap' }, timestamp(span.startTimestamp)), node('td', { class: 'mono nowrap' }, timestamp(span.endTimestamp)),
        node('td', { class: 'nowrap' }, span.duration === null || span.duration === undefined ? 'Not available' : milliseconds(span.duration)),
        node('td', { class: 'mono' }, text(span.parentSpanId))));
      const columns = ['Span and raw data', 'Operation', 'Status', 'Start (UTC)', 'End (UTC)', 'Duration', 'Parent span ID'];
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    const previous = button('Previous spans', () => navigate({ page: detail.page - 1 }));
    const next = button('Next spans', () => navigate({ page: detail.page + 1 }));
    previous.disabled = detail.page <= 1;
    next.disabled = !detail.hasMore;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(detail.spanCount)} spans · ${number(spans.length)} on this page · Page ${number(detail.page)}`), node('div', { class: 'actions' }, previous, next)));
    return panel;
  }

  function logAttributes(attributes) {
    const entries = pairs(attributes);
    if (!entries.length) return node('p', { class: 'muted small' }, 'No attributes recorded.');
    return node('dl', { class: 'key-values log-attributes', 'aria-label': 'Log attributes' }, entries.map(([name, attribute]) => [
      node('dt', {}, name), node('dd', {}, node('span', { class: 'badge badge-sm' }, text(attribute?.type, 'Unknown type')), node('pre', {}, text(attribute?.value))),
    ]));
  }

  function renderLogs(main, result, params) {
    document.title = 'Logs · GoodSentry';
    main.replaceChildren(pageHeader('Logs', 'Search recorded logs and inspect their typed attributes.', button('Refresh', () => loadRoute())));
    const search = field('Search logs', 'search', { name: 'query', value: params.get('query') || '', placeholder: 'Message text' });
    search.wrap.classList.add('search-field');
    const project = projectFilter(params);
    const level = selectField('Level', [['', 'All levels'], ...['trace', 'debug', 'info', 'warn', 'warning', 'error', 'fatal'].map(value => [value, value])], params.get('level') || '');
    const environment = field('Environment', 'text', { name: 'environment', value: params.get('environment') || '', placeholder: 'All environments' });
    const trace = field('Trace ID', 'text', { name: 'trace', value: params.get('trace') || '', placeholder: 'All traces', spellcheck: 'false', autocomplete: 'off' });
    const service = field('Service', 'text', { name: 'service', value: params.get('service') || '', maxlength: 4000, placeholder: 'Exact service name', autocomplete: 'off' });
    const window = telemetryWindow(params, 'logs');
    const form = node('form', { class: 'filters log-filters', role: 'search', 'aria-label': 'Filter logs' }, search.wrap, project.wrap, level.wrap, environment.wrap, trace.wrap, service.wrap, window.wrap,
      node('button', { class: 'btn button', type: 'submit' }, 'Apply filters'));
    form.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ project: project.input.value, query: search.input.value.trim(), level: level.input.value, environment: environment.input.value.trim(), trace: trace.input.value.trim(), service: service.input.value, ...window.changes(), page: null });
    });
    main.append(form, window.summary);
    const logs = result.data || [];
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const panel = node('section', { class: 'card panel', 'aria-label': 'Logs' });
    if (!logs.length) {
      panel.append(empty(page > 1 ? 'No more logs on this page' : 'No matching logs', 'Check your filters, or send logs from your application SDK.',
        button('Clear filters', () => navigate({ project: null, query: null, level: null, environment: null, trace: null, service: null, start: null, end: null, window: null, page: null }))));
    } else {
      const rows = logs.map(log => {
        const projectName = state.projects.find(project => String(project.id) === String(log.projectId))?.name || log.projectId;
        const disclosure = node('details', { class: 'log-entry' }, node('summary', {}, node('span', { class: 'log-message' }, text(log.body, '(empty message)'))),
          node('div', { class: 'log-detail' }, node('h2', {}, 'Log attributes'), logAttributes(log.attributes),
            node('div', { class: 'log-identifiers' }, keyValues([['Log ID', log.id], ['Span ID', log.spanId]]))));
        return node('tr', {}, node('td', { class: 'nowrap' }, node('time', { datetime: log.timestamp }, date(log.timestamp, true))),
          node('td', {}, badge(log.level)), node('td', { class: 'log-message-column' }, disclosure), node('td', {}, text(projectName)),
          node('td', {}, text(log.service)), node('td', {}, text(log.environment)), node('td', {}, traceReference(log.traceId, log.projectId, params)));
      });
      const columns = ['Time', 'Level', 'Message and attributes', 'Project', 'Service', 'Environment', 'Trace'];
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    const previous = button('Previous', () => navigate({ page: page - 1 }));
    const next = button('Next', () => navigate({ page: page + 1 }));
    previous.disabled = page <= 1;
    next.disabled = logs.length < pageSize;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(logs.length)} ${logs.length === 1 ? 'log' : 'logs'} on this page · Page ${page}`), node('div', { class: 'actions' }, previous, next)));
    main.append(panel);
  }

  async function copyValue(value, label = 'DSN') {
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(value);
      else {
        const copy = node('textarea', { class: 'copy-buffer', 'aria-label': `${label} to copy`, readonly: true }, value);
        document.body.append(copy);
        copy.select();
        const succeeded = document.execCommand('copy');
        copy.remove();
        if (!succeeded) throw new Error('Copy unavailable');
      }
      notify(`${label} copied.`);
    } catch (_) { notify(`Copy unavailable. Select the ${label} text and copy it manually.`); }
  }

  function renderProjects(main) {
    document.title = 'Projects · GoodSentry';
    main.replaceChildren(pageHeader('Projects', 'Each application gets its own SDK key and DSN.', button('Create project', openProjectDialog, 'primary')));
    const panel = node('section', { class: 'card panel', 'aria-label': 'Projects' });
    if (!state.projects.length) {
      panel.append(empty('Create your first project', 'Choose a name and platform, then copy the project DSN into your Sentry SDK configuration.', button('Create project', openProjectDialog, 'primary')));
    } else {
      const rows = state.projects.map(project => node('tr', {},
        node('td', {}, node('div', { class: 'project-name' }, project.name), node('div', { class: 'muted small mono' }, project.slug)),
        node('td', {}, badge(project.platform)),
        node('td', {}, node('div', { class: 'dsn' }, node('code', {}, project.dsn), button('Copy', () => copyValue(project.dsn), 'small'))),
        node('td', {}, routeLink('View issues', { view: 'issues', project: project.id, query: null, status: null, environment: null, page: null }, { class: 'btn button small' }))));
      const columns = ['Project', 'Platform', 'DSN', ''].map((name, index) => node('th', { scope: 'col', ...(index === 3 ? { 'aria-label': 'Actions' } : {}) }, name));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns)), node('tbody', {}, rows))));
      panel.append(...state.projects.map(sourceMapDisclosure));
      main.append(node('p', { class: 'muted small connection-help' }, 'Use the DSN with your existing Sentry SDK. Keep the SDK configuration in your application; GoodSentry receives events at the project address.'));
    }
    main.append(panel);
  }

  function canManageMonitors() {
    return ['owner', 'admin'].includes(state.organization.role);
  }

  function monitorPath(id) {
    const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/monitors`;
    return id ? `${path}/${encodeURIComponent(id)}` : path;
  }

  function monitorStatus(monitor) {
    return node('div', { class: 'actions', role: 'group', 'aria-label': 'Confirmed monitor status' }, badge(monitor.status || 'unknown'), !monitor.enabled ? node('span', { class: 'badge badge-sm' }, 'Paused') : null);
  }

  function renderMonitors(main, result) {
    document.title = 'Uptime · GoodSentry';
    const manage = canManageMonitors();
    main.replaceChildren(pageHeader('Uptime', 'HTTP, TCP and heartbeat monitoring.', node('div', { class: 'actions' }, button('Refresh', () => loadRoute()), manage ? button('Create monitor', () => openMonitorDialog(), 'primary') : null)));
    const panel = node('section', { class: 'card panel', 'aria-label': 'Monitors' });
    const monitors = result.data || [];
    if (!monitors.length) panel.append(empty('No monitors yet', manage ? 'Add a service endpoint or incoming heartbeat to start monitoring.' : 'An organization owner or admin can add a monitor.', manage ? button('Create monitor', () => openMonitorDialog(), 'primary') : null));
    else {
      const columns = ['Monitor', 'Status', 'Type', 'Target', 'Interval', 'Last check'];
      const rows = monitors.map(monitor => node('tr', {},
        node('td', { class: 'monitor-name' }, routeLink(monitor.name, { view: 'monitors', monitor: monitor.id, page: null, changes: null }, { class: 'issue-link issue-title' }),
          monitor.project ? node('p', { class: 'small muted' }, state.projects.find(project => project.slug === monitor.project)?.name || monitor.project) : null),
        node('td', {}, monitorStatus(monitor)), node('td', {}, node('span', { class: 'badge badge-sm' }, monitor.type)),
        node('td', { class: 'monitor-target' }, monitor.type === 'heartbeat' ? 'Incoming heartbeat' : monitor.url),
        node('td', { class: 'nowrap' }, `${number(monitor.intervalSeconds)} s`),
        node('td', { class: 'nowrap' }, monitor.lastCheck ? date(monitor.lastCheck, true) : 'Not checked yet')));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    main.append(panel);
  }

  function monitorConfiguration(monitor) {
    return Object.fromEntries(['name', 'type', 'url', 'project', 'environment', 'intervalSeconds', 'timeoutSeconds', 'expectedStatus', 'expectedBody', 'confirmationThreshold', 'enabled'].map(key => [key, monitor[key]]));
  }

  function renderMonitor(main, monitor, history, latest, params) {
    document.title = `${monitor.name} · Uptime · GoodSentry`;
    const actions = node('div', { class: 'actions' }, button('Refresh', () => loadRoute()));
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const confirmation = node('div');
    if (canManageMonitors()) {
      const pause = button(monitor.enabled ? 'Pause monitor' : 'Resume monitor', async () => {
        pause.disabled = true;
        try {
          await request(monitorPath(monitor.id), { method: 'PUT', body: { ...monitorConfiguration(monitor), enabled: !monitor.enabled } });
          notify(monitor.enabled ? 'Monitoring paused.' : 'Monitoring resumed.');
          await loadRoute();
        } catch (failure) { formError(error, failure); pause.disabled = false; }
      });
      actions.append(button('Edit monitor', () => openMonitorDialog(monitor)), pause, button('Delete monitor', () => {
        const remove = button('Delete permanently', async () => {
          remove.disabled = true;
          try {
            await request(monitorPath(monitor.id), { method: 'DELETE' });
            navigate({ view: 'monitors', monitor: null, page: null, changes: null });
            notify('Monitor and its check history deleted.');
          } catch (failure) { formError(error, failure); remove.disabled = false; }
        }, 'danger');
        confirmation.replaceChildren(node('div', { class: 'monitor-confirmation' }, node('p', {}, `Delete ${monitor.name} and all its check history?`), node('div', { class: 'actions' }, button('Cancel deletion', () => confirmation.replaceChildren()), remove)));
        remove.focus();
      }));
    }
    const heading = pageHeader(monitor.name, monitor.type === 'heartbeat' ? 'Incoming heartbeat monitor' : `${monitor.type} · ${monitor.url}`, actions);
    heading.classList.add('monitor-heading');
    const summary = detailPanel('Current state', node('div', { class: 'panel-body' }, monitorStatus(monitor), keyValues([
      ['Interval', `${number(monitor.intervalSeconds)} seconds`], ['Last check', monitor.lastCheck ? date(monitor.lastCheck) : 'Not checked yet'],
      ['Last status change', monitor.lastChange ? date(monitor.lastChange) : 'No changes yet'], ['Next deadline', monitor.enabled ? date(monitor.nextCheck) : 'Paused'],
      ['Consecutive failures', monitor.consecutiveFailures], ['Last probe result', latest ? latest.up ? 'Up' : 'Down' : 'Not checked yet'],
      ['Last reason', latest ? text(latest.reason) : 'Not checked yet'], ['Last HTTP status', latest?.statusCode ?? 'Not applicable'],
      ['Last latency', latest?.responseTimeMs === undefined || latest?.responseTimeMs === null ? 'Not applicable' : milliseconds(latest.responseTimeMs)],
    ])));
    summary.setAttribute('aria-label', 'Current monitor state');
    const panels = node('div', { class: 'monitor-summary' }, summary,
      detailPanel('Configuration', node('div', { class: 'panel-body' }, keyValues([
        ['Type', monitor.type], ['Project', monitor.project || 'None'], ['Environment', monitor.environment || 'Not set'], ['Failure threshold', monitor.confirmationThreshold],
        ['Timeout', monitor.type === 'heartbeat' ? 'Not applicable' : `${monitor.timeoutSeconds} seconds`],
        ['Expected HTTP status', ['HEAD', 'GET', 'POST'].includes(monitor.type) ? monitor.expectedStatus : 'Not applicable'],
        ['Expected body', monitor.expectedBody || 'Not set'],
      ]))));
    main.replaceChildren(routeLink('← All monitors', { monitor: null, page: null, changes: null }, { class: 'back-link' }), heading, error, confirmation, panels);
    if (monitor.type === 'heartbeat') {
      const heartbeat = field('Heartbeat URL', 'text', { readonly: true, value: monitor.heartbeatURL || '' });
      main.append(detailPanel('Send a heartbeat', node('div', { class: 'panel-body form-stack' }, node('p', { class: 'muted small' }, 'Send an HTTP POST to this capability URL before each deadline. Keep it private.'), heartbeat.wrap,
        node('div', { class: 'actions' }, button('Copy heartbeat URL', () => copyValue(monitor.heartbeatURL, 'Heartbeat URL'))))));
    }
    main.append(monitorChecks(monitor, history, params));
  }

  function monitorChecks(monitor, history, params) {
    const changes = selectField('Check history', [['true', 'Transitions only'], ['false', 'All checks']], params.get('changes') === 'false' ? 'false' : 'true');
    const form = node('form', { class: 'filters transaction-filters', 'aria-label': 'Filter monitor checks' }, changes.wrap, node('button', { type: 'submit', class: 'btn button' }, 'Apply history filter'));
    form.addEventListener('submit', event => { event.preventDefault(); navigate({ changes: changes.input.value, page: null }); });
    const panel = node('section', { class: 'card panel', 'aria-label': 'Monitor check history' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Check history')));
    const checks = history.data || [];
    if (!checks.length) panel.append(empty('No checks in this view', changes.input.value === 'true' ? 'Confirmed status changes will appear here. Select All checks to inspect every recorded result.' : 'Results appear after the monitor runs or receives a heartbeat.'));
    else {
      const columns = ['Completed', 'Probe result', 'Confirmed status', 'Change', 'Reason', 'HTTP status', 'Latency'];
      const rows = checks.map(check => node('tr', {},
        node('td', { class: 'nowrap' }, node('time', { datetime: check.completedAt }, date(check.completedAt, true))),
        node('td', {}, badge(check.up ? 'up' : 'down')), node('td', {}, badge(check.status)),
        node('td', {}, check.isChange ? `${check.previousStatus} → ${check.status}` : 'No change'), node('td', { class: 'monitor-reason' }, text(check.reason)),
        node('td', {}, check.statusCode ?? 'Not applicable'), node('td', { class: 'nowrap' }, check.responseTimeMs === undefined || check.responseTimeMs === null ? 'Not applicable' : milliseconds(check.responseTimeMs))));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const previous = button('Previous checks', () => navigate({ page: page - 1 }));
    const next = button('Next checks', () => navigate({ page: page + 1 }));
    previous.disabled = page <= 1;
    next.disabled = !history.hasMore;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(checks.length)} checks on this page · Page ${page}`), node('div', { class: 'actions' }, previous, next)));
    return node('div', { class: 'monitor-history' }, form, panel);
  }

  function openMonitorDialog(monitor) {
    const config = monitor || { name: '', type: 'HEAD', url: '', project: '', environment: '', intervalSeconds: 60, timeoutSeconds: 20, expectedStatus: 200, expectedBody: '', confirmationThreshold: 1, enabled: true };
    const dialog = node('dialog', { class: 'monitor-dialog', 'aria-labelledby': 'monitor-dialog-title' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Monitor name', 'text', { name: 'name', value: config.name, required: true, maxlength: 400 });
    const type = selectField('Monitor type', [['HEAD', 'HTTP HEAD'], ['GET', 'HTTP GET'], ['POST', 'HTTP POST'], ['TCP', 'TCP'], ['heartbeat', 'Incoming heartbeat']], config.type, monitor ? { disabled: true } : {});
    const target = field('Target URL', 'text', { name: 'url', value: config.url, maxlength: 2000, autocomplete: 'off', spellcheck: 'false' });
    const project = selectField('Project', [['', 'No project'], ...state.projects.map(item => [item.slug, item.name])], config.project || '');
    const environment = field('Monitor environment', 'text', { name: 'environment', value: config.environment || '', maxlength: 256, autocomplete: 'off' });
    const interval = field('Interval (seconds)', 'number', { name: 'intervalSeconds', required: true, min: 1, max: 86400, step: 1, value: config.intervalSeconds });
    const timeout = field('Timeout (seconds)', 'number', { name: 'timeoutSeconds', required: true, min: 1, max: 60, step: 1, value: config.timeoutSeconds });
    const expectedStatus = field('Expected HTTP status', 'number', { name: 'expectedStatus', required: true, min: 100, max: 599, step: 1, value: config.expectedStatus });
    const expectedBody = field('Expected body substring', 'textarea', { name: 'expectedBody', rows: 2, maxlength: 4000 });
    expectedBody.input.value = config.expectedBody || '';
    const threshold = field('Failure confirmation threshold', 'number', { name: 'confirmationThreshold', required: true, min: 1, max: 100, step: 1, value: config.confirmationThreshold });
    const enabled = node('input', { type: 'checkbox', checked: config.enabled });
    const note = node('p', { class: 'muted small' });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, monitor ? 'Save monitor' : 'Create monitor');
    const close = () => dialog.close();
    function updateFields() {
      const heartbeat = type.input.value === 'heartbeat';
      const http = ['HEAD', 'GET', 'POST'].includes(type.input.value);
      target.wrap.hidden = heartbeat;
      target.input.disabled = heartbeat;
      target.input.required = !heartbeat;
      target.wrap.querySelector('label').textContent = type.input.value === 'TCP' ? 'TCP host and port' : 'Target URL';
      timeout.wrap.hidden = heartbeat;
      timeout.input.disabled = heartbeat;
      expectedStatus.wrap.hidden = !http;
      expectedStatus.input.disabled = !http;
      expectedBody.wrap.hidden = !['GET', 'POST'].includes(type.input.value);
      expectedBody.input.disabled = expectedBody.wrap.hidden;
      note.textContent = heartbeat ? 'A heartbeat must arrive within each interval. No additional grace interval is added.' : type.input.value === 'TCP' ? 'Enter a host and port. Bracket IPv6 addresses. Timeout applies to the connection.' : type.input.value === 'POST' ? 'POST checks send an empty body. Expected body is a literal substring in the first 1 MiB of the response.' : type.input.value === 'GET' ? 'Expected body is a literal substring in the first 1 MiB of the response.' : 'HEAD checks validate the response status without reading its body.';
    }
    type.input.addEventListener('change', updateFields);
    updateFields();
    const form = node('form', { class: 'form-stack', 'aria-label': monitor ? 'Edit monitor configuration' : 'Create monitor configuration' }, error,
      node('div', { class: 'monitor-fields' }, name.wrap, type.wrap, target.wrap, project.wrap, environment.wrap, interval.wrap, timeout.wrap, expectedStatus.wrap, threshold.wrap, expectedBody.wrap), note,
      node('label', { class: 'checkbox-field' }, enabled, 'Enabled'), node('div', { class: 'dialog-actions' }, button('Cancel', close), submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      if (Array.from(name.input.value.trim()).length > 200 || Array.from(environment.input.value.trim()).length > 128 || (['GET', 'POST'].includes(type.input.value) && Array.from(expectedBody.input.value).length > 2000)) {
        formError(error, new Error('Use at most 200 characters for the monitor name, 128 for its environment and 2000 for the expected body.'));
        return;
      }
      const kind = type.input.value;
      const heartbeat = kind === 'heartbeat';
      const payload = { name: name.input.value.trim(), type: kind, url: heartbeat ? '' : target.input.value.trim(), project: project.input.value, environment: environment.input.value.trim(),
        intervalSeconds: Number(interval.input.value), timeoutSeconds: heartbeat ? config.timeoutSeconds : Number(timeout.input.value),
        expectedStatus: ['HEAD', 'GET', 'POST'].includes(kind) ? Number(expectedStatus.input.value) : config.expectedStatus,
        expectedBody: ['GET', 'POST'].includes(kind) ? expectedBody.input.value : '', confirmationThreshold: Number(threshold.input.value), enabled: enabled.checked };
      submit.disabled = true;
      submit.textContent = 'Saving…';
      try {
        const saved = await request(monitor ? monitorPath(monitor.id) : monitorPath(), { method: monitor ? 'PUT' : 'POST', body: payload });
        dialog.close();
        navigate({ view: 'monitors', monitor: saved.id, page: null, changes: null });
        notify(monitor ? 'Monitor updated.' : 'Monitor created.');
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = monitor ? 'Save monitor' : 'Create monitor'; }
    });
    dialog.append(node('header', {}, node('h2', { id: 'monitor-dialog-title' }, monitor ? 'Edit monitor' : 'Create monitor'), button('Close', close, 'small')), form);
    dialog.addEventListener('close', () => dialog.remove());
    document.body.append(dialog);
    dialog.showModal();
    name.input.focus();
  }

  function sourceMapDisclosure(project) {
    const disclosure = node('details', { class: 'source-map-disclosure', 'aria-label': `Source maps for ${project.name}` }, node('summary', {}, `Source maps · ${project.name}`));
    let initialized = false;
    disclosure.addEventListener('toggle', () => {
      if (!disclosure.open || initialized) return;
      initialized = true;
      const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects/${encodeURIComponent(project.id)}/sourcemaps`;
      const metadata = node('div');
      const refresh = button('Refresh source maps', () => loadMetadata(), 'small');
      const body = node('div', { class: 'source-map-body' }, node('div', { class: 'panel-title source-map-header' }, node('h2', {}, 'Uploaded source maps'), refresh), metadata);
      async function loadMetadata() {
        refresh.disabled = true;
        metadata.replaceChildren(node('p', { class: 'muted source-map-loading', role: 'status', 'aria-busy': 'true' }, 'Loading source maps…'));
        try {
          const result = await request(path);
          const maps = result.data || [];
          if (!maps.length) metadata.replaceChildren(node('p', { class: 'muted source-map-empty' }, 'No source maps uploaded.'));
          else {
            const columns = ['Artifact', 'Release', 'Distribution', 'Debug ID', 'Size', 'Uploaded', 'Checksum'];
            const rows = maps.map(map => node('tr', {},
              node('td', { class: 'artifact-name' }, node('strong', {}, map.name), node('div', { class: 'small muted mono' }, `Artifact ${map.id}`)),
              node('td', {}, text(map.release, 'Not set')), node('td', {}, text(map.dist, 'Not set')), node('td', {}, node('code', { class: 'artifact-identity' }, text(map.debugId, 'Not set'))),
              node('td', { class: 'nowrap' }, `${number(map.size)} B`), node('td', { class: 'nowrap' }, node('time', { datetime: map.dateCreated }, date(map.dateCreated, true))),
              node('td', {}, node('code', { class: 'artifact-identity' }, map.checksum))));
            metadata.replaceChildren(node('div', { class: 'table-scroll', 'aria-label': 'Source map metadata' }, node('table', {},
              node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
          }
        } catch (error) { errorState(metadata, error, loadMetadata); }
        finally { refresh.disabled = false; }
      }
      if (['owner', 'admin'].includes(state.organization.role)) body.append(sourceMapForm(project, path, loadMetadata));
      else body.append(node('p', { class: 'muted source-map-permission' }, 'Organization owners and admins can upload source maps.'));
      disclosure.append(body);
      loadMetadata();
    });
    return disclosure;
  }

  function sourceMapForm(project, path, refresh) {
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const status = node('p', { class: 'source-map-status', role: 'status' });
    const name = field('Artifact name or URL', 'text', { name: 'name', required: true, maxlength: 2048, autocomplete: 'off' });
    const release = field('Release', 'text', { name: 'release', maxlength: 200, autocomplete: 'off' });
    const dist = field('Distribution', 'text', { name: 'dist', maxlength: 200, autocomplete: 'off' });
    const debug = field('Debug ID', 'text', { name: 'debugId', maxlength: 64, autocomplete: 'off', spellcheck: 'false' });
    const file = field('Source map file', 'file', { name: 'file', required: true, accept: '.map,.json,application/json' });
    const helpID = `source-map-help-${++fieldNumber}`;
    release.input.setAttribute('aria-describedby', helpID);
    debug.input.setAttribute('aria-describedby', helpID);
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Upload source map');
    const form = node('form', { class: 'source-map-form form-stack', 'aria-label': `Upload source map for ${project.name}` }, node('h2', {}, 'Upload source map'),
      node('p', { class: 'muted small', id: helpID }, 'Provide a release or debug ID to match the correct build. The artifact name identifies the generated file URL. Distribution is optional.'),
      error, node('div', { class: 'source-map-fields' }, name.wrap, release.wrap, dist.wrap, debug.wrap, file.wrap),
      node('div', { class: 'actions' }, submit), status);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      status.textContent = '';
      if (!release.input.value.trim() && !debug.input.value.trim()) {
        formError(error, new Error('Provide a release or debug ID before uploading this source map.'));
        return;
      }
      const payload = new FormData(form);
      for (const fieldName of ['name', 'release', 'dist', 'debugId']) payload.set(fieldName, String(payload.get(fieldName) || '').trim());
      submit.disabled = true;
      submit.textContent = 'Uploading…';
      try {
        const result = await request(path, { method: 'POST', body: payload });
        status.textContent = result.created ? 'Source map uploaded.' : 'Identical source map already exists. No duplicate was created.';
        await refresh();
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = 'Upload source map'; }
    });
    return form;
  }

  function openProjectDialog() {
    const dialog = node('dialog', { 'aria-labelledby': 'project-dialog-title' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Project name', 'text', { name: 'name', required: true, maxlength: 200, autocomplete: 'off' });
    const platform = selectField('Platform', [['javascript', 'JavaScript / Browser'], ['node', 'Node.js'], ['python', 'Python'], ['go', 'Go'], ['java', 'Java'], ['php', 'PHP'], ['ruby', 'Ruby'], ['csharp', '.NET'], ['other', 'Other']], 'javascript');
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Create project');
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
    return node('section', { class: 'card panel' }, node('div', { class: 'panel-title' }, node('h2', {}, title)), content);
  }

  function stackFrame(frame) {
    const location = [frame.filename || frame.abs_path || frame.module || 'Unknown file', frame.lineno, frame.colno].filter(value => value !== undefined && value !== null && value !== '').join(':');
    const row = node('li', { class: 'stack-frame' }, node('div', { class: 'frame-heading' }, node('span', { class: 'frame-function' }, frame.function || '<anonymous>'), frame.in_app ? node('span', { class: 'badge badge-sm' }, 'App frame') : null), node('div', { class: 'frame-path' }, location));
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
      node('div', { class: 'detail-meta' }, statusLabel, badge(issue.level), node('span', { class: 'muted' }, issue.project?.name || 'Project'), issue.environment ? node('span', { class: 'badge badge-sm' }, issue.environment) : null)), statusActions);
    const stats = node('dl', { class: 'stat-list' }, [['Events', number(issue.count)], ['First seen', date(issue.firstSeen, true)], ['Last seen', date(issue.lastSeen, true)]].map(([name, value]) => node('div', { class: 'stat' }, node('dt', {}, name), node('dd', {}, value))));
    const summary = node('section', { class: 'card panel' }, stats, node('div', { class: 'panel-body' },
      node('p', { class: 'muted small' }, 'Latest event'), node('p', { class: 'mono' }, text(event.event_id || event.eventID)),
      event.message || event.logentry?.formatted ? node('pre', { class: 'event-message' }, text(event.message || event.logentry.formatted)) : null));
    const left = node('div', {}, summary, stackTrace(event), breadcrumbs(event), commentsPanel(issue, detail.comments || []));
    left.append(node('details', { class: 'card panel' }, node('summary', {}, 'Original event JSON'), node('pre', { class: 'event-json' }, JSON.stringify(event, null, 2))));
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
    const submit = node('button', { class: 'btn button primary', type: 'submit' }, 'Post comment');
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
