export function createFeedbackUI({ node, request, button, detailPanel, formError, date, number }) {
  function issue({ issue, organization, initialFeedback, total, view, triage, submissionContent, associatedContent, associatedIssueLink }) {
    const scope = JSON.stringify([organization, issue.id]);
    if (view.feedback?.scope !== scope) view.feedback = { scope, page: 1, selectedEvent: null, mode: null };
    const state = view.feedback;
    const path = `/api/v1/issues/${encodeURIComponent(issue.id)}/feedback`;
    const status = node('p', { class: 'small', role: 'status', 'aria-live': 'polite' });
    const rows = node('div', { class: 'feedback-body' });
    const pages = node('div');
    const inspectionArea = node('div', { class: 'feedback-body' });
    const panel = detailPanel('Feedback', node('div', {}, node('div', { class: 'panel-body form-stack feedback-body' }, status, rows, pages, inspectionArea)));
    panel.setAttribute('aria-label', 'Feedback submissions');
    const panelHeading = panel.querySelector('h2');
    panelHeading.tabIndex = -1;

    let pageSequence = 0;
    let inspectionSequence = 0;
    let loading = false;
    let items = state.page === 1 && Array.isArray(initialFeedback) ? initialFeedback : [];
    let count = Number(total) || 0;
    let hasMore = count > state.page * 25;
    let originButton = null;

    const alive = () => triage.alive() && panel.isConnected;
    const currentPage = () => state.page;
    const register = control => triage.register(control);
    const disable = (control, value) => { control.disabled = value; register(control); };

    function clearInspection() {
      inspectionSequence++;
      state.selectedEvent = null;
      state.mode = null;
      originButton = null;
      inspectionArea.replaceChildren();
    }

    function associationLabel(value) {
      return ({ absent: 'No associated error', pending: 'Waiting for associated error', linked: 'Associated error linked', unavailable: 'Associated error unavailable' })[value] || 'Associated error unavailable';
    }

    function inspectionIsCurrent(sequence, eventID, mode) {
      return alive() && sequence === inspectionSequence && state.selectedEvent === eventID && state.mode === mode;
    }

    function closeInspection() {
      if (!alive()) return;
      const target = originButton;
      clearInspection();
      if (target?.isConnected) target.focus(); else panelHeading.focus();
    }

    function showInspectionError(target, failure, retry) {
      target.replaceChildren(
        node('p', { class: 'form-error', role: 'alert' }, failure.status === 404 ? 'This feedback detail is no longer available. Refresh feedback to check for changes.' : failure.message || 'Feedback details could not be loaded.'),
        node('div', { class: 'actions' }, register(button('Retry feedback detail', retry, 'small'))),
      );
    }

    async function inspect(item, mode, control, moveFocus = true) {
      if (!alive() || !item?.eventID || (mode === 'associated' && item.associationState !== 'linked')) return;
      inspectionSequence++;
      const sequence = inspectionSequence;
      state.selectedEvent = item.eventID;
      state.mode = mode;
      originButton = control;
      const content = node('div', { class: 'feedback-body' }, node('p', { class: 'panel-body muted', role: 'status' }, 'Loading feedback details…'));
      const close = register(button('Close feedback detail', closeInspection, 'small'));
      const title = mode === 'submission' ? 'Feedback submission' : 'Associated error';
      const detail = detailPanel(title, node('div', {}, node('div', { class: 'panel-body actions feedback-body' }, node('code', { class: 'wrap-anywhere' }, item.eventID), close), content));
      detail.setAttribute('aria-label', `${title} ${item.eventID}`);
      const oldHeading = detail.querySelector('h2');
      const heading = node('h3', {}, oldHeading.textContent);
      oldHeading.replaceWith(heading);
      heading.tabIndex = -1;
      inspectionArea.replaceChildren(detail);
      const isCurrent = () => inspectionIsCurrent(sequence, item.eventID, mode);
      try {
        if (mode === 'submission') {
          const raw = await request(`/api/0/organizations/${encodeURIComponent(organization)}/issues/${encodeURIComponent(issue.id)}/events/${encodeURIComponent(item.eventID)}/json/`);
          if (!isCurrent()) return;
          if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('Feedback event details are unavailable. Try again.');
          const rendered = submissionContent(raw, item.eventID, isCurrent);
          content.replaceChildren(rendered.content);
          rendered.start?.();
        } else {
          const result = await request(`${path}/${encodeURIComponent(item.eventID)}/associated-event`);
          if (!isCurrent()) return;
          if (!result || result.eventID !== item.associatedEventID || typeof result.issueID !== 'string' || !result.issueID || !result.event || typeof result.event !== 'object' || Array.isArray(result.event)) {
            throw new Error('The associated error response is unavailable. Refresh feedback to check its current association.');
          }
          const rendered = associatedContent(result.event, isCurrent);
          const issueLink = associatedIssueLink(result.issueID);
          const link = issueLink instanceof Node ? issueLink : node('span', { class: 'wrap-anywhere' }, issueLink);
          content.replaceChildren(node('div', { class: 'panel-body form-stack feedback-body' },
            node('p', { class: 'small' }, 'Current issue: ', link), rendered.content));
          rendered.start?.();
        }
        if (moveFocus && isCurrent()) heading.focus();
      } catch (failure) {
        if (!isCurrent()) return;
        showInspectionError(content, failure, () => inspect(item, mode, originButton));
        if (moveFocus) heading.focus();
      }
    }

    function render() {
      const refresh = register(button('Refresh feedback', () => load(currentPage(), true, true), 'small'));
      disable(refresh, loading);
      const previous = register(button('Previous feedback', () => load(state.page - 1, true), 'small'));
      const next = register(button('Next feedback', () => load(state.page + 1, true), 'small'));
      disable(previous, loading || state.page <= 1);
      disable(next, loading || !hasMore);
      const list = node('ul', { class: 'comment-list', 'aria-label': 'Feedback submissions' }, items.map(item => {
        const inspectSubmission = register(button('Inspect submission', event => inspect(item, 'submission', event.currentTarget), 'small'));
        inspectSubmission.setAttribute('aria-label', `Inspect feedback submission ${item.eventID}`);
        disable(inspectSubmission, loading);
        const inspectAssociated = register(button('Inspect associated error', event => inspect(item, 'associated', event.currentTarget), 'small'));
        inspectAssociated.setAttribute('aria-label', `Inspect associated error for feedback ${item.eventID}`);
        disable(inspectAssociated, loading || item.associationState !== 'linked');
        return node('li', { class: 'comment feedback-body' },
          node('div', { class: 'comment-top feedback-body' }, node('div', { class: 'min-w-0 feedback-body' },
            node('strong', {}, 'Feedback submission'),
            node('p', { class: 'small muted' }, 'Received ', node('time', { datetime: item.dateCreated, title: item.dateCreated }, date(item.dateCreated))),
            node('p', { class: 'small' }, associationLabel(item.associationState))),
          node('div', { class: 'actions' }, inspectSubmission, inspectAssociated)),
          item.name ? node('p', { class: 'small wrap-anywhere' }, 'Name: ', item.name) : null,
          item.email ? node('p', { class: 'small wrap-anywhere' }, 'Email: ', item.email) : null,
          item.url ? node('p', { class: 'small wrap-anywhere' }, 'Source: ', item.url) : null,
          node('p', { class: 'whitespace-pre-wrap wrap-anywhere feedback-body' }, item.message || ''),
          item.associationState === 'pending' ? node('p', { class: 'small muted' }, 'The referenced error has not been observed yet.') : null,
          item.associationState === 'unavailable' ? node('p', { class: 'small muted' }, 'The referenced event is not available as an associated error.') : null);
      }));
      rows.replaceChildren(items.length ? list : node('p', { class: 'panel-body muted', role: 'status' }, loading ? 'Loading feedback…' : count ? 'No feedback submissions on this page.' : 'No feedback submissions for this issue.'));
      pages.replaceChildren(node('div', { class: 'pagination', 'aria-label': 'Feedback pages' },
        node('span', { 'aria-live': 'polite' }, `Page ${number(state.page)} · ${number(count)} feedback submissions`),
        node('div', { class: 'actions' }, refresh, previous, next)));
      status.textContent = loading ? `Loading feedback · Page ${number(state.page)}` : `${number(count)} feedback submissions · Page ${number(state.page)}`;
    }

    async function load(page, moveFocus = false, refresh = false) {
      if (!alive() || page < 1 || loading) return;
      const sequence = ++pageSequence;
      const priorEvent = refresh && page === state.page ? state.selectedEvent : null;
      const priorMode = priorEvent ? state.mode : null;
      const priorOrigin = originButton;
      loading = true;
      if (page !== state.page) clearInspection();
      state.page = page;
      if (page !== 1) items = [];
      if (page === 1 && !refresh && Array.isArray(initialFeedback)) items = initialFeedback;
      if (moveFocus) panelHeading.focus();
      render();
      try {
        const result = await request(`${path}?page=${page}`);
        if (!alive() || sequence !== pageSequence) return;
        if (!Array.isArray(result?.data) || typeof result.total !== 'number' || !Number.isInteger(result.total) || result.total < 0 || Number(result.page) !== page || result.pageSize !== 25 || typeof result.hasMore !== 'boolean') throw new Error('The feedback response is unavailable. Try again.');
        items = result.data;
        count = Number(result.total);
        hasMore = result.hasMore;
        loading = false;
        state.page = Number(result.page);
        render();
        if (priorEvent) {
          const stillPresent = items.find(item => item.eventID === priorEvent);
          if (stillPresent && (priorMode !== 'associated' || stillPresent.associationState === 'linked')) inspect(stillPresent, priorMode, priorOrigin, false);
          else clearInspection();
        }
      } catch (failure) {
        if (!alive() || sequence !== pageSequence) return;
        loading = false;
        render();
        rows.replaceChildren(node('div', { class: 'panel-body form-stack' },
          node('p', { class: 'form-error', role: 'alert' }, failure.message || 'Feedback could not be loaded.'),
          register(button('Retry feedback', () => load(page, true, true), 'small'))));
        status.textContent = `Could not load feedback · Page ${number(page)}`;
        if (priorEvent) clearInspection();
      }
    }

    render();
    return { panel, start: () => {
      if (state.page !== 1) return load(state.page, false, true);
      if (!state.selectedEvent) return;
      const selected = items.find(item => item.eventID === state.selectedEvent);
      if (selected && (state.mode !== 'associated' || selected.associationState === 'linked')) return inspect(selected, state.mode, null, false);
      clearInspection();
    } };
  }

  return { issue };
}
