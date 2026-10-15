export function createHashesUI({ node, request, button, detailPanel, formError, notify, date, number }) {
  function pagination(link) {
    const pages = {};
    for (const part of (link || '').split(/,\s*(?=<)/)) {
      const attributes = Object.fromEntries([...part.matchAll(/;\s*([a-z]+)="([^"]*)"/g)].map(match => [match[1], match[2]]));
      if (!['previous', 'next'].includes(attributes.rel)) continue;
      if (!['true', 'false'].includes(attributes.results) || !/^0:(0|[1-9][0-9]*):[01]$/.test(attributes.cursor) || Number(attributes.cursor.split(':')[1]) > 100000000) continue;
      pages[attributes.rel] = { cursor: attributes.cursor, available: attributes.results === 'true' };
    }
    if (!pages.previous || !pages.next) throw new Error('Pagination information is unavailable. Refresh the active signatures to try again.');
    return pages;
  }

  function issue(currentIssue, organization, view, triage) {
    const scope = JSON.stringify([organization, currentIssue.id]);
    if (view.hashes?.scope !== scope) view.hashes = { scope, cursor: '', selected: new Set(), focus: false };
    const draft = view.hashes;
    const path = `/api/0/organizations/${encodeURIComponent(organization)}/issues/${encodeURIComponent(currentIssue.id)}/hashes/`;
    const status = node('p', { class: 'small', role: 'status' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const confirmation = node('div', { class: 'form-stack', hidden: true });
    const content = node('div');
    const reset = button('Reset selected signatures', confirmReset, 'small');
    const clear = button('Clear signature selection', () => { draft.selected.clear(); changed(); }, 'small');
    const refresh = button('Refresh active signatures', () => load(draft.cursor), 'small');
    const panel = detailPanel('Future grouping', node('div', {}, node('div', { class: 'panel-body form-stack' },
      node('p', { class: 'muted small' }, 'Active signatures decide where matching captures go. Reset a signature to start another issue on its next capture. Existing events stay here.'),
      status, node('div', { class: 'actions' }, reset, clear, refresh), confirmation, error), content));
    panel.setAttribute('aria-label', 'Future grouping');
    const heading = panel.querySelector('h2');
    heading.tabIndex = -1;
    const alive = () => triage.alive() && panel.isConnected;
    let sequence = 0, loading = false, rows = [], boxes = [], pageCheckbox;
    const disable = (control, value) => { control.disabled = value; triage.register(control); };

    function sync() {
      status.textContent = `${number(draft.selected.size)} selected · Select up to 1,000 active signatures. Selections stay across pages of this issue.`;
      disable(reset, loading || draft.selected.size === 0);
      disable(clear, draft.selected.size === 0);
      disable(refresh, loading);
      for (const { input, id } of boxes) {
        input.checked = draft.selected.has(id);
        disable(input, loading || (!input.checked && draft.selected.size >= 1000));
      }
      if (pageCheckbox) {
        const count = rows.filter(row => draft.selected.has(row.id)).length;
        pageCheckbox.checked = rows.length > 0 && count === rows.length;
        pageCheckbox.indeterminate = count > 0 && count < rows.length;
        disable(pageCheckbox, loading || rows.length === 0);
      }
    }

    function changed() {
      confirmation.hidden = true;
      confirmation.replaceChildren();
      error.textContent = '';
      sync();
    }

    function confirmReset() {
      if (!alive() || loading || draft.selected.size === 0) return;
      const confirm = triage.register(button('Confirm grouping reset', submit, 'primary small'));
      const cancel = triage.register(button('Cancel grouping reset', () => { confirmation.hidden = true; reset.focus(); }, 'small'));
      confirmation.replaceChildren(node('p', {}, `Reset ${number(draft.selected.size)} selected ${draft.selected.size === 1 ? 'signature' : 'signatures'}? Existing events stay here. The next matching capture creates another issue.`),
        node('p', { class: 'muted small' }, 'Historical Original groups separation is available below and moves recorded history.'), node('div', { class: 'actions' }, confirm, cancel));
      confirmation.hidden = false;
      cancel.focus();
    }

    async function submit() {
      if (!alive() || loading || draft.selected.size === 0) return;
      const query = new URLSearchParams();
      [...draft.selected].sort().forEach(id => query.append('id', id));
      await triage.mutate(() => request(`${path}?${query}`, { method: 'DELETE' }), async () => {
        draft.selected.clear();
        draft.cursor = '';
        draft.focus = true;
        changed();
        notify('Future grouping reset. Existing events remain in their recorded groups.');
        await triage.refresh();
      }, error, () => panel.isConnected);
    }

    function sample(event) {
      if (!event) return node('p', { class: 'muted small' }, 'No retained event sample.');
      return node('div', { class: 'form-stack' },
        node('p', { class: 'wrap-anywhere' }, event.title || event.message || 'Recorded event'),
        node('p', { class: 'small muted wrap-anywhere' }, 'Latest event ', node('code', {}, event.eventID || event.id),
          event.dateCreated ? [' · ', node('time', { datetime: event.dateCreated, title: event.dateCreated }, date(event.dateCreated))] : []));
    }

    function renderPage(pages, moveFocus) {
      pageCheckbox = node('input', { type: 'checkbox', class: 'size-4 shrink-0 cursor-pointer accent-primary', 'aria-label': 'Select all active signatures on this page', onchange: () => {
        const add = pageCheckbox.checked;
        if (add && draft.selected.size + rows.filter(row => !draft.selected.has(row.id)).length > 1000) {
          formError(error, new Error('Select at most 1,000 signatures. Clear some selections before adding this page.'));
          sync(); return;
        }
        for (const row of rows) { if (add) draft.selected.add(row.id); else draft.selected.delete(row.id); }
        changed();
      } });
      const list = node('ul', { class: 'comment-list', 'aria-label': 'Active signatures' }, rows.map(row => {
        const input = node('input', { type: 'checkbox', class: 'size-4 shrink-0 cursor-pointer accent-primary', 'aria-label': `Select signature ${row.id}`, onchange: () => {
          if (input.checked) draft.selected.add(row.id); else draft.selected.delete(row.id);
          changed();
        } });
        boxes.push({ input, id: row.id });
        return node('li', { class: 'comment' }, node('label', { class: 'actions' }, input, node('code', { class: 'mono small wrap-anywhere' }, row.id)), sample(row.latestEvent));
      }));
      const previous = button('Previous signatures', () => load(pages.previous.cursor, true), 'small');
      const next = button('Next signatures', () => load(pages.next.cursor, true), 'small');
      disable(previous, !pages.previous.available);
      disable(next, !pages.next.available);
      const offset = Number((draft.cursor || '0:0:0').split(':')[1]);
      content.replaceChildren(...(rows.length ? [node('div', { class: 'panel-body' }, node('label', { class: 'actions small' }, pageCheckbox, 'Select this page')), list]
        : [node('p', { class: 'panel-body muted', role: 'status' }, offset ? 'No active signatures on this page. Use Previous signatures or refresh from the first page.' : 'No active signatures. Recorded events stay in this issue; the next matching capture can start another issue.')]),
      node('div', { class: 'pagination' }, node('span', {}, `Active signatures · Page ${number(Math.floor(offset / 10) + 1)}`), node('div', { class: 'actions' }, previous, next)));
      if (offset && !rows.length) content.append(node('div', { class: 'panel-body actions' }, triage.register(button('First signature page', () => load('', true), 'small'))));
      sync();
      if (moveFocus) heading.focus();
    }

    async function load(cursor = '', moveFocus = false) {
      if (!alive()) return;
      const current = ++sequence;
      draft.cursor = cursor;
      loading = true;
      rows = []; boxes = []; pageCheckbox = undefined;
      confirmation.hidden = true;
      content.replaceChildren(node('p', { class: 'panel-body muted', role: 'status' }, 'Loading active signatures…'));
      sync();
      try {
        const query = new URLSearchParams();
        if (cursor) query.set('cursor', cursor);
        const result = await request(`${path}${query.size ? `?${query}` : ''}`, { withLinks: true });
        if (!alive() || current !== sequence) return;
        if (!Array.isArray(result.data) || result.data.some(row => !row || typeof row.id !== 'string' || !/^[0-9a-f]{32}$/.test(row.id))) throw new Error('The active signatures response is unavailable. Try again.');
        const pages = pagination(result.link);
        rows = result.data;
        loading = false;
        renderPage(pages, moveFocus || draft.focus);
        draft.focus = false;
      } catch (failure) {
        if (!alive() || current !== sequence) return;
        loading = false;
        const loadError = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
        content.replaceChildren(node('div', { class: 'panel-body form-stack' }, loadError, triage.register(button('Retry active signatures', () => load(cursor, moveFocus), 'small'))));
        formError(loadError, failure, view);
        sync();
      }
    }

    sync();
    return { panel, start: () => load(draft.cursor) };
  }

  return { issue };
}
