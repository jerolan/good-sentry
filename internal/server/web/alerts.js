export function createAlertsUI({ state, node, request, navigate, routeLink, button, field, selectField, pageHeader, empty, keyValues, formError, notify, date, number, text, reload }) {
  const configKeys = ['name', 'kind', 'enabled', 'quantity', 'windowMinutes', 'cooldownSeconds', 'environments', 'recipients'];
  const canManage = () => ['owner', 'admin'].includes(state.organization?.role);
  const config = rule => Object.fromEntries(configKeys.map(key => [key, rule[key]]));
  const kindName = kind => kind === 'uptime' ? 'Uptime transitions' : 'Error frequency';
  const status = value => node('span', { class: `badge delivery-state ${value === 'sent' ? 'resolved' : value === 'failed' ? 'error' : value === 'processing' ? 'warning' : ''}` }, value);
  const projectPath = project => `/api/v1/organizations/${encodeURIComponent(state.organization.slug)}/projects/${encodeURIComponent(project.id)}`;
  const rulePath = (project, id) => `${projectPath(project)}/alerts${id ? `/${encodeURIComponent(id)}` : ''}`;

  async function render(main, params, isCurrent) {
    if (!canManage()) {
      main.replaceChildren(pageHeader('Alerts', 'Only organization owners and admins can manage alert rules and inspect deliveries.'));
      return;
    }
    if (state.me.features?.alerts !== true) {
      main.replaceChildren(pageHeader('Alerts', 'Alerts are disabled on this server.'));
      return;
    }
    const project = state.projects.find(item => item.slug === params.get('alertProject')) || state.projects[0];
    if (!project) {
      main.replaceChildren(pageHeader('Alerts', 'Email and webhook alerts for your projects.'), node('section', { class: 'panel' }, empty('Create a project first', 'Alert rules belong to a project.', routeLink('Open projects', { view: 'projects' }, { class: 'button primary' }))));
      return;
    }
    const ruleID = params.get('rule');
    const deliveryID = params.get('delivery');
    const query = new URLSearchParams({ page: String(Math.max(1, parseInt(params.get('page'), 10) || 1)) });
    if (ruleID) query.set('rule', ruleID);
    const [rules, deliveries, rule, delivery] = await Promise.all([
      request(rulePath(project)), request(`${projectPath(project)}/alert-deliveries?${query}`),
      ruleID ? request(rulePath(project, ruleID)) : null,
      deliveryID ? request(`${projectPath(project)}/alert-deliveries/${encodeURIComponent(deliveryID)}`) : null,
    ]);
    if (!isCurrent()) return;
    document.title = 'Alerts · GoodSentry';
    main.replaceChildren(pageHeader('Alerts', 'Project rules, durable deliveries and recorded attempts.', node('div', { class: 'actions' }, button('Refresh alerts', reload), button('Create alert rule', () => openRule(project), 'primary'))));
    if (state.me.features?.alertDelivery !== true) main.append(node('section', { class: 'panel alert-section', 'aria-label': 'Alert delivery availability' }, node('div', { class: 'panel-body' }, node('h2', {}, 'Automatic delivery is not running'), node('p', { class: 'muted small' }, 'Rules and test requests can queue durable deliveries. Queued does not mean sent; inspect recorded delivery and attempt states below.'))));
    main.append(filters(project, rules.data || [], params));
    if (rule) main.append(ruleDetail(project, rule));
    main.append(ruleList(project, rules.data || []));
    if (delivery) main.append(deliveryDetail(delivery));
    main.append(deliveryList(project, deliveries, params));
  }

  function filters(project, rules, params) {
    const selected = selectField('Alert project', state.projects.map(item => [item.slug, item.name]), project.slug);
    const rule = selectField('Alert rule', [['', 'All rules'], ...rules.map(item => [item.id, item.name])], params.get('rule') || '');
    selected.input.addEventListener('change', () => {
      rule.input.disabled = selected.input.value !== project.slug;
      if (rule.input.disabled) rule.input.value = '';
    });
    const form = node('form', { class: 'filters alert-filters', 'aria-label': 'Filter alerts and deliveries' }, selected.wrap, rule.wrap, node('button', { class: 'button', type: 'submit' }, 'Apply alert filters'));
    form.addEventListener('submit', event => {
      event.preventDefault();
      navigate({ alertProject: selected.input.value, rule: rule.input.value || null, delivery: null, page: null });
    });
    return form;
  }

  function table(columns, rows) {
    return node('div', { class: 'table-scroll' }, node('table', {}, node('thead', {}, node('tr', {}, columns.map(label => node('th', { scope: 'col' }, label)))), node('tbody', {}, rows)));
  }

  function ruleList(project, rules) {
    const panel = node('section', { class: 'panel alert-section', 'aria-label': 'Project alert rules' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Alert rules')));
    if (!rules.length) panel.append(empty('No alert rules yet', 'Create a rule to notify project teammates, an email address or a webhook.', button('Create alert rule', () => openRule(project), 'primary')));
    else panel.append(table(['Rule', 'Kind', 'State', 'Condition', 'Environments', 'Recipients', 'Last triggered'], rules.map(rule => node('tr', {},
      node('td', { class: 'alert-name' }, routeLink(rule.name, { alertProject: project.slug, rule: rule.id, delivery: null, page: null }, { class: 'issue-link issue-title' })),
      node('td', {}, kindName(rule.kind)), node('td', {}, node('span', { class: 'badge' }, rule.enabled ? 'Enabled' : 'Paused')),
      node('td', {}, rule.kind === 'uptime' ? 'Confirmed down and recovery' : `${number(rule.quantity)} events in ${number(rule.windowMinutes)} minutes`),
      node('td', { class: 'alert-value' }, rule.environments.length ? rule.environments.join(', ') : 'All environments'),
      node('td', {}, number(rule.recipients.length)), node('td', { class: 'nowrap' }, rule.lastTriggered ? date(rule.lastTriggered, true) : 'Never triggered')))));
    return panel;
  }

  function ruleDetail(project, rule) {
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const confirmation = node('div');
    const actions = node('div', { class: 'actions' }, button('Edit alert rule', () => openRule(project, rule)));
    const pause = button(rule.enabled ? 'Pause alert rule' : 'Enable alert rule', async () => {
      pause.disabled = true;
      try {
        await request(rulePath(project, rule.id), { method: 'PUT', body: { ...config(rule), enabled: !rule.enabled } });
        notify(rule.enabled ? 'Alert rule paused.' : 'Alert rule enabled.');
        reload();
      } catch (failure) { formError(error, failure); pause.disabled = false; }
    });
    const test = button('Test alert rule', async () => {
      test.disabled = true;
      try {
        const result = await request(`${rulePath(project, rule.id)}/test`, { method: 'POST' });
        notify(`${number(result.queued)} test deliveries queued. Refresh to inspect worker results.`);
        reload();
      } catch (failure) { formError(error, failure); test.disabled = false; }
    });
    actions.append(pause, test, button('Delete alert rule', () => {
      const remove = button('Delete rule permanently', async () => {
        remove.disabled = true;
        try {
          await request(rulePath(project, rule.id), { method: 'DELETE' });
          navigate({ alertProject: project.slug, rule: null, delivery: null, page: null });
          notify('Alert rule deleted. Completed delivery history is retained.');
        } catch (failure) { formError(error, failure); remove.disabled = false; }
      }, 'danger');
      confirmation.replaceChildren(node('div', { class: 'monitor-confirmation' }, node('p', {}, `Delete ${rule.name}? Pending deliveries will be canceled. Completed history remains.`), node('div', { class: 'actions' }, button('Cancel rule deletion', () => confirmation.replaceChildren()), remove)));
      remove.focus();
    }));
    return node('section', { class: 'panel alert-section', 'aria-label': 'Selected alert rule' },
      node('div', { class: 'panel-title alert-panel-heading' }, node('div', {}, node('h2', { class: 'alert-value' }, rule.name), node('p', { class: 'muted small' }, `${kindName(rule.kind)} · ${rule.enabled ? 'Enabled' : 'Paused'}`)), actions),
      node('div', { class: 'panel-body form-stack' }, error, confirmation, keyValues([
        ['Condition', rule.kind === 'uptime' ? 'Confirmed down and recovery transitions' : `${number(rule.quantity)} events in ${number(rule.windowMinutes)} minutes`],
        ['Cooldown', rule.kind === 'uptime' ? 'Does not suppress uptime transitions' : `${number(rule.cooldownSeconds)} seconds`],
        ['Environments', rule.environments.length ? rule.environments.join(', ') : 'All environments'], ['Updated', date(rule.updatedAt)],
        ...rule.recipients.map((recipient, index) => [`Recipient ${index + 1}`, recipient.type === 'team_email' ? 'Project team email' : `${recipient.type}: ${recipient.destination}`]),
      ])));
  }

  function deliveryList(project, result, params) {
    const deliveries = result.data || [];
    const panel = node('section', { class: 'panel alert-section', 'aria-label': 'Alert deliveries' }, node('div', { class: 'panel-title' }, node('h2', {}, 'Deliveries')));
    if (!deliveries.length) panel.append(empty('No deliveries in this view', 'Future triggers and queued tests will appear here.'));
    else panel.append(table(['Delivery', 'Rule', 'Recipient', 'State', 'Attempts', 'Last reason', 'Next attempt', 'Created'], deliveries.map(delivery => node('tr', {},
      node('td', { class: 'alert-name' }, routeLink(delivery.id, { alertProject: project.slug, delivery: delivery.id }, { class: 'issue-link mono' })),
      node('td', { class: 'alert-value' }, delivery.ruleName), node('td', { class: 'alert-destination' }, node('span', { class: 'badge' }, delivery.recipient.type), node('p', {}, delivery.recipient.destination)),
      node('td', {}, status(delivery.status)), node('td', {}, number(delivery.attempts)), node('td', { class: 'alert-value' }, delivery.lastReason || 'No result yet'),
      node('td', { class: 'nowrap' }, ['pending', 'processing'].includes(delivery.status) ? date(delivery.nextAttempt, true) : 'No retry scheduled'),
      node('td', { class: 'nowrap' }, date(delivery.createdAt, true))))));
    const page = Math.max(1, parseInt(params.get('page'), 10) || 1);
    const previous = button('Previous deliveries', () => navigate({ page: page - 1, delivery: null }));
    const next = button('Next deliveries', () => navigate({ page: page + 1, delivery: null }));
    previous.disabled = page <= 1;
    next.disabled = !result.hasMore;
    panel.append(node('div', { class: 'pagination' }, node('span', {}, `${number(deliveries.length)} deliveries on this page · Page ${page}`), node('div', { class: 'actions' }, previous, next)));
    return panel;
  }

  function deliveryDetail(result) {
    const { delivery } = result;
    const attempts = result.attempts || [];
    const payload = delivery.payload;
    let resource;
    try {
      const url = new URL(payload.url);
      if (['http:', 'https:'].includes(url.protocol)) resource = node('a', { href: url.href, class: 'alert-resource' }, payload.url);
    } catch (_) { /* An invalid resource remains visible as text. */ }
    const panel = node('section', { class: 'panel alert-section', 'aria-label': 'Selected delivery' },
      node('div', { class: 'panel-title alert-panel-heading' }, node('h2', { class: 'alert-value' }, `Delivery ${delivery.id}`), routeLink('Close delivery detail', { delivery: null }, { class: 'button small' })),
      node('div', { class: 'panel-body form-stack' }, status(delivery.status), keyValues([
        ['Rule', delivery.ruleName], ['Recipient', `${delivery.recipient.type}: ${delivery.recipient.destination}`], ['Attempts', delivery.attempts],
        ['Last reason', delivery.lastReason || 'No result yet'], ['Last remote status', delivery.lastStatusCode ?? 'Not applicable'],
        ['Created', date(delivery.createdAt)], ['Sent', delivery.sentAt ? date(delivery.sentAt) : 'Not sent'],
        ['Next attempt', ['pending', 'processing'].includes(delivery.status) ? date(delivery.nextAttempt) : 'No retry scheduled'],
      ]), node('div', { class: 'alert-value' }, node('h3', {}, 'Related resource'), resource || node('span', {}, text(payload.url))),
      node('details', {}, node('summary', {}, 'Compact delivery payload'), node('pre', { class: 'event-json' }, JSON.stringify(payload, null, 2)))));
    const history = node('section', { 'aria-label': 'Delivery attempts' }, node('div', { class: 'panel-title' }, node('h3', {}, 'Attempt history')));
    if (!attempts.length) history.append(node('p', { class: 'panel-body muted' }, 'No worker attempt has started.'));
    else history.append(table(['Attempt', 'Started', 'Completed', 'State', 'Reason', 'Remote status', 'Elapsed'], attempts.map(attempt => node('tr', {},
      node('td', {}, number(attempt.number)), node('td', { class: 'nowrap' }, date(attempt.startedAt, true)),
      node('td', { class: 'nowrap' }, attempt.completedAt ? date(attempt.completedAt, true) : 'In progress'), node('td', {}, attempt.status),
      node('td', { class: 'alert-value' }, attempt.reason || 'No result yet'), node('td', {}, attempt.statusCode ?? 'Not applicable'),
      node('td', { class: 'nowrap' }, attempt.elapsedMs === null || attempt.elapsedMs === undefined ? 'Not available' : `${number(attempt.elapsedMs)} ms`)))));
    panel.append(history);
    return panel;
  }

  function openRule(project, rule) {
    const initial = rule || { name: '', kind: 'error_frequency', enabled: true, quantity: 1, windowMinutes: 5, cooldownSeconds: 900, environments: [], recipients: [{ type: 'team_email', destination: '' }] };
    const dialog = node('dialog', { class: 'alert-dialog', 'aria-labelledby': 'alert-dialog-title' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const name = field('Rule name', 'text', { required: true, maxlength: 400, value: initial.name });
    const kind = selectField('Alert kind', [['error_frequency', 'Error frequency'], ['uptime', 'Uptime transitions']], initial.kind, rule ? { disabled: true } : {});
    const quantity = field('Event threshold', 'number', { required: true, min: 1, max: 1000000, step: 1, value: initial.quantity });
    const window = field('Window (minutes)', 'number', { required: true, min: 1, max: 10080, step: 1, value: initial.windowMinutes });
    const cooldown = field('Cooldown (seconds)', 'number', { required: true, min: 0, max: 604800, step: 1, value: initial.cooldownSeconds });
    const environments = field('Environments', 'textarea', { rows: 2, maxlength: 25800 });
    environments.input.value = initial.environments.join('\n');
    environments.wrap.append(node('p', { class: 'help' }, 'One environment per line. Leave empty to include all environments.'));
    const note = node('p', { class: 'muted small' });
    const enabled = node('input', { type: 'checkbox', checked: initial.enabled });
    const recipientRows = node('div', { class: 'alert-recipients' });
    const recipients = [];
    let recipientNumber = 0;
    const add = button('Add recipient', () => addRecipient({ type: 'email', destination: '' }));
    function updateRecipients() {
      add.disabled = recipients.length >= 20;
      for (const item of recipients) item.remove.disabled = recipients.length === 1;
    }
    function addRecipient(recipient) {
      const index = ++recipientNumber;
      const type = selectField(`Recipient type ${index}`, [['team_email', 'Project team email'], ['email', 'Email address'], ['webhook', 'Generic webhook (JSON)'], ['slack', 'Slack incoming webhook'], ['discord', 'Discord webhook'], ['teams', 'Microsoft Teams webhook'], ['googlechat', 'Google Chat webhook'], ['feishu', 'Feishu / Lark bot'], ['ntfy', 'ntfy topic URL'], ['zulip', 'Zulip Slack-compatible webhook'], ['mattermost', 'Mattermost incoming webhook']], recipient.type);
      const destination = field(`Destination ${index}`, 'text', { value: recipient.destination || '', maxlength: 4000, autocomplete: 'off' });
      const providerHelp = node('p', { class: 'small' });
      destination.wrap.append(providerHelp);
      const row = node('div', { class: 'alert-recipient' });
      const item = { type, destination, row };
      item.remove = button(`Remove recipient ${index}`, () => { row.remove(); recipients.splice(recipients.indexOf(item), 1); updateRecipients(); }, 'small');
      function changeType() {
        const team = type.input.value === 'team_email';
        destination.wrap.hidden = team;
        destination.input.disabled = team;
        destination.input.required = !team;
        const providers = {
          slack: ['Slack webhook URL', 'Use the channel\'s incoming webhook URL. Keep it secret.'],
          discord: ['Discord webhook URL', 'Use a Discord channel webhook URL. Keep it secret.'],
          teams: ['Microsoft Teams webhook URL', 'Use a Teams workflow or incoming webhook URL. Keep it secret.'],
          googlechat: ['Google Chat webhook URL', 'Use the space\'s Google Chat webhook URL. Keep it secret.'],
          feishu: ['Feishu / Lark bot URL', 'Use a custom bot webhook URL without request signing. Keep it secret.'],
          ntfy: ['ntfy topic URL', 'Use the topic URL, including its secret topic name.'],
          mattermost: ['Mattermost webhook URL', 'Use the incoming webhook URL from Mattermost. Keep it secret.'],
          zulip: ['Zulip webhook URL', 'Use Zulip\'s Slack-compatible incoming webhook URL. Keep it secret.'],
        };
        const provider = providers[type.input.value];
        providerHelp.hidden = !provider;
        providerHelp.textContent = provider ? provider[1] : '';
        const label = type.input.value === 'email' ? 'Email address' : provider ? provider[0] : 'Webhook URL';
        destination.wrap.querySelector('label').textContent = `${label} ${index}`;
        destination.input.placeholder = provider ? 'Webhook URL (contains a secret)' : 'https://example.com/alert';
      }
      type.input.addEventListener('change', changeType);
      changeType();
      row.append(type.wrap, destination.wrap, item.remove);
      recipients.push(item); recipientRows.append(row); updateRecipients();
    }
    initial.recipients.forEach(addRecipient);
    function changeKind() {
      const uptime = kind.input.value === 'uptime';
      for (const input of [quantity, window, cooldown]) { input.wrap.hidden = uptime; input.input.disabled = uptime; }
      note.textContent = uptime ? 'Notify on confirmed down and recovery transitions for project monitors. Frequency cooldown does not suppress uptime transitions.' : 'Notify when future project error events cross the threshold in the rolling window. Cooldown suppresses repeated crossings; creating a rule does not send historical events.';
    }
    kind.input.addEventListener('change', changeKind); changeKind();
    const close = () => dialog.close();
    const submit = node('button', { type: 'submit', class: 'button primary' }, rule ? 'Save alert rule' : 'Create alert rule');
    const form = node('form', { class: 'form-stack', 'aria-label': rule ? 'Edit alert rule configuration' : 'Create alert rule configuration' }, error,
      node('div', { class: 'alert-fields' }, name.wrap, kind.wrap, quantity.wrap, window.wrap, cooldown.wrap, environments.wrap), note,
      node('label', { class: 'checkbox-field' }, enabled, 'Enabled'), node('fieldset', { class: 'alert-recipient-set' }, node('legend', {}, 'Recipients'), recipientRows, add),
      node('p', { class: 'muted small' }, 'Project team email resolves current teammates. Provider URLs create native channel messages; generic webhooks receive GoodSentry JSON. Test deliveries use the same worker as normal alerts.'),
      node('div', { class: 'dialog-actions' }, button('Cancel', close), submit));
    form.addEventListener('submit', async event => {
      event.preventDefault(); error.textContent = '';
      const values = environments.input.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean);
      if (Array.from(name.input.value.trim()).length > 200 || values.length > 100 || values.some(value => Array.from(value).length > 128)) {
        formError(error, new Error('Use a name of at most 200 characters and at most 100 environments of 128 characters each.'));
        return;
      }
      const payload = { name: name.input.value.trim(), kind: kind.input.value, enabled: enabled.checked, quantity: Number(quantity.input.value), windowMinutes: Number(window.input.value), cooldownSeconds: Number(cooldown.input.value), environments: values,
        recipients: recipients.map(item => ({ type: item.type.input.value, destination: item.type.input.value === 'team_email' ? '' : item.destination.input.value.trim() })) };
      submit.disabled = true; submit.textContent = 'Saving…';
      try {
        const saved = await request(rulePath(project, rule?.id), { method: rule ? 'PUT' : 'POST', body: payload });
        dialog.close(); navigate({ view: 'alerts', alertProject: project.slug, rule: saved.id, delivery: null, page: null });
        notify(rule ? 'Alert rule updated.' : 'Alert rule created.');
      } catch (failure) { formError(error, failure); }
      finally { submit.disabled = false; submit.textContent = rule ? 'Save alert rule' : 'Create alert rule'; }
    });
    dialog.append(node('header', {}, node('h2', { id: 'alert-dialog-title' }, rule ? 'Edit alert rule' : 'Create alert rule'), button('Close', close, 'small')), form);
    dialog.addEventListener('close', () => dialog.remove()); document.body.append(dialog); dialog.showModal(); name.input.focus();
  }

  return { render };
}
