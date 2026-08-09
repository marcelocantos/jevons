// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T375 — the cockpit either boots completely or says so out loud.
//
// THE FAULT THIS EXISTS FOR. web/index.html carries one ~9000-line inline
// <script>. Every `let`/`const` in it, and every listener it registers, live
// in that single evaluation. A throw anywhere at its top level aborts the
// remainder: the bindings below the throw stay permanently in the temporal
// dead zone, while listeners and intervals registered ABOVE the throw (or by
// the modules loaded before it) keep firing against those dead bindings. One
// transient fault therefore becomes a permanent, recurring
// window.onerror cascade with no owner-visible cause — 🎯T374's
// workingEl/rhsBottomTab/agentInspectInput storm, once every fleet poll.
//
// Two independent triggers were observed for exactly that shape:
//   - a <script src> whose module is absent from the serving path, so its
//     global is undefined and the next top-level caller throws (🎯T374);
//   - index.html itself served mid-edit, because the daily daemon serves
//     web/ from disk while fleet workers write it. At 21:47:14 the live log
//     took four errors in 0.5s — "isAgentOriginBubble is not defined",
//     paintBody@/:4488 — a function another worker had called before it had
//     written the definition.
//
// WHY THIS IS THE READ-SIDE ANSWER, NOT A SECOND WRITE-SIDE GUARD. 🎯T374's
// serve-time check (internal/server/index_guard.go) decides one thing:
// does every referenced FILE exist on the serving path. It cannot decide
// whether an edit INSIDE index.html is semantically finished — that is not
// decidable by inspection from outside, at any cost, which is why the honest
// write-side fix is worktree isolation / publish-on-commit (🎯T254.2) rather
// than a cleverer static check. So this module takes the other branch of the
// target's acceptance: whatever the cause, an incomplete boot FAILS LOUDLY
// with owner-visible recovery instead of degrading into a silent cascade.
//
// MECHANISM. Loaded first, before any other local module, so it is live even
// when a later one 404s. index.html's inline script ends with markBooted().
// If the grace window closes with no markBooted(), the boot aborted:
//   1. every interval registered since install is cleared — that is what
//      converts a one-shot fault into a recurring storm, and it is the only
//      thing clearing does;
//   2. an owner-visible banner names the captured cause and offers reload.
// A completed boot is never touched, and errors AFTER markBooted() are
// ordinary runtime errors that belong to their own feature, not here.
//
// DOM-free decision logic (classify/describeError) so Node hermetic tests can
// require() it without a browser.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.BootSentinel = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Marks the injected recovery banner. Tests assert on the literal, so it
  // is part of the contract rather than an implementation detail.
  const BANNER_ATTR = 'data-jevons-boot-error';

  // How long after the document is parsed we wait for markBooted(). The
  // inline script is synchronous, so a healthy page reports booted before
  // DOMContentLoaded returns; the window only absorbs scheduling jitter.
  const DEFAULT_GRACE_MS = 1200;

  // Global set on the window when the boot is declared failed, so product
  // code that runs later can decline to do more work on a dead page.
  const FAILED_FLAG = '__jevonsBootFailed';

  // describeError renders a captured error into one owner-readable line.
  // Deliberately tolerant: the interesting cases arrive as ErrorEvent, as a
  // PromiseRejectionEvent reason, or as a bare string, and a boot failure is
  // the worst possible moment to throw while formatting a message.
  function describeError(e) {
    if (!e) return '';
    const msg = String((e.message != null ? e.message : e.reason != null ? e.reason : e) || '').trim();
    const src = e.source ? String(e.source) : '';
    if (!src) return msg;
    const line = (e.line == null || e.line === 0) ? '' : ':' + e.line;
    return msg ? msg + ' (' + src + line + ')' : src + line;
  }

  // classify is the entire decision. state:
  //   booted    — markBooted() was called
  //   errors    — captured before/at the check, oldest first
  //   elapsedMs — since install
  //   graceMs   — how long markBooted() is allowed to take
  //
  // Returns status 'ok' | 'pending' | 'failed'. Note what is NOT a failure:
  // a booted page with errors in it. Those are runtime faults belonging to
  // whatever raised them; treating them as boot failures here would tear the
  // cockpit's timers down over an unrelated bug, which is worse than the
  // disease. Symmetrically, a page that never booted IS a failure even with
  // zero captured errors — a module that 404s and leaves its global
  // undefined can abort the script through a path no listener saw.
  function classify(state) {
    const s = state || {};
    if (s.booted) return { status: 'ok' };
    const grace = s.graceMs == null ? DEFAULT_GRACE_MS : s.graceMs;
    if ((s.elapsedMs || 0) < grace) return { status: 'pending' };

    const errors = s.errors || [];
    const cause = errors.length ? describeError(errors[0]) : '';
    return {
      status: 'failed',
      headline: 'Cockpit did not finish loading — it is not safe to use.',
      cause: cause,
      detail: cause
        ? 'First failure: ' + cause
        : 'The page script stopped before completing and reported no error. '
          + 'A module is most likely missing or mid-edit on the serving path.',
    };
  }

  // install wires the sentinel into a real document. Returns a handle with
  // markBooted(), state() and check() so index.html and the browser oracle
  // drive the same code rather than a test-only twin.
  function install(win, doc, opts) {
    const o = opts || {};
    const graceMs = o.graceMs == null ? DEFAULT_GRACE_MS : o.graceMs;
    const now = o.now || function () { return Date.now(); };
    const started = now();

    const errors = [];
    let booted = false;
    let settled = false;

    // Intervals registered after this point. A boot failure leaves the page
    // full of them, each firing against dead bindings; they are the reason
    // the live symptom recurred every ~30s rather than appearing once.
    const intervals = [];
    const realSetInterval = win.setInterval;
    if (typeof realSetInterval === 'function') {
      win.setInterval = function () {
        const id = realSetInterval.apply(win, arguments);
        intervals.push(id);
        return id;
      };
    }

    function capture(e) {
      if (!e) return;
      errors.push({
        message: e.message != null ? e.message
          : (e.reason && e.reason.message != null) ? e.reason.message
            : e.reason,
        source: e.filename || '',
        line: e.lineno || 0,
      });
    }
    // Capture phase: run before jlog.js and anything else that may stop
    // propagation, so the first cause is genuinely the first.
    win.addEventListener('error', capture, true);
    win.addEventListener('unhandledrejection', capture, true);

    function banner(verdict) {
      const host = doc.body || doc.documentElement;
      if (!host) return null;
      const el = doc.createElement('div');
      el.setAttribute(BANNER_ATTR, '1');
      el.setAttribute('role', 'alert');
      el.style.cssText = 'position:fixed;top:0;left:0;right:0;z-index:2147483647;'
        + 'background:#7f1d1d;color:#fff;'
        + 'font:13px/1.45 -apple-system,system-ui,sans-serif;'
        + 'padding:10px 14px;border-bottom:2px solid #ef4444';
      const strong = doc.createElement('strong');
      strong.textContent = verdict.headline;
      const detail = doc.createElement('div');
      // textContent, not innerHTML: the detail carries an error message that
      // may quote page content, and a broken cockpit must not become an
      // injection surface while announcing that it is broken.
      detail.textContent = verdict.detail;
      const btn = doc.createElement('button');
      btn.type = 'button';
      btn.textContent = 'Reload';
      btn.style.cssText = 'margin-top:6px;padding:3px 12px;border-radius:4px;'
        + 'border:1px solid #fca5a5;background:#fff;color:#7f1d1d;cursor:pointer';
      btn.addEventListener('click', function () { win.location.reload(); });
      el.appendChild(strong);
      el.appendChild(detail);
      el.appendChild(btn);
      host.appendChild(el);
      return el;
    }

    function check() {
      if (settled) return { status: booted ? 'ok' : 'failed', settled: true };
      const verdict = classify({
        booted: booted,
        errors: errors,
        elapsedMs: now() - started,
        graceMs: graceMs,
      });
      if (verdict.status === 'pending') return verdict;
      settled = true;
      if (verdict.status === 'ok') {
        if (typeof realSetInterval === 'function') win.setInterval = realSetInterval;
        return verdict;
      }
      win[FAILED_FLAG] = true;
      for (let i = 0; i < intervals.length; i++) {
        try { win.clearInterval(intervals[i]); } catch (_) { /* already gone */ }
      }
      intervals.length = 0;
      try { banner(verdict); } catch (_) { /* banner is best-effort */ }
      return verdict;
    }

    function markBooted() {
      booted = true;
      if (typeof realSetInterval === 'function') win.setInterval = realSetInterval;
      settled = true;
    }

    // Both events: DOMContentLoaded is the earliest point at which the
    // inline script has run to completion or died, and load is the backstop
    // for the case where DOMContentLoaded already fired before install.
    const arm = function () { win.setTimeout(check, graceMs); };
    if (doc.readyState === 'loading') {
      doc.addEventListener('DOMContentLoaded', arm);
    } else {
      arm();
    }
    win.addEventListener('load', arm);

    const handle = {
      markBooted: markBooted,
      check: check,
      state: function () {
        return { booted: booted, errors: errors.slice(), settled: settled };
      },
    };
    win.__jevonsBoot = handle;
    return handle;
  }

  return {
    BANNER_ATTR: BANNER_ATTR,
    DEFAULT_GRACE_MS: DEFAULT_GRACE_MS,
    FAILED_FLAG: FAILED_FLAG,
    classify: classify,
    describeError: describeError,
    install: install,
  };
}));
