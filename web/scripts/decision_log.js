// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure decision-log field builders (🎯T120 / docs/design/logging-telemetry-audit.md).
// DOM-free for Node hermetic tests. Feeds jLog → POST /api/log → slog + journal.
//
// Field contract (audit §3.2):
//   component — thread_route | attention | send_queue | history | fleet | cost | …
//   decision  — enum (match | no-match | enqueue | send | interrupt | hydrate_page | …)
//   corr      — page-session id when provided
// Draft previews truncated (~120); secret-shaped keys dropped.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.DecisionLog = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const DRAFT_MAX = 120;

  // Canonical component ids (audit §3.2).
  const COMPONENT = {
    thread_route: 'thread_route',
    attention: 'attention',
    send_queue: 'send_queue',
    history: 'history',
    fleet: 'fleet',
    cost: 'cost',
    chat_conn: 'chat_conn', // 🎯T140 connect/replay spans
  };

  // Keys that must never appear in decision fields.
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

  function withEnvelope(component, decision, fields, corr) {
    const out = sanitizeFields(fields);
    if (component) out.component = String(component);
    if (decision) out.decision = String(decision);
    if (corr != null && corr !== '') out.corr = String(corr);
    return out;
  }

  // Msg convention for jLog: "decision.<area>" → slog "browser: decision.<area>".
  function decisionMsg(area) {
    return 'decision.' + String(area || 'unknown');
  }

  // ThreadRoute.route hit — ALWAYS log (including when match rewrites wire).
  // hit: { threadId|null, score, reason }
  // decision = reason (match | no-match | ambiguous | explicit-prefix | empty | no-terms)
  function formatRouteDecision(hit, corr) {
    const h = hit || {};
    const reason = h.reason == null ? '' : String(h.reason);
    return withEnvelope(COMPONENT.thread_route, reason || 'route', {
      threadId: h.threadId == null ? null : String(h.threadId),
      score: typeof h.score === 'number' ? h.score : Number(h.score) || 0,
      reason: reason,
    }, corr);
  }

  // AttentionThreads.handleComposer result.
  // result: { kind, purpose?, threadId?, routed? }
  // opts: { command?, draft?, corr? }
  // decision = kind (send | local | empty)
  function formatComposerDecision(result, opts) {
    const r = result || {};
    const o = opts || {};
    const kind = r.kind == null ? '' : String(r.kind);
    const fields = { kind: kind };
    if (o.command != null && o.command !== '') fields.command = String(o.command);
    if (r.purpose != null && r.purpose !== '') fields.purpose = String(r.purpose);
    if (r.threadId != null && r.threadId !== '') fields.threadId = String(r.threadId);
    if (typeof r.routed === 'boolean') fields.routed = r.routed;
    if (o.draft != null && o.draft !== '') fields.draft = truncateDraft(o.draft);
    return withEnvelope(COMPONENT.attention, kind || 'composer', fields, o.corr);
  }

  // SendQueue.decideSend: decision = enqueue | send | interrupt | noop
  function formatSendDecision(decision, corr) {
    const d = decision || {};
    const action = d.action == null ? '' : String(d.action);
    const label = (action === 'send' && d.interrupt) ? 'interrupt' : action;
    const fields = {};
    if (typeof d.interrupt === 'boolean') fields.interrupt = d.interrupt;
    if (d.text != null && d.text !== '') fields.draft = truncateDraft(d.text);
    if (typeof d.depth === 'number') fields.depth = d.depth;
    return withEnvelope(COMPONENT.send_queue, label || 'send', fields, corr);
  }

  // Focus / lifecycle: decision = main | pursue | park | dismiss | capture
  function formatFocusDecision(action, opts) {
    const o = opts || {};
    const act = action == null ? '' : String(action);
    const fields = {};
    if (o.threadId != null && o.threadId !== '') fields.threadId = String(o.threadId);
    if (o.from != null && o.from !== '') fields.from = String(o.from);
    if (o.draft != null && o.draft !== '') fields.draft = truncateDraft(o.draft);
    return withEnvelope(COMPONENT.attention, act || 'focus', fields, o.corr);
  }

  // History hydrate / reconnect (🎯T120.4).
  // decision: hydrate_start | hydrate_page | hydrate_done | hydrate_error | reconnect
  // opts: { before?, after?, lines?, older?, oldestIndex?, err?, corr?, conn_id?, ms? }
  function formatHistoryDecision(decision, opts) {
    const o = opts || {};
    const fields = {};
    if (o.before != null) fields.before = Number(o.before);
    if (o.after != null) fields.after = Number(o.after);
    if (o.lines != null) fields.lines = Number(o.lines);
    if (o.older != null) fields.older = Number(o.older);
    if (o.oldestIndex != null) fields.oldestIndex = Number(o.oldestIndex);
    if (o.err != null && o.err !== '') fields.err = truncateDraft(String(o.err), 120);
    if (o.conn_id != null && o.conn_id !== '') fields.conn_id = String(o.conn_id);
    if (o.ms != null) fields.ms = Number(o.ms);
    return withEnvelope(COMPONENT.history, decision == null ? 'history' : String(decision), fields, o.corr);
  }

  // 🎯T140: /ws/chat connection spans (open → first_frame → history_meta → ui_ready).
  // decision: open | first_frame | history_meta | ui_ready | transport_replaced | stale_frame
  // opts: { conn_id?, concurrent?, ms?, frames?, bytes?, replay_ms?, corr?, prev_conn_id?, generation? }
  function formatConnectDecision(decision, opts) {
    const o = opts || {};
    const fields = {};
    if (o.conn_id != null && o.conn_id !== '') fields.conn_id = String(o.conn_id);
    if (o.prev_conn_id != null && o.prev_conn_id !== '') fields.prev_conn_id = String(o.prev_conn_id);
    if (o.concurrent != null) fields.concurrent = Number(o.concurrent);
    if (o.ms != null) fields.ms = Number(o.ms);
    if (o.frames != null) fields.frames = Number(o.frames);
    if (o.bytes != null) fields.bytes = Number(o.bytes);
    if (o.replay_ms != null) fields.replay_ms = Number(o.replay_ms);
    if (o.generation != null) fields.generation = Number(o.generation);
    if (o.type != null && o.type !== '') fields.type = String(o.type);
    if (o.oldestIndex != null) fields.oldestIndex = Number(o.oldestIndex);
    return withEnvelope(COMPONENT.chat_conn, decision == null ? 'conn' : String(decision), fields, o.corr);
  }

  // Cost / fleet failure helpers (bounded warn).
  function formatCostDecision(decision, opts) {
    const o = opts || {};
    const fields = {};
    if (o.err != null && o.err !== '') fields.err = truncateDraft(String(o.err), 120);
    return withEnvelope(COMPONENT.cost, decision == null ? 'poll' : String(decision), fields, o.corr);
  }

  function formatFleetDecision(decision, opts) {
    const o = opts || {};
    const fields = {};
    if (o.err != null && o.err !== '') fields.err = truncateDraft(String(o.err), 120);
    return withEnvelope(COMPONENT.fleet, decision == null ? 'refresh' : String(decision), fields, o.corr);
  }

  return {
    DRAFT_MAX: DRAFT_MAX,
    COMPONENT: COMPONENT,
    decisionMsg: decisionMsg,
    truncateDraft: truncateDraft,
    isSecretKey: isSecretKey,
    sanitizeFields: sanitizeFields,
    withEnvelope: withEnvelope,
    formatRouteDecision: formatRouteDecision,
    formatComposerDecision: formatComposerDecision,
    formatSendDecision: formatSendDecision,
    formatFocusDecision: formatFocusDecision,
    formatHistoryDecision: formatHistoryDecision,
    formatConnectDecision: formatConnectDecision,
    formatCostDecision: formatCostDecision,
    formatFleetDecision: formatFleetDecision,
  };
}));
