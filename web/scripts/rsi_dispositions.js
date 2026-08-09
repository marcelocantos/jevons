// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// RHS "Coach" tab model (🎯T354). DOM-free pure logic for hermetic tests:
// GET /api/rsi/dispositions payload → owner-visible rows + counts + calm
// empty state. The owner sees what the ambient RSI coach (🎯T243/T353)
// judged and what the overseer did with it (🎯T333 disposition ledger) —
// v1 is a bare list, no viz.
//
// Honest empty is a first-class state here: "no judgments yet" must never
// render as an error, and a fetch failure must never render as "nothing
// found" (both read the same to an owner watching for coach activity).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.RSIDispositions = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var API_PATH = '/api/rsi/dispositions';
  var POLL_MS = 20000;

  // Disposition ids mirrored from internal/rsi/disposition.go. Server
  // normalizes blank → pending, but the client re-applies it so a store
  // written by an older daemon still renders honestly.
  var PENDING = 'pending';
  var FILE = 'file';
  var PARK = 'park';
  var IGNORE = 'ignore_with_reason';
  var ACT_OTHER = 'act_other';

  var DISPOSITION_LABELS = {
    pending: 'pending',
    file: 'filed',
    park: 'parked',
    ignore_with_reason: 'ignored',
    act_other: 'acted',
  };

  var SEVERITY_RANK = { high: 3, medium: 2, low: 1 };

  function str(v) {
    return v == null ? '' : String(v);
  }

  // dispositionOf(entry) — blank/unknown disposition reads as pending.
  function dispositionOf(entry) {
    var d = str(entry && entry.disposition).trim().toLowerCase();
    if (d === FILE || d === PARK || d === IGNORE || d === ACT_OTHER) return d;
    return PENDING;
  }

  // dispositionLabel(id) — short owner-facing word for the chip.
  function dispositionLabel(id) {
    return DISPOSITION_LABELS[str(id).trim().toLowerCase()] || PENDING;
  }

  // parseTime(v) → epoch ms, or 0 when absent/unparseable. Go's zero time
  // ("0001-01-01T00:00:00Z") is not a real timestamp — treat it as absent.
  function parseTime(v) {
    var s = str(v).trim();
    if (!s || s.indexOf('0001-01-01') === 0) return 0;
    var ms = Date.parse(s);
    return isNaN(ms) ? 0 : ms;
  }

  // row(entry) — one owner-visible list row. Keeps the raw fields the
  // acceptance names: observation/name, severity, delivered_at, fingerprint,
  // evidence summary, disposition (+ reason / target_id when set).
  function row(entry) {
    var e = entry || {};
    var disposition = dispositionOf(e);
    var name = str(e.name).trim();
    var observation = str(e.observation).trim();
    return {
      fingerprint: str(e.fingerprint).trim(),
      // Never render an anonymous row: fall back through name → observation.
      title: name || observation || '(unnamed judgment)',
      observation: observation,
      severity: str(e.severity).trim().toLowerCase(),
      evidence: str(e.evidence).trim(),
      // 🎯T353 provenance: retro judgments come from the backward mine.
      retro: str(e.mode).trim().toLowerCase() === 'retro',
      disposition: disposition,
      dispositionLabel: dispositionLabel(disposition),
      pending: disposition === PENDING,
      reason: str(e.reason).trim(),
      targetID: str(e.target_id).trim(),
      outcome: str(e.outcome).trim(),
      deliveredAt: parseTime(e.delivered_at),
      dispositionAt: parseTime(e.disposition_at),
    };
  }

  // sortRows(rows) — newest delivery first; ties broken by severity then
  // fingerprint so the list order is stable across polls.
  function sortRows(rows) {
    return rows.slice().sort(function (a, b) {
      if (a.deliveredAt !== b.deliveredAt) return b.deliveredAt - a.deliveredAt;
      var sa = SEVERITY_RANK[a.severity] || 0;
      var sb = SEVERITY_RANK[b.severity] || 0;
      if (sa !== sb) return sb - sa;
      return a.fingerprint < b.fingerprint ? -1 : (a.fingerprint > b.fingerprint ? 1 : 0);
    });
  }

  // normalizePayload(payload, err) → render model.
  // Empty store and fetch failure are DISTINCT states: `empty` is calm,
  // `error` is loud, and neither is ever silently the other.
  function normalizePayload(payload, err) {
    if (err) {
      return {
        rows: [],
        total: 0,
        pending: 0,
        empty: false,
        error: str((err && err.message) || err) || 'request failed',
        path: '',
      };
    }
    var p = payload || {};
    var list = Array.isArray(p.judgments) ? p.judgments : [];
    var rows = sortRows(list.map(row));
    var pending = typeof p.pending === 'number'
      ? p.pending
      : rows.filter(function (r) { return r.pending; }).length;
    var total = typeof p.total === 'number' ? p.total : rows.length;
    return {
      rows: rows,
      total: total,
      pending: pending,
      empty: rows.length === 0,
      error: str(p.error).trim(),
      path: str(p.path).trim(),
    };
  }

  // emptyText(model) — calm honest empty state, never phrased as a failure.
  function emptyText() {
    return 'No coach judgments yet.';
  }

  // countsText(model) — tab meta line: "12 judgments · 3 pending".
  function countsText(model) {
    var m = model || {};
    var total = m.total || 0;
    if (!total) return '';
    var s = total + (total === 1 ? ' judgment' : ' judgments');
    if (m.pending) s += ' · ' + m.pending + ' pending';
    return s;
  }

  // detailText(row) — the trailing "what the overseer did" clause.
  // Filed rows name their target; ignored rows must show the reason (an
  // ignore with no visible reason is indistinguishable from a dropped one).
  function detailText(r) {
    if (!r || r.pending) return '';
    var parts = [];
    if (r.targetID) parts.push('🎯' + r.targetID.replace(/^🎯/, ''));
    if (r.reason) parts.push(r.reason);
    if (r.outcome) parts.push(r.outcome);
    return parts.join(' — ');
  }

  return {
    API_PATH: API_PATH,
    POLL_MS: POLL_MS,
    PENDING: PENDING,
    FILE: FILE,
    PARK: PARK,
    IGNORE: IGNORE,
    ACT_OTHER: ACT_OTHER,
    DISPOSITION_LABELS: DISPOSITION_LABELS,
    dispositionOf: dispositionOf,
    dispositionLabel: dispositionLabel,
    parseTime: parseTime,
    row: row,
    sortRows: sortRows,
    normalizePayload: normalizePayload,
    emptyText: emptyText,
    countsText: countsText,
    detailText: detailText,
  };

}));
