// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure decision-log field builders for high-value send-path telemetry.
// DOM-free so Node hermetic tests can require() without a browser.
// Truncates drafts; strips secret-shaped keys. Used with jLog → /api/log.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.DecisionLog = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const DRAFT_MAX = 120;

  // Keys that must never appear in decision fields (case-insensitive match
  // on the key name, or common secret suffixes).
  const SECRET_KEY_RE = /^(password|passwd|secret|token|authorization|api[_-]?key|cookie|set-cookie|credential|private[_-]?key|access[_-]?token|refresh[_-]?token)$/i;

  function truncateDraft(text, maxLen) {
    const n = typeof maxLen === 'number' && maxLen > 0 ? maxLen : DRAFT_MAX;
    const s = text == null ? '' : String(text);
    if (s.length <= n) return s;
    return s.slice(0, Math.max(1, n - 1)) + '…';
  }

  function isSecretKey(key) {
    return SECRET_KEY_RE.test(String(key || ''));
  }

  // Shallow copy fields, dropping secret keys and empty undefined values.
  function sanitizeFields(fields) {
    const out = {};
    if (!fields || typeof fields !== 'object') return out;
    const keys = Object.keys(fields);
    for (let i = 0; i < keys.length; i++) {
      const k = keys[i];
      if (isSecretKey(k)) continue;
      const v = fields[k];
      if (v === undefined) continue;
      out[k] = v;
    }
    return out;
  }

  // Merge standard decision envelope: component, decision, optional corr.
  function withEnvelope(component, decision, fields, corr) {
    const out = sanitizeFields(fields);
    if (component) out.component = String(component);
    if (decision) out.decision = String(decision);
    if (corr != null && corr !== '') out.corr = String(corr);
    return out;
  }

  // ThreadRoute.route hit — ALWAYS log (including when match rewrites wire).
  // hit: { threadId|null, score, reason }
  function formatRouteDecision(hit, corr) {
    const h = hit || {};
    return withEnvelope('ThreadRoute', 'route', {
      threadId: h.threadId == null ? null : String(h.threadId),
      score: typeof h.score === 'number' ? h.score : Number(h.score) || 0,
      reason: h.reason == null ? '' : String(h.reason),
    }, corr);
  }

  // AttentionThreads.handleComposer result (+ optional command from parsePrefix).
  // result: { kind, purpose?, threadId?, routed? }
  // opts: { command?, draft?, corr? }
  function formatComposerDecision(result, opts) {
    const r = result || {};
    const o = opts || {};
    const fields = {
      kind: r.kind == null ? '' : String(r.kind),
    };
    if (o.command != null && o.command !== '') {
      fields.command = String(o.command);
    }
    if (r.purpose != null && r.purpose !== '') {
      fields.purpose = String(r.purpose);
    }
    if (r.threadId != null && r.threadId !== '') {
      fields.threadId = String(r.threadId);
    }
    if (typeof r.routed === 'boolean') {
      fields.routed = r.routed;
    }
    if (o.draft != null && o.draft !== '') {
      fields.draft = truncateDraft(o.draft);
    }
    return withEnvelope('AttentionThreads', 'composer', fields, o.corr);
  }

  // SendQueue.decideSend result: enqueue | send | interrupt | noop.
  // decision: { action, text?, interrupt? }
  function formatSendDecision(decision, corr) {
    const d = decision || {};
    const action = d.action == null ? '' : String(d.action);
    // Surface interrupt as its own action label when busy+Control+Enter.
    const label = (action === 'send' && d.interrupt) ? 'interrupt' : action;
    const fields = {
      action: label,
      queueAction: action,
    };
    if (typeof d.interrupt === 'boolean') {
      fields.interrupt = d.interrupt;
    }
    if (d.text != null && d.text !== '') {
      fields.draft = truncateDraft(d.text);
    }
    return withEnvelope('SendQueue', 'send', fields, corr);
  }

  // Focus / lifecycle transitions: main | pursue | park | dismiss.
  // opts: { threadId?, from?, draft?, corr? }
  function formatFocusDecision(action, opts) {
    const o = opts || {};
    const fields = {
      action: action == null ? '' : String(action),
    };
    if (o.threadId != null && o.threadId !== '') {
      fields.threadId = String(o.threadId);
    }
    if (o.from != null && o.from !== '') {
      fields.from = String(o.from);
    }
    if (o.draft != null && o.draft !== '') {
      fields.draft = truncateDraft(o.draft);
    }
    return withEnvelope('AttentionThreads', 'focus', fields, o.corr);
  }

  return {
    DRAFT_MAX: DRAFT_MAX,
    truncateDraft: truncateDraft,
    isSecretKey: isSecretKey,
    sanitizeFields: sanitizeFields,
    withEnvelope: withEnvelope,
    formatRouteDecision: formatRouteDecision,
    formatComposerDecision: formatComposerDecision,
    formatSendDecision: formatSendDecision,
    formatFocusDecision: formatFocusDecision,
  };
}));
