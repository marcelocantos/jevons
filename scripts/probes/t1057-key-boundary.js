// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
// Paste into Safari Web Inspector console for the already-running Flutter
// cockpit, ONLY in an owner-approved Pippa diagnostic window. Does not
// preventDefault, change focus/selection, read draft contents, or send data.
(() => {
  window.__jevonsT1057Probe?.stop();
  const rows = [];
  const timers = new Set();
  const box = () => {
    const el = document.activeElement;
    return el instanceof HTMLTextAreaElement && el.matches('textarea[data-composer]') ? el : null;
  };
  const offsets = (el) => el ? [el.selectionStart, el.selectionEnd] : null;
  const record = (row) => {
    rows.push(row);
    if (rows.length > 40) rows.shift();
    console.log('T1057 key boundary', row);
  };
  const onKeyDown = (e) => {
    const el = box();
    if (!el || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Enter'].includes(e.key)) return;
    const row = {
      phase: 'keydown', key: e.key, code: e.code,
      modifiers: [e.shiftKey, e.metaKey, e.ctrlKey, e.altKey],
      trusted: e.isTrusted, composer: el.dataset.composer, before: offsets(el),
    };
    // The app's keydown listeners have run by the next task. Capture both
    // defaultPrevented and post-default caret offsets without changing either.
    const timer = setTimeout(() => {
      timers.delete(timer);
      record({ ...row, prevented: e.defaultPrevented, after: offsets(el) });
    }, 0);
    timers.add(timer);
  };
  const onInput = (e) => {
    const el = box();
    if (el) record({ phase: e.type, inputType: e.inputType || null, composer: el.dataset.composer, selection: offsets(el) });
  };
  document.addEventListener('keydown', onKeyDown, true);
  document.addEventListener('beforeinput', onInput, true);
  document.addEventListener('input', onInput, true);
  window.__jevonsT1057Probe = {
    rows,
    stop() {
      document.removeEventListener('keydown', onKeyDown, true);
      document.removeEventListener('beforeinput', onInput, true);
      document.removeEventListener('input', onInput, true);
      timers.forEach(clearTimeout);
      timers.clear();
      delete window.__jevonsT1057Probe;
    },
  };
  console.log('T1057 probe installed; stop with window.__jevonsT1057Probe.stop()');
})();
