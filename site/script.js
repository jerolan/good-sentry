(() => {
  const status = document.getElementById('copy-status');
  let timer;
  async function copy(text) {
    if (navigator.clipboard && window.isSecureContext) {
      try { await navigator.clipboard.writeText(text); return true; } catch (_) { /* Use the selection fallback below. */ }
    }
    const input = document.createElement('textarea');
    input.value = text;
    input.readOnly = true;
    input.setAttribute('aria-label', 'Commands to copy');
    input.style.cssText = 'position:fixed;left:-9999px;top:0;opacity:0';
    document.body.append(input);
    input.select();
    let copied = false;
    try { copied = document.execCommand('copy'); } catch (_) { /* Report the manual option. */ }
    input.remove();
    return copied;
  }
  document.querySelectorAll('[data-copy]').forEach(button => {
    button.addEventListener('click', async () => {
      const source = document.getElementById(button.dataset.copy);
      if (!source) return;
      const copied = await copy(source.textContent.trim());
      button.focus({ preventScroll: true });
      clearTimeout(timer);
      status.textContent = copied ? 'Quickstart commands copied.' : 'Copy unavailable. Select the terminal commands and copy them manually.';
      const label = button.querySelector('[data-copy-label]');
      label.textContent = copied ? 'Copied' : 'Copy commands';
      timer = setTimeout(() => { status.textContent = ''; label.textContent = 'Copy commands'; }, 4000);
    });
  });
})();
