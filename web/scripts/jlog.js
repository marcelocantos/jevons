// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Browser-side logger that forwards events to jevonsd's /api/log so
// client-side diagnostics interleave with server logs in /tmp/jevonsd.log.
// Useful for correlating browser state (PTT engagement, AudioContext
// transitions, voice-WS readyState, send-path decisions) with server events.
//
// Usage:
//   jLog('info',  'ptt engage', {vadReset: true});
//   jLog('warn',  'voice WS closed unexpectedly');
//   jLog('error', 'commit failed', {readyState});
//   jLog('info',  'thread route', {component:'ThreadRoute', decision:'route', ...});
//
// Standard field keys (when present): component, decision, corr.
// Posts are fire-and-forget; failures are swallowed (network issues
// shouldn't break the UI). The same message is also emitted to the
// browser console so devtools still works.

(function () {
  'use strict';

  const VALID = new Set(['info', 'warn', 'error', 'debug']);

  // Browser-side console method to mirror level → console.
  const CONSOLE_FOR = {
    info:  (typeof console !== 'undefined' && console.info)  || console.log,
    warn:  (typeof console !== 'undefined' && console.warn)  || console.log,
    error: (typeof console !== 'undefined' && console.error) || console.log,
    debug: (typeof console !== 'undefined' && console.debug) || console.log,
  };

  /**
   * Normalize fields so component / decision / corr are first-class when
   * present (DecisionLog helpers already set them; callers may pass them
   * ad hoc). Returns undefined when empty so the wire omits "fields".
   */
  function normalizeFields(fields) {
    if (!fields || typeof fields !== 'object') return undefined;
    const out = {};
    const keys = Object.keys(fields);
    for (let i = 0; i < keys.length; i++) {
      const k = keys[i];
      const v = fields[k];
      if (v === undefined) continue;
      out[k] = v;
    }
    // Promote common aliases if only present under nested shapes later —
    // for now just ensure stringification of the three standard keys.
    if (out.component != null) out.component = String(out.component);
    if (out.decision != null) out.decision = String(out.decision);
    if (out.corr != null && out.corr !== '') out.corr = String(out.corr);
    else if (out.corr === '') delete out.corr;
    return Object.keys(out).length ? out : undefined;
  }

  /**
   * jLog forwards a structured log entry to jevonsd AND mirrors it to
   * the browser console.
   *
   * @param {'info'|'warn'|'error'|'debug'} level
   * @param {string} msg
   * @param {object} [fields] optional structured fields (component, decision, corr, …)
   */
  window.jLog = function jLog(level, msg, fields) {
    if (!VALID.has(level)) level = 'info';
    const normalized = normalizeFields(fields);
    const consoleFn = CONSOLE_FOR[level] || console.log;
    if (normalized) consoleFn.call(console, `[jLog/${level}] ${msg}`, normalized);
    else            consoleFn.call(console, `[jLog/${level}] ${msg}`);
    try {
      fetch('/api/log', {
        method:  'POST',
        headers: {'Content-Type': 'application/json'},
        body:    JSON.stringify({level: level, msg: msg, fields: normalized}),
        // keepalive lets the request finish even if the page unloads
        // (useful for unhandled-error reports).
        keepalive: true,
      }).catch(function () {});
    } catch (_) { /* fetch unavailable or threw synchronously — swallow */ }
  };

  // Auto-forward unhandled errors and promise rejections.
  window.addEventListener('error', function (e) {
    window.jLog('error', 'window.onerror', {
      component: 'window',
      message:  e.message,
      filename: e.filename,
      lineno:   e.lineno,
      colno:    e.colno,
      stack:    e.error && e.error.stack,
    });
  });
  window.addEventListener('unhandledrejection', function (e) {
    const reason = e.reason;
    window.jLog('error', 'unhandledrejection', {
      component: 'window',
      message: reason && reason.message ? reason.message : String(reason),
      stack:   reason && reason.stack,
    });
  });
})();
