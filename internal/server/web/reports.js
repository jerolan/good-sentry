export function createReportsUI({ node, request, button, detailPanel, formError, date, number }) {
  function issue({ issue, organization, initialReports, total, view, triage, eventContent }) {
    const scope = JSON.stringify([organization, issue.id]);
    if (view.reports?.scope !== scope) view.reports = { scope, page: 1, selectedEvent: null };
    const state = view.reports;
    const path = `/api/v1/issues/${encodeURIComponent(issue.id)}/user-reports`;
    const status = node('p', { class: 'small', role: 'status', 'aria-live': 'polite' });
    const error = node('div', { class: 'form-error', role: 'alert', tabindex: '-1' });
    const rows = node('div');
    const eventArea = node('div');
    const pages = node('div');
    const panel = detailPanel('User reports', node('div', {}, node('div', { class: 'panel-body form-stack user-reports-body' }, status,
      node('div', { class: 'actions', 'aria-label': 'User report actions' }), error, rows, pages, eventArea)));
    panel.setAttribute('aria-label', 'User reports');
    const panelHeading = panel.querySelector('h2');
    panelHeading.tabIndex = -1;
    const alive = () => triage.alive() && panel.isConnected;
    let pageSequence = 0, eventSequence = 0, pageLoading = false;
    let reports = state.page === 1 && Array.isArray(initialReports) ? initialReports : [];
    let count = Number(total) || 0;
    let hasMore = count > state.page * 25;
    const eventButtons = new Map();
    let originButton = null;
    const actions = panel.querySelector('.actions');
    const disable = (control, value) => { control.disabled = value; triage.register(control); };
    const pageCount = () => Math.max(1, Math.ceil(count / 25));
    const pageLabel = () => `Page ${number(state.page)}${state.page <= pageCount() ? ` of ${number(pageCount())}` : ''}`;

    function clearEvent() {
      eventSequence++;
      state.selectedEvent = null;
      originButton = null;
      eventArea.replaceChildren();
    }

    function showEventError(target, failure, retry) {
      target.replaceChildren(node('p', { class: 'form-error', role: 'alert' }, failure.status === 404 ? 'This reported event is no longer available. Refresh user reports to check for changes.' : failure.message || 'The reported event could not be loaded.'),
        node('div', { class: 'actions' }, triage.register(button('Retry reported event', retry, 'small'))));
    }

    function closeEvent() {
      if (!alive()) return;
      const target = originButton;
      clearEvent();
      if (target?.isConnected) target.focus(); else panelHeading.focus();
    }

    async function openEvent(report, control, moveFocus = true) {
      if (!alive() || !report?.eventID) return;
      clearEvent();
      const selected = report.eventID;
      state.selectedEvent = selected;
      originButton = control;
      const current = ++eventSequence;
      const content = node('div', {}, node('p', { class: 'panel-body muted', role: 'status' }, 'Loading reported event…'));
      const eventPanel = detailPanel('Reported event', node('div', {}, node('div', { class: 'panel-body actions' },
        node('code', { class: 'wrap-anywhere' }, selected), triage.register(button('Close reported event', closeEvent, 'small'))), content));
      eventPanel.setAttribute('aria-label', `Reported event ${selected}`);
      const originalHeading = eventPanel.querySelector('h2');
      const eventHeading = node('h3', {}, originalHeading.textContent);
      originalHeading.replaceWith(eventHeading);
      eventHeading.tabIndex = -1;
      eventArea.replaceChildren(eventPanel);
      const currentSelection = () => alive() && current === eventSequence && state.selectedEvent === selected;
      try {
        const raw = await request(`/api/0/organizations/${encodeURIComponent(organization)}/issues/${encodeURIComponent(issue.id)}/events/${encodeURIComponent(selected)}/json/`);
        if (!currentSelection()) return;
        if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('Reported event details are unavailable. Try again.');
        const rendered = eventContent(raw, selected, currentSelection);
        content.replaceChildren(rendered.content);
        rendered.start?.();
        if (moveFocus) eventHeading.focus();
      } catch (failure) {
        if (!currentSelection()) return;
        showEventError(content, failure, () => openEvent(report, originButton));
        if (moveFocus) eventHeading.focus();
      }
    }

    function render() {
      actions.replaceChildren();
      error.textContent = '';
      eventButtons.clear();
      const refresh = button('Refresh user reports', () => load(state.page, false, true), 'small');
      disable(refresh, pageLoading);
      actions.append(refresh);
      const previous = button('Previous user reports', () => load(state.page - 1, true), 'small');
      const next = button('Next user reports', () => load(state.page + 1, true), 'small');
      disable(previous, pageLoading || state.page <= 1);
      disable(next, pageLoading || !hasMore || state.page >= pageCount());
      const shown = node('ul', { class: 'comment-list', 'aria-label': 'User reports' }, reports.map(report => {
        const name = report.name || report.email || 'Anonymous reporter';
        const viewEvent = button('View event', event => openEvent(report, event.currentTarget), 'small');
        viewEvent.setAttribute('aria-label', `View reported event ${report.eventID}`);
        viewEvent.title = report.eventID;
        eventButtons.set(report.eventID, viewEvent);
        disable(viewEvent, pageLoading);
        return node('li', { class: 'comment' }, node('div', { class: 'comment-top' }, node('div', { class: 'min-w-0 wrap-anywhere' },
          node('strong', {}, name), report.name && report.email ? node('p', { class: 'small muted wrap-anywhere' }, report.email) : null,
          node('p', { class: 'small muted' }, 'Received ', node('time', { datetime: report.dateCreated, title: report.dateCreated }, date(report.dateCreated)))), viewEvent),
        node('p', { class: 'small wrap-anywhere' }, report.comments || ''), report.eventID ? null : node('p', { class: 'small muted' }, 'No linked event is available.'));
      }));
      rows.replaceChildren(...(reports.length ? [shown] : [node('p', { class: 'panel-body muted', role: 'status' }, pageLoading ? 'Loading user reports…' : count ? 'No user reports on this page.' : 'No user reports for this issue.')]));
      const pager = node('div', { class: 'pagination', 'aria-label': 'User report pages' },
        node('span', { 'aria-live': 'polite' }, `${pageLabel()} · ${number(count)} user reports`),
        node('div', { class: 'actions' }, previous, next));
      pages.replaceChildren(pager);
      status.textContent = pageLoading ? `Loading user reports · ${pageLabel()}` : `${number(count)} user reports · ${pageLabel()}`;
    }

    async function load(page, moveFocus, focusPanel = moveFocus) {
      if (!alive() || page < 1 || pageLoading) return;
      const current = ++pageSequence;
      pageLoading = true;
      if (page !== state.page) { reports = []; hasMore = count > page * 25; }
      state.page = page;
      if (moveFocus) clearEvent();
      status.textContent = `Loading user reports · Page ${number(page)}`;
      render();
      if (focusPanel) panelHeading.focus();
      try {
        const result = await request(`${path}?page=${page}`);
        if (!alive() || current !== pageSequence) return;
        if (!Array.isArray(result?.data) || !Number.isInteger(Number(result.total)) || Number(result.page) !== page || result.pageSize !== 25 || typeof result.hasMore !== 'boolean') throw new Error('The user reports response is unavailable. Try again.');
        reports = result.data;
        count = Number(result.total);
        hasMore = result.hasMore;
        pageLoading = false;
        state.page = Number(result.page);
        render();
        if (state.selectedEvent && !reports.some(report => report.eventID === state.selectedEvent)) clearEvent();
        if (state.selectedEvent) {
          originButton = eventButtons.get(state.selectedEvent);
          if (!eventArea.childElementCount) openEvent(reports.find(report => report.eventID === state.selectedEvent), originButton, false);
        }
      } catch (failure) {
        if (!alive() || current !== pageSequence) return;
        pageLoading = false;
        render();
        rows.replaceChildren(node('div', { class: 'panel-body form-stack' }, node('p', { class: 'form-error', role: 'alert' }, failure.message),
          triage.register(button('Retry user reports', () => load(page, moveFocus, true), 'small'))));
        status.textContent = `Could not load user reports · Page ${number(page)}`;
      }
    }

    render();
    return { panel, start: () => load(state.page, false) };
  }

  return { issue };
}
