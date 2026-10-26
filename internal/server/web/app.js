(() => {
  'use strict';

  const app = document.getElementById('app');
  const notifications = document.getElementById('notifications');
  const configuredBase = document.querySelector('meta[name="goodsentry-base"]')?.content || '';
  const base = configuredBase === '__GOODSENTRY_BASE__' ? '' : configuredBase.replace(/\/$/, '');
  const pageSize = 25;
  const viewNames = { issues: 'Issues', feedback: 'Feedback', projects: 'Projects', transactions: 'Transactions', logs: 'Logs', monitors: 'Uptime', alerts: 'Alerts', releases: 'Releases', 'api-tokens': 'API tokens', people: 'People', organization: 'Organization', admin: 'Administration', account: 'Account' };
  const traceReset = { traceView: null, traceProject: null, tracePage: null, traceNode: null, traceErrorsPage: null, returnView: null };
  const state = { me: null, organization: null, projects: [], requestVersion: 0, authConfig: { oidc: { enabled: false } } };
  const commentDrafts = new Map();
  const commentEditDrafts = new Map();
  const commentEditIssueIDs = new Map();
  // Group observations select verification candidates; they do not prove ownership or deletion.
  const issueDraftOwners = new Map();
  const mergeSelection = { scope: null, issues: new Map() };
  const issueDeletionWarning = 'This removes each whole current group, its recorded events, attachments, comments, assignment, mute and resolution policies. Future matching captures can create a new issue.';
  let issueDeletion;
  let issueViewContext;
  let issueRenderVersion = 0;
  let fieldNumber = 0;
  let notificationTimer;
  let alertsUI;
  let releasesUI;
  let projectKeysUI;
  let hashesUI;
  let reportsUI;
  let feedbackUI;
  let peopleUI;
  let issuedAPIToken;

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
    // Preserve large integers and extreme exponents as display text. Validate
    // first, and leave quoted strings and finite decimal values untouched.
    const precise = source.replace(/"(?:\\.|[^"\\])*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g, token => {
      if (token.startsWith('"')) return token;
      const value = Number(token);
      return !Number.isFinite(value) || (/^-?\d+$/.test(token) && !Number.isSafeInteger(value)) ? JSON.stringify(token) : token;
    });
    return precise === source ? parsed : JSON.parse(precise);
  }

  async function request(path, options = {}) {
    let response;
    const { jsonText, withLinks = false, binary = false, ...fetchOptions } = options;
    const multipart = options.body instanceof FormData;
    const headers = { Accept: binary ? 'application/octet-stream' : 'application/json', ...options.headers };
    if (jsonText !== undefined || (options.body !== undefined && !multipart)) headers['Content-Type'] = 'application/json';
    if (options.method && options.method !== 'GET' && state.me?.csrfToken) {
      headers['X-CSRF-Token'] = state.me.csrfToken;
    }
    try {
      response = await fetch(base + path, {
        ...fetchOptions, credentials: 'same-origin', headers,
        body: jsonText !== undefined ? jsonText : options.body === undefined ? undefined : multipart ? options.body : JSON.stringify(options.body),
      });
    } catch (_) {
      throw new Error('Cannot reach GoodSentry. Check your connection and try again.');
    }
    if (response.status === 204) return null;
    if (binary && response.ok) return response.blob();
    const endpoint = path.split('?')[0];
    const data = await response.text().then(source => endpoint.endsWith('/logs') || /\/logs\/\d+$/.test(endpoint) || /\/transactions\/(event|span)\/[^/]+$/.test(endpoint) || /\/traces\/[^/]+\/nodes\/[^/]+$/.test(endpoint) || /\/releases(?:\/|$)/.test(endpoint)
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
      if (response.status === 409 && typeof data?.mergedInto === 'string' && /^[1-9][0-9]*$/.test(data.mergedInto)) error.mergedInto = data.mergedInto;
      throw error;
    }
    return withLinks ? { data, link: response.headers.get('Link') } : data;
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

  function formError(target, error, issueView) {
    target.textContent = error.message;
    if (error.status === 409 && error.mergedInto) target.append(' ', currentGroupLink(error.mergedInto, issueView));
    target.focus();
  }

  function currentGroupLink(id, view) {
    const changes = { view: issueViewName(new URLSearchParams(location.search)), issue: id };
    return node('a', { href: routeURL(changes), onclick: event => {
      if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      if (view) issueViewContext = { organization: state.organization.slug, issueId: id, view };
      navigate(changes);
    } }, 'Open current group');
  }

  function isIssueView(view) { return view === 'issues' || view === 'feedback'; }

  function issueViewName(params) { return params.get('view') === 'feedback' ? 'feedback' : 'issues'; }

  function issueScope(params) {
    return JSON.stringify([state.organization?.slug, issueViewName(params), params.get('project') || '', params.get('query') || '', params.get('status') || 'unresolved', params.get('environment') || '', params.get('release') || '', params.get('sort') || 'date']);
  }

  function sortedIssueIDs(ids) {
    return [...ids].sort((left, right) => BigInt(left) < BigInt(right) ? -1 : BigInt(left) > BigInt(right) ? 1 : 0);
  }

  function rememberIssueCommentOwners(detail, view) {
    view.knownCommentOwners ??= new Map();
    if (detail.issue.groupSize !== 1) return;
    for (const record of detail.comments || []) {
      view.knownCommentOwners.set(record.id, detail.issue.id);
      if (commentEditDrafts.has(record.id)) commentEditIssueIDs.set(record.id, detail.issue.id);
    }
  }

  function issueDraftCandidates(ids) {
    const selected = new Set(ids);
    const comments = [...commentDrafts].filter(([, draft]) => draft !== '').map(([id]) => id);
    return [...new Set([...comments, ...commentEditIssueIDs.values()])]
      .filter(id => selected.has(id) || selected.has(issueDraftOwners.get(id)));
  }

  function clearDeletedIssueDrafts(ids) {
    for (const id of ids) { commentDrafts.delete(id); issueDraftOwners.delete(id); }
    for (const [commentID, issueID] of commentEditIssueIDs) {
      if (ids.has(issueID)) { commentEditDrafts.delete(commentID); commentEditIssueIDs.delete(commentID); }
    }
  }

  async function confirmDeletedIssueDrafts(organization, ids) {
    const remaining = [...ids];
    const account = state.me;
    await Promise.all(Array.from({ length: Math.min(4, remaining.length) }, async () => {
      while (remaining.length && account && state.me === account) {
        const id = remaining.shift();
        try { await request(`/api/0/organizations/${encodeURIComponent(organization)}/issues/${encodeURIComponent(id)}/`); }
        catch (failure) {
          if (failure.status === 404 && state.me === account) clearDeletedIssueDrafts(new Set([id]));
        }
      }
    }));
  }

  function finishIssueDeletion(organization, ids, view) {
    const candidates = new Set([...ids, ...(view?.knownOriginIDs || [])]);
    const draftIDs = issueDraftCandidates(candidates).filter(id => !view || id !== ids[0]);
    if (view) clearDeletedIssueDrafts(new Set([ids[0]]));
    for (const id of ids) mergeSelection.issues.delete(id);
    const message = view ? 'Issue deleted.' : 'Deletion request completed.';
    issueDeletion = { organization, scope: issueScope(new URLSearchParams(location.search)), message, bulk: !view };
    notify(message);
    navigate({ view: issueViewName(new URLSearchParams(location.search)), issue: null });
    if (draftIDs.length) void confirmDeletedIssueDrafts(organization, draftIDs);
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
    if (Object.hasOwn(changes, 'view') || Object.hasOwn(changes, 'org')) clearAPIToken();
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
      feedback: 'M4 4h16v12H8l-4 4V4ZM8 8h8M8 12h5',
      transactions: 'M4 17l5-6 4 3 7-9m-5 0h5v5',
      logs: 'M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01',
      monitors: 'M2 12h5l3-8 4 16 3-8h5',
      alerts: 'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4',
      projects: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
      'api-tokens': 'M14 7a5 5 0 1 0 3 9l4-4-3-3-2 2M7 7h.01',
      releases: 'M20 12l-8 8L3 11V3h8l9 9ZM7 7h.01',
      account: 'M20 21a8 8 0 0 0-16 0M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z',
      admin: 'M12 3 4 7v5c0 5 3.4 8.2 8 9 4.6-.8 8-4 8-9V7l-8-4Zm-2 6v6m4-6v6',
      people: 'M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
      organization: 'M3 21h18M5 21V7l7-4 7 4v14M9 10h1m4 0h1M9 14h1m4 0h1M9 18h1m4 0h1',
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

  function authFrame(eyebrow, title, description, children, focus) {
    const intro = node('aside', { class: 'auth-intro' }, brand(),
      node('div', {}, node('h2', {}, 'See the error. Find the cause.'), node('p', {}, 'Your application errors, stack traces and release context in one workspace.')),
      node('div', { class: 'auth-foot' }, 'GoodSentry · Error tracking'));
    const box = node('div', { class: 'auth-box' }, node('div', { class: 'eyebrow' }, eyebrow), node('h1', { tabindex: '-1' }, title), node('p', {}, description), children);
    app.replaceChildren(node('div', { class: 'auth-layout' }, intro, node('main', { id: 'main', class: 'auth-main' }, box)));
    focus?.focus();
  }

  function renderAuth(setup, notice = '') {
    clearAPIToken();
    document.querySelectorAll('dialog[open]').forEach(dialog => dialog.close());
    state.projects = [];
    commentDrafts.clear();
    commentEditDrafts.clear();
    commentEditIssueIDs.clear();
    issueDraftOwners.clear();
    issueDeletion = undefined;
    mergeSelection.issues.clear();
    mergeSelection.scope = null;
    issueViewContext = undefined;
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
    if (!setup) form.append(node('button', { type: 'button', class: 'btn button', onclick: () => renderResetRequest() }, 'Forgot password?'));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = setup ? 'Creating workspace…' : 'Signing in…';
      try {
        const admitted = await request(setup ? '/api/v1/setup' : '/api/v1/login', {
          method: 'POST', body: { email: email.input.value, password: password.input.value, ...(setup ? { name: name.input.value, organization: organization.input.value } : {}) },
        });
        if (!setup && admitted && admitted.twoFactorRequired) {
          renderTwoFactorChallenge(admitted.challengeToken, email.input.value);
          return;
        }
        state.me = admitted;
        await loadRoute(true);
      } catch (failure) {
        formError(error, failure);
      } finally {
        submit.disabled = false;
        submit.textContent = setup ? 'Create workspace' : 'Sign in';
      }
    });
    const children = node('div', {}, notice ? node('p', { class: 'form-notice', role: 'status', tabindex: '-1' }, notice) : null, error, form);
    if (!setup && state.authConfig?.oidc?.enabled) {
      children.append(
        node('p', { class: 'muted small', 'aria-hidden': 'true' }, 'or'),
        node('a', { class: 'btn button', href: base + '/auth/oidc/start' }, 'Continue with ' + (state.authConfig.oidc.name || 'single sign-on')));
    }
    const ssoWorkspace = setup ? null : new URLSearchParams(location.search).get('sso');
    if (ssoWorkspace) {
      const sso = node('div', { class: 'form-stack', 'aria-label': 'Workspace single sign-on' },
        node('p', { class: 'small muted' }, 'Loading workspace single sign-on…'));
      children.append(sso);
      request('/api/v1/organizations/' + encodeURIComponent(ssoWorkspace) + '/sso')
        .then(result => {
          if (!sso.isConnected) return;
          const apps = result.apps || [];
          if (!apps.length) {
            sso.replaceChildren(node('p', { class: 'small muted' }, `No single sign-on applications are configured for ${result.organization.name}.`));
            return;
          }
          sso.replaceChildren(
            node('p', { class: 'small muted' }, `Continue to ${result.organization.name} with its identity provider, or sign in with a password below.`),
            ...apps.map(app => node('a', { class: 'btn button', href: app.startUrl }, 'Continue with ' + app.name)));
        })
        .catch(failure => {
          if (sso.isConnected) sso.replaceChildren(node('p', { class: 'form-error' }, failure.message));
        });
    }
    authFrame(setup ? 'First-time setup' : 'Your workspace', setup ? 'Welcome to GoodSentry' : 'Sign in to GoodSentry',
      setup ? 'Create the first owner account and organization. Then connect your application with its project DSN.' : 'Use your account to view and manage application errors.', children, (setup ? name : email).input);
  }

  function renderTwoFactorChallenge(challengeToken, email) {
    document.title = 'Two-factor sign-in · GoodSentry';
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const code = field('Authentication code', 'text', { name: 'code', inputmode: 'numeric', autocomplete: 'one-time-code', required: true, maxlength: 32 });
    code.wrap.append(node('p', { class: 'help' }, 'Enter the six-digit code from your authenticator app, or one of your recovery codes.'));
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Verify and sign in');
    const form = node('form', { class: 'form-stack' }, code.wrap, error, submit);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = 'Verifying…';
      try {
        state.me = await request('/api/v1/login/two-factor', { method: 'POST', body: { challengeToken, code: code.input.value } });
        await loadRoute(true);
      } catch (failure) {
        formError(error, failure);
        if (failure.status === 401) {
          error.append(' ', button('Start over', () => renderAuth(false)));
        }
      } finally {
        if (submit.isConnected) {
          submit.disabled = false;
          submit.textContent = 'Verify and sign in';
        }
      }
    });
    authFrame('Two-factor authentication', 'Confirm it is you', `${email} requires a second step before the workspace opens.`, form, code.input);
  }

  function renderResetRequest() {
    document.title = 'Reset password · GoodSentry';
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const notice = node('div', { class: 'form-notice', role: 'status', tabindex: '-1', hidden: true });
    const email = field('Email', 'email', { name: 'email', autocomplete: 'username', required: true, maxlength: 254 });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Send reset link');
    const form = node('form', { class: 'form-stack' }, email.wrap, notice, error, submit,
      node('button', { type: 'button', class: 'btn button', onclick: () => renderAuth(false) }, 'Back to sign in'));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = 'Sending…';
      try {
        await request('/api/v1/account/password/reset/request', { method: 'POST', body: { email: email.input.value } });
        notice.hidden = false;
        notice.textContent = 'If an account exists for that email, a password reset message will arrive shortly.';
        notice.focus();
      } catch (failure) {
        formError(error, failure);
      } finally {
        submit.disabled = false;
        submit.textContent = 'Send reset link';
      }
    });
    authFrame('Account recovery', 'Reset your password', 'Enter the email address used to sign in. We will send a single-use link when the account exists.', form, email.input);
  }

  function renderResetPassword(token) {
    document.title = 'Choose a new password · GoodSentry';
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const next = field('New password', 'password', { name: 'newPassword', autocomplete: 'new-password', required: true, minlength: 12, maxlength: 72 });
    const confirm = field('Confirm new password', 'password', { name: 'confirmPassword', autocomplete: 'new-password', required: true, minlength: 12, maxlength: 72 });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Set new password');
    const form = node('form', { class: 'form-stack' }, next.wrap, confirm.wrap, error, submit,
      node('button', { type: 'button', class: 'btn button', onclick: () => renderResetRequest() }, 'Request another link'));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      if (next.input.value !== confirm.input.value) {
        error.textContent = 'The new passwords do not match.';
        error.focus();
        return;
      }
      submit.disabled = true;
      submit.textContent = 'Setting password…';
      try {
        await request('/api/v1/account/password/reset', { method: 'POST', body: { token, newPassword: next.input.value } });
        history.replaceState(null, '', routeURL({ resetToken: null }));
        renderAuth(false, 'Password reset. Sign in with your new password.');
      } catch (failure) {
        formError(error, failure);
      } finally {
        if (form.isConnected) {
          submit.disabled = false;
          submit.textContent = 'Set new password';
        }
      }
    });
    authFrame('Account recovery', 'Choose a new password', 'This link can be used once. Setting the password signs out every existing session and revokes personal API tokens.', form, next.input);
  }

  async function renderInvitation(token) {
    document.title = 'Join your workspace · GoodSentry';
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const loading = node('div', { class: 'form-notice', role: 'status' }, 'Checking this invitation…');
    authFrame('Workspace invitation', 'Join GoodSentry', 'Validating the single-use invitation link.', node('div', {}, loading, error));
    let invitation;
    try {
      invitation = await request('/api/v1/invitations/inspect', { method: 'POST', body: { token } });
    } catch (failure) {
      error.textContent = failure.message;
      return;
    }
    const name = field('Your name', 'text', { name: 'name', autocomplete: 'name', required: !invitation.existingAccount, maxlength: 120 });
    const password = field(invitation.existingAccount ? 'Your current password' : 'Choose a password', 'password', { name: 'password', autocomplete: invitation.existingAccount ? 'current-password' : 'new-password', required: true, minlength: 12, maxlength: 72 });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, invitation.existingAccount ? 'Sign in and join' : 'Create account and join');
    const form = node('form', { class: 'form-stack' }, invitation.existingAccount ? null : name.wrap, password.wrap, error, submit);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      submit.disabled = true;
      submit.textContent = 'Joining…';
      try {
        state.me = await request('/api/v1/invitations/accept', { method: 'POST', body: { token, password: password.input.value, ...(invitation.existingAccount ? {} : { name: name.input.value }) } });
        history.replaceState(null, '', routeURL({ inviteToken: null }));
        await loadRoute(true);
      } catch (failure) {
        formError(error, failure);
        submit.disabled = false;
        submit.textContent = invitation.existingAccount ? 'Sign in and join' : 'Create account and join';
      }
    });
    const details = node('div', { class: 'invitation-details' }, node('p', {}, node('strong', {}, invitation.organizationName), ' invited ', node('strong', {}, invitation.email), ` as ${invitation.role}.`));
    authFrame('Workspace invitation', `Join ${invitation.organizationName}`, invitation.existingAccount ? 'Use your existing account password to accept.' : 'Choose your name and a password for the new account.', node('div', {}, details, form), invitation.existingAccount ? password.input : name.input);
  }

  function renderWizard(hash) {
    document.title = 'Connect Sentry Wizard · GoodSentry';
    const organizations = (state.me.organizations || []).filter(item => ['owner', 'admin'].includes(item.role));
    if (!organizations.length) {
      authFrame('Sentry setup', 'Organization permission required',
        'Sentry Wizard needs an owner or admin who can issue a source-map credential.',
        node('div', { class: 'form-stack' }, node('p', { class: 'muted' }, 'Ask an organization owner or admin to run the wizard.'),
          node('a', { class: 'btn button', href: routeURL({ wizard: null, view: null }) }, 'Open dashboard')));
      return;
    }
    const requested = new URLSearchParams(location.search).get('org');
    const selected = organizations.some(item => item.slug === requested) ? requested : organizations[0].slug;
    const organization = selectField('Organization', organizations.map(item => [item.slug, item.name]), selected, { required: true });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Connect sentry-wizard');
    const form = node('form', { class: 'form-stack', 'aria-label': 'Connect Sentry Wizard' }, organization.wrap, error, submit);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = 'Connecting…';
      try {
        await request('/api/0/wizard-set-token/', { method: 'POST', body: { hash, organization: organization.input.value } });
        const organizationName = organizations.find(item => item.slug === organization.input.value)?.name || organization.input.value;
        authFrame('Sentry setup', 'Sentry Wizard is connected',
          `Return to your terminal. ${organizationName} setup data is available to the wizard for ten minutes.`,
          node('div', { class: 'form-stack' },
            node('p', { class: 'form-notice', role: 'status' }, 'The terminal should finish setup automatically.'),
            node('a', { class: 'btn button', href: routeURL({ wizard: null, view: null }) }, 'Open dashboard')));
      } catch (failure) {
        formError(error, failure);
        submit.disabled = false;
        submit.textContent = 'Connect sentry-wizard';
      }
    });
    authFrame('Sentry setup', 'Connect Sentry Wizard',
      'Choose the organization this project belongs to. GoodSentry will issue a short-lived source-map credential for the wizard.',
      form, organization.input);
  }

  function renderShell(view) {
    const organizations = state.me.organizations || [];
    const org = state.organization;
    const authRequired = state.me.features?.authRequired !== false;
    const selector = node('select', { class: 'select org-select', 'aria-label': 'Organization', onchange: event => {
      navigate({ ...traceReset, org: event.target.value, issue: null, monitor: null, project: null, release: null, alertProject: null, rule: null, delivery: null, sampleSource: null, sample: null, spanGroupsSource: null, spanGroups: null, traceMatch: null, version: null, commitPage: null, deployPage: null, sourceMaps: null, page: null, changes: null });
    } }, organizations.map(item => node('option', { value: item.slug }, item.name)));
    if (org) selector.value = org.slug;
    const currentParams = new URLSearchParams(location.search);
    const nav = node('nav', { class: 'nav', 'aria-label': 'Main navigation' },
      ['issues', 'feedback', 'transactions', 'logs', 'monitors', 'alerts', 'releases', 'projects', 'api-tokens', 'people', 'organization', 'admin']
        .filter(section => section !== 'monitors' || state.me.features?.uptime === true)
        .filter(section => section !== 'logs' || state.me.features?.logs !== false)
        .filter(section => section !== 'alerts' || (state.me.features?.alerts === true && ['owner', 'admin'].includes(org?.role)))
        .filter(section => section !== 'api-tokens' || ['owner', 'admin'].includes(org?.role))
        .filter(section => section !== 'people' || ['owner', 'manager', 'admin'].includes(org?.role))
        .filter(section => section !== 'organization' || ['owner', 'manager', 'admin'].includes(org?.role))
        .filter(section => section !== 'admin' || (state.me.features?.admin === true && (state.me.user?.isInstanceAdmin === true || ['owner', 'admin'].includes(org?.role))))
        .map(section =>
        routeLink([navigationIcon(section), viewNames[section]],
          { ...traceReset, view: section, issue: null, monitor: null, alertProject: null, rule: null, delivery: null, sampleSource: null, sample: null, spanGroupsSource: null, spanGroups: null, traceMatch: null, version: null, commitPage: null, deployPage: null, sourceMaps: null, page: null, changes: null, query: null, status: null, sort: null, environment: null, release: null, level: null, trace: null, service: null, window: null, start: null, end: null,
            ...(isIssueView(view) && isIssueView(section) ? Object.fromEntries(['query', 'status', 'sort', 'environment', 'release'].map(name => [name, currentParams.get(name)])) : {}) },
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
    const accountLink = routeLink('Account', { ...traceReset, view: 'account', issue: null, monitor: null, project: null, release: null, alertProject: null, rule: null, delivery: null, sampleSource: null, sample: null, spanGroupsSource: null, spanGroups: null, traceMatch: null, version: null, commitPage: null, deployPage: null, sourceMaps: null, page: null, changes: null, query: null, status: null, sort: null, environment: null, level: null, trace: null, service: null, window: null, start: null, end: null }, { class: 'btn btn-sm account-link' });
    const sidebar = node('aside', { class: 'sidebar' }, brand(),
      node('div', { class: 'org-switch' }, node('p', { class: 'org-label' }, 'Organization'), selector), nav,
      node('div', { class: 'account' }, node('strong', {}, text(state.me.user.name, state.me.user.email)), node('span', { class: 'small' }, state.me.user.email),
        node('div', { class: 'account-actions' }, accountLink, authRequired ? logout : node('span', { class: 'badge warning' }, 'Open access'))));
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
    releasesUI?.closeDialogs();
    projectKeysUI?.closeDialogs();
    if (!state.me) return;
    const version = ++state.requestVersion;
    const params = new URLSearchParams(location.search);
    const wizard = params.get('wizard');
    if (wizard) {
      if (/^[0-9a-f]{64}$/.test(wizard)) {
        renderWizard(wizard);
      } else {
        history.replaceState(null, '', routeURL({ wizard: null, view: null }));
        renderAuth(false, 'This wizard link is invalid or expired. Start sentry-wizard again.');
      }
      return;
    }
    state.organization = state.me.organizations?.find(item => item.slug === params.get('org')) || state.me.organizations?.[0] || null;
    const requestedView = params.get('view');
    let view = Object.prototype.hasOwnProperty.call(viewNames, requestedView) ? requestedView : 'issues';
    const scope = issueScope(params);
    if (issueDeletion && (!isIssueView(view) || issueDeletion.organization !== state.organization?.slug || issueDeletion.scope !== scope)) issueDeletion = undefined;
    if (!isIssueView(view) || mergeSelection.scope !== scope) {
      mergeSelection.issues.clear();
      mergeSelection.scope = scope;
    }
    if (!isIssueView(view) || !params.get('issue') || issueViewContext?.organization !== state.organization?.slug || issueViewContext?.issueId !== params.get('issue')) issueViewContext = undefined;
    if (view !== 'api-tokens' || issuedAPIToken?.organization !== state.organization?.slug) clearAPIToken();
    let main = renderShell(view);
    if (view === 'account') {
      renderAccount(main);
      if (focus) main.querySelector('h1')?.focus();
      return;
    }
    if (view === 'admin') {
      if (state.me.features?.admin !== true) {
        main.append(pageHeader(viewNames[view], 'Administration is disabled on this server.'));
        return;
      }
      loading(main, 'Loading administration…');
      try {
        let overview = null;
        try {
          overview = await request('/api/v1/admin/overview');
        } catch (error) {
          if (error.status !== 403 || state.me.user?.isInstanceAdmin !== true) throw error;
        }
        if (version !== state.requestVersion) return;
        let users = null;
        if (state.me.user?.isInstanceAdmin === true) {
          const query = new URLSearchParams({ page: String(Math.max(1, parseInt(params.get('page'), 10) || 1)), perPage: '25' });
          if (params.get('query')) query.set('query', params.get('query'));
          users = await request(`/api/v1/admin/users?${query}`);
          if (version !== state.requestVersion) return;
        }
        renderAdmin(main, overview, users, params);
      } catch (error) {
        if (version === state.requestVersion) errorState(main, error, () => loadRoute(focus));
      }
      if (focus) main.querySelector('h1')?.focus();
      return;
    }
    if (!state.organization) {
      main.append(node('div', { class: 'card panel no-org' }, empty('No organization access', 'Your account does not belong to an organization. Contact your workspace owner to request access.')));
      return;
    }
    if ((view === 'monitors' && state.me.features?.uptime !== true) || (view === 'logs' && state.me.features?.logs === false) || (view === 'admin' && state.me.features?.admin !== true)) {
      main.append(pageHeader(viewNames[view], `${viewNames[view]} is disabled on this server.`));
      return;
    }
    if (view === 'api-tokens' && !['owner', 'admin'].includes(state.organization.role)) {
      main.append(pageHeader('API tokens', 'Organization owners and admins manage automation credentials.'),
        node('section', { class: 'card panel' }, empty('Permission required', 'Ask an organization owner or admin to manage API tokens.')));
      return;
    }
    if (view === 'people' && !['owner', 'manager', 'admin'].includes(state.organization.role)) {
      main.append(pageHeader('People and teams', 'Organization owners, managers and admins manage members and teams.'),
        node('section', { class: 'card panel' }, empty('Permission required', 'Ask an organization owner, manager or admin to manage people and teams.')));
      return;
    }
    loading(main, isIssueView(view) && params.get('issue') ? 'Loading issue…' : `Loading ${viewNames[view].toLowerCase()}…`);
    try {
      const projects = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects`);
      if (version !== state.requestVersion) return;
      state.projects = projects.data || [];
      if (params.get('issue') && isIssueView(view)) {
        const requestedID = params.get('issue');
        const detail = await request(`/api/v1/issues/${encodeURIComponent(requestedID)}`);
        if (version !== state.requestVersion) return;
        const categoryView = detail.issue.category === 'feedback' ? 'feedback' : 'issues';
        const changedView = view !== categoryView;
        if (changedView) {
          view = categoryView;
          params.set('view', view);
          mergeSelection.issues.clear();
          mergeSelection.scope = issueScope(params);
          issueDeletion = undefined;
          history.replaceState(null, '', routeURL({ view, issue: detail.issue.id }));
          main = renderShell(view);
        }
        const detailView = issueViewContext?.view || { draftIssueId: requestedID };
        delete detailView.commentsResult;
        rememberIssueCommentOwners(detail, detailView);
        if (detail.issue.id !== requestedID && !changedView) history.replaceState(null, '', routeURL({ issue: detail.issue.id }));
        renderIssue(main, detail, detailView);
      } else if (view === 'projects') {
        renderProjects(main);
      } else if (view === 'api-tokens') {
        const query = new URLSearchParams({ page: params.has('page') ? params.get('page') : '1' });
        const result = await request(`${apiTokenPath()}?${query}`);
        if (version !== state.requestVersion) return;
        renderAPITokens(main, result);
      } else if (view === 'people') {
        const { createPeopleUI } = await import('./people.js');
        if (version !== state.requestVersion) return;
        peopleUI ||= createPeopleUI({ state, node, request, button, field, pageHeader, empty, formError, notify });
        await peopleUI.render(main, params, () => version === state.requestVersion);
      } else if (view === 'organization') {
        await renderOrganizationSettings(main, version, params);
      } else if (view === 'releases') {
        const { createReleasesUI } = await import('./releases.js');
        if (version !== state.requestVersion) return;
        releasesUI ||= createReleasesUI({ state, node, request, navigate, routeLink, button, field, selectField, pageHeader, empty, formError, notify, number, text, errorState, reload: () => loadRoute() });
        await releasesUI.render(main, params, () => version === state.requestVersion);
      } else if (view === 'alerts') {
        const { createAlertsUI } = await import('./alerts.js');
        if (version !== state.requestVersion) return;
        alertsUI ||= createAlertsUI({ state, node, request, navigate, routeLink, button, field, selectField, pageHeader, empty, keyValues, formError, notify, date, number, text, reload: () => loadRoute() });
        await alertsUI.render(main, params, () => version === state.requestVersion);
      } else if (view === 'monitors') {
        const path = monitorPath(params.get('monitor'));
        if (!params.get('monitor')) {
          const [monitors, statusPages] = await Promise.all([request(path), request(statusPagePath())]);
          if (version !== state.requestVersion) return;
          renderMonitors(main, monitors, statusPages);
        } else {
          const historyQuery = new URLSearchParams({ changes: params.get('changes') === 'false' ? 'false' : 'true', page: String(Math.max(1, parseInt(params.get('page'), 10) || 1)) });
          const historyIsLatest = historyQuery.get('changes') === 'false' && historyQuery.get('page') === '1';
          const [monitor, history, latest] = await Promise.all([request(path), request(`${path}/checks?${historyQuery}`), historyIsLatest ? Promise.resolve(null) : request(`${path}/checks?changes=false&page=1`)]);
          if (version !== state.requestVersion) return;
          renderMonitor(main, monitor, history, (latest || history).data?.[0], params);
        }
      } else if (view === 'transactions' && params.has('traceView')) {
        await loadTrace(main, params, version);
      } else if (view === 'transactions' && (params.has('spanGroupsSource') || params.has('spanGroups'))) {
        const source = params.get('spanGroupsSource');
        const id = params.get('spanGroups');
        if (!source || !id) throw new Error('This operations link is incomplete. Open Operations from the Transactions list.');
        const query = new URLSearchParams({ page: params.has('page') ? params.get('page') : '1' });
        for (const name of ['project', 'start', 'end']) if (params.has(name)) query.set(name, params.get(name));
        let result;
        try {
          result = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/transactions/${encodeURIComponent(source)}/${encodeURIComponent(id)}/span-groups?${query}`);
        } catch (error) {
          if (error.status !== 503) throw error;
          if (version === state.requestVersion) {
            renderSpanGroupsPending(main);
            if (focus) main.querySelector('h1')?.focus();
          }
          return;
        }
        if (version !== state.requestVersion) return;
        renderSpanGroups(main, result, params);
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
        const filters = view === 'transactions' ? ['project', 'start', 'end'] : ['project', 'query', 'level', 'environment', 'trace', 'traceMatch', 'service', 'start', 'end', 'page'];
        for (const name of filters) {
          if (params.has(name)) query.set(name, params.get(name));
        }
        const result = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/${view}?${query}`);
        if (version !== state.requestVersion) return;
        if (view === 'transactions') renderTransactions(main, result, params);
        else renderLogs(main, result, params);
      } else {
        const query = new URLSearchParams();
        for (const name of ['project', 'query', 'environment', 'release', 'status', 'sort', 'page']) {
          if (params.get(name)) query.set(name, params.get(name));
        }
        if (!query.has('status')) query.set('status', 'unresolved');
        if (!query.has('sort')) query.set('sort', 'date');
        query.set('category', view === 'feedback' ? 'feedback' : 'error');
        const issues = await request(`/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/issues?${query}`);
        if (version !== state.requestVersion) return;
        const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
        const lastPage = Math.max(1, Math.ceil((Number(issues.total) || 0) / pageSize));
        if (issueDeletion && !issues.data?.length && page > lastPage) {
          history.replaceState(null, '', routeURL({ page: lastPage === 1 ? null : lastPage }));
          return loadRoute(focus);
        }
        renderIssues(main, issues, params);
      }
      if (focus) main.querySelector('h1')?.focus();
    } catch (error) {
      if (version === state.requestVersion) {
        if (params.has('traceView') && view === 'transactions') renderTraceFailure(main, error, false);
        else if (isIssueView(view) && !params.get('issue') && state.me) renderIssues(main, null, params, error);
        else errorState(main, error, () => loadRoute(focus));
      }
    }
  }

  function renderAdmin(main, overview, users, params) {
    document.title = 'Administration · GoodSentry';
    const instance = overview?.instance || {};
    const organizations = overview?.organizations || [];
    const featureNames = { logs: 'Logs', uptime: 'Uptime', mcp: 'MCP', openapi: 'OpenAPI', admin: 'Administration', alerts: 'Alerts', alertDelivery: 'Alert delivery' };
    const retentionNames = { events: 'Events', logs: 'Logs', transactions: 'Transactions and spans', uptime: 'Uptime checks', files: 'Files and attachments', releases: 'Release metadata' };
    const stat = (label, value) => node('div', { class: 'stat' }, node('dt', {}, label), node('dd', {}, value));
    const organizationRows = organizations.map(organization => node('tr', {},
      node('td', {}, node('strong', {}, organization.name), node('div', { class: 'small muted mono' }, organization.slug)),
      node('td', {}, badge(organization.role)),
      node('td', { class: 'nowrap' }, number(organization.projects)),
      node('td', { class: 'nowrap' }, number(organization.members)),
      node('td', { class: 'nowrap' }, number(organization.issues)),
      node('td', { class: 'nowrap' }, number(organization.events)),
      node('td', { class: 'nowrap' }, number(organization.logs))));
    const features = Object.entries(overview?.features || {}).map(([name, enabled]) =>
      node('span', { class: `badge ${enabled ? 'resolved' : 'ignored'}` }, `${featureNames[name] || name}: ${enabled ? 'on' : 'off'}`));
    const retention = Object.entries(overview?.retention || {}).map(([name, days]) =>
      node('tr', {}, node('td', {}, retentionNames[name] || name), node('td', { class: 'nowrap' }, `${number(days)} days`)));
    const sections = [pageHeader('Administration', overview
      ? 'Operational summary for organizations where you currently have owner or admin authority.'
      : 'Instance user management for current instance administrators.', button('Refresh', () => loadRoute()))];
    if (overview) sections.push(
      node('section', { class: 'card panel' },
        node('div', { class: 'panel-title' }, node('h2', {}, 'Instance'), node('span', { class: 'badge' }, instance.goVersion || 'Go')),
        node('dl', { class: 'stat-list' }, stat('Service', instance.name || 'GoodSentry'), stat('Version', instance.version || 'Unknown'), stat('Authentication', instance.authMode || 'Unknown')),
        node('div', { class: 'panel-body form-stack' }, node('p', { class: 'small muted' }, `${instance.database || 'Database state unavailable'}. Open mode is intended for local use or a trusted private network; enable GOODSENTRY_AUTH=required before public exposure.`))),
      node('section', { class: 'card panel' },
        node('div', { class: 'panel-title' }, node('h2', {}, 'Managed organizations'), node('span', { class: 'small muted' }, `${number(organizations.length)} visible`)),
        node('div', { class: 'table-scroll' }, node('table', {},
          node('thead', {}, node('tr', {}, ['Organization', 'Your role', 'Projects', 'Members', 'Issues', 'Events', 'Logs'].map(name => node('th', { scope: 'col' }, name)))),
          node('tbody', {}, organizationRows)))),
      node('section', { class: 'card panel panel-body form-stack' },
        node('h2', {}, 'Capabilities'), node('div', { class: 'actions' }, features),
        node('h2', {}, 'Retention'),
        node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, node('th', { scope: 'col' }, 'Data'), node('th', { scope: 'col' }, 'Lifetime'))), node('tbody', {}, retention))))
    );
    if (users) sections.push(renderAdminUsers(users, params));
    main.replaceChildren(...sections);
  }

  function renderAdminUsers(users, params) {
    const query = params.get('query') || '';
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const search = field('Filter by email or name', 'search', { name: 'query', maxlength: 100, value: query });
    const searchForm = node('form', { class: 'actions', 'aria-label': 'Search users' }, search.wrap,
      button('Search', () => searchForm.requestSubmit(), 'small'));
    searchForm.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ view: 'admin', query: search.input.value.trim() || null, page: null });
    });
    const rows = (users.data || []).map(user => {
      const accountState = node('span', { class: `badge ${user.isActive ? 'resolved' : 'ignored'}` }, user.isActive ? 'Active' : 'Suspended');
      const adminState = node('span', { class: `badge ${user.isInstanceAdmin ? 'resolved' : 'ignored'}` }, user.isInstanceAdmin ? 'Instance admin' : 'Standard access');
      const controls = node('div', { class: 'actions' });
      const activity = button(user.isActive ? 'Suspend' : 'Reactivate', async () => {
        activity.disabled = true;
        try {
          await request(`/api/v1/admin/users/${encodeURIComponent(user.id)}`, { method: 'PATCH', body: { isActive: !user.isActive } });
          notify(`${user.email} updated.`);
          state.me = await request('/api/v1/me');
          if (!state.me.user?.isInstanceAdmin && !(state.me.organizations || []).some(item => ['owner', 'admin'].includes(item.role))) {
            history.replaceState(null, '', routeURL({ view: 'issues', query: null, page: null }));
          }
          await loadRoute();
        } catch (error) { notify(error.message); activity.disabled = false; }
      }, 'small');
      const admin = button(user.isInstanceAdmin ? 'Remove admin' : 'Make admin', async () => {
        admin.disabled = true;
        try {
          await request(`/api/v1/admin/users/${encodeURIComponent(user.id)}`, { method: 'PATCH', body: { isInstanceAdmin: !user.isInstanceAdmin } });
          notify(`${user.email} updated.`);
          state.me = await request('/api/v1/me');
          if (!state.me.user?.isInstanceAdmin && !(state.me.organizations || []).some(item => ['owner', 'admin'].includes(item.role))) {
            history.replaceState(null, '', routeURL({ view: 'issues', query: null, page: null }));
          }
          await loadRoute();
        } catch (error) { notify(error.message); admin.disabled = false; }
      }, 'small');
      if (!user.isActive) admin.disabled = true;
      controls.append(activity, admin);
      return node('tr', {},
        node('td', {}, node('strong', {}, user.email), node('div', { class: 'small muted' }, user.name)),
        node('td', { class: 'nowrap' }, number(user.organizationCount)),
        node('td', {}, accountState),
        node('td', {}, adminState),
        node('td', {}, controls));
    });
    const previous = button('Previous', () => navigate({ view: 'admin', query: query || null, page: page > 2 ? page - 1 : null }), 'small');
    const next = button('Next', () => navigate({ view: 'admin', query: query || null, page: page + 1 }), 'small');
    previous.disabled = page <= 1;
    next.disabled = page * users.perPage >= users.total;
    return node('section', { class: 'card panel form-stack' },
      node('div', { class: 'panel-title' }, node('h2', {}, 'Account users'), node('span', { class: 'small muted' }, `${number(users.total)} accounts`)),
      searchForm,
      node('div', { class: 'table-scroll' }, node('table', {},
        node('thead', {}, node('tr', {}, ['User', 'Organizations', 'Account', 'Instance access', 'Actions'].map(name => node('th', { scope: 'col' }, name)))),
        node('tbody', {}, rows.length ? rows : node('tr', {}, node('td', { colspan: '5' }, empty('No users found', query ? 'Try a different email or name.' : 'No accounts exist yet.')))))),
      node('div', { class: 'pagination' }, node('span', {}, `Page ${page}`), node('div', { class: 'actions' }, previous, next)));
  }

  function renderAccount(main) {
    document.title = 'Account · GoodSentry';
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const current = field('Current password', 'password', { name: 'currentPassword', autocomplete: 'current-password', required: true, maxlength: 72 });
    const next = field('New password', 'password', { name: 'newPassword', autocomplete: 'new-password', required: true, minlength: 12, maxlength: 72 });
    const confirm = field('Confirm new password', 'password', { name: 'confirmPassword', autocomplete: 'new-password', required: true, minlength: 12, maxlength: 72 });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Change password');
    const form = node('form', { class: 'form-stack account-form', 'aria-label': 'Change password' }, current.wrap, next.wrap, confirm.wrap, submit);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      if (next.input.value !== confirm.input.value) {
        error.textContent = 'The new passwords do not match.';
        error.focus();
        return;
      }
      submit.disabled = true;
      submit.textContent = 'Changing password…';
      try {
        await request('/api/v1/account/password', { method: 'POST', body: { currentPassword: current.input.value, newPassword: next.input.value } });
        state.me = null;
        state.requestVersion++;
        renderAuth(false, 'Password changed. Sign in with your new password.');
      } catch (failure) {
        if (!state.me) return;
        if (failure.status === 503 || !failure.status) {
          error.textContent = 'The password change could not be confirmed. Sign in with your new password to verify the result.';
          error.append(' ', button('Go to sign in', () => {
            state.me = null;
            state.requestVersion++;
            renderAuth(false, 'Verify your account by signing in with your new password.');
          }));
          error.focus();
        } else {
          formError(error, failure);
        }
      } finally {
        if (form.isConnected) {
          submit.disabled = false;
          submit.textContent = 'Change password';
        }
      }
    });
    const identity = node('div', { class: 'account-identity' }, node('strong', {}, text(state.me.user.name, state.me.user.email)), node('span', { class: 'muted' }, state.me.user.email));
    const emailPanel = node('section', { class: 'card panel account-panel', 'aria-label': 'Email addresses' });
    const twoFactorPanel = node('section', { class: 'card panel account-panel', 'aria-label': 'Two-factor authentication' });
    const deletePanel = node('section', { class: 'card panel account-panel', 'aria-label': 'Delete account' });
    main.replaceChildren(
      node('div', { class: 'page-header' }, node('div', {}, node('div', { class: 'eyebrow' }, 'Your account'), node('h1', { tabindex: '-1' }, 'Account'),
        node('p', {}, 'Manage the credentials used to sign in to GoodSentry.'))),
      node('section', { class: 'card panel panel-body account-panel' }, identity,
        node('p', { class: 'small muted' }, 'Changing your password signs out every session and revokes personal API tokens in every organization. Project SDK keys keep working.'),
        error, form), emailPanel, twoFactorPanel, deletePanel);

    const deleteError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const deleteControl = button('Delete account', openDeleteAccount, 'danger');
    deletePanel.append(
      node('div', { class: 'panel-title' }, node('h2', {}, 'Delete account')),
      node('div', { class: 'panel-body form-stack' },
        node('p', { class: 'muted' }, 'Deleting your account removes your sessions, API tokens, memberships, teams, verified email addresses, linked sign-in identities and sent invitations. This cannot be undone.'),
        node('p', { class: 'small muted' }, 'If you are the only owner of an organization, add another owner in People before deleting your account.'),
        deleteError, deleteControl));

    function openDeleteAccount() {
      const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'delete-account-title' });
      const dialogError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      const typedEmail = field('Type your email address', 'email', { name: 'confirmEmail', autocomplete: 'off', required: true, maxlength: 254 });
      const confirm = node('button', { type: 'submit', class: 'btn button danger', disabled: true }, 'Delete account permanently');
      const form = node('form', { class: 'form-stack', 'aria-label': 'Confirm account deletion' },
        dialogError,
        node('p', {}, `Delete ${state.me.user.email}? Your organization data remains available to its other members.`),
        typedEmail.wrap,
        node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), confirm));
      typedEmail.input.addEventListener('input', () => {
        confirm.disabled = typedEmail.input.value.trim().toLowerCase() !== state.me.user.email.toLowerCase();
      });
      form.addEventListener('submit', async event => {
        event.preventDefault();
        dialogError.textContent = '';
        confirm.disabled = true;
        confirm.textContent = 'Deleting…';
        try {
          await request('/api/0/users/me/', { method: 'DELETE' });
          dialog.close();
          state.me = null;
          state.requestVersion++;
          renderAuth(false, 'Your account has been deleted.');
        } catch (failure) {
          formError(dialogError, failure);
          confirm.disabled = typedEmail.input.value.trim().toLowerCase() !== state.me.user.email.toLowerCase();
          confirm.textContent = 'Delete account permanently';
        }
      });
      dialog.append(node('header', {}, node('h2', { id: 'delete-account-title' }, 'Delete account?'), button('Close', () => dialog.close(), 'small')), form);
      dialog.addEventListener('close', () => dialog.remove());
      document.body.append(dialog);
      dialog.showModal();
      typedEmail.input.focus();
    }

    const emailPath = `/api/0/users/${encodeURIComponent(state.me.user.id)}/emails/`;
    const emailError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const emailInput = field('New email address', 'email', { name: 'email', autocomplete: 'email', required: true, maxlength: 254 });
    const addEmail = node('button', { type: 'submit', class: 'btn button primary' }, 'Add email');
    const emailForm = node('form', { class: 'form-stack', 'aria-label': 'Add email address' }, emailError, emailInput.wrap, addEmail);
    const emailBody = node('div', { class: 'panel-body form-stack' });
    emailPanel.append(node('div', { class: 'panel-title' }, node('h2', {}, 'Email addresses'), node('span', { class: 'badge' }, 'Password sign-in')));
    emailPanel.append(emailBody);

    function updateIdentity() {
      identity.replaceChildren(node('strong', {}, text(state.me.user.name, state.me.user.email)), node('span', { class: 'muted' }, state.me.user.email));
    }

    async function emailAction(path, method, body, success) {
      emailError.textContent = '';
      try {
        const result = await request(path, { method, body });
        notify(success);
        await loadEmails();
        return result;
      } catch (failure) {
        formError(emailError, failure);
        emailError.focus();
        return null;
      }
    }

    function openRemoveEmail(item) {
      const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'remove-email-title' });
      const removeError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      const remove = node('button', { type: 'submit', class: 'btn button danger' }, 'Remove email');
      const removeForm = node('form', { class: 'form-stack' }, node('h2', { id: 'remove-email-title' }, 'Remove email?'), removeError,
        node('p', { class: 'wrap-anywhere' }, `Remove ${item.email} from this account?`), node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), remove));
      removeForm.addEventListener('submit', async event => {
        event.preventDefault(); remove.disabled = true;
        try {
          await request(emailPath, { method: 'DELETE', body: { email: item.email } });
          dialog.close(); notify('Email address removed.'); await loadEmails();
        } catch (failure) { formError(removeError, failure); removeError.focus(); }
        finally { remove.disabled = false; }
      });
      dialog.append(removeForm);
      emailPanel.append(dialog); dialog.showModal(); remove.focus();
    }

    async function loadEmails() {
      emailBody.replaceChildren(node('p', { class: 'small muted' }, 'Loading email addresses…'));
      let emails;
      try { emails = await request(emailPath); }
      catch (failure) {
        emailBody.replaceChildren(node('div', {}, emailError, empty('Could not load email addresses', failure.message, button('Try again', loadEmails))));
        return;
      }
      emailError.textContent = '';
      const list = node('div', { class: 'release-project-list', role: 'list' });
      for (const item of emails) {
        const badges = node('div', { class: 'actions' });
        if (item.isPrimary) badges.append(node('span', { class: 'badge resolved' }, 'Primary'));
        badges.append(node('span', { class: 'badge ' + (item.isVerified ? 'resolved' : 'warning') }, item.isVerified ? 'Verified' : 'Pending verification'));
        const actions = node('div', { class: 'actions' });
        if (!item.isVerified) actions.append(button('Resend verification', () => emailAction(emailPath + 'confirm/', 'POST', { email: item.email }, 'Verification email sent.')));
        if (item.isVerified && !item.isPrimary) actions.append(button('Make primary', async () => {
          const updated = await emailAction(emailPath, 'PUT', { email: item.email }, 'Primary email updated.');
          if (updated) { state.me.user.email = updated.email; updateIdentity(); }
        }));
        if (!item.isPrimary) actions.append(button('Remove', () => openRemoveEmail(item), 'danger'));
        list.append(node('div', { class: 'release-project', role: 'listitem' }, node('div', {}, node('strong', { class: 'wrap-anywhere' }, item.email), badges), actions));
      }
      emailBody.replaceChildren(emailError, list, emailForm);
    }
    emailForm.addEventListener('submit', async event => {
      event.preventDefault(); emailError.textContent = ''; addEmail.disabled = true; addEmail.textContent = 'Adding…';
      try {
        await request(emailPath, { method: 'POST', body: { email: emailInput.input.value } });
        emailInput.input.value = ''; notify('Verification email sent.'); await loadEmails(); emailInput.input.focus();
      } catch (failure) { formError(emailError, failure); emailError.focus(); }
      finally { if (addEmail.isConnected) { addEmail.disabled = false; addEmail.textContent = 'Add email'; } }
    });
    const twoFactorPath = '/api/v1/me/two-factor';
    const twoFactorError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const twoFactorBody = node('div', { class: 'panel-body form-stack' });
    let pendingRecoveryCodes = null;
    twoFactorPanel.append(node('div', { class: 'panel-title' }, node('h2', {}, 'Two-factor authentication')), twoFactorBody);

    function recoveryCodeList(codes) {
      const list = node('div', { class: 'release-project-list', role: 'list' });
      for (const code of codes) list.append(node('div', { class: 'release-project', role: 'listitem' }, node('code', { class: 'mono wrap-anywhere' }, code)));
      return list;
    }

    function openTwoFactorPassword(title, description, action, success) {
      const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'two-factor-password-title' });
      const dialogError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      const password = field('Current password', 'password', { name: 'password', autocomplete: 'current-password', required: true, maxlength: 72 });
      const submit = node('button', { type: 'submit', class: 'btn button danger' }, title);
      const form = node('form', { class: 'form-stack' }, node('h2', { id: 'two-factor-password-title' }, title), dialogError,
        node('p', { class: 'muted' }, description), password.wrap, node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), submit));
      form.addEventListener('submit', async event => {
        event.preventDefault();
        dialogError.textContent = '';
        submit.disabled = true;
        try {
          const result = await action(password.input.value);
          dialog.close();
          notify(success);
          if (result && result.recoveryCodes) pendingRecoveryCodes = result.recoveryCodes;
          await loadTwoFactor();
        } catch (failure) {
          formError(dialogError, failure);
        } finally {
          submit.disabled = false;
        }
      });
      dialog.append(form);
      twoFactorPanel.append(dialog);
      dialog.showModal();
      password.input.focus();
    }

    function showEnrollment(enrollment) {
      const code = field('Authentication code', 'text', { name: 'code', inputmode: 'numeric', autocomplete: 'one-time-code', required: true, maxlength: 6 });
      const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Confirm and enable');
      const form = node('form', { class: 'form-stack', 'aria-label': 'Confirm two-factor enrollment' }, code.wrap, submit);
      form.addEventListener('submit', async event => {
        event.preventDefault();
        twoFactorError.textContent = '';
        submit.disabled = true;
        submit.textContent = 'Confirming…';
        try {
          const result = await request(twoFactorPath + '/confirm', { method: 'POST', body: { code: code.input.value } });
          pendingRecoveryCodes = result.recoveryCodes;
          notify('Two-factor authentication enabled.');
          await loadTwoFactor();
        } catch (failure) {
          formError(twoFactorError, failure);
        } finally {
          if (submit.isConnected) {
            submit.disabled = false;
            submit.textContent = 'Confirm and enable';
          }
        }
      });
      twoFactorBody.replaceChildren(twoFactorError,
        node('p', { class: 'muted' }, 'Add this secret to your authenticator app, then confirm with the six-digit code it shows. Keep the secret until the code is accepted.'),
        node('div', { class: 'two-factor-value' }, node('code', { class: 'mono wrap-anywhere' }, enrollment.secret), button('Copy secret', () => copyValue(enrollment.secret, 'Secret'), 'small')),
        node('div', { class: 'two-factor-value' }, node('code', { class: 'mono wrap-anywhere' }, enrollment.otpauthUrl), button('Copy setup link', () => copyValue(enrollment.otpauthUrl, 'Setup link'), 'small')),
        form,
        node('div', { class: 'actions' }, button('Cancel', () => loadTwoFactor())));
      code.input.focus();
    }

    async function startEnrollment() {
      twoFactorError.textContent = '';
      let enrollment;
      try {
        enrollment = await request(twoFactorPath, { method: 'POST' });
      } catch (failure) {
        formError(twoFactorError, failure);
        return;
      }
      showEnrollment(enrollment);
    }

    async function loadTwoFactor() {
      twoFactorBody.replaceChildren(node('p', { class: 'small muted' }, 'Loading two-factor settings…'));
      let settings;
      try {
        settings = await request(twoFactorPath);
      } catch (failure) {
        twoFactorBody.replaceChildren(node('div', {}, twoFactorError, empty('Could not load two-factor settings', failure.message, button('Try again', loadTwoFactor))));
        return;
      }
      twoFactorError.textContent = '';
      const children = [twoFactorError];
      if (pendingRecoveryCodes) {
        children.push(node('div', { class: 'form-notice', role: 'status', tabindex: '-1' }, 'Save these recovery codes now. Each one signs you in once if you lose your authenticator. They are not shown again.'));
        children.push(recoveryCodeList(pendingRecoveryCodes));
        children.push(node('div', { class: 'actions' }, button('Copy recovery codes', () => copyValue(pendingRecoveryCodes.join('\n'), 'Recovery codes'))));
        pendingRecoveryCodes = null;
      }
      if (settings.enabled) {
        children.push(node('p', { class: 'muted' }, 'Two-factor authentication is on for this account.'));
        children.push(node('p', { class: 'small muted' }, `${settings.recoveryCodesRemaining} unused recovery ${settings.recoveryCodesRemaining === 1 ? 'code' : 'codes'} remaining.`));
        children.push(node('div', { class: 'actions' },
          button('Regenerate recovery codes', () => openTwoFactorPassword('Regenerate recovery codes', 'Every current recovery code stops working immediately.', password => request(twoFactorPath + '/recovery-codes', { method: 'POST', body: { password } }), 'Recovery codes regenerated.')),
          button('Disable two-factor authentication', () => openTwoFactorPassword('Disable two-factor authentication', 'Your account will sign in with a password alone.', password => request(twoFactorPath, { method: 'DELETE', body: { password } }), 'Two-factor authentication disabled.'), 'danger')));
      } else if (settings.pending) {
        children.push(node('p', { class: 'muted' }, 'Enrollment is not confirmed yet. Start again to get a new secret and confirm it with a code.'));
        children.push(node('div', { class: 'actions' }, button('Start enrollment again', startEnrollment, 'primary')));
      } else {
        children.push(node('p', { class: 'muted' }, 'Require a time-based code from an authenticator app when you sign in. Recovery codes cover a lost device.'));
        children.push(node('div', { class: 'actions' }, button('Enable two-factor authentication', startEnrollment, 'primary')));
      }
      twoFactorBody.replaceChildren(...children);
    }

    loadEmails();
    loadTwoFactor();
  }

  async function renderOrganizationSettings(main, version, params) {
    document.title = 'Organization · GoodSentry';
    const org = state.organization;
    main.replaceChildren(pageHeader('Organization', 'Manage the workspace name and lifecycle.', button('Refresh', () => loadRoute())));
    let detail;
    try {
      detail = await request(`/api/0/organizations/${encodeURIComponent(org.slug)}/`);
    } catch (failure) {
      if (version !== state.requestVersion) return;
      main.append(node('section', { class: 'card panel' }, empty('Could not load organization settings', failure.message, button('Try again', () => loadRoute()))));
      return;
    }
    if (version !== state.requestVersion) return;
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Organization name', 'text', { value: detail.name, required: true, maxlength: 200, autocomplete: 'organization' });
    const save = node('button', { type: 'submit', class: 'btn button primary' }, 'Save organization name');
    const form = node('form', { class: 'form-stack', 'aria-label': 'Rename organization' }, error, name.wrap, save);
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      save.disabled = true;
      save.textContent = 'Saving…';
      try {
        const updated = await request(`/api/0/organizations/${encodeURIComponent(org.slug)}/`, { method: 'PUT', body: { name: name.input.value } });
        state.organization.name = updated.name;
        const selected = state.me.organizations.find(item => item.slug === org.slug);
        if (selected) selected.name = updated.name;
        notify('Organization name updated.');
        await loadRoute(true);
      } catch (failure) {
        formError(error, failure);
      } finally {
        save.disabled = false;
        save.textContent = 'Save organization name';
      }
    });
    const details = node('dl', { class: 'org-details' },
      node('div', {}, node('dt', {}, 'Organization ID'), node('dd', { class: 'mono' }, detail.id)),
      node('div', {}, node('dt', {}, 'Slug'), node('dd', { class: 'mono' }, detail.slug)),
      node('div', {}, node('dt', {}, 'Your role'), node('dd', {}, detail.access.includes('org:write') ? org.role : 'member')),
      node('div', {}, node('dt', {}, 'Created'), node('dd', {}, date(detail.dateCreated))));
    const renamePanel = node('section', { class: 'card panel', 'aria-label': 'Organization details' },
      node('div', { class: 'panel-title' }, node('h2', {}, 'Workspace details')),
      node('div', { class: 'panel-body form-stack' }, details, form));
    main.append(renamePanel);
    const ssoPanel = node('section', { class: 'card panel', 'aria-label': 'Single sign-on applications' });
    const ssoBody = node('div', { class: 'panel-body form-stack' });
    ssoPanel.append(node('div', { class: 'panel-title' }, node('h2', {}, 'Single sign-on'), node('span', { class: 'badge' }, 'OpenID Connect')), ssoBody);
    main.append(ssoPanel);
    const ssoPath = `/api/0/organizations/${encodeURIComponent(org.slug)}/social-apps/`;
    const ssoError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const manageSSO = ['owner', 'admin'].includes(org.role);

    function openSSODialog(app) {
      const editing = Boolean(app);
      const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'sso-dialog-title' });
      const dialogError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      const name = field('Application name', 'text', { name: 'name', required: true, maxlength: 40, value: app?.name || '' });
      const clientID = field('Client ID', 'text', { name: 'clientID', required: true, maxlength: 191, value: app?.clientID || '' });
      const clientSecret = field(editing ? 'New client secret (optional)' : 'Client secret', 'password', { name: 'clientSecret', autocomplete: 'new-password', maxlength: 191, ...(editing ? {} : { required: true }) });
      const provider = selectField('Provider', [['openid_connect', 'OpenID Connect'], ['google', 'Google'], ['github', 'GitHub'], ['gitlab', 'GitLab']], app?.brand || 'openid_connect', {});
      const serverURL = field('Issuer or server URL', 'url', { name: 'serverUrl', maxlength: 500, placeholder: 'https://issuer.example.com', value: app?.serverUrl || '' });
      const submit = node('button', { type: 'submit', class: 'btn button primary' }, editing ? 'Save application' : 'Add application');
      const form = node('form', { class: 'form-stack', 'aria-label': editing ? 'Edit single sign-on application' : 'Add single sign-on application' },
        node('h2', { id: 'sso-dialog-title' }, editing ? 'Edit single sign-on application' : 'Add single sign-on application'),
        dialogError, name.wrap, clientID.wrap, clientSecret.wrap, editing ? null : provider.wrap, serverURL.wrap,
        node('p', { class: 'small muted' }, editing
          ? 'Leave the secret or issuer URL empty to keep the stored value.'
          : 'The server URL must be https when set. GitHub and GitLab default to their hosted services and accept a self-hosted server URL; OpenID Connect requires an issuer. Copy the callback URL shown after saving into your identity provider.'),
        node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), submit));
      form.addEventListener('submit', async event => {
        event.preventDefault();
        dialogError.textContent = '';
        submit.disabled = true;
        submit.textContent = 'Saving…';
        const body = { name: name.input.value, clientID: clientID.input.value };
        if (clientSecret.input.value) body.clientSecret = clientSecret.input.value;
        if (serverURL.input.value.trim()) body.serverUrl = serverURL.input.value.trim();
        if (!editing) body.provider = provider.input.value;
        try {
          await request(editing ? ssoPath + encodeURIComponent(app.id) + '/' : ssoPath, { method: editing ? 'PUT' : 'POST', body });
          dialog.close();
          notify(editing ? 'Application updated.' : 'Application added.');
          await loadSSO();
        } catch (failure) {
          formError(dialogError, failure);
        } finally {
          submit.disabled = false;
          submit.textContent = editing ? 'Save application' : 'Add application';
        }
      });
      dialog.append(form);
      ssoPanel.append(dialog);
      dialog.showModal();
      name.input.focus();
    }

    function openDeleteSSO(app) {
      const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'delete-sso-title' });
      const deleteError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      const remove = node('button', { type: 'submit', class: 'btn button danger' }, 'Delete application');
      const form = node('form', { class: 'form-stack' }, node('h2', { id: 'delete-sso-title' }, 'Delete application?'), deleteError,
        node('p', { class: 'wrap-anywhere' }, `Delete ${app.name}? People who only sign in through this application lose that path.`),
        node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), remove));
      form.addEventListener('submit', async event => {
        event.preventDefault();
        deleteError.textContent = '';
        remove.disabled = true;
        try {
          await request(ssoPath + encodeURIComponent(app.id) + '/', { method: 'DELETE' });
          dialog.close();
          notify('Application deleted.');
          await loadSSO();
        } catch (failure) {
          formError(deleteError, failure);
        } finally {
          remove.disabled = false;
        }
      });
      dialog.append(form);
      ssoPanel.append(dialog);
      dialog.showModal();
      remove.focus();
    }

    async function loadSSO() {
      ssoBody.replaceChildren(node('p', { class: 'small muted' }, 'Loading single sign-on applications…'));
      let apps;
      try {
        apps = await request(ssoPath);
      } catch (failure) {
        ssoBody.replaceChildren(node('div', {}, ssoError, empty('Could not load single sign-on applications', failure.message, button('Try again', loadSSO))));
        return;
      }
      ssoError.textContent = '';
      const children = [ssoError,
        node('p', { class: 'muted' }, 'Let people sign in to this workspace with your identity provider. Register each callback URL at the provider, then share the login link.')];
      const loginLink = apps.length ? apps[0].loginUrl : location.origin + base + '/login/' + encodeURIComponent(org.slug);
      if (!apps.length) children.push(node('p', { class: 'small muted' }, 'No applications configured yet.'));
      for (const app of apps) {
        const actions = node('div', { class: 'actions' }, button('Copy callback', () => copyValue(app.callbackUrl, 'Callback URL')));
        if (manageSSO) actions.append(button('Edit', () => openSSODialog(app)), button('Delete', () => openDeleteSSO(app), 'danger'));
        children.push(node('div', { class: 'release-project', role: 'listitem' },
          node('div', {}, node('strong', {}, app.name),
            node('span', { class: 'badge' }, app.brand === 'google' ? 'Google' : app.brand === 'github' ? 'GitHub' : app.brand === 'gitlab' ? 'GitLab' : 'OpenID Connect'),
            node('div', { class: 'small muted mono wrap-anywhere' }, app.callbackUrl)),
          actions));
      }
      children.push(node('div', { class: 'actions' },
        button('Copy login link', () => copyValue(loginLink, 'Login link')),
        manageSSO ? button('Add application', () => openSSODialog(), 'primary') : null));
      ssoBody.replaceChildren(...children);
    }
    loadSSO();

    const deletePanel = node('section', { class: 'card panel', 'aria-label': 'Delete organization' },
      node('div', { class: 'panel-title' }, node('h2', {}, 'Delete organization')),
      node('div', { class: 'panel-body form-stack' },
        node('p', { class: 'muted' }, org.role === 'owner'
          ? 'Deleting this organization removes its projects, events, issues, teams, alerts and related data. This cannot be undone.'
          : 'Only an organization owner can delete this workspace.'),
        org.role === 'owner' ? button('Delete organization', openDeleteOrganization, 'danger') : null));
    main.append(deletePanel);

    function openDeleteOrganization() {
      const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'delete-organization-title' });
      const deleteError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      const confirm = node('button', { type: 'submit', class: 'btn button danger' }, 'Delete organization permanently');
      const deleteForm = node('form', { class: 'form-stack', 'aria-label': 'Confirm organization deletion' },
        deleteError,
        node('p', {}, `Delete ${detail.name}? This permanently removes all projects and stored data for this organization.`),
        node('div', { class: 'dialog-actions' }, button('Cancel', () => dialog.close()), confirm));
      deleteForm.addEventListener('submit', async event => {
        event.preventDefault();
        deleteError.textContent = '';
        confirm.disabled = true;
        confirm.textContent = 'Deleting…';
        try {
          await request(`/api/0/organizations/${encodeURIComponent(org.slug)}/`, { method: 'DELETE' });
          dialog.close();
          state.me.organizations = state.me.organizations.filter(item => item.slug !== org.slug);
          const next = state.me.organizations[0] || null;
          const target = new URLSearchParams({ view: 'issues' });
          if (next) target.set('org', next.slug);
          history.pushState(null, '', location.pathname + '?' + target.toString());
          notify('Organization deleted.');
          loadRoute(true);
        } catch (failure) {
          formError(deleteError, failure);
          confirm.disabled = false;
          confirm.textContent = 'Delete organization permanently';
        }
      });
      dialog.append(node('header', {}, node('h2', { id: 'delete-organization-title' }, 'Delete organization'), button('Close', () => dialog.close(), 'small')), deleteForm);
      dialog.addEventListener('close', () => dialog.remove());
      document.body.append(dialog);
      dialog.showModal();
      confirm.focus();
    }
  }

  function pageHeader(title, description, action) {
    return node('div', { class: 'page-header' }, node('div', {}, node('div', { class: 'eyebrow' }, state.organization?.name || 'Instance'), node('h1', { tabindex: '-1' }, title), description ? node('p', {}, description) : null), action);
  }

  function badge(value) {
    const known = ['unresolved', 'resolved', 'ignored', 'error', 'fatal', 'warning', 'info', 'debug'];
    const variant = ({ warn: 'warning', up: 'resolved', down: 'error', unknown: 'ignored' })[value] || value;
    return node('span', { class: `badge badge-sm ${known.includes(variant) ? variant : ''}` }, text(value, 'Unknown'));
  }

  function renderIssues(main, result, params, failure) {
    const feedback = issueViewName(params) === 'feedback';
    const title = feedback ? 'Feedback' : 'Issues';
    document.title = `${title} · GoodSentry`;
    main.replaceChildren(pageHeader(title, feedback ? 'Read and triage feedback from your application users.' : 'Find, investigate and triage application errors.', node('div', { class: 'actions' }, button('Refresh', () => loadRoute()), button('Create project', () => openProjectDialog(), 'primary'))));
    const outcome = issueDeletion?.scope === issueScope(params) ? node('div', { class: 'card panel panel-body form-stack', role: 'status', tabindex: '-1' },
      node('p', {}, issueDeletion.message), issueDeletion.bulk ? node('p', { class: 'small muted' }, 'If any selected issue was already missing, the whole request made no changes. Remaining issues can be selected again after refresh.') : null) : null;
    if (outcome) main.append(outcome);
    if (!state.projects.length) {
      main.append(node('section', { class: 'card panel' }, empty('Connect your first application', 'Create a project to get a DSN. Set it in your existing Sentry SDK to start receiving errors.', button('Create project', () => openProjectDialog(), 'primary'))));
      return;
    }
    const search = field(feedback ? 'Search feedback' : 'Search issues', 'search', { name: 'query', placeholder: feedback ? 'Feedback text or tags' : 'Message or exception', value: params.get('query') || '' });
    search.wrap.classList.add('search-field');
    const project = selectField('Project', [['', 'All projects'], ...state.projects.map(item => [String(item.id), item.name])], params.get('project') || '');
    const status = selectField('Status', [['all', 'All statuses'], ['unresolved', 'Unresolved'], ['resolved', 'Resolved'], ['ignored', 'Ignored']], params.get('status') || 'unresolved');
    const environment = field('Environment', 'text', { name: 'environment', value: params.get('environment') || '', placeholder: 'All environments' });
    const release = field('Release', 'text', { name: 'release', value: params.get('release') || '', placeholder: 'All releases', maxlength: 510, autocomplete: 'off' });
    const validateRelease = () => release.input.setCustomValidity(Array.from(release.input.value).length > 255 ? 'Use at most 255 Unicode characters for the release.' : '');
    release.input.addEventListener('input', validateRelease);
    validateRelease();
    const sort = selectField('Sort by', [['date', 'Last seen'], ['freq', feedback ? 'Submission count' : 'Event count'], ['new', 'First seen']], params.get('sort') || 'date');
    const helpID = `issue-search-help-${++fieldNumber}`;
    search.input.setAttribute('aria-describedby', helpID);
    const clearFilters = () => navigate({ query: null, project: null, status: null, environment: null, release: null, sort: null, page: null, issue: null });
    const filters = node('form', { class: 'filters', role: 'search', 'aria-label': feedback ? 'Filter feedback' : 'Filter issues' }, search.wrap, project.wrap, status.wrap, environment.wrap, release.wrap, sort.wrap,
      node('div', { class: 'actions' }, node('button', { class: 'btn button', type: 'submit' }, 'Apply filters'), button('Clear filters', clearFilters)));
    filters.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ query: search.input.value.trim(), project: project.input.value, status: status.input.value, environment: environment.input.value, release: release.input.value, sort: sort.input.value, page: null, issue: null });
    });
    main.append(filters, node('div', { class: 'connection-help muted small', id: helpID },
      node('p', {}, 'Search text or tags: region:eu, "literal phrase", region:[eu,us], !region:eu. Quote "*" to match a literal star.'),
      node('p', {}, 'Environment, release, tags and text must match the same stored event. Counts, first seen and last seen cover the issue’s full history.')));
    const panel = node('section', { class: 'card panel', 'aria-label': title });
    if (failure) {
      errorState(panel, failure, () => loadRoute(true));
      main.append(panel);
      if (outcome) {
        outcome.append(node('p', {}, 'The issues list could not be refreshed. Retry the list to see its current state; the deletion request will not be sent again.'));
        outcome.focus();
      }
      return;
    }
    const issues = result.data || [];
    const merge = issueMergeControls(main, issues);
    panel.append(merge.panel);
    if (!issues.length) {
      panel.append(empty(feedback ? 'No matching feedback' : 'No matching issues', feedback ? 'Submissions appear here when your Sentry SDK sends feedback. Check the selected filters, or clear them to see more.' : 'Errors will appear here when your SDK sends them. Check the selected project, status, environment and release, or clear your filters.'));
    } else {
      const rows = issues.map(issue => node('tr', {},
        node('td', {}, merge.checkbox(issue)),
        node('td', { class: 'issue-column' }, routeLink(issue.title, { issue: issue.id }, { class: 'issue-link issue-title' }),
          node('div', { class: 'issue-subtitle' }, node('span', { class: 'mono' }, issue.shortId || issue.id), node('span', {}, issue.project?.name || 'Project'), issue.environment ? node('span', {}, issue.environment) : null)),
        node('td', {}, badge(issue.level)), node('td', {}, badge(issue.status)), node('td', { class: 'nowrap' }, number(issue.count)),
        node('td', { class: 'nowrap' }, node('time', { datetime: issue.lastSeen, title: date(issue.lastSeen) }, date(issue.lastSeen, true)))));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, node('th', { scope: 'col' }, merge.pageCheckbox), [feedback ? 'Feedback' : 'Issue', 'Level', 'Status', feedback ? 'Submissions' : 'Events', 'Last seen'].map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    merge.sync();
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const total = Number(result.total) || 0;
    const previous = button('Previous', () => navigate({ page: page - 1 }));
    const next = button('Next', () => navigate({ page: page + 1 }));
    previous.disabled = page <= 1;
    next.disabled = page * pageSize >= total;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(total)} ${total === 1 ? 'issue' : 'issues'} · Page ${page}`), node('div', { class: 'actions' }, previous, next)));
    main.append(panel);
  }

  function issueMergeControls(main, issues) {
    const scope = mergeSelection.scope, version = state.requestVersion;
    const organization = state.organization.slug;
    const alive = () => main.isConnected && version === state.requestVersion && scope === mergeSelection.scope && !!state.me;
    const selected = mergeSelection.issues;
    for (const issue of issues) if (selected.has(issue.id)) selected.set(issue.id, issue);
    const boxes = [];
    let pending = false, confirming = null;
    const status = node('p', { class: 'small', role: 'status' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const confirmation = node('div', { class: 'form-stack', hidden: true });
    const clear = button('Clear selection', () => { selected.clear(); changed(); }, 'small');
    const merge = button('Merge selected', () => { confirming = 'merge'; sync(); confirmation.querySelector('button')?.focus(); }, 'small');
    const remove = button('Delete selected', () => { confirming = 'delete'; sync(); confirmation.querySelector('button')?.focus(); }, 'danger small');
    const pageCheckbox = node('input', { type: 'checkbox', class: 'size-4 shrink-0 cursor-pointer accent-primary', 'aria-label': 'Select all issues on this page', onchange: () => {
      const add = pageCheckbox.checked;
      if (add && selected.size + issues.filter(issue => !selected.has(issue.id)).length > 1000) {
        formError(error, new Error('Select at most 1,000 issues. Clear some selections before adding this page.'));
        sync();
        return;
      }
      for (const issue of issues) { if (add) selected.set(issue.id, issue); else selected.delete(issue.id); }
      changed();
    } });
    function changed() { confirming = null; error.textContent = ''; sync(); }
    async function submit() {
      if (!alive() || pending || selected.size < 2 || selected.size > 100 || new Set([...selected.values()].map(issue => issue.projectId)).size !== 1) return;
      const ids = sortedIssueIDs(selected.keys());
      pending = true;
      error.textContent = '';
      sync();
      try {
        const result = await request(`/api/v1/organizations/${encodeURIComponent(organization)}/issues/merge`, { method: 'POST', body: { issueIds: ids } });
        if (!alive()) return;
        selected.clear();
        navigate({ issue: result.issue.id });
        notify(result.changed ? 'Issues merged.' : 'These issues already belong to one group.');
      } catch (failure) {
        if (alive()) formError(error, failure);
      } finally {
        pending = false;
        if (alive()) sync();
      }
    }
    async function submitDelete() {
      if (!alive() || pending || selected.size < 1 || selected.size > 1000) return;
      const ids = sortedIssueIDs(selected.keys());
      pending = true;
      error.textContent = '';
      sync();
      try {
        const query = new URLSearchParams(ids.map(id => ['id', id]));
        await request(`/api/0/organizations/${encodeURIComponent(organization)}/issues/?${query}`, { method: 'DELETE' });
        if (alive()) finishIssueDeletion(organization, ids);
      } catch (failure) {
        if (alive()) formError(error, failure);
      } finally {
        pending = false;
        if (alive()) sync();
      }
    }
    function sync() {
      const count = selected.size;
      const mixed = new Set([...selected.values()].map(issue => issue.projectId)).size > 1;
      const root = count ? sortedIssueIDs(selected.keys())[0] : null;
      status.textContent = `${count} selected · Up to 1,000. Selections stay across pages with the same filters.`;
      clear.disabled = pending || count === 0;
      merge.disabled = pending || count < 2 || count > 100 || mixed;
      merge.textContent = pending && confirming === 'merge' ? 'Merging…' : 'Merge selected';
      remove.disabled = pending || count === 0;
      remove.textContent = pending && confirming === 'delete' ? 'Deleting…' : 'Delete selected';
      const selectedHere = issues.filter(issue => selected.has(issue.id)).length;
      pageCheckbox.checked = issues.length > 0 && selectedHere === issues.length;
      pageCheckbox.indeterminate = selectedHere > 0 && selectedHere < issues.length;
      pageCheckbox.disabled = pending || issues.length === 0;
      for (const { input, issue } of boxes) {
        input.checked = selected.has(issue.id);
        input.disabled = pending || (!input.checked && count >= 1000);
      }
      explanation.textContent = mixed ? 'Deleting supports selected groups across projects. Merging requires 2–100 issues from one project.' : count > 100 ? 'More than 100 issues are selected. Delete the selected groups, or reduce the selection to 2–100 issues to merge.' : 'Merging requires 2–100 issues from one project. The lowest ID keeps its state; recorded events and original groups are retained.';
      confirmation.hidden = !confirming || count === 0 || confirming === 'merge' && (count < 2 || count > 100 || mixed);
      if (confirming && !confirmation.hidden) {
        const deleting = confirming === 'delete';
        const confirm = button(deleting ? 'Confirm deletion' : 'Confirm merge', deleting ? submitDelete : submit, deleting ? 'danger small' : 'primary small');
        const cancel = button(deleting ? 'Cancel deletion' : 'Cancel merge', () => { confirming = null; sync(); (deleting ? remove : merge).focus(); }, 'small');
        confirm.disabled = pending;
        cancel.disabled = pending;
        confirmation.replaceChildren(node('p', {}, deleting ? `Delete the whole groups for ${count} selected ${count === 1 ? 'issue' : 'issues'}?` : `Merge ${count} selected issues into issue ${root}?`),
          ...(deleting ? [node('p', { class: 'muted small' }, issueDeletionWarning)] : []), node('div', { class: 'actions' }, confirm, cancel));
      }
    }
    const explanation = node('p', { class: 'muted small' });
    const panel = node('div', { class: 'panel-body form-stack', role: 'group', 'aria-label': 'Selected issues' }, status, node('div', { class: 'actions' }, merge, remove, clear), explanation, confirmation, error);
    return { panel, pageCheckbox, sync, checkbox: issue => {
      const input = node('input', { type: 'checkbox', class: 'size-4 shrink-0 cursor-pointer accent-primary', 'aria-label': `Select issue ${issue.shortId || issue.id}`, onchange: () => {
        if (input.checked && !selected.has(issue.id) && selected.size >= 1000) {
          formError(error, new Error('Select at most 1,000 issues. Clear some selections before adding another issue.'));
          sync(); return;
        }
        if (input.checked) selected.set(issue.id, issue); else selected.delete(issue.id);
        changed();
      } });
      boxes.push({ input, issue });
      return input;
    } };
  }

  function projectFilter(params) {
    return selectField('Project', [['', 'All projects'], ...state.projects.map(project => [String(project.id), project.name])], params.get('project') || '');
  }

  function traceReference(traceID, projectID, params) {
    if (!traceID) return node('span', { class: 'muted small' }, 'No trace ID');
    const copy = button('Copy', () => copyValue(traceID, 'Trace ID'), 'small');
    copy.setAttribute('aria-label', 'Copy trace ID');
    return node('div', { class: 'trace-reference' },
      state.me.features?.logs === false ? node('code', {}, traceID) : routeLink(traceID, { ...traceReset, view: 'logs', issue: null, sampleSource: null, sample: null, spanGroupsSource: null, spanGroups: null, project: projectID || null, trace: traceID, traceMatch: null, query: null, environment: null, level: null, service: null, status: null, sort: null, page: null, start: params?.get('start') || null, end: params?.get('end') || null, window: params?.get('window') || null }, { class: 'mono', title: 'View logs with this trace ID' }), copy, traceEntry(traceID, params));
  }

  function traceEntry(traceID, params = new URLSearchParams(location.search)) {
    if (typeof traceID !== 'string' || !/^[a-f\d]{32}$/i.test(traceID)) return null;
    return routeLink('View trace', { ...traceReset, view: 'transactions', traceView: traceID.toLowerCase(), returnView: params.get('view') || 'issues' }, { class: 'btn button small', title: 'View this trace across all authorized projects and stored time' });
  }

  function traceBackLink() {
    const requested = new URLSearchParams(location.search).get('returnView');
    const view = Object.prototype.hasOwnProperty.call(viewNames, requested) ? requested : 'transactions';
    return routeLink(`← Back to ${viewNames[view].toLowerCase()}`, { ...traceReset, view }, { class: 'back-link' });
  }

  async function loadTrace(main, params, version) {
    const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/traces/${encodeURIComponent(params.get('traceView'))}`;
    const scope = new URLSearchParams();
    if (params.has('traceProject')) scope.set('project', params.get('traceProject'));
    const graphQuery = new URLSearchParams(scope);
    graphQuery.set('page', params.has('tracePage') ? params.get('tracePage') : '1');
    let graph;
    try { graph = await request(`${path}?${graphQuery}`); }
    catch (error) { if (version === state.requestVersion) renderTraceFailure(main, error, error.status === 503); return; }
    if (version !== state.requestVersion) return;
    const errorQuery = new URLSearchParams(scope);
    errorQuery.set('page', params.has('traceErrorsPage') ? params.get('traceErrorsPage') : '1');
    const [errors, selected] = await Promise.allSettled([
      request(`${path}/errors?${errorQuery}`),
      params.get('traceNode') ? request(`${path}/nodes/${encodeURIComponent(params.get('traceNode'))}?${scope}`) : Promise.resolve(null),
    ]);
    if (version !== state.requestVersion) return;
    renderTrace(main, graph, errors, selected, params);
  }

  function renderTraceFailure(main, error, indexing) {
    document.title = 'Trace · GoodSentry';
    const body = node('section', { class: 'card panel', 'aria-label': indexing ? 'Trace indexing' : 'Trace error' }, node('div', { class: 'panel-body form-stack', role: indexing ? 'status' : 'alert' },
      node('h2', {}, indexing ? 'Historical trace data is being indexed' : 'Could not load this trace'),
      node('p', { class: 'muted' }, indexing ? 'The matching transaction history is being prepared. No partial waterfall is shown. Try again shortly.' : error.message),
      node('div', { class: 'actions' }, button(indexing ? 'Retry trace' : 'Try again', () => loadRoute(true)))));
    main.replaceChildren(traceBackLink(), pageHeader('Trace', 'All stored time in the selected project scope.'), body);
  }

  function traceTiming(item, graph) {
    const knownDuration = item.duration !== null && item.duration !== undefined;
    const knownOffset = item.offset !== null && item.offset !== undefined;
    const partial = !knownDuration && (!item.startTimestamp || !item.endTimestamp);
    const label = knownDuration ? `${traceMilliseconds(item.duration)}${item.duration === 0 ? ' (point)' : ''}`
      : item.startTimestamp && item.endTimestamp ? 'Invalid interval' : item.startTimestamp ? 'Recorded start; duration unavailable' : item.endTimestamp ? 'Recorded end; duration unavailable' : 'Timing unavailable';
    const track = node('div', { class: 'trace-track', role: 'img', 'aria-label': `${knownOffset ? `Offset ${traceMilliseconds(item.offset)}; ` : ''}${label}` });
    if (knownOffset && (knownDuration || partial)) {
      const extent = Number(graph.duration);
      const left = extent > 0 ? Math.max(0, Math.min(100, Number(item.offset) / extent * 100)) : 0;
      const marker = node('span', { class: `trace-bar${!knownDuration || item.duration === 0 ? ' point' : ''}${!knownDuration ? ' partial' : ''}`, 'aria-hidden': true });
      marker.style.left = `${left}%`;
      if (knownDuration && item.duration > 0) marker.style.width = `${extent > 0 ? Math.min(100 - left, Number(item.duration) / extent * 100) : 0}%`;
      track.append(marker);
    }
    return node('div', { class: 'trace-timing' }, track, node('p', { class: 'small' }, label, knownOffset ? ` · ${traceMilliseconds(item.offset)} offset` : ''),
      node('div', { class: 'trace-endpoints mono' }, node('span', {}, `Start: ${item.startTimestamp || 'Not available'}`), node('span', {}, `End: ${item.endTimestamp || 'Not available'}`)));
  }

  function traceMilliseconds(value) {
    return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 3 }).format(value)} ms`;
  }

  function renderTrace(main, graph, errors, selected, params) {
    document.title = `Trace ${graph.traceId} · GoodSentry`;
    const actions = node('div', { class: 'actions' }, button('Refresh', () => loadRoute()), button('Copy trace ID', () => copyValue(graph.traceId, 'Trace ID')));
    if (graph.logsEnabled) actions.append(routeLink('View related logs', { ...traceReset, view: 'logs', trace: graph.traceId, traceMatch: 'canonical', project: params.get('traceProject') || null, issue: null, sample: null, sampleSource: null, spanGroups: null, spanGroupsSource: null, page: null, query: null, level: null, environment: null, service: null, start: null, end: null, window: null }, { class: 'btn button', title: 'Related logs across all stored time' }));
    const heading = pageHeader('Trace', graph.traceId, actions); heading.classList.add('transaction-heading');
    const project = selectField('Trace project', [['', 'All projects'], ...state.projects.map(item => [String(item.id), item.name])], params.get('traceProject') || '');
    const form = node('form', { class: 'filters transaction-filters', 'aria-label': 'Trace project scope' }, project.wrap, node('button', { class: 'btn button', type: 'submit' }, 'Apply scope'));
    form.addEventListener('submit', event => { event.preventDefault(); navigate({ traceProject: project.input.value, tracePage: null, traceNode: null, traceErrorsPage: null }); });
    const parentLabels = { none: 'No recorded parent', resolved: 'Resolved parent', missing: 'Missing parent', ambiguous: 'Ambiguous parent', invalid: 'Invalid parent ID', cycle: 'Cycle reference' };
    const stats = [['Nodes', graph.nodeCount], ['Projects', graph.projectCount], ['Errors', graph.errorCount], ...(graph.logsEnabled ? [['Logs', graph.logCount]] : []), ['Untimed intervals', graph.untimedCount], ['Invalid span IDs', graph.invalidSpanIdCount]];
    const summary = node('section', { class: 'card panel', 'aria-label': 'Trace summary' },
      node('dl', { class: 'stat-list' }, stats.map(([label, value]) => node('div', { class: 'stat' }, node('dt', {}, label), node('dd', {}, number(value))))),
      node('div', { class: 'panel-body form-stack' }, node('p', { class: 'muted small' }, 'All stored time. This trace is independent of the filters used to open it. New events can change its pages; Refresh loads a new snapshot.'),
        keyValues([['Recorded start (UTC)', graph.startTimestamp], ['Recorded end (UTC)', graph.endTimestamp], ['Recorded time range', graph.duration === null ? 'Not available' : traceMilliseconds(graph.duration)]]),
        node('div', { class: 'actions small', 'aria-label': 'Relationship counts' }, Object.entries(parentLabels).map(([status, label]) => node('span', { class: 'badge badge-sm' }, `${label}: ${number(graph.relationships?.[status])}`))),
        graph.logsEnabled ? null : node('p', { class: 'muted small' }, 'Related logs are disabled on this server.')));
    const forest = detailPanel('Waterfall', node('div', { class: 'panel-body muted small' }, 'Only recorded parent references create links. Indentation is limited visually; each row shows its actual depth. Unknown, ambiguous and cyclic relationships stay visible.'));
    forest.setAttribute('aria-label', 'Trace waterfall');
    const items = graph.data || [];
    if (!items.length) forest.append(empty('No transaction or span nodes', 'This trace has related events but no stored transaction or span nodes in the selected project scope.'));
    else {
      const rows = items.map(item => {
        const name = node('div', { class: 'trace-node-label' }, routeLink(text(item.name, 'Unnamed node'), { traceNode: item.id }, { class: 'issue-link issue-title', 'aria-current': params.get('traceNode') === item.id ? 'true' : null }),
          node('p', { class: 'muted small' }, `${item.kind} · ${item.projectName}`), node('p', { class: 'mono small' }, item.id),
          node('p', { class: 'small' }, `${text(item.op)} · Status: ${text(item.status)}`), node('p', { class: 'muted small' }, `Depth ${item.depth} · ${number(item.childCount)} children`),
          item.environment || item.release ? node('p', { class: 'muted small' }, [item.environment, item.release].filter(Boolean).join(' · ')) : null,
          item.summaryTruncated ? node('p', { class: 'trace-preview-note' }, 'Preview truncated. Select this node to inspect the full original JSON.') : null);
        name.style.paddingLeft = `${Math.min(6, Math.max(0, item.depth)) * 12}px`;
        const relation = node('div', { class: 'trace-relationship' }, node('strong', {}, parentLabels[item.parentStatus] || text(item.parentStatus)),
          node('p', { class: 'small' }, `Span ID: ${text(item.spanIdStatus)}`), node('code', {}, text(item.spanId, 'No recorded span ID')),
          node('p', { class: 'small' }, 'Recorded parent: ', node('code', {}, text(item.parentSpanId, 'None'))),
          item.parentId && item.parentPage > 0 ? routeLink(`Jump to parent (page ${item.parentPage})`, { tracePage: item.parentPage, traceNode: item.parentId }, { class: 'small' }) : null);
        return node('tr', { class: params.get('traceNode') === item.id ? 'trace-selected' : null }, node('td', { class: 'trace-node-column' }, name), node('td', { class: 'trace-timeline-column' }, traceTiming(item, graph)), node('td', { class: 'trace-relationship-column' }, relation));
      });
      forest.append(node('div', { class: 'table-scroll' }, node('table', { class: 'trace-table' }, node('thead', {}, node('tr', {}, ['Node and depth', 'Recorded timing', 'Recorded relationship'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, rows))));
    }
    const previous = button('Previous nodes', () => navigate({ tracePage: graph.page - 1 })); const next = button('Next nodes', () => navigate({ tracePage: graph.page + 1 }));
    previous.disabled = graph.page <= 1; next.disabled = !graph.hasMore;
    forest.append(node('div', { class: 'pagination' }, node('span', {}, `${number(graph.nodeCount)} nodes · Page ${number(graph.page)}`), node('div', { class: 'actions' }, previous, next)));
    main.replaceChildren(traceBackLink(), heading, form, summary, forest, traceRawPanel(selected, params), traceErrorsPanel(errors));
  }

  function traceRawPanel(result, params) {
    const panel = detailPanel('Selected node JSON', null); panel.setAttribute('aria-label', 'Selected trace node');
    if (!params.get('traceNode')) panel.append(node('p', { class: 'panel-body muted small' }, 'Select a node name to inspect its original payload.'));
    else if (result.status === 'rejected') panel.append(node('div', { class: 'panel-body form-stack', role: 'alert' }, node('p', {}, result.reason.status === 503 ? 'Historical node data is being indexed. Try again shortly.' : result.reason.message), node('div', { class: 'actions' }, button('Retry selected node', () => loadRoute()))));
    else {
      const item = result.value;
      panel.append(node('div', { class: 'panel-body form-stack' }, node('p', { class: 'muted small' }, 'Full original payload for this selected record.'),
        keyValues([['Node ID', item.id], ['Source', item.source], ['Record ID', item.recordId], ['Ordinal', item.ordinal], ['Project ID', item.projectId]])), node('pre', { class: 'event-json' }, JSON.stringify(item.data, null, 2)));
    }
    return panel;
  }

  function traceErrorsPanel(result) {
    const panel = detailPanel('Related errors', node('p', { class: 'panel-body muted small' }, 'All stored time in the selected trace project scope.'));
    panel.setAttribute('aria-label', 'Trace errors');
    if (result.status === 'rejected') { panel.append(node('div', { class: 'panel-body form-stack', role: 'alert' }, node('p', {}, result.reason.message), node('div', { class: 'actions' }, button('Retry errors', () => loadRoute())))); return panel; }
    const errors = result.value; const items = errors.data || [];
    if (!items.length) panel.append(empty('No related errors', 'No stored errors match this trace and project scope.'));
    else {
      const rows = items.map(item => node('tr', {}, node('td', { class: 'transaction-span-name' }, text(item.title), item.summaryTruncated ? node('p', { class: 'trace-preview-note' }, 'Error title preview truncated.') : null),
        node('td', {}, text(item.projectName)), node('td', {}, badge(item.level)), node('td', { class: 'mono nowrap' }, node('time', { datetime: item.timestamp }, item.timestamp)), node('td', { class: 'mono' }, text(item.spanId)),
        node('td', {}, item.issueId ? routeLink('Open issue', { ...traceReset, view: 'issues', issue: item.issueId, project: item.projectId, sampleSource: null, sample: null, spanGroupsSource: null, spanGroups: null, page: null, query: null, status: null, sort: null, level: null, environment: null, release: null, trace: null, traceMatch: null, service: null, start: null, end: null, window: null }, { class: 'btn button small' }) : 'No linked issue')));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, ['Error', 'Project', 'Level', 'Time (UTC)', 'Span ID', 'Issue'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, rows))));
    }
    const previous = button('Previous errors', () => navigate({ traceErrorsPage: errors.page - 1 })); const next = button('Next errors', () => navigate({ traceErrorsPage: errors.page + 1 }));
    previous.disabled = errors.page <= 1; next.disabled = !errors.hasMore;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(errors.total)} errors · Page ${number(errors.page)}`), node('div', { class: 'actions' }, previous, next)));
    return panel;
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
          ? routeLink(text(transaction.transaction, 'Unnamed transaction'), { sampleSource: transaction.latestSample.source, sample: transaction.latestSample.id, spanGroupsSource: null, spanGroups: null, page: null }, { class: 'issue-link', title: 'Open the latest transaction occurrence' })
          : text(transaction.transaction, 'Unnamed transaction'))),
        node('td', {}, node('span', { class: 'mono' }, text(transaction.op, 'Not provided'))),
        node('td', { class: 'nowrap' }, number(transaction.count)), node('td', { class: 'nowrap' }, number(transaction.errorCount)),
        node('td', { class: 'nowrap' }, milliseconds(transaction.avgDuration)), node('td', { class: 'nowrap' }, milliseconds(transaction.p50)),
        node('td', { class: 'nowrap' }, milliseconds(transaction.p95)),
        node('td', { class: 'nowrap' }, node('time', { datetime: transaction.lastSeen }, date(transaction.lastSeen, true))),
        node('td', {}, traceReference(transaction.latestTraceId, transaction.latestSample?.projectId || params.get('project'), params)),
        node('td', {}, transaction.latestSample ? routeLink('Operations', { spanGroupsSource: transaction.latestSample.source, spanGroups: transaction.latestSample.id, sampleSource: null, sample: null, page: null }, { class: 'btn button small', title: 'View child-operation statistics' }) : 'Not available')));
      const columns = ['Transaction', 'Operation', 'Events', 'Errors', 'Average', 'p50', 'p95', 'Last seen', 'Latest trace', 'Operations'];
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(name => node('th', { scope: 'col' }, name)))), node('tbody', {}, rows))));
    }
    main.append(panel);
  }

  function allTransactionsLink() {
    return routeLink('← All transactions', { sampleSource: null, sample: null, spanGroupsSource: null, spanGroups: null, page: null }, { class: 'back-link' });
  }

  function renderSpanGroupsPending(main) {
    document.title = 'Indexing operations · GoodSentry';
    main.replaceChildren(allTransactionsLink(), pageHeader('Transaction operations', 'Statistics are being prepared for stored transactions.'),
      node('section', { class: 'card panel', 'aria-label': 'Historical indexing', role: 'status' },
        node('div', { class: 'panel-body form-stack' }, node('h2', {}, 'Historical spans are being indexed'),
          node('p', { class: 'muted' }, 'Operation statistics will appear when indexing of the matching transactions finishes. No partial metrics are shown. Try again shortly.'),
          node('div', { class: 'actions' }, button('Retry operation statistics', () => loadRoute(true))))));
  }

  function renderSpanGroups(main, result, params) {
    document.title = 'Transaction operations · GoodSentry';
    const occurrence = routeLink('Open occurrence', { sampleSource: result.sample.source, sample: result.sample.id, spanGroupsSource: null, spanGroups: null, page: null }, { class: 'btn button' });
    const heading = pageHeader('Transaction operations', text(result.transaction, 'Unnamed transaction'), node('div', { class: 'actions' }, button('Refresh', () => loadRoute()), occurrence));
    heading.classList.add('transaction-heading');
    const project = params.get('project');
    const projectName = project ? state.projects.find(item => String(item.id) === project)?.name || project : 'All projects';
    const summary = node('section', { class: 'card panel', 'aria-label': 'Operation summary' },
      node('dl', { class: 'stat-list' }, [['Transactions', result.transactionCount], ['Attributed spans', result.spanCount], ['Timed spans', result.timedSpanCount]].map(([label, value]) =>
        node('div', { class: 'stat' }, node('dt', {}, label), node('dd', {}, number(value))))),
      node('div', { class: 'panel-body form-stack' }, keyValues([['Transaction', result.transaction], ['Operation', result.op], ['Project scope', projectName]]),
        node('p', { class: 'muted small' }, 'Legacy spans come from matching transactions. Standalone spans use their recorded segment association within the same project and trace. Segment roots are excluded.'),
        node('p', { class: 'muted small' }, 'Counts include spans without valid timing. Average, p95 and accumulated time use valid durations only; Not available means no valid timing was recorded.'),
        node('p', { class: 'muted small' }, 'Accumulated time sums individual span durations. Overlapping spans can make it exceed elapsed transaction time.')));
    const panel = detailPanel('Operations', null);
    panel.setAttribute('aria-label', 'Operation breakdown');
    const groups = result.data || [];
    if (!groups.length) panel.append(empty(result.page > 1 ? 'No operations on this page' : result.transactionCount === 0 ? 'No transactions in this interval' : 'No child operations recorded',
      result.page > 1 ? 'Choose an earlier page to inspect the recorded operations.' : result.transactionCount === 0 ? 'The reference occurrence identifies this group, but the selected project and time window contain no matching transactions.' : 'The matching transactions have no attributed child spans.'));
    else {
      const columns = ['Operation', 'Description', 'Spans', 'Timed spans', 'Average', 'p95', 'Accumulated time'];
      const rows = groups.map(group => node('tr', {}, node('td', { class: 'transaction-name mono' }, text(group.op)), node('td', { class: 'transaction-span-name' }, text(group.description)),
        node('td', { class: 'nowrap' }, number(group.count)), node('td', { class: 'nowrap' }, number(group.timedCount)),
        ['avgDuration', 'p95Duration', 'totalTime'].map(key => node('td', { class: 'nowrap' }, group[key] === null || group[key] === undefined ? 'Not available' : milliseconds(group[key])))));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, rows))));
    }
    const previous = button('Previous operations', () => navigate({ page: result.page - 1 }));
    const next = button('Next operations', () => navigate({ page: result.page + 1 }));
    previous.disabled = result.page <= 1;
    next.disabled = !result.hasMore;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(result.groupCount)} operation groups · ${number(groups.length)} on this page · Page ${number(result.page)}`), node('div', { class: 'actions' }, previous, next)));
    main.replaceChildren(allTransactionsLink(), heading, telemetryWindow(params, 'transactions').summary, summary, panel);
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
    main.replaceChildren(allTransactionsLink(),
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
    const canonicalTrace = params.get('traceMatch') === 'canonical';
    const service = field('Service', 'text', { name: 'service', value: params.get('service') || '', maxlength: 4000, placeholder: 'Exact service name', autocomplete: 'off' });
    const window = telemetryWindow(params, 'logs');
    const form = node('form', { class: 'filters log-filters', role: 'search', 'aria-label': 'Filter logs' }, search.wrap, project.wrap, level.wrap, environment.wrap, trace.wrap, service.wrap, window.wrap,
      node('button', { class: 'btn button', type: 'submit' }, 'Apply filters'));
    form.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ project: project.input.value, query: search.input.value.trim(), level: level.input.value, environment: environment.input.value.trim(), trace: trace.input.value.trim(), traceMatch: canonicalTrace && /^[a-f\d]{32}$/i.test(trace.input.value.trim()) ? 'canonical' : null, service: service.input.value, ...window.changes(), page: null });
    });
    main.append(form, window.summary);
    if (canonicalTrace) main.append(node('p', { class: 'time-window-summary' }, node('span', { class: 'badge badge-sm' }, 'Trace matching'), 'Identifies traces regardless of letter case'));
    const logs = result.data || [];
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const panel = node('section', { class: 'card panel', 'aria-label': 'Logs' });
    if (!logs.length) {
      panel.append(empty(page > 1 ? 'No more logs on this page' : 'No matching logs', 'Check your filters, or send logs from your application SDK.',
        button('Clear filters', () => navigate({ project: null, query: null, level: null, environment: null, trace: null, traceMatch: null, service: null, start: null, end: null, window: null, page: null }))));
    } else {
      const rows = logs.map(log => {
        const projectName = state.projects.find(project => String(project.id) === String(log.projectId))?.name || log.projectId;
        const isOTLP = log.source === 'otlp';
        const disclosure = isOTLP
          ? otlpRowDisclosure(log)
          : node('details', { class: 'log-entry' }, node('summary', {}, node('span', { class: 'log-message' }, text(log.body, '(empty message)'))),
            node('div', { class: 'log-detail' }, node('h2', {}, 'Log attributes'), logAttributes(log.attributes),
              node('div', { class: 'log-identifiers' }, keyValues([['Log ID', log.id], ['Span ID', log.spanId]]))));
        return node('tr', {}, node('td', { class: 'nowrap' }, node('time', { datetime: log.timestamp }, date(log.timestamp, true))),
          node('td', {}, badge(log.level), isOTLP ? node('span', { class: 'badge badge-sm', 'aria-label': 'Received through OpenTelemetry OTLP' }, 'OTLP') : null), node('td', { class: 'log-message-column' }, disclosure), node('td', {}, text(projectName)),
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
    const manage = ['owner', 'admin'].includes(state.organization.role);
    main.replaceChildren(pageHeader('Projects', 'Each application gets its own SDK key and DSN.', button('Create project', () => openProjectDialog(), 'primary')));
    const panel = node('section', { class: 'card panel', 'aria-label': 'Projects' });
    if (!state.projects.length) {
      panel.append(empty('Create your first project', 'Choose a name and platform, then copy the project DSN into your Sentry SDK configuration.', button('Create project', () => openProjectDialog(), 'primary')));
    } else {
      const rows = state.projects.map(project => node('tr', {},
        node('td', {}, node('div', { class: 'project-name' }, project.name), node('div', { class: 'muted small mono' }, project.slug)),
        node('td', {}, badge(project.platform)),
        node('td', {}, node('div', { class: 'dsn' }, node('code', {}, project.dsn), button('Copy', () => copyValue(project.dsn), 'small'))),
        node('td', {}, node('div', { class: 'actions' },
          routeLink('View issues', { view: 'issues', project: project.id, query: null, status: null, environment: null, release: null, page: null }, { class: 'btn button small' }),
          button('Manage SDK keys', () => { void openProjectKeys(project); }, 'small'),
          manage ? button('Edit', () => openProjectDialog(project), 'small') : null,
          manage ? button('Delete', () => confirmProjectDelete(project), 'danger small') : null))));
      const columns = ['Project', 'Platform', 'DSN', ''].map((name, index) => node('th', { scope: 'col', ...(index === 3 ? { 'aria-label': 'Actions' } : {}) }, name));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns)), node('tbody', {}, rows))));
      panel.append(...state.projects.map(sourceMapDisclosure));
      panel.append(...state.projects.map(otlpDisclosure));
      main.append(node('p', { class: 'muted small connection-help' }, 'Use the DSN with your existing Sentry SDK. Keep the SDK configuration in your application; GoodSentry receives events at the project address.'));
    }
    main.append(panel);
  }

  async function openProjectKeys(project) {
    try {
      const { createProjectKeysUI } = await import('./project_keys.js');
      projectKeysUI ||= createProjectKeysUI({ state, node, request, button, field, date, number, copyValue, formError, notify });
      projectKeysUI.open(project);
    } catch (error) {
      notify(`Could not open SDK keys: ${error.message}`);
    }
  }

  function projectPath(project) {
    return `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects/${encodeURIComponent(project.id)}`;
  }

  function confirmProjectDelete(project) {
    const dialog = node('dialog', { 'aria-labelledby': 'project-delete-title' });
    const close = () => dialog.close();
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const submit = button('Delete project permanently', async () => {
      submit.disabled = true;
      try {
        await request(projectPath(project), { method: 'DELETE' });
        dialog.close();
        await loadRoute();
        notify('Project deleted.');
      } catch (failure) {
        formError(error, failure);
        submit.disabled = false;
      }
    }, 'danger');
    dialog.append(node('header', {}, node('h2', { id: 'project-delete-title' }, 'Delete ' + project.name), button('Close', close, 'small')),
      node('div', { class: 'panel-body form-stack' }, error,
        node('p', {}, 'This permanently deletes the project, its DSN keys, events, issues, logs, traces, releases, attachments and debug files. This cannot be undone.'),
        node('div', { class: 'actions' }, button('Cancel', close), submit)));
    dialog.addEventListener('close', () => dialog.remove(), { once: true });
    document.body.append(dialog);
    dialog.showModal();
    submit.focus();
  }

  function clearAPIToken() {
    issuedAPIToken = null;
    document.getElementById('api-token-secret')?.remove();
  }

  function apiTokenPath(id) {
    const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/api-tokens`;
    return id ? `${path}/${encodeURIComponent(id)}` : path;
  }

  function renderAPITokens(main, result) {
    document.title = 'API tokens · GoodSentry';
    main.replaceChildren(pageHeader('API tokens', 'Scoped access for automation in this organization.', button('Refresh', () => loadRoute())));
    if (issuedAPIToken) {
      const secret = issuedAPIToken.record.token;
      const reveal = node('section', { id: 'api-token-secret', class: 'card panel', 'aria-label': 'New API token' },
        node('div', { class: 'panel-body form-stack' }, node('h2', {}, 'Save your API token'),
          node('p', { class: 'muted' }, 'This secret is shown only once. Copy it now and store it securely; it cannot be retrieved after you dismiss or leave this view.'),
          node('code', { class: 'token-secret', 'aria-label': 'API token secret' }, secret),
          node('div', { class: 'actions' }, button('Copy token', () => copyValue(secret, 'API token'), 'primary'), button('Dismiss token', clearAPIToken))));
      main.append(reveal);
    }
    main.append(apiTokenForm());
    const panel = node('section', { class: 'card panel', 'aria-label': 'API token metadata' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Issued tokens')));
    const tokens = result.data || [];
    if (!tokens.length) panel.append(empty('No API tokens on this page', 'Create a credential for your automation, or choose an earlier page.'));
    else {
      const rows = tokens.map(token => {
        const status = token.revokedAt ? 'Revoked' : Date.parse(token.expiresAt) <= Date.now() ? 'Expired' : 'Active';
        const controls = node('div', { class: 'actions' });
        const revoke = button('Revoke', () => {
          const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
          const confirm = button('Confirm revoke', async () => {
            confirm.disabled = true;
            try {
              await request(apiTokenPath(token.id), { method: 'DELETE' });
              if (issuedAPIToken?.record.id === token.id) clearAPIToken();
              notify('API token revoked.');
              await loadRoute();
            } catch (failure) { formError(error, failure); confirm.disabled = false; }
          }, 'danger small');
          controls.replaceChildren(node('p', { class: 'small' }, 'Revoke this token? Subsequent requests will be denied.'), error, confirm, button('Cancel', () => controls.replaceChildren(revoke), 'small'));
        }, 'small');
        if (!token.revokedAt) controls.append(revoke);
        return node('tr', {},
          node('td', { class: 'token-name' }, node('strong', {}, token.name), node('div', { class: 'small muted' }, `Token ${token.id}`), node('code', {}, `${token.prefix}…`)),
          node('td', {}, node('span', { class: `badge badge-sm ${status === 'Active' ? 'resolved' : 'ignored'}` }, status), token.revokedAt ? node('div', {}, node('time', { class: 'small muted', datetime: token.revokedAt }, date(token.revokedAt))) : null),
          node('td', {}, token.scopes.map(scope => node('div', { class: 'small mono' }, scope))),
          node('td', { class: 'token-projects' }, token.allProjects ? 'All current and future projects' : token.projects.length ? token.projects.map(project => node('div', {}, project.name, node('span', { class: 'small muted mono' }, ` (${project.slug})`))) : 'No remaining selected projects'),
          node('td', { class: 'small nowrap' }, `User ${token.createdBy}`, node('div', { class: 'muted' }, node('time', { datetime: token.dateCreated }, date(token.dateCreated)))),
          node('td', { class: 'nowrap' }, node('time', { datetime: token.expiresAt }, date(token.expiresAt))), node('td', { class: 'token-actions' }, controls));
      });
      panel.append(node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, ['Token', 'Status', 'Scopes', 'Projects', 'Created by / created', 'Expires', 'Actions'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, rows))));
    }
    const previous = button('Previous', () => navigate({ page: result.page - 1 }));
    const next = button('Next', () => navigate({ page: result.page + 1 }));
    previous.disabled = result.page <= 1;
    next.disabled = !result.hasMore;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(tokens.length)} tokens on this page · Page ${number(result.page)}`), node('div', { class: 'actions' }, previous, next)));
    main.append(panel);
  }

  function apiTokenForm() {
    const name = field('Token name', 'text', { name: 'name', required: true, autocomplete: 'off', placeholder: 'Project automation' });
    name.wrap.append(node('p', { class: 'help' }, '1–100 characters. Names may be reused.'));
    const lifetime = field('Lifetime (days)', 'number', { name: 'expiresInDays', value: 90, required: true, min: 1, max: 366, step: 1 });
      const availableScopes = ['org:read', 'org:write', 'project:read', 'project:write', 'project:releases', 'event:read', 'event:write', 'event:admin'];
      if (state.me.features?.mcp === true && state.me.features?.logs !== false) availableScopes.push('log:read');
      const scopes = availableScopes.map(scope => ({ scope, input: node('input', { type: 'checkbox', name: 'scopes', value: scope, checked: scope === 'org:read' || scope === 'project:read' }) }));
    const scopeFields = node('fieldset', { class: 'token-fieldset form-stack' }, node('legend', {}, 'Permissions'),
      scopes.map(({ scope, input }) => node('label', { class: 'checkbox-field' }, input, scope)),
      node('p', { class: 'small muted' }, 'org:read lists authorized projects and organization people. org:write invites members and manages teams. project:read reads project metadata, project-scoped event data, attachments and legacy user reports. project:write submits legacy user feedback and reads event attachments; it does not grant other project reads or changes. event:read reads issues, issue events and performance telemetry. event:write reads issues and issue events, changes issue status, assignment and supported mute or release policies individually or in bulk, merges issue groups, and resets future grouping. log:read reads retained logs and is available only for MCP-enabled installations. event:admin deletes whole issue groups and their recorded history; it does not grant read or other write permissions. project:releases reads and manages releases, commits and deploys; writes require a current owner or admin.'));
    const access = selectField('Project access', [['all', 'All current and future projects'], ['selected', 'Selected projects only']], 'all');
    const projects = state.projects.map(project => ({ id: project.id, input: node('input', { type: 'checkbox', value: project.id }), name: project.name }));
    const projectFields = node('fieldset', { class: 'token-fieldset form-stack', hidden: true }, node('legend', {}, 'Selected projects'),
      projects.map(project => node('label', { class: 'checkbox-field' }, project.input, project.name)), projects.length ? null : node('p', { class: 'muted small' }, 'Create a project before issuing a restricted token.'));
    access.input.addEventListener('change', () => { projectFields.hidden = access.input.value !== 'selected'; });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, 'Create token');
    const form = node('form', { class: 'panel-body form-stack', 'aria-label': 'Create API token' },
      node('h2', {}, 'Create API token'), error, node('div', { class: 'token-form-fields' }, name.wrap, lifetime.wrap), scopeFields, access.wrap,
      node('p', { class: 'muted small' }, 'All current and future projects includes projects created after this token. Selected projects grants access only to your choices.'),
      projectFields, node('div', { class: 'actions' }, submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      const selectedScopes = scopes.filter(item => item.input.checked).map(item => item.scope);
      const projectIds = access.input.value === 'selected' ? projects.filter(item => item.input.checked).map(item => item.id) : [];
      if (!selectedScopes.length || (access.input.value === 'selected' && !projectIds.length)) {
        formError(error, new Error(!selectedScopes.length ? 'Choose at least one scope.' : 'Choose at least one project for restricted access.'));
        return;
      }
      submit.disabled = true;
      submit.textContent = 'Creating token…';
      const version = state.requestVersion;
      const organization = state.organization.slug;
      try {
        const record = await request(apiTokenPath(), { method: 'POST', body: { name: name.input.value, scopes: selectedScopes, projectIds, expiresInDays: Number(lifetime.input.value) } });
        if (version !== state.requestVersion) return;
        issuedAPIToken = { organization, record };
        await loadRoute();
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = 'Create token'; }
    });
    return node('section', { class: 'card panel' }, form);
  }

  function canManageMonitors() {
    return ['owner', 'admin'].includes(state.organization.role);
  }

  function monitorPath(id) {
    const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/monitors`;
    return id ? `${path}/${encodeURIComponent(id)}` : path;
  }

  function statusPagePath(id) {
    const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/status-pages`;
    return id ? `${path}/${encodeURIComponent(id)}` : path;
  }

  function monitorStatus(monitor) {
    return node('div', { class: 'actions', role: 'group', 'aria-label': 'Confirmed monitor status' }, badge(monitor.status || 'unknown'), !monitor.enabled ? node('span', { class: 'badge badge-sm' }, 'Paused') : null);
  }

  function renderMonitors(main, result, statusPagesResult) {
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
    renderStatusPages(main, statusPagesResult?.data || [], monitors, manage);
  }

  function renderStatusPages(main, pages, monitors, manage) {
    const heading = node('div', { class: 'page-header' },
      node('div', {}, node('h2', {}, 'Status pages'), node('p', { class: 'muted' }, 'Public service health pages built from selected uptime monitors.')),
      node('div', { class: 'actions' }, manage ? button('Create status page', () => openStatusPageDialog(monitors), 'primary') : null));
    const panel = node('section', { class: 'card panel', 'aria-label': 'Status pages' });
    if (!pages.length) {
      panel.append(empty('No status pages yet', manage ? 'Create a page, choose its monitors and publish it when ready.' : 'An organization owner or admin can configure status pages.'));
    } else {
      const rows = pages.map(page => node('tr', {},
        node('td', {}, node('strong', { class: 'wrap-anywhere' }, page.name), node('div', { class: 'small muted' }, '/status/' + page.slug)),
        node('td', {}, node('span', { class: 'badge badge-sm' }, page.isPublic ? 'Public' : 'Private')),
        node('td', {}, String((page.monitors || []).length)),
        node('td', {}, node('div', { class: 'actions' },
          page.isPublic ? node('a', { class: 'btn button small', href: base + '/status/' + encodeURIComponent(page.slug), target: '_blank', rel: 'noopener' }, 'Open') : null,
          manage ? button('Edit', () => openStatusPageDialog(monitors, page), 'small') : null))));
      panel.append(node('div', { class: 'table-scroll' }, node('table', {},
        node('thead', {}, node('tr', {}, ['Status page', 'Visibility', 'Monitors', 'Actions'].map(name => node('th', { scope: 'col' }, name)))),
        node('tbody', {}, rows))));
    }
    main.append(node('div', { class: 'status-pages-section' }, heading, panel));
  }

  function openStatusPageDialog(monitors, page) {
    const dialog = node('dialog', { class: 'monitor-dialog', 'aria-labelledby': 'status-page-dialog-title' });
    const close = () => dialog.close();
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Name', 'text', { required: true, maxlength: 200, value: page?.name || '' });
    const isPublic = node('input', { type: 'checkbox', checked: Boolean(page?.isPublic) });
    const selected = new Set((page?.monitors || []).map(monitor => monitor.id));
    const monitorInputs = monitors.map(monitor => ({ id: monitor.id, input: node('input', { type: 'checkbox', checked: selected.has(monitor.id) }), name: monitor.name }));
    const monitorChoices = monitorInputs.length
      ? node('fieldset', { class: 'token-fieldset' }, node('legend', {}, 'Monitors'), monitorInputs.map(monitor => node('label', { class: 'checkbox-field' }, monitor.input, monitor.name)))
      : node('p', { class: 'muted small' }, 'Create a monitor before publishing a status page.');
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, page ? 'Save status page' : 'Create status page');
    const form = node('form', { class: 'form-stack', 'aria-label': page ? 'Edit status page' : 'Create status page' }, error, name.wrap,
      node('label', { class: 'checkbox-field' }, isPublic, 'Publish without sign-in'), monitorChoices,
      node('div', { class: 'dialog-actions' }, button('Cancel', close), submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      submit.disabled = true;
      error.textContent = '';
      try {
        await request(page ? statusPagePath(page.slug) : statusPagePath(), {
          method: page ? 'PUT' : 'POST',
          body: { name: name.input.value, isPublic: isPublic.checked, monitorIds: monitorInputs.filter(monitor => monitor.input.checked).map(monitor => monitor.id) },
        });
        dialog.close();
        await loadRoute();
        notify(page ? 'Status page updated.' : 'Status page created.');
      } catch (failure) {
        formError(error, failure);
        submit.disabled = false;
      }
    });
    dialog.append(node('header', {}, node('h2', { id: 'status-page-dialog-title' }, page ? 'Edit status page' : 'Create status page'), button('Close', close, 'small')), form);
    document.body.append(dialog);
    dialog.addEventListener('close', () => dialog.remove(), { once: true });
    dialog.showModal();
    name.input.focus();
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
    const missingResult = monitor.lastCheck ? 'No retained result' : 'Not checked yet';
    const summary = detailPanel('Current state', node('div', { class: 'panel-body' }, monitorStatus(monitor), keyValues([
      ['Interval', `${number(monitor.intervalSeconds)} seconds`], ['Last check', monitor.lastCheck ? date(monitor.lastCheck) : 'Not checked yet'],
      ['Last status change', monitor.lastChange ? date(monitor.lastChange) : 'No changes yet'], ['Next deadline', monitor.enabled ? date(monitor.nextCheck) : 'Paused'],
      ['Consecutive failures', monitor.consecutiveFailures], ['Last probe result', latest ? latest.up ? 'Up' : 'Down' : missingResult],
      ['Last reason', latest ? text(latest.reason) : missingResult], ['Last HTTP status', latest?.statusCode ?? 'Not applicable'],
      ['Last latency', latest?.responseTimeMs === undefined || latest?.responseTimeMs === null ? 'Not applicable' : milliseconds(latest.responseTimeMs)],
    ])));
    summary.setAttribute('aria-label', 'Current monitor state');
    if (!latest && monitor.lastCheck) summary.querySelector('.panel-body').append(node('p', { class: 'muted small', role: 'status' }, 'No check results are retained. The last known monitor state and schedule remain available.'));
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
        node('td', {}, check.isChange ? `${check.previousStatus} → ${check.status}` : 'No change'), node('td', { class: 'monitor-reason' }, monitor.type === 'SSL' && check.reason === 'tls_error' ? 'Certificate or secure connection failed' : text(check.reason)),
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
    const type = selectField('Monitor type', [['HEAD', 'HTTP HEAD'], ['GET', 'HTTP GET'], ['POST', 'HTTP POST'], ['TCP', 'TCP'], ['SSL', 'SSL certificate'], ['heartbeat', 'Incoming heartbeat']], config.type, monitor ? { disabled: true } : {});
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
      target.wrap.querySelector('label').textContent = type.input.value === 'TCP' ? 'TCP host and port' : type.input.value === 'SSL' ? 'HTTPS origin' : 'Target URL';
      timeout.wrap.hidden = heartbeat;
      timeout.input.disabled = heartbeat;
      expectedStatus.wrap.hidden = !http;
      expectedStatus.input.disabled = !http;
      expectedBody.wrap.hidden = !['GET', 'POST'].includes(type.input.value);
      expectedBody.input.disabled = expectedBody.wrap.hidden;
      note.textContent = heartbeat ? 'A heartbeat must arrive within each interval. No additional grace interval is added.' : type.input.value === 'TCP' ? 'Enter a host and port. Bracket IPv6 addresses. Timeout applies to the connection.' : type.input.value === 'SSL' ? 'Checks certificate trust, hostname and current validity at an HTTPS origin, such as https://example.com. Timeout applies to the complete check.' : type.input.value === 'POST' ? 'POST checks send an empty body. Expected body is a literal substring in the first 1 MiB of the response.' : type.input.value === 'GET' ? 'Expected body is a literal substring in the first 1 MiB of the response.' : 'HEAD checks validate the response status without reading its body.';
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
    const disclosure = node('details', { class: 'source-map-disclosure', open: new URLSearchParams(location.search).get('sourceMaps') === project.id, 'aria-label': `Source maps for ${project.name}` }, node('summary', {}, `Source maps · ${project.name}`));
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

  function otlpEndpoint(project) {
    const dsn = new URL(project.dsn);
    return dsn.origin + dsn.pathname.replace(/\/[^/]+\/?$/, '/v1/logs');
  }

  function otlpDisclosure(project) {
    const enabled = state.me.features?.logs !== false;
    const disclosure = node('details', { class: 'otlp-disclosure', open: new URLSearchParams(location.search).get('otlp') === project.id, 'aria-label': `OpenTelemetry logs for ${project.name}` },
      node('summary', {}, `OpenTelemetry · ${project.name}`));
    if (!enabled) {
      disclosure.append(node('div', { class: 'otlp-body' },
        node('p', { class: 'muted' }, 'OpenTelemetry log ingestion is disabled on this server. Enable GOODSENTRY_ENABLE_LOGS to receive OTLP logs.')));
      return disclosure;
    }
    const key = new URL(project.dsn).username;
    const endpoint = otlpEndpoint(project);
    const copyEndpoint = button('Copy endpoint', () => copyValue(endpoint, 'OTLP endpoint'), 'small');
    const copyKey = button('Copy key', () => copyValue(key, 'OTLP key'), 'small');
    const environment = `pip install opentelemetry-sdk==1.45.1 opentelemetry-exporter-otlp-proto-http==1.45.1\n\n` +
      `export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="${endpoint}"\n` +
      `export OTEL_EXPORTER_OTLP_LOGS_PROTOCOL="http/protobuf"\n` +
      `export OTEL_EXPORTER_OTLP_LOGS_HEADERS="x-goodsentry-key=${key}"\n` +
      `export OTEL_EXPORTER_OTLP_LOGS_COMPRESSION="gzip"\n`;
    const example = `from opentelemetry._logs import LogRecord, SeverityNumber\n` +
      `from opentelemetry.sdk._logs import LoggerProvider\n` +
      `from opentelemetry.sdk._logs.export import BatchLogRecordProcessor\n` +
      `from opentelemetry.exporter.otlp.proto.http import Compression\n` +
      `from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter\n\n` +
      `exporter = OTLPLogExporter(\n` +
      `    endpoint="${endpoint}",\n` +
      `    headers={"x-goodsentry-key": "${key}"},\n` +
      `    compression=Compression.Gzip,\n` +
      `)\n` +
      `provider = LoggerProvider()\n` +
      `provider.add_log_record_processor(BatchLogRecordProcessor(exporter))\n` +
      `provider.get_logger("my.application").emit(LogRecord(\n` +
      `    body="Hello from OpenTelemetry",\n` +
      `    severity_number=SeverityNumber.INFO,\n` +
      `    severity_text="INFO",\n` +
      `))\n` +
      `provider.force_flush()\n` +
      `provider.shutdown()\n`;
    disclosure.append(node('div', { class: 'otlp-body form-stack' },
      node('h2', {}, 'OpenTelemetry log ingestion'),
      node('p', { class: 'muted small' }, 'Send logs from any OpenTelemetry HTTP exporter. This endpoint is separate from your Sentry DSN: exporters use a header key instead of DSN credentials.'),
      node('div', { class: 'dsn' }, node('code', { 'aria-label': 'OTLP endpoint' }, endpoint), copyEndpoint),
      node('div', { class: 'dsn' }, node('code', { 'aria-label': 'OTLP key header' }, `X-GoodSentry-Key: ${key}`), copyKey),
      node('h3', {}, 'Install the exporter'),
      node('pre', { class: 'mono', 'aria-label': 'Exporter environment' }, environment),
      node('h3', {}, 'Send a Python log'),
      node('pre', { class: 'mono', 'aria-label': 'Python exporter example' }, example),
      node('p', { class: 'muted small' }, `Sentry DSN (SDK errors): ${project.dsn}. OTLP exporter endpoint: ${endpoint}.`)));
    return disclosure;
  }

  function otlpRowDisclosure(log) {
    const disclosure = node('details', { class: 'log-entry otlp-entry' }, node('summary', {}, node('span', { class: 'log-message' }, text(log.body, '(empty message)'))));
    const content = node('div', { class: 'log-detail otlp-detail' });
    disclosure.append(content);
    const load = async () => {
      if (!disclosure.open || content.dataset.loaded) return;
      content.dataset.loaded = 'pending';
      content.replaceChildren(node('p', { class: 'muted otlp-loading', role: 'status', 'aria-busy': 'true' }, 'Loading OTLP detail…'));
      const version = state.requestVersion;
      const path = `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/logs/${encodeURIComponent(log.id)}`;
      try {
        const detail = await request(path);
        if (version !== state.requestVersion || !disclosure.isConnected) return;
        content.dataset.loaded = 'done';
        content.replaceChildren(...otlpDetailNodes(detail));
      } catch (error) {
        if (version !== state.requestVersion || !disclosure.isConnected) return;
        content.dataset.loaded = '';
        content.replaceChildren(node('div', { class: 'error-state', role: 'alert' },
          node('h2', {}, 'Could not load OTLP detail'), node('p', {}, error.message),
          button('Try again', load)));
      }
    };
    disclosure.addEventListener('toggle', load);
    return disclosure;
  }

  function otlpValue(value) {
    const types = { stringValue: 'string', boolValue: 'boolean', intValue: 'int64', doubleValue: 'double', bytesValue: 'bytes (base64)', arrayValue: 'array', kvlistValue: 'key/value list', stringValueStrindex: 'string dictionary index' };
    const kind = Object.keys(value || {}).find(key => Object.hasOwn(types, key));
    return node('div', { class: 'otlp-value' }, node('span', { class: 'badge badge-sm' }, kind ? types[kind] : 'empty AnyValue'),
      node('pre', { class: 'mono' }, JSON.stringify(value ?? {}, null, 2)));
  }

  function otlpAttributes(attributes) {
    if (!attributes?.length) return node('p', { class: 'muted small' }, 'No attributes');
    return node('dl', { class: 'otlp-attributes' }, attributes.map(attribute => [
      node('dt', {}, attribute.keyStrindex ? `Dictionary key index: ${attribute.keyStrindex}` : JSON.stringify(attribute.key ?? '')),
      node('dd', {}, otlpValue(attribute.value)),
    ]));
  }

  function otlpContext(name, value, schemaURL) {
    return node('section', { class: 'otlp-context', 'aria-label': name }, node('h3', {}, name),
      node('p', { class: 'muted small mono' }, `Schema URL: ${schemaURL ? JSON.stringify(schemaURL) : '(absent)'}`),
      value == null ? node('p', { class: 'muted' }, '(absent)') : [
        name === 'Scope' ? keyValues([['Name', JSON.stringify(value.name ?? '')], ['Version', JSON.stringify(value.version ?? '')]]) : null,
        node('h4', {}, 'Attributes'), otlpAttributes(value.attributes),
        keyValues([['Dropped attributes', value.droppedAttributesCount ?? 0]]),
      ]);
  }

  function otlpDetailNodes(detail) {
    const data = detail.data || {};
    const record = data.logRecord || {};
    return [
      node('h2', {}, 'OpenTelemetry log detail'),
      node('div', { class: 'log-identifiers' }, keyValues([
        ['Log ID', detail.id], ['Source', detail.source], ['Severity number', record.severityNumber],
        ['Severity text', record.severityText], ['Event name', record.eventName],
        ['Timestamp source', detail.timestampSource], ['Received at', detail.receivedAt], ['Trace flags', detail.traceFlags],
        ['Trace ID', record.traceId], ['Span ID', record.spanId], ['Original flags', record.flags ?? 0],
        ['Dropped attributes', record.droppedAttributesCount ?? 0],
      ])),
      node('h3', {}, 'Timestamps (exact nanoseconds)'),
      keyValues([['timeUnixNano', record.timeUnixNano], ['observedTimeUnixNano', record.observedTimeUnixNano]]),
      node('h3', {}, 'Body'), otlpValue(record.body),
      otlpContext('Resource', data.resource, data.resourceSchemaUrl),
      otlpContext('Scope', data.scope, data.scopeSchemaUrl),
      node('h3', {}, 'Record attributes'), otlpAttributes(record.attributes),
      node('h3', {}, 'Log record'),
      node('pre', { class: 'mono' }, text(record)),
    ];
  }

  function sourceMapForm(project, path, refresh) {
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const status = node('p', { class: 'source-map-status', role: 'status' });
    const name = field('Artifact name or URL', 'text', { name: 'name', required: true, maxlength: 2048, autocomplete: 'off' });
    const release = field('Release', 'text', { name: 'release', maxlength: 510, autocomplete: 'off' });
    release.input.addEventListener('input', () => release.input.setCustomValidity(Array.from(release.input.value).length > 255 ? 'Use at most 255 Unicode characters for the release.' : ''));
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
      if (!release.input.value && !debug.input.value.trim()) {
        formError(error, new Error('Provide a release or debug ID before uploading this source map.'));
        return;
      }
      const payload = new FormData(form);
      for (const fieldName of ['name', 'dist', 'debugId']) payload.set(fieldName, String(payload.get(fieldName) || '').trim());
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

  function openProjectDialog(project) {
    const dialog = node('dialog', { 'aria-labelledby': 'project-dialog-title' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Project name', 'text', { name: 'name', required: true, maxlength: 200, autocomplete: 'off', value: project?.name || '' });
    const platform = selectField('Platform', [['javascript', 'JavaScript / Browser'], ['node', 'Node.js'], ['python', 'Python'], ['go', 'Go'], ['java', 'Java'], ['php', 'PHP'], ['ruby', 'Ruby'], ['csharp', '.NET'], ['other', 'Other']], project?.platform || 'javascript');
    const submit = node('button', { type: 'submit', class: 'btn button primary' }, project ? 'Save project' : 'Create project');
    const close = () => dialog.close();
    const form = node('form', { class: 'form-stack' }, error, name.wrap, platform.wrap,
      node('div', { class: 'dialog-actions' }, button('Cancel', close), submit));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      submit.textContent = project ? 'Saving…' : 'Creating…';
      try {
        await request(project ? projectPath(project) : `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects`, { method: project ? 'PUT' : 'POST', body: { name: name.input.value.trim(), platform: platform.input.value } });
        dialog.close();
        if (project) {
          await loadRoute();
        } else {
          history.replaceState(null, '', routeURL({ view: 'projects', project: null, page: null }));
          await loadRoute(true);
        }
        notify(project ? 'Project updated.' : 'Project created. Copy its DSN to connect your application.');
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = project ? 'Save project' : 'Create project'; }
    });
    dialog.append(node('header', {}, node('h2', { id: 'project-dialog-title' }, project ? 'Edit project' : 'Create project'), button('Close', close, 'small')), form);
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

  function issueTags(values, issue) {
    const entries = pairs(values).filter(entry => entry.length === 2 && typeof entry[0] === 'string' && typeof entry[1] === 'string');
    if (!entries.length) return node('p', { class: 'muted small' }, 'None recorded.');
    return node('dl', { class: 'key-values' }, entries.map(([key, value]) => [node('dt', {}, key), node('dd', {},
      routeLink(value || '""', { view: issue.category === 'feedback' ? 'feedback' : 'issues', issue: null, project: issue.projectId, query: `tags[${JSON.stringify(key)}]:${JSON.stringify(value)}`, status: 'all', page: null },
        { class: 'text-button', title: `Find issues with tag ${JSON.stringify(key)}:${JSON.stringify(value)}` }))]));
  }

  function detailPanel(title, content) {
    return node('section', { class: 'card panel' }, node('div', { class: 'panel-title' }, node('h2', {}, title)), content);
  }

  function eventAttachments(issuePath, eventID, current) {
    const path = `${issuePath}/events/${encodeURIComponent(eventID)}/attachments`;
    const content = node('div');
    const status = node('p', { class: 'small', role: 'status' });
    const downloadError = node('p', { class: 'form-error', role: 'alert' });
    let page = 1, sequence = 0;
    const refresh = button('Refresh attachments', () => load(1), 'small');
    const previous = button('Previous attachments', () => load(page - 1), 'small');
    const next = button('Next attachments', () => load(page + 1), 'small');
    const pageLabel = node('span', { class: 'muted small', 'aria-live': 'polite' });
    const panel = detailPanel('Event attachments', node('div', { class: 'panel-body form-stack' },
      node('div', { class: 'actions' }, refresh), content,
      node('div', { class: 'actions', 'aria-label': 'Attachment pages' }, previous, pageLabel, next), status, downloadError));
    panel.setAttribute('aria-label', 'Event attachments');
    const alive = () => current() && panel.isConnected;

    async function download(file, control) {
      control.disabled = true;
      status.textContent = 'Downloading attachment…';
      downloadError.textContent = '';
      try {
        const blob = await request(`${path}/${encodeURIComponent(file.id)}`, { binary: true });
        if (!alive()) return;
        const url = URL.createObjectURL(blob);
        const link = node('a', { href: url, download: file.downloadName });
        document.body.append(link);
        try { link.click(); }
        finally { link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000); }
        status.textContent = 'Download started.';
      } catch (error) {
        if (!alive()) return;
        status.textContent = '';
        downloadError.textContent = error.status === 404
          ? 'This attachment is no longer stored or accessible. Refresh attachments to see the current files.'
          : error.message;
      } finally { control.disabled = false; }
    }

    async function load(requestedPage) {
      const version = ++sequence;
      refresh.disabled = previous.disabled = next.disabled = true;
      status.textContent = '';
      downloadError.textContent = '';
      pageLabel.textContent = `Page ${requestedPage}`;
      content.replaceChildren(node('p', { class: 'muted', role: 'status', 'aria-busy': 'true' }, 'Loading attachments…'));
      let more = false;
      try {
        const result = await request(`${path}?page=${requestedPage}`);
        if (!alive() || version !== sequence) return;
        page = requestedPage;
        const files = result.data || [];
        more = result.hasMore;
        if (!files.length) {
          content.replaceChildren(node('p', { class: 'muted', role: 'status' }, page === 1 ? 'No attachments stored for this event.' : 'No attachments on this page.'));
        } else {
          const rows = files.map(file => {
            const control = button('Download', () => download(file, control), 'small');
            control.setAttribute('aria-label', `Download ${file.downloadName}`);
            const name = Array.from(file.filename);
            const filename = name.length > 120
              ? node('details', {}, node('summary', {}, name.slice(0, 100).join('') + '…'), node('p', { class: 'small' }, file.filename))
              : node('strong', {}, file.filename);
            return node('tr', {},
              node('td', { class: 'artifact-name' }, filename, file.metadataTruncated ? node('p', { class: 'small muted' }, 'Name or type shortened for display.') : null),
              node('td', { class: 'artifact-name' }, text(file.contentType)),
              node('td', { class: 'nowrap' }, `${number(file.size)} B`),
              node('td', { class: 'nowrap' }, node('time', { datetime: file.dateCreated }, date(file.dateCreated, true))),
              node('td', {}, control));
          });
          content.replaceChildren(node('div', { class: 'table-scroll', 'aria-label': 'Attachment metadata', tabindex: '0' }, node('table', {},
            node('thead', {}, node('tr', {}, ['Name', 'Type', 'Size', 'Uploaded', 'Download'].map(name => node('th', { scope: 'col' }, name)))),
            node('tbody', {}, rows))));
        }
      } catch (error) {
        if (!alive() || version !== sequence) return;
        errorState(content, error, () => load(requestedPage));
      } finally {
        if (alive() && version === sequence) {
          refresh.disabled = false;
          previous.disabled = page <= 1;
          next.disabled = !more;
          pageLabel.textContent = `Page ${page}`;
        }
      }
    }
    return { panel, start: () => load(1) };
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

  function issueAnalytics(analytics) {
    const content = node('div', { class: 'panel-body form-stack' });
    const panel = detailPanel('Events in the last 24 hours', content);
    panel.setAttribute('aria-label', 'Issue event analytics');
    const series = analytics?.stats?.['24h'];
    if (!Array.isArray(series) || series.length !== 24) {
      content.append(node('p', { class: 'muted' }, 'Event analytics are unavailable. Refresh this issue to load them.'));
      return panel;
    }
    const total = series.reduce((sum, [, count]) => sum + count, 0);
    const peak = Math.max(1, ...series.map(([, count]) => count));
    const timestamp = epoch => new Date(epoch * 1000).toISOString();
    const time = epoch => node('time', { datetime: timestamp(epoch) }, timestamp(epoch).replace('T', ' ').replace('.000Z', ' UTC'));
    content.append(node('p', { class: 'issue-chart-total' }, `${number(total)} ${total === 1 ? 'event' : 'events'}`),
      node('p', { class: 'muted small' }, 'All environments. Each bar is one hour; times are UTC.'));
    if (total === 0) content.append(node('p', { class: 'muted' }, 'No events in the last 24 hours.'));
    const svg = (tag, attributes = {}, text) => {
      const element = document.createElementNS('http://www.w3.org/2000/svg', tag);
      for (const [name, value] of Object.entries(attributes)) element.setAttribute(name, String(value));
      if (text !== undefined) element.textContent = text;
      return element;
    };
    const plot = svg('svg', { class: 'issue-chart-bars', viewBox: '0 0 240 100', preserveAspectRatio: 'none', role: 'img', 'aria-label': `${number(total)} events across 24 hourly buckets. Exact counts and UTC times are available in Hourly event data.` });
    series.forEach(([start, count], index) => {
      const height = count / peak * 100;
      const bar = svg('rect', { class: 'issue-chart-bar', x: index * 10 + 1, y: 100 - height, width: 8, height, rx: 1, 'aria-hidden': 'true' });
      bar.append(svg('title', {}, `${timestamp(start)} · ${number(count)} ${count === 1 ? 'event' : 'events'}`));
      plot.append(bar);
    });
    content.append(node('figure', { class: 'issue-chart' }, plot,
      node('figcaption', { class: 'issue-chart-range muted small' }, node('span', {}, time(series[0][0])), node('span', {}, time(series.at(-1)[0] + 3600)))));
    const table = node('table', { class: 'issue-chart-table' }, node('caption', {}, 'Hourly event counts (UTC)'),
      node('thead', {}, node('tr', {}, ['From (UTC)', 'To (UTC)', 'Events'].map(label => node('th', { scope: 'col' }, label)))),
      node('tbody', {}, series.map(([start, count]) => node('tr', {}, node('td', {}, time(start)), node('td', {}, time(start + 3600)), node('td', {}, number(count))))));
    content.append(node('details', { class: 'issue-chart-data' }, node('summary', {}, 'Hourly event data'), node('div', { class: 'table-scroll' }, table)));
    return panel;
  }

  function renderIssue(main, detail, previousView) {
    const issue = detail.issue;
    const feedback = issue.category === 'feedback';
    const event = detail.event || {};
    const hasEvents = Array.isArray(detail.events) && detail.events.length > 0;
    const view = previousView || {};
    view.draftIssueId ??= issue.id;
    view.knownOriginIDs = new Set([issue.id]);
    issueDraftOwners.set(issue.id, issue.id);
    if (view.draftIssueId !== issue.id) issueDraftOwners.set(view.draftIssueId, issue.id);
    view.commentPage ??= 1;
    view.activityPage ??= 1;
    view.assigneePage ??= 1;
    view.assigneeQuery ??= '';
    view.commentsResult ??= { data: detail.comments || [], total: detail.commentTotal ?? detail.comments?.length ?? 0, page: 1, pageSize };
    issueViewContext = { organization: state.organization.slug, issueId: issue.id, view };
    const renderVersion = ++issueRenderVersion;
    const routeVersion = state.requestVersion;
    const alive = () => renderVersion === issueRenderVersion && routeVersion === state.requestVersion && main.isConnected && !!state.me;
    const path = `/api/v1/issues/${encodeURIComponent(issue.id)}`;
    let mutationPending = false;
    const mutationControls = new Map();
    const register = control => {
      mutationControls.set(control, control.disabled);
      if (mutationPending) control.disabled = true;
      return control;
    };
    const mutate = async (operation, success, target, current = () => true) => {
      if (mutationPending || !alive() || !current()) return;
      mutationPending = true;
      mutationControls.forEach((_, control) => { if (control.isConnected) control.disabled = true; });
      target.textContent = '';
      try {
        const result = await operation();
        if (alive() && current()) await success(result);
      } catch (failure) {
        if (alive() && current()) formError(target, failure, view);
      } finally {
        mutationPending = false;
        if (alive()) mutationControls.forEach((disabled, control) => {
          if (control.isConnected) control.disabled = disabled;
          else mutationControls.delete(control);
        });
      }
    };
    document.title = `${issue.title} · GoodSentry`;
    const statusLabel = badge(issue.status);
    const actionError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const statusActions = node('div', { class: 'actions', 'aria-label': feedback ? 'Feedback status' : 'Issue status' });
    const patchIssue = (changes, message, errorTarget = actionError, current = () => true) => mutate(
      () => request(path, { method: 'PATCH', body: changes }),
      updated => {
        renderIssue(main, { ...detail, issue: updated }, view);
        main.querySelector('h1')?.focus();
        notify(message);
      }, errorTarget, current);
    for (const [status, label] of [['unresolved', 'Reopen'], ['resolved', 'Resolve'], ['ignored', 'Ignore']]) {
      if (issue.status === status) continue;
      statusActions.append(register(button(feedback && status === 'ignored' ? 'Ignore indefinitely' : label, () => patchIssue({ status }, `${feedback ? 'Feedback' : 'Issue'} ${status === 'unresolved' ? 'reopened' : status}.`), status === 'resolved' ? 'primary' : '')));
    }
    statusActions.append(register(button('Refresh issue', () => loadRoute(true))));
    const heading = node('div', { class: 'page-header' }, node('div', {},
      node('div', { class: 'eyebrow' }, issue.shortId || issue.id), node('h1', { class: 'detail-title', tabindex: '-1' }, issue.title),
      node('div', { class: 'detail-meta' }, statusLabel, badge(issue.level), issue.regressedAt ? node('span', { class: 'badge badge-sm error' }, 'Regressed') : null, issue.groupSize != null ? node('span', { class: 'badge badge-sm' }, `${number(issue.groupSize)} original ${issue.groupSize === 1 ? 'group' : 'groups'}`) : null, node('span', { class: 'muted' }, issue.project?.name || 'Project'), issue.environment ? node('span', { class: 'badge badge-sm' }, issue.environment) : null)), statusActions);
    const affectedUsers = detail.analytics?.userCount;
    const stats = node('dl', { class: 'stat-list issue-stat-list' }, [[feedback ? 'Submissions' : 'Events', number(issue.count)], ['Affected users', affectedUsers == null ? 'Unavailable' : number(affectedUsers)], ['First seen', date(issue.firstSeen, true)], ['Last seen', date(issue.lastSeen, true)]].map(([name, value]) => node('div', { class: 'stat' }, node('dt', {}, name), node('dd', {}, value), name === 'Affected users' ? node('p', { class: 'stat-note' }, 'Identified retained users') : null)));
    const summary = node('section', { class: 'card panel', 'aria-label': 'Issue summary' }, stats, node('div', { class: 'panel-body' },
      node('p', { class: 'muted small' }, hasEvents ? 'Latest event' : 'No retained events'),
      hasEvents ? node('p', { class: 'mono' }, text(event.event_id || event.eventID)) : null,
      event.message || event.logentry?.formatted ? node('pre', { class: 'event-message' }, text(event.message || event.logentry.formatted)) : null,
      hasEvents ? null : node('p', { role: 'status' }, 'This issue has no stored event payloads. Comments, activity and triage remain available.')));
    const selectedTrace = traceEntry(event.contexts?.trace?.trace_id);
    if (selectedTrace) summary.lastElementChild.append(node('div', { class: 'event-message actions' }, selectedTrace));
    for (const [label, timestamp] of [['Last resolved', issue.resolvedAt], ['Regressed', issue.regressedAt]]) {
      if (timestamp) summary.lastElementChild.append(node('p', { class: 'event-message small' }, `${label}: `, node('time', { datetime: timestamp, title: timestamp }, date(timestamp))));
    }
    const refresh = async () => {
      const updated = await request(path);
      if (!alive()) return false;
      delete view.commentsResult;
      rememberIssueCommentOwners(updated, view);
      if (updated.issue.id !== issue.id) history.replaceState(null, '', routeURL({ issue: updated.issue.id }));
      renderIssue(main, updated, view);
      return true;
    };
    const triage = { alive, register, mutate, patchIssue, refresh };
    const deleteError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const deleteConfirmation = node('section', { class: 'card panel panel-body form-stack', hidden: true, 'aria-label': 'Confirm issue deletion' });
    const deleteIssue = register(button(feedback ? 'Delete feedback' : 'Delete issue', () => { deleteConfirmation.hidden = false; confirmDelete.focus(); }, 'danger'));
    const confirmDelete = register(button('Confirm deletion', async () => {
      confirmDelete.textContent = 'Deleting…';
      await triage.mutate(() => request(`/api/0/organizations/${encodeURIComponent(state.organization.slug)}/issues/${encodeURIComponent(issue.id)}/`, { method: 'DELETE' }),
        () => finishIssueDeletion(state.organization.slug, [issue.id], view), deleteError, () => !deleteConfirmation.hidden);
      if (alive()) confirmDelete.textContent = 'Confirm deletion';
    }, 'danger'));
    const cancelDelete = register(button('Cancel deletion', () => { deleteConfirmation.hidden = true; deleteError.textContent = ''; deleteIssue.focus(); }));
    deleteConfirmation.append(node('h2', {}, feedback ? 'Delete this feedback group?' : 'Delete this issue?'), node('p', {}, feedback ? 'This removes the whole feedback group, its submissions, attachments, comments and assignment. Future submissions can create a new group.' : issueDeletionWarning), deleteError, node('div', { class: 'actions' }, confirmDelete, cancelDelete));
    statusActions.append(deleteIssue);
    let discardConfirmation = null;
    if (issue.category === 'error') {
      const discardError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      discardConfirmation = node('section', { class: 'card panel panel-body form-stack', hidden: true, 'aria-label': 'Confirm permanent issue discard' });
      const discardIssue = register(button('Delete and discard forever', () => { discardConfirmation.hidden = false; confirmDiscard.focus(); }, 'danger'));
      const confirmDiscard = register(button('Confirm permanent discard', async () => {
        confirmDiscard.textContent = 'Discarding…';
        await triage.mutate(() => request(`/api/0/organizations/${encodeURIComponent(state.organization.slug)}/issues/${encodeURIComponent(issue.id)}/`, { method: 'PUT', body: { discard: true } }),
          () => finishIssueDeletion(state.organization.slug, [issue.id], view), discardError, () => !discardConfirmation.hidden);
        if (alive()) confirmDiscard.textContent = 'Confirm permanent discard';
      }, 'danger'));
      const cancelDiscard = register(button('Cancel discard', () => { discardConfirmation.hidden = true; discardError.textContent = ''; discardIssue.focus(); }));
      discardConfirmation.append(node('h2', {}, 'Delete and discard this issue forever?'), node('p', {}, 'This permanently removes the issue. Future errors with its active fingerprints will be discarded instead of creating a new issue. This cannot be undone.'), discardError, node('div', { class: 'actions' }, confirmDiscard, cancelDiscard));
      statusActions.append(discardIssue);
    }
    const activity = issueActivity(path, view, triage);
    const comments = issueComments(issue, path, view, triage, () => activity.load(view.activityPage));
    const assignment = issueAssignment(issue, path, view, triage);
    const mute = feedback ? null : issueMute(issue, view, triage);
    const resolution = feedback ? null : issueResolution(issue, path, view, triage);
    const origins = issueOrigins(issue, path, view, triage);
    // The selected raw event is the first retained history entry in IssueDetail.
    const attachments = hasEvents && !feedback ? eventAttachments(path, detail.events[0].eventID, alive) : null;
    const grouping = detailPanel('Future grouping', node('p', { class: 'panel-body muted', role: 'status' }, 'Loading active signatures…'));
    const reports = detailPanel(feedback ? 'Feedback submissions' : 'User reports', node('p', { class: 'panel-body muted', role: 'status' }, feedback ? 'Loading feedback submissions…' : 'Loading user reports…'));
    const left = feedback ? node('div', {}, summary, reports, issueAnalytics(detail.analytics), comments.panel, activity.panel, grouping, origins.panel) :
      node('div', {}, summary, issueAnalytics(detail.analytics), grouping, origins.panel, hasEvents ? stackTrace(event) : null, hasEvents ? breadcrumbs(event) : null, attachments?.panel, reports, comments.panel, activity.panel);
    if (hasEvents && !feedback) left.append(node('details', { class: 'card panel' }, node('summary', {}, 'Original event JSON'), node('pre', { class: 'event-json' }, JSON.stringify(event, null, 2))));
    const meta = [['Platform', event.platform || issue.platform], ['Environment', event.environment || issue.environment], ['Release', event.release || issue.release], ['SDK', event.sdk ? [event.sdk.name, event.sdk.version].filter(Boolean).join(' ') : null]];
    const right = node('aside', { class: 'detail-aside', 'aria-label': hasEvents ? 'Event details' : 'Issue details' },
      resolution?.panel,
      mute,
      assignment.panel,
      detailPanel(hasEvents && !feedback ? 'Event details' : 'Last known metadata', node('div', { class: 'panel-body' }, keyValues(meta))),
      hasEvents && !feedback ? detailPanel('Tags', node('div', { class: 'panel-body' }, issueTags(event.tags, issue))) : null,
      hasEvents && !feedback ? detailPanel('User', node('div', { class: 'panel-body' }, keyValues(event.user))) : null);
    if (!feedback && event.contexts && Object.keys(event.contexts).length) right.append(detailPanel('Contexts', node('div', { class: 'panel-body' }, Object.entries(event.contexts).map(([name, context]) => node('section', { class: 'context-block' }, node('h3', {}, name), typeof context === 'object' && context ? keyValues(context) : node('p', {}, text(context)))))));
    if (!feedback && event.request) right.append(detailPanel('Request', node('div', { class: 'panel-body' }, keyValues(event.request))));
    if (!feedback && event.extra && Object.keys(event.extra).length) right.append(detailPanel('Extra', node('div', { class: 'panel-body' }, keyValues(event.extra))));
    main.replaceChildren(routeLink(feedback ? '← All feedback' : '← All issues', { issue: null }, { class: 'back-link' }), heading, actionError, deleteConfirmation, ...(discardConfirmation ? [discardConfirmation] : []), node('div', { class: 'detail-grid' }, left, right));
    attachments?.start();
    comments.start();
    activity.load(view.activityPage);
    assignment.load(view.assigneePage);
    resolution?.start();
    origins.load(view.origins.page);
    loadIssueHashes(grouping, issue, view, triage);
    if (feedback) loadIssueFeedback(reports, issue, detail, view, triage);
    else loadIssueReports(reports, issue, detail, view, triage);
  }

  async function loadIssueReports(placeholder, issue, detail, view, triage, retry = 0) {
    try {
      const { createReportsUI } = await import(retry ? `./reports.js?retry=${retry}` : './reports.js');
      if (!triage.alive() || !placeholder.isConnected) return;
      reportsUI ||= createReportsUI({ node, request, button, detailPanel, formError, date, number });
      const eventContent = (event, eventID, isCurrent) => selectedEventContent(event, issue, { eventID, isCurrent, triage, withAttachments: true, label: 'Reported event JSON' });
      const reports = reportsUI.issue({ issue, organization: state.organization.slug, initialReports: detail.userReports, total: detail.userReportCount, view, triage, eventContent });
      placeholder.replaceWith(reports.panel);
      if (retry) reports.panel.querySelector('h2')?.focus();
      reports.start();
    } catch (failure) {
      if (!triage.alive() || !placeholder.isConnected) return;
      placeholder.replaceChildren(node('div', { class: 'panel-title' }, node('h2', {}, 'User reports')),
        node('div', { class: 'panel-body form-stack' }, node('p', { class: 'form-error', role: 'alert' }, failure.message), button('Retry user reports', () => loadIssueReports(placeholder, issue, detail, view, triage, retry + 1), 'small')));
      if (retry) placeholder.querySelector('button')?.focus();
    }
  }

  function selectedEventContent(event, issue, { eventID, isCurrent, triage, withAttachments, label }) {
    const path = `/api/v1/issues/${encodeURIComponent(issue.id)}`;
    const attachments = withAttachments ? eventAttachments(path, eventID, () => triage.alive() && isCurrent()) : null;
    const meta = [['Platform', event.platform], ['Environment', event.environment], ['Release', event.release], ['SDK', event.sdk ? [event.sdk.name, event.sdk.version].filter(Boolean).join(' ') : null]];
    const message = event.type === 'feedback' ? event.contexts?.feedback?.message : event.message || event.logentry?.formatted;
    const content = node('div', { class: 'reported-event-content' },
      node('div', { class: 'panel-body' }, message ? node('pre', { class: 'event-message' }, text(message)) : null, keyValues(meta)),
      stackTrace(event), breadcrumbs(event), attachments?.panel,
      detailPanel('Tags', node('div', { class: 'panel-body' }, issueTags(event.tags, issue))),
      detailPanel('User', node('div', { class: 'panel-body' }, keyValues(event.user))));
    if (event.contexts && Object.keys(event.contexts).length) content.append(detailPanel('Contexts', node('div', { class: 'panel-body' }, Object.entries(event.contexts).map(([name, context]) => node('section', { class: 'context-block' }, node('h3', {}, name), typeof context === 'object' && context ? keyValues(context) : node('p', {}, text(context)))))));
    if (event.request) content.append(detailPanel('Request', node('div', { class: 'panel-body' }, keyValues(event.request))));
    if (event.extra && Object.keys(event.extra).length) content.append(detailPanel('Extra', node('div', { class: 'panel-body' }, keyValues(event.extra))));
    content.append(node('details', { class: 'card panel' }, node('summary', {}, label), node('pre', { class: 'event-json' }, JSON.stringify(event, null, 2))));
    content.querySelectorAll('h3').forEach(heading => heading.replaceWith(node('h5', {}, heading.textContent)));
    content.querySelectorAll('h2').forEach(heading => heading.replaceWith(node('h4', {}, heading.textContent)));
    return { content, start: attachments?.start };
  }

  async function loadIssueFeedback(placeholder, issue, detail, view, triage, retry = 0) {
    try {
      const { createFeedbackUI } = await import(retry ? `./feedback.js?retry=${retry}` : './feedback.js');
      if (!triage.alive() || !placeholder.isConnected) return;
      feedbackUI ||= createFeedbackUI({ node, request, button, detailPanel, formError, date, number });
      const submissionContent = (event, eventID, isCurrent) => selectedEventContent(event, issue, { eventID, isCurrent, triage, withAttachments: true, label: 'Submission JSON' });
      const associatedContent = (event, isCurrent) => selectedEventContent(event, { ...issue, category: 'error' }, { isCurrent, triage, withAttachments: false, label: 'Associated event JSON' });
      const associatedIssueLink = id => routeLink('Open associated issue', { ...traceReset, view: 'issues', issue: id, project: issue.projectId, query: null, status: null, environment: null, release: null, sort: null, page: null }, { class: 'btn button small' });
      const feedback = feedbackUI.issue({ issue, organization: state.organization.slug, initialFeedback: detail.feedbackItems, total: detail.feedbackCount, view, triage, submissionContent, associatedContent, associatedIssueLink });
      placeholder.replaceWith(feedback.panel);
      if (retry) feedback.panel.querySelector('h2')?.focus();
      feedback.start();
    } catch (failure) {
      if (!triage.alive() || !placeholder.isConnected) return;
      placeholder.replaceChildren(node('div', { class: 'panel-title' }, node('h2', {}, 'Feedback submissions')),
        node('div', { class: 'panel-body form-stack' }, node('p', { class: 'form-error', role: 'alert' }, failure.message), button('Retry feedback module', () => loadIssueFeedback(placeholder, issue, detail, view, triage, retry + 1), 'small')));
      if (retry) placeholder.querySelector('button')?.focus();
    }
  }

  async function loadIssueHashes(placeholder, issue, view, triage) {
    try {
      const { createHashesUI } = await import('./hashes.js');
      if (!triage.alive() || !placeholder.isConnected) return;
      hashesUI ||= createHashesUI({ node, request, button, detailPanel, formError, notify, date, number });
      const grouping = hashesUI.issue(issue, state.organization.slug, view, triage);
      placeholder.replaceWith(grouping.panel);
      grouping.start();
    } catch (failure) {
      if (!triage.alive() || !placeholder.isConnected) return;
      const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
      error.textContent = failure.message;
      placeholder.replaceChildren(node('div', { class: 'panel-title' }, node('h2', {}, 'Future grouping')),
        node('div', { class: 'panel-body form-stack' }, error, button('Retry future grouping', () => loadIssueHashes(placeholder, issue, view, triage), 'small')));
    }
  }

  function issueOrigins(issue, path, view, triage) {
    view.origins ??= { query: '', search: '', page: 1, selected: new Map(), outcome: null };
    const draft = view.origins, selected = draft.selected;
    const search = field('Search original groups', 'search', { value: draft.query, autocomplete: 'off' });
    const validate = () => search.input.setCustomValidity(new TextEncoder().encode(search.input.value).length > 200 || search.input.value.includes('\0') ? 'Use at most 200 bytes without null characters.' : '');
    search.input.addEventListener('input', () => { draft.query = search.input.value; validate(); });
    validate();
    const searchForm = node('form', { class: 'form-stack', 'aria-label': 'Find original groups' }, search.wrap, node('div', { class: 'actions' }, node('button', { class: 'btn button small', type: 'submit' }, 'Search original groups')));
    const status = node('p', { class: 'small', role: 'status' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const outcome = node('div', { class: 'form-stack' });
    const confirmation = node('div', { class: 'form-stack', hidden: true });
    const content = node('div');
    let sequence = 0, selectingAll = false, confirming = false, rows = [], boxes = [], pageCheckbox;
    const clear = button('Clear original-group selection', () => { selected.clear(); changed(); }, 'small');
    const separate = button('Separate selected', () => { confirming = true; sync(); }, 'small');
    const all = button('Select all original groups', selectAll, 'small');
    const panel = detailPanel('Original groups', node('div', {}, node('div', { class: 'panel-body form-stack' },
      node('p', { class: 'muted small' }, 'Each row is an original group. Separation moves its recorded history; comments and activity stay with their original groups. Historical only groups do not redirect captures assigned to a newer issue.'), searchForm,
      status, node('div', { class: 'actions' }, separate, clear, all), confirmation, error, outcome), content));
    panel.setAttribute('aria-label', 'Original groups');
    const alive = () => triage.alive() && panel.isConnected;
    const disabled = (control, value) => { control.disabled = value; triage.register(control); };
    function changed() { confirming = false; error.textContent = ''; sync(); }
    function sync() {
      status.textContent = `${selected.size} selected · Select up to 100, including the current root. Selections stay across pages and searches.`;
      disabled(clear, selectingAll || selected.size === 0);
      disabled(separate, selectingAll || selected.size === 0);
      disabled(all, selectingAll || issue.groupSize > 100);
      all.textContent = selectingAll ? 'Selecting original groups…' : 'Select all original groups';
      for (const { input, origin } of boxes) {
        input.checked = selected.has(origin.id);
        disabled(input, selectingAll || (!input.checked && selected.size >= 100));
      }
      if (pageCheckbox) {
        const count = rows.filter(origin => selected.has(origin.id)).length;
        pageCheckbox.checked = rows.length > 0 && count === rows.length;
        pageCheckbox.indeterminate = count > 0 && count < rows.length;
        disabled(pageCheckbox, selectingAll || rows.length === 0);
      }
      confirmation.hidden = !confirming || selected.size === 0;
      if (confirming) {
        const confirm = button('Confirm separation', submit, 'primary small');
        const cancel = button('Cancel separation', () => { confirming = false; sync(); }, 'small');
        disabled(confirm, selectingAll);
        disabled(cancel, selectingAll);
        confirmation.replaceChildren(node('p', {}, `Separate ${selected.size} original ${selected.size === 1 ? 'group' : 'groups'}? Unselected groups stay together under their lowest ID.`),
          ...(selected.has(issue.id) ? [node('p', { class: 'muted small' }, 'The selected current root keeps its own state. The remaining group uses the state of its new root.')] : []), node('div', { class: 'actions' }, confirm, cancel));
      }
    }
    function moved(id) {
      const failure = new Error('This issue now belongs to a different group. Open the current group to continue.');
      failure.status = 409;
      failure.mergedInto = id;
      return failure;
    }
    function showOutcome() {
      if (!draft.outcome) return;
      const result = draft.outcome;
      const ids = sortedIssueIDs(new Set([...result.detachedIssueIds, ...(result.remainingIssueId ? [result.remainingIssueId] : [])]));
      outcome.replaceChildren(node('p', { role: 'status' }, result.changed ? 'Original groups separated.' : 'The selected groups are already separate.'),
        node('div', { class: 'actions' }, ids.map(id => routeLink(`Open issue ${id}`, { issue: id }, { class: 'btn button small' }))));
    }
    async function submit() {
      if (!alive() || selected.size === 0 || selectingAll) return;
      const ids = sortedIssueIDs(selected.keys());
      await triage.mutate(() => request(`${path}/unmerge`, { method: 'POST', body: { originIds: ids } }), async result => {
        selected.clear();
        draft.page = 1;
        draft.outcome = result;
        showOutcome();
        if (await triage.refresh()) notify(result.changed ? 'Original groups separated.' : 'The selected groups are already separate.');
      }, error, () => panel.isConnected);
    }
    async function selectAll() {
      if (!alive() || selectingAll) return;
      selectingAll = true;
      error.textContent = '';
      sync();
      try {
        const first = await request(`${path}/origins?page=1`);
        if (!alive()) return;
        if (first.issueId !== issue.id) throw moved(first.issueId);
        if (first.total > 100) throw new Error('This group has more than 100 original groups. Select individual groups or pages, up to 100 at a time.');
        const pages = await Promise.all(Array.from({ length: Math.max(0, Math.ceil(first.total / pageSize) - 1) }, (_, index) => request(`${path}/origins?page=${index + 2}`)));
        if (!alive()) return;
        if (pages.some(result => result.issueId !== issue.id || result.total !== first.total)) throw new Error('Group membership changed while selecting. Refresh the original groups and try again.');
        const origins = [first, ...pages].flatMap(result => result.data);
        if (new Set(origins.map(origin => origin.id)).size !== first.total) throw new Error('Group membership changed while selecting. Refresh the original groups and try again.');
        selected.clear();
        for (const origin of origins) { selected.set(origin.id, origin); view.knownOriginIDs.add(origin.id); issueDraftOwners.set(origin.id, issue.id); }
        confirming = false;
      } catch (failure) { if (alive()) formError(error, failure, view); }
      finally { selectingAll = false; if (alive()) sync(); }
    }
    async function load(page) {
      const current = ++sequence;
      draft.page = page;
      rows = []; boxes = []; pageCheckbox = undefined;
      content.replaceChildren(node('p', { class: 'panel-body muted', role: 'status' }, 'Loading original groups…'));
      try {
        const query = new URLSearchParams({ page: String(page), query: draft.search });
        const result = await request(`${path}/origins?${query}`);
        if (!alive() || current !== sequence) return;
        if (result.issueId !== issue.id) throw moved(result.issueId);
        rows = result.data;
        for (const origin of rows) { view.knownOriginIDs.add(origin.id); issueDraftOwners.set(origin.id, issue.id); }
        pageCheckbox = node('input', { type: 'checkbox', class: 'size-4 shrink-0 cursor-pointer accent-primary', 'aria-label': 'Select all original groups on this page', onchange: () => {
          const add = pageCheckbox.checked;
          if (add && selected.size + rows.filter(origin => !selected.has(origin.id)).length > 100) {
            formError(error, new Error('Select at most 100 original groups. Clear some selections before adding this page.'));
            sync(); return;
          }
          for (const origin of rows) { if (add) selected.set(origin.id, origin); else selected.delete(origin.id); }
          changed();
        } });
        const dates = timestamp => timestamp ? node('time', { datetime: timestamp, title: timestamp }, date(timestamp)) : 'Not recorded';
        const body = rows.map(origin => {
          if (selected.has(origin.id)) selected.set(origin.id, origin);
          const input = node('input', { type: 'checkbox', class: 'size-4 shrink-0 cursor-pointer accent-primary', 'aria-label': `Select original group ${origin.id}`, onchange: () => {
            if (input.checked) selected.set(origin.id, origin); else selected.delete(origin.id);
            changed();
          } });
          boxes.push({ input, origin });
          return node('tr', {}, node('td', {}, input), node('td', { class: 'issue-column' }, node('strong', { class: 'wrap-anywhere' }, origin.title),
            node('div', { class: 'issue-subtitle' }, node('span', { class: 'mono' }, `Issue ${origin.id}`), origin.isRoot ? node('span', { class: 'badge badge-sm' }, 'Current root') : null, origin.receivesNewEvents === false ? node('span', { class: 'badge badge-sm' }, 'Historical only') : null),
            node('p', { class: 'mono small wrap-anywhere' }, origin.fingerprint)), node('td', {}, number(origin.count)), node('td', {}, dates(origin.firstSeen)), node('td', {}, dates(origin.lastSeen)));
        });
        content.replaceChildren(rows.length ? node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, node('th', { scope: 'col' }, pageCheckbox), ['Original group', 'Events', 'First seen', 'Last seen'].map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, body))) : node('p', { class: 'panel-body muted' }, result.total ? 'No original groups on this page. Choose an earlier page.' : 'No matching original groups.'), issueHistoryPager('origins', result, load));
        sync();
      } catch (failure) {
        if (alive() && current === sequence) {
          const loadError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
          formError(loadError, failure, view);
          content.replaceChildren(node('div', { class: 'panel-body form-stack' }, loadError, button('Retry original groups', () => load(page), 'small')));
        }
      }
    }
    searchForm.addEventListener('submit', event => { event.preventDefault(); draft.search = search.input.value; load(1); });
    showOutcome();
    sync();
    return { panel, load };
  }

  function issueHistoryPager(kind, result, load) {
    const singular = { comments: 'comment', activity: 'activity entry', assignees: 'assignee', releases: 'release', origins: 'original group' }[kind];
    const plural = kind === 'activity' ? 'activity entries' : kind === 'origins' ? 'original groups' : kind;
    const previous = button(`Previous ${kind}`, () => load(result.page - 1));
    const next = button(`Next ${kind}`, () => load(result.page + 1));
    previous.disabled = result.page <= 1;
    next.disabled = result.page * result.pageSize >= result.total;
    return node('div', { class: 'pagination' }, node('span', {}, `${number(result.total)} ${result.total === 1 ? singular : plural} · Page ${number(result.page)}`), node('div', { class: 'actions' }, previous, next));
  }

  function issueActor(actor) {
    if (!actor) return 'Unassigned';
    return actor.name || actor.email || actor.slug || (actor.type === 'team' ? 'Unnamed team' : 'Member');
  }

  function issueResolutionDescription(resolution) {
    if (!resolution) return 'Simple resolution';
    if (resolution.mode === 'release') return `Chosen release: ${resolution.release?.version ?? 'Not recorded'}`;
    return resolution.release ? `Next release: ${resolution.release.version}` : `Waiting for the next release · Baseline: ${resolution.baseline?.version ?? 'Not recorded'}`;
  }

  function issueResolution(issue, path, view, triage) {
    view.resolutionDraft ??= { version: issue.resolution?.mode === 'release' ? issue.resolution.release?.version || '' : '', query: '', search: '', page: 1, open: false };
    const draft = view.resolutionDraft;
    const content = node('div', { class: 'panel-body form-stack' });
    const reference = (label, value) => {
      if (!value) return;
      const times = node('dl', { class: 'key-values small' });
      for (const [name, timestamp] of [['Created (UTC)', value.dateCreated], ['Released (UTC)', value.dateReleased]]) times.append(node('dt', {}, name), node('dd', {}, timestamp ? node('time', { datetime: timestamp }, timestamp) : 'Not recorded'));
      content.append(node('div', { class: 'form-stack' }, node('p', { class: 'muted small' }, label), node('code', { class: 'whitespace-pre-wrap wrap-anywhere' }, value.version), times));
    };
    if (issue.status !== 'resolved') content.append(node('p', {}, 'This issue is not resolved.'));
    else if (!issue.resolution) content.append(node('p', {}, 'Simple resolution. Any new error reopens this issue.'));
    else {
      const policy = issue.resolution;
      content.append(node('p', {}, policy.mode === 'release' ? 'Resolved in a chosen release.' : policy.release ? 'The next release has been selected.' : 'Waiting for the next eligible project release.'),
        node('p', { class: 'muted small' }, policy.comparison === 'semantic' ? 'Comparison: semantic version order.' : 'Comparison: recorded release dates.'));
      reference(policy.mode === 'release' ? 'Chosen release' : 'Selected next release', policy.release);
      reference('Original observed baseline', policy.baseline);
      content.append(node('p', { class: 'muted small' }, 'Saved release references stay fixed if release metadata changes.'));
    }
    content.append(node('p', { class: 'muted small' }, 'Errors without a valid release reopen this issue. Build metadata does not change semantic version order.'));
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const version = field('Chosen release version', 'text', { value: draft.version, required: true, autocomplete: 'off' });
    const validateVersion = () => version.input.setCustomValidity(Array.from(version.input.value).length > 255 ? 'Use at most 255 Unicode characters.' : '');
    version.input.addEventListener('input', () => { draft.version = version.input.value; validateVersion(); });
    validateVersion();
    const choose = value => { draft.version = value; version.input.value = value; validateVersion(); version.input.focus(); };
    const observed = button('Use observed release', () => choose(issue.release), 'small');
    observed.disabled = !issue.release;
    const simple = button('Resolve without release', () => triage.patchIssue({ status: 'resolved', resolution: null }, 'Issue resolved without a release condition.', error, () => form.isConnected), 'small');
    simple.disabled = issue.status === 'resolved' && !issue.resolution;
    triage.register(simple);
    const next = triage.register(button('Resolve in next release', () => triage.patchIssue({ status: 'resolved', resolution: { mode: 'next_release' } }, 'Issue resolved in the next release.', error, () => form.isConnected), 'small'));
    const form = node('form', { class: 'form-stack', 'aria-label': 'Choose release resolution' }, version.wrap,
      node('p', { class: 'muted small' }, 'Version identity includes its exact spaces, case and slashes.'),
      node('div', { class: 'actions' }, triage.register(node('button', { class: 'btn button primary', type: 'submit' }, 'Resolve in release')), observed),
      node('div', { class: 'actions' }, simple, next), error);
    form.addEventListener('submit', event => {
      event.preventDefault();
      triage.patchIssue({ status: 'resolved', resolution: { mode: 'release', version: version.input.value } }, 'Issue resolved in the chosen release.', error, () => form.isConnected);
    });
    content.append(form);
    if (issue.release) content.append(node('div', {}, node('p', { class: 'muted small' }, 'Last SDK-observed release'), node('code', { class: 'whitespace-pre-wrap wrap-anywhere' }, issue.release)));
    const search = field('Search project releases', 'search', { value: draft.query, autocomplete: 'off' });
    const validateSearch = () => search.input.setCustomValidity(new TextEncoder().encode(search.input.value).length > 200 ? 'Use at most 200 bytes for this search.' : '');
    search.input.addEventListener('input', () => {
      draft.query = search.input.value;
      validateSearch();
    });
    validateSearch();
    const searchForm = node('form', { class: 'form-stack', 'aria-label': 'Find a release for this issue' }, search.wrap, node('button', { class: 'btn button', type: 'submit' }, 'Search releases'));
    const results = node('div');
    const browse = node('details', { open: draft.open }, node('summary', {}, 'Browse project releases'), node('div', { class: 'form-stack' }, searchForm, results));
    content.append(browse);
    const panel = detailPanel('Resolution', content);
    panel.setAttribute('aria-label', 'Resolution');
    let sequence = 0, started = false;
    async function load(page) {
      const current = ++sequence;
      draft.page = page;
      results.replaceChildren(node('p', { class: 'muted small', role: 'status' }, 'Loading releases…'));
      try {
        const query = new URLSearchParams({ page: String(page), query: draft.search });
        const result = await request(`${path}/releases?${query}`);
        if (!triage.alive() || current !== sequence || !panel.isConnected) return;
        const list = node('ul', { class: 'comment-list', 'aria-label': 'Project release candidates' }, result.data.map(release => {
          const use = button('Use release', () => { choose(release.version); browse.open = false; draft.open = false; }, 'small');
          use.setAttribute('aria-label', `Use release ${release.version}`);
          return node('li', { class: 'comment' }, node('code', { class: 'whitespace-pre-wrap wrap-anywhere' }, release.version), node('p', { class: 'small muted' }, release.status),
            node('p', { class: 'mono small' }, 'Created (UTC): ', node('time', { datetime: release.dateCreated }, release.dateCreated)),
            release.dateReleased ? node('p', { class: 'mono small' }, 'Released (UTC): ', node('time', { datetime: release.dateReleased }, release.dateReleased)) : null, use);
        }));
        results.replaceChildren(result.data.length ? list : node('p', { class: 'muted small' }, result.total ? 'No releases on this page. Choose an earlier page.' : 'No matching releases for this issue’s project.'), issueHistoryPager('releases', result, load));
      } catch (failure) {
        if (triage.alive() && current === sequence && panel.isConnected) results.replaceChildren(node('div', { class: 'form-stack', role: 'alert' }, node('p', {}, failure.message), button('Retry releases', () => load(page))));
      }
    }
    const start = () => { if (browse.open && !started) { started = true; load(draft.page); } };
    browse.addEventListener('toggle', () => { draft.open = browse.open; if (panel.isConnected) start(); });
    searchForm.addEventListener('submit', event => { event.preventDefault(); draft.search = search.input.value; started = true; load(1); });
    return { panel, start };
  }

  function issueMuteDescription(ignore) {
    if (!ignore) return 'Indefinitely';
    const conditions = [];
    if (ignore.minutes != null) conditions.push(`${number(ignore.minutes)} ${ignore.minutes === 1 ? 'minute' : 'minutes'}`);
    if (ignore.count != null) {
      const errors = `${number(ignore.count)} additional ${ignore.count === 1 ? 'error' : 'errors'}`;
      conditions.push(ignore.window == null ? errors : `${errors} within any ${number(ignore.window)} minutes`);
    }
    if (ignore.userCount != null) {
      const users = `${number(ignore.userCount)} additional ${ignore.userCount === 1 ? 'user' : 'users'}`;
      conditions.push(ignore.userWindow == null ? users : `${users} within any ${number(ignore.userWindow)} minutes`);
    }
    return conditions.join(' or ');
  }

  function issueMute(issue, view, triage) {
    view.muteDraft ??= { minutes: issue.ignore?.minutes == null ? '' : String(issue.ignore.minutes), count: issue.ignore?.count == null ? '' : String(issue.ignore.count), userCount: issue.ignore?.userCount == null ? '' : String(issue.ignore.userCount), window: issue.ignore?.window == null ? '' : String(issue.ignore.window), userWindow: issue.ignore?.userWindow == null ? '' : String(issue.ignore.userWindow) };
    const helpID = `mute-help-${++fieldNumber}`;
    const minutes = field('Minutes (optional)', 'text', { value: view.muteDraft.minutes, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 10, autocomplete: 'off', 'aria-describedby': helpID });
    const count = field('Additional errors (optional)', 'text', { value: view.muteDraft.count, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 10, autocomplete: 'off', 'aria-describedby': helpID });
    const userCount = field('Additional users (optional)', 'text', { value: view.muteDraft.userCount, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 10, autocomplete: 'off', 'aria-describedby': helpID });
    const window = field('Count window in minutes (optional)', 'text', { value: view.muteDraft.window, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 5, autocomplete: 'off', 'aria-describedby': helpID });
    const userWindow = field('User window in minutes (optional)', 'text', { value: view.muteDraft.userWindow, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 5, autocomplete: 'off', 'aria-describedby': helpID });
    for (const [key, control] of [['minutes', minutes], ['count', count], ['userCount', userCount], ['window', window], ['userWindow', userWindow]]) control.input.addEventListener('input', () => { view.muteDraft[key] = control.input.value; });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const current = node('div', { class: 'form-stack' });
    if (issue.status !== 'ignored') current.append(node('p', {}, 'No notification mute is active.'));
    else if (!issue.ignore) current.append(node('p', {}, 'Muted indefinitely.'));
    else {
      current.append(node('p', {}, `Muted for ${issueMuteDescription(issue.ignore)}.`));
      if (issue.ignore.until) current.append(node('p', { class: 'small' }, 'Ends: ', node('time', { datetime: issue.ignore.until, title: issue.ignore.until }, date(issue.ignore.until)), ' (your local time)'), node('p', { class: 'mono small wrap-anywhere' }, `UTC: ${issue.ignore.until}`));
      if (issue.ignore.remaining != null) current.append(node('p', {}, `${number(issue.ignore.remaining)} additional ${issue.ignore.remaining === 1 ? 'error' : 'errors'} remaining.`));
      if (issue.ignore.userRemaining != null) current.append(node('p', {}, `${number(issue.ignore.userRemaining)} additional ${issue.ignore.userRemaining === 1 ? 'user' : 'users'} remaining.`));
    }
    const clear = button('Mute indefinitely', () => triage.patchIssue({ status: 'ignored', ignore: null }, 'Mute is now indefinite.', error, () => form.isConnected), 'small');
    clear.disabled = issue.status !== 'ignored' || !issue.ignore;
    triage.register(clear);
    const submit = triage.register(node('button', { class: 'btn button primary', type: 'submit' }, 'Apply mute'));
    const form = node('form', { class: 'panel-body form-stack', 'aria-label': 'Mute notifications' }, current,
      node('p', { class: 'muted small' }, 'New issue notifications pause while error capture continues. If both conditions are set, either condition ends the mute.'),
      node('p', { class: 'muted small', id: helpID }, 'Set minutes, additional errors, additional users, or any combination. Each window requires its matching positive count and accepts 1 to 10,080 minutes; other conditions accept whole numbers up to 2,147,483,647.'),
      minutes.wrap, count.wrap, userCount.wrap, window.wrap, userWindow.wrap, error, node('div', { class: 'actions' }, submit, clear));
    form.addEventListener('submit', event => {
      event.preventDefault();
      const ignore = {};
      for (const [key, control] of [['minutes', minutes], ['count', count], ['userCount', userCount], ['window', window], ['userWindow', userWindow]]) {
        const value = control.input.value;
        if (!value) continue;
        const maximum = key === 'window' || key === 'userWindow' ? 10080 : 2147483647;
        if (!/^[0-9]+$/.test(value) || Number(value) < 1 || Number(value) > maximum) {
          formError(error, new Error(key === 'window' || key === 'userWindow' ? 'Use a whole window from 1 to 10,080 minutes.' : 'Use whole numbers from 1 to 2,147,483,647 for each mute condition.'));
          return;
        }
        ignore[key] = Number(value);
      }
      if (ignore.window != null && ignore.count == null) { formError(error, new Error('A count window requires an additional error count.')); return; }
      if (ignore.userWindow != null && ignore.userCount == null) { formError(error, new Error('A user window requires an additional user count.')); return; }
      if (!Object.keys(ignore).length) { formError(error, new Error('Enter minutes, an additional error count, additional users, or any combination.')); return; }
      triage.patchIssue({ status: 'ignored', ignore }, 'Mute updated.', error, () => form.isConnected);
    });
    const panel = detailPanel('Mute notifications', form);
    panel.setAttribute('aria-label', 'Mute notifications');
    return panel;
  }

  function issueAssignment(issue, path, view, triage) {
    const search = field('Search people and teams', 'search', { value: view.assigneeQuery, maxlength: 200, autocomplete: 'off' });
    search.input.addEventListener('input', () => {
      view.assigneeQuery = search.input.value;
      search.input.setCustomValidity(new TextEncoder().encode(search.input.value).length > 200 ? 'Use at most 200 bytes for this search.' : '');
    });
    const clear = button('Clear assignment', () => triage.patchIssue({ assignedTo: null }, 'Assignment cleared.'), 'small');
    clear.disabled = !issue.assignedTo;
    triage.register(clear);
    const results = node('div');
    const form = node('form', { class: 'panel-body form-stack', 'aria-label': 'Find an assignee' },
      node('div', { class: 'form-stack' }, node('div', {}, node('p', { class: 'muted small' }, 'Assigned to'), node('strong', { class: 'wrap-anywhere' }, issueActor(issue.assignedTo))), clear),
      search.wrap, node('button', { class: 'btn button', type: 'submit' }, 'Search assignees'));
    const panel = detailPanel('Assignment', node('div', {}, form, results));
    panel.setAttribute('aria-label', 'Assignment');
    let sequence = 0;
    async function load(page) {
      const current = ++sequence;
      view.assigneePage = page;
      view.assigneeQuery = search.input.value;
      results.replaceChildren(node('p', { class: 'panel-body muted', role: 'status' }, 'Loading assignees…'));
      try {
        const query = new URLSearchParams({ page: String(page), query: view.assigneeQuery });
        const result = await request(`${path}/assignees?${query}`);
        if (!triage.alive() || current !== sequence || !panel.isConnected) return;
        const list = node('ul', { class: 'comment-list', 'aria-label': 'Assignable people and teams' }, result.data.map(actor => {
          const type = actor.type === 'team' ? 'Team' : 'Person';
          const control = button('Assign', () => triage.patchIssue({ assignedTo: { type: actor.type, id: actor.id } }, 'Assignment updated.'), 'small');
          control.setAttribute('aria-label', `Assign to ${type.toLowerCase()} ${issueActor(actor)}${actor.email ? ` (${actor.email})` : actor.slug ? ` (${actor.slug})` : ''}`);
          control.disabled = issue.assignedTo?.type === actor.type && issue.assignedTo?.id === actor.id;
          return node('li', { class: 'comment' }, node('div', { class: 'comment-top' }, node('div', { class: 'min-w-0 wrap-anywhere' }, node('strong', {}, issueActor(actor)), node('p', { class: 'small muted' }, [type, actor.email || actor.slug].filter(Boolean).join(' · '))), triage.register(control)));
        }));
        results.replaceChildren(result.data.length ? list : node('p', { class: 'panel-body muted' }, 'No matching people or teams on this page.'), issueHistoryPager('assignees', result, load));
      } catch (failure) {
        if (triage.alive() && current === sequence && panel.isConnected) results.replaceChildren(node('div', { class: 'panel-body form-stack', role: 'alert' }, node('p', {}, failure.message), button('Retry assignees', () => load(page))));
      }
    }
    form.addEventListener('submit', event => { event.preventDefault(); load(1); });
    return { panel, load };
  }

  function issueActivity(path, view, triage) {
    const content = node('div');
    const panel = detailPanel('Activity', node('div', {}, node('div', { class: 'panel-body actions' }, button('Refresh activity', () => load(view.activityPage), 'small')), content));
    panel.setAttribute('aria-label', 'Activity');
    let sequence = 0;
    const labels = { first_seen: 'First error recorded', regressed: 'Issue regressed', resolved: 'Marked resolved', resolution_bound: 'Next release selected', unresolved: 'Reopened issue', ignored: 'Marked ignored', unignored: 'Mute ended', assigned: 'Assigned issue', unassigned: 'Cleared assignment', comment_added: 'Added a comment', comment_edited: 'Edited a comment', comment_deleted: 'Deleted a comment', merged: 'Merged issues', unmerged: 'Separated original groups', grouping_reset: 'Reset future grouping' };
    function entry(item) {
      const actor = item.user?.name || item.user?.email || (['first_seen', 'regressed', 'unignored', 'resolution_bound'].includes(item.type) ? 'System' : 'Actor not recorded');
      const data = item.data || {};
      let description = '';
      if (item.type === 'assigned') description = `${issueActor(data.from)} → ${issueActor(data.to)}`;
      else if (item.type === 'unassigned' && data.from) description = `Previously assigned to ${issueActor(data.from)}`;
      else if (['resolved', 'unresolved', 'ignored'].includes(item.type) && data.from && data.to) description = `${data.from} → ${data.to}`;
      if (item.type === 'resolved' && Object.hasOwn(data, 'resolution')) description = [description, issueResolutionDescription(data.resolution)].filter(Boolean).join(' · ');
      else if (item.type === 'resolution_bound') description = issueResolutionDescription(data.resolution);
      else if (item.type === 'ignored' && Object.hasOwn(data, 'ignore')) description = [description, `Mute: ${issueMuteDescription(data.ignore)}`].filter(Boolean).join(' · ');
      else if (item.type === 'unignored') description = [data.reason === 'duration' ? 'Duration reached' : data.reason === 'count' ? 'Additional error count reached' : data.reason === 'user_count' ? 'Additional user count reached' : 'Mute condition ended', data.ignore ? `Previous condition: ${issueMuteDescription(data.ignore)}` : '', data.eventID ? `New event ${data.eventID}` : ''].filter(Boolean).join(' · ');
      else if (item.type === 'regressed') {
        const reason = { newer_release: 'Newer release observed', next_release: 'Next release reached', missing_release: 'Missing or invalid release' }[data.reason];
        description = [data.eventID ? `New event ${data.eventID}` : '', data.resolvedAt ? `Previously resolved ${date(data.resolvedAt)}` : '', reason, data.resolution ? issueResolutionDescription(data.resolution) : ''].filter(Boolean).join(' · ');
      }
      else if (item.type.startsWith('comment_') && data.commentID) description = `Comment ${data.commentID}`;
      else if (item.type === 'merged') {
        if (Array.isArray(data.sourceIssueIds)) description = `Merged issues ${data.sourceIssueIds.join(', ')} · Recorded events: ${number(data.count)}`;
        else {
          description = `Original issue ${text(data.sourceIssueId)} joined issue ${text(data.mergedInto)}`;
          if (data.previous) {
            const previous = data.previous;
            description += ` · Previous status: ${text(previous.status)} · Events: ${number(previous.count)} · Assignment: ${issueActor(previous.assignedTo)}`;
            if (previous.resolvedAt) description += ` · Resolved: ${date(previous.resolvedAt)}`;
            if (previous.regressedAt) description += ` · Regressed: ${date(previous.regressedAt)}`;
            if (previous.status === 'ignored') {
              description += ` · Mute: ${issueMuteDescription(previous.ignore)}`;
              if (previous.ignore?.until) description += ` · Mute ends: ${date(previous.ignore.until)}`;
              if (previous.ignore?.remaining != null) description += ` · Additional errors remaining: ${number(previous.ignore.remaining)}`;
              if (previous.ignore?.userRemaining != null) description += ` · Additional users remaining: ${number(previous.ignore.userRemaining)}`;
            }
            if (previous.status === 'resolved') description += ` · ${issueResolutionDescription(previous.resolution)}`;
          }
        }
      } else if (item.type === 'grouping_reset') {
        if (Array.isArray(data.hashIds)) description = `Future grouping reset for ${number(data.hashIds.length)} ${data.hashIds.length === 1 ? 'signature' : 'signatures'}. Existing events stay in their recorded groups.`;
      } else if (item.type === 'unmerged') {
        description = [data.sourceIssueId ? `Separated from issue ${data.sourceIssueId}` : '', Array.isArray(data.originIds) ? `Selected original groups: ${data.originIds.join(', ')}` : '',
          data.remainingIssueId ? `Remaining group: ${data.remainingIssueId}` : 'No remaining group', `Recorded events: ${number(data.count)}`].filter(Boolean).join(' · ');
      }
      return node('li', { class: 'comment' }, node('div', { class: 'comment-top' }, node('strong', {}, labels[item.type] || 'Recorded activity'), node('time', { datetime: item.dateCreated, title: item.dateCreated }, date(item.dateCreated))), node('p', { class: 'muted small' }, actor), description ? node('p', {}, description) : null);
    }
    async function load(page) {
      const current = ++sequence;
      view.activityPage = page;
      content.replaceChildren(node('p', { class: 'panel-body muted', role: 'status' }, 'Loading activity…'));
      try {
        const result = await request(`${path}/activity?page=${page}`);
        if (!triage.alive() || current !== sequence || !panel.isConnected) return;
        content.replaceChildren(result.data.length ? node('ol', { class: 'comment-list', 'aria-label': 'Recorded activity' }, result.data.map(entry)) : node('p', { class: 'panel-body muted' }, 'No recorded activity on this page.'), issueHistoryPager('activity', result, load));
      } catch (failure) {
        if (triage.alive() && current === sequence && panel.isConnected) content.replaceChildren(node('div', { class: 'panel-body form-stack', role: 'alert' }, node('p', {}, failure.message), button('Retry activity', () => load(page))));
      }
    }
    return { panel, load };
  }

  function issueComments(issue, path, view, triage, refreshActivity) {
    const history = node('div');
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const comment = field('Add a comment', 'textarea', { name: 'text', required: true, maxlength: 10000, rows: 3, placeholder: 'What should the team know?' });
    const draftIssueId = view.draftIssueId;
    comment.input.value = commentDrafts.get(draftIssueId) || '';
    comment.input.addEventListener('input', () => commentDrafts.set(draftIssueId, comment.input.value));
    const submit = triage.register(node('button', { class: 'btn button primary', type: 'submit' }, 'Post comment'));
    const form = node('form', { class: 'comment-form form-stack' }, error, comment.wrap, node('div', { class: 'actions' }, submit));
    const panel = detailPanel('Comments', node('div', {}, node('div', { class: 'panel-body actions' }, button('Refresh comments', () => load(view.commentPage), 'small')), history, form));
    panel.setAttribute('aria-label', 'Comments');
    let sequence = 0;
    function renderComment(record) {
      const row = node('li', { class: 'comment' });
      const draftKey = record.id;
      function show() {
        const heading = node('div', { class: 'comment-top' }, node('strong', {}, record.user?.name || record.user?.email || 'Member'), node('time', { datetime: record.dateCreated, title: record.dateCreated }, date(record.dateCreated)));
        row.replaceChildren(heading, ...(record.updatedAt ? [node('p', { class: 'small muted' }, 'Edited ', node('time', { datetime: record.updatedAt, title: record.updatedAt }, date(record.updatedAt)))] : []), node('p', {}, record.text));
        if (record.canEdit) row.append(node('div', { class: 'actions' }, triage.register(button('Edit comment', edit, 'small')), triage.register(button('Delete comment', remove, 'small'))));
      }
      function edit() {
        if (!triage.alive() || !row.isConnected) return;
        const input = field('Edit comment text', 'textarea', { required: true, maxlength: 10000, rows: 3 });
        input.input.value = commentEditDrafts.get(draftKey) ?? record.text;
        commentEditDrafts.set(draftKey, input.input.value);
        const knownOwner = commentEditIssueIDs.get(draftKey) || view.knownCommentOwners?.get(draftKey);
        if (knownOwner) commentEditIssueIDs.set(draftKey, knownOwner);
        input.input.addEventListener('input', () => commentEditDrafts.set(draftKey, input.input.value));
        const editError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
        const cancel = button('Cancel edit', () => { commentEditDrafts.delete(draftKey); commentEditIssueIDs.delete(draftKey); show(); }, 'small');
        const editor = node('form', { class: 'form-stack', 'aria-label': 'Edit comment' }, input.wrap, editError,
          node('div', { class: 'actions' }, triage.register(node('button', { class: 'btn button primary small', type: 'submit' }, 'Save comment')), triage.register(cancel)));
        editor.addEventListener('submit', event => {
          event.preventDefault();
          const draft = input.input.value;
          const value = draft.trim();
          if (!value) { formError(editError, new Error('Enter a comment before saving.')); return; }
          triage.mutate(() => request(`${path}/comments/${encodeURIComponent(record.id)}`, { method: 'PATCH', body: { text: value } }), async () => {
            if (commentEditDrafts.get(draftKey) === draft) { commentEditDrafts.delete(draftKey); commentEditIssueIDs.delete(draftKey); }
            await load(view.commentPage);
            if (triage.alive()) { refreshActivity(); notify('Comment updated.'); }
          }, editError, () => editor.isConnected);
        });
        row.replaceChildren(editor);
        input.input.focus();
      }
      function remove() {
        if (!triage.alive() || !row.isConnected) return;
        const deleteError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
        const confirmation = node('div', { class: 'form-stack' }, node('p', {}, 'Delete this comment? Its text will be removed.'), deleteError);
        confirmation.append(node('div', { class: 'actions' }, triage.register(button('Confirm deletion', () => triage.mutate(
          () => request(`${path}/comments/${encodeURIComponent(record.id)}`, { method: 'DELETE' }),
          async () => {
            commentEditDrafts.delete(draftKey);
            commentEditIssueIDs.delete(draftKey);
            await load(view.commentPage);
            if (triage.alive()) { refreshActivity(); notify('Comment deleted.'); }
          }, deleteError, () => confirmation.isConnected), 'danger small')), triage.register(button('Cancel deletion', show, 'small'))));
        row.replaceChildren(node('p', {}, record.text), confirmation);
      }
      show();
      if (record.canEdit && commentEditDrafts.has(draftKey)) queueMicrotask(() => { if (row.isConnected && triage.alive()) edit(); });
      return row;
    }
    function render(result) {
      view.commentsResult = result;
      history.replaceChildren(result.data.length ? node('ol', { class: 'comment-list', 'aria-label': 'Comments' }, result.data.map(renderComment)) : node('p', { class: 'panel-body muted' }, result.total ? 'No comments on this page. Choose an earlier page.' : 'No comments yet. Share what you found with your team.'), issueHistoryPager('comments', result, load));
    }
    async function load(page) {
      const current = ++sequence;
      view.commentPage = page;
      history.replaceChildren(node('p', { class: 'panel-body muted', role: 'status' }, 'Loading comments…'));
      try {
        const result = await request(`${path}/comments?page=${page}`);
        if (triage.alive() && current === sequence && panel.isConnected) render(result);
      } catch (failure) {
        if (triage.alive() && current === sequence && panel.isConnected) history.replaceChildren(node('div', { class: 'panel-body form-stack', role: 'alert' }, node('p', {}, failure.message), button('Retry comments', () => load(page))));
      }
    }
    form.addEventListener('submit', event => {
      event.preventDefault();
      const value = comment.input.value.trim();
      if (!value) { formError(error, new Error('Enter a comment before posting.')); return; }
      const draft = comment.input.value;
      triage.mutate(() => request(`${path}/comments`, { method: 'POST', body: { text: value } }), async () => {
        if (comment.input.value === draft) { comment.input.value = ''; commentDrafts.delete(draftIssueId); }
        await load(1);
        if (triage.alive()) { refreshActivity(); notify('Comment posted.'); comment.input.focus(); }
      }, error, () => form.isConnected);
    });
    return { panel, start: () => view.commentsResult?.page === view.commentPage ? render(view.commentsResult) : load(view.commentPage) };
  }

  async function initialize() {
    try {
      const setup = await request('/api/v1/setup');
      if (setup.required) { renderAuth(true); return; }
      state.authConfig = await request('/api/v1/auth/config').catch(() => ({ oidc: { enabled: false } }));
      const authError = new URLSearchParams(location.search).get('authError');
      if (authError) {
        history.replaceState(null, '', routeURL({ authError: null }));
        renderAuth(false, authError === 'no-account'
          ? 'No GoodSentry account is linked to that verified identity. Ask an organization owner for an invitation.'
          : 'Single sign-on could not be completed. Try again or sign in with your password.');
        return;
      }
      const resetToken = new URLSearchParams(location.search).get('resetToken');
      if (resetToken) {
        if (/^[0-9a-f]{64}$/.test(resetToken)) renderResetPassword(resetToken);
        else {
          history.replaceState(null, '', routeURL({ resetToken: null }));
          renderAuth(false, 'This password reset link is invalid. Request a new link to continue.');
        }
        return;
      }
      const inviteToken = new URLSearchParams(location.search).get('inviteToken');
      if (inviteToken) {
        if (/^[0-9a-f]{64}$/.test(inviteToken)) renderInvitation(inviteToken);
        else {
          history.replaceState(null, '', routeURL({ inviteToken: null }));
          renderAuth(false, 'This invitation link is invalid or expired.');
        }
        return;
      }
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
  addEventListener('pagehide', clearAPIToken);
  initialize();
})();
