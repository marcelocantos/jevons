// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure closed-aside history helpers (🎯T270).
// DOM-free so Node hermetic tests can require() it.
//
// Product path: dismiss archives to durable GET /api/asides/history.
// UI delineates side chat (aside:/capture:) from target filing (target:).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.AsideHistory = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const KIND_SIDE = 'side';
  const KIND_CAPTURE = 'capture';
  const KIND_TARGET = 'target';

  const HISTORY_PATH = '/api/asides/history';

  /**
   * Normalize create/dismiss kind tags to the closed set.
   * @param {string} kind
   * @param {string} [purpose] attention purpose (file-target) fallback
   */
  function normalizeKind(kind, purpose) {
    const k = String(kind == null ? '' : kind).trim().toLowerCase();
    const p = String(purpose == null ? '' : purpose).trim().toLowerCase();
    if (k === KIND_TARGET || k === 'file-target' || k === 'target-aside' || k === 'filing' ||
        p === 'file-target' || p === 'target-aside') {
      return KIND_TARGET;
    }
    if (k === KIND_CAPTURE || p === 'capture') {
      return KIND_CAPTURE;
    }
    if (k === KIND_SIDE || k === 'aside' || k === 'side-chat' || k === 'freeform' || k === '') {
      return KIND_SIDE;
    }
    // Command aliases from composer prefixes.
    if (k === 'aside') return KIND_SIDE;
    return KIND_SIDE;
  }

  /** Owner-facing type label for history rows. */
  function kindLabel(kind) {
    switch (normalizeKind(kind)) {
      case KIND_TARGET:
        return 'Target filing';
      case KIND_CAPTURE:
        return 'Capture';
      default:
        return 'Side chat';
    }
  }

  /**
   * Map composer prefix command → history kind.
   * @param {string} command aside | capture | target | …
   */
  function kindFromCommand(command) {
    const c = String(command == null ? '' : command).trim().toLowerCase();
    if (c === 'target') return KIND_TARGET;
    if (c === 'capture') return KIND_CAPTURE;
    if (c === 'aside') return KIND_SIDE;
    return KIND_SIDE;
  }

  /**
   * CSS / data-kind token for row chrome (distinct visual buckets).
   * capture groups with side-chat as "explicit" non-filing; target alone is filing.
   */
  function kindClass(kind) {
    const k = normalizeKind(kind);
    if (k === KIND_TARGET) return 'aside-hist-target';
    if (k === KIND_CAPTURE) return 'aside-hist-capture';
    return 'aside-hist-side';
  }

  /**
   * Group flag: explicit side-chat family vs target-filing (acceptance #2).
   * capture is explicit non-filing; target is filing.
   */
  function isTargetFiling(kind) {
    return normalizeKind(kind) === KIND_TARGET;
  }

  function isExplicitSideChat(kind) {
    const k = normalizeKind(kind);
    return k === KIND_SIDE || k === KIND_CAPTURE;
  }

  /**
   * Normalize API / fixture records for render.
   * @param {object|null} rec
   */
  function normalizeRecord(rec) {
    if (!rec || typeof rec !== 'object') return null;
    const id = String(rec.id || rec.name || '').trim();
    if (!id) return null;
    const kind = normalizeKind(rec.kind, rec.purpose);
    const title = String(rec.title || rec.description || id).trim() || id;
    return {
      id: id,
      title: title,
      kind: kind,
      kindLabel: rec.kind_label || rec.kindLabel || kindLabel(kind),
      kindClass: kindClass(kind),
      isTargetFiling: isTargetFiling(kind),
      isExplicitSideChat: isExplicitSideChat(kind),
      parent: rec.parent || '',
      workdir: rec.workdir || '',
      closedAt: rec.closed_at || rec.closedAt || '',
      closedUnix: typeof rec.closed_unix === 'number' ? rec.closed_unix
        : (typeof rec.closedUnix === 'number' ? rec.closedUnix : 0),
    };
  }

  /**
   * Parse GET /api/asides/history JSON body → sorted model rows (newest first).
   * @param {object|Array|null} payload
   */
  function recordsFromPayload(payload) {
    let raw = [];
    if (Array.isArray(payload)) {
      raw = payload;
    } else if (payload && Array.isArray(payload.asides)) {
      raw = payload.asides;
    } else if (payload && Array.isArray(payload.history)) {
      raw = payload.history;
    }
    const out = [];
    for (let i = 0; i < raw.length; i++) {
      const n = normalizeRecord(raw[i]);
      if (n) out.push(n);
    }
    out.sort(function (a, b) {
      if (a.closedUnix !== b.closedUnix) return b.closedUnix - a.closedUnix;
      return a.id < b.id ? 1 : -1;
    });
    return out;
  }

  /**
   * Pure HTML for history list (type badge + title). Empty state included.
   * @param {Array} records normalizeRecord rows
   */
  function historyListHtml(records) {
    const rows = Array.isArray(records) ? records : [];
    if (!rows.length) {
      return '<div class="aside-history-empty" data-aside-history-empty="1">' +
        'No closed asides yet</div>';
    }
    let html = '<ul class="aside-history-list" data-aside-history-list="1">';
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i];
      const badge = r.isTargetFiling ? 'Target filing' : (r.kind === KIND_CAPTURE ? 'Capture' : 'Side chat');
      const cls = r.kindClass || kindClass(r.kind);
      html += '<li class="aside-history-row ' + cls + '" data-aside-id="' +
        escapeAttr(r.id) + '" data-aside-kind="' + escapeAttr(r.kind) + '">' +
        '<span class="aside-history-kind" data-kind="' + escapeAttr(r.kind) + '">' +
        escapeHtml(badge) + '</span>' +
        '<span class="aside-history-title">' + escapeHtml(r.title) + '</span>' +
        '</li>';
    }
    html += '</ul>';
    return html;
  }

  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function escapeAttr(s) {
    return escapeHtml(s).replace(/'/g, '&#39;');
  }

  /**
   * Product path for closed-aside history fetch.
   */
  function historyPath() {
    return HISTORY_PATH;
  }

  /**
   * Whether two history rows are distinguishable by type chrome (oracle).
   */
  function typesDistinguishable(a, b) {
    const ra = normalizeRecord(a);
    const rb = normalizeRecord(b);
    if (!ra || !rb) return false;
    if (ra.kind === rb.kind) return ra.kindLabel === rb.kindLabel;
    return ra.kindLabel !== rb.kindLabel && ra.kindClass !== rb.kindClass;
  }

  return {
    KIND_SIDE: KIND_SIDE,
    KIND_CAPTURE: KIND_CAPTURE,
    KIND_TARGET: KIND_TARGET,
    HISTORY_PATH: HISTORY_PATH,
    normalizeKind: normalizeKind,
    kindLabel: kindLabel,
    kindFromCommand: kindFromCommand,
    kindClass: kindClass,
    isTargetFiling: isTargetFiling,
    isExplicitSideChat: isExplicitSideChat,
    normalizeRecord: normalizeRecord,
    recordsFromPayload: recordsFromPayload,
    historyListHtml: historyListHtml,
    historyPath: historyPath,
    typesDistinguishable: typesDistinguishable,
    escapeHtml: escapeHtml,
  };
}));
