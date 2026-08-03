// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// RHS bullseye frontier table helpers (🎯T131). DOM-free pure logic for
// hermetic tests: payload → rows, tab model, calm empty state. Ledger path
// always comes from the server/bullseye discovery response — never invented
// client-side as a fixed bullseye.yaml string.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.FrontierTable = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var TAB_TRANSCRIPT = 'transcript';
  var TAB_FRONTIER = 'frontier';
  var POLL_MS = 8000;

  // nextBottomTab(prev, click) — tab ids only; unknown → frontier.
  function nextBottomTab(prev, click) {
    var t = String(click || '').toLowerCase();
    if (t === TAB_TRANSCRIPT || t === TAB_FRONTIER) return t;
    return prev === TAB_TRANSCRIPT ? TAB_TRANSCRIPT : TAB_FRONTIER;
  }

  // When a fleet agent is selected, product switches to transcript tab.
  // When selection clears, stay on current tab (usually frontier remains).
  function tabAfterAgentSelect(hasSelection) {
    return hasSelection ? TAB_TRANSCRIPT : TAB_FRONTIER;
  }

  // normalizePayload(apiJSON|err) → { available, ledger, cwd, rows, empty, error }
  function normalizePayload(payload, err) {
    if (err) {
      return {
        available: false,
        ledger: '',
        cwd: '',
        rows: [],
        empty: true,
        error: String(err.message || err),
      };
    }
    var p = payload || {};
    var targets = Array.isArray(p.targets) ? p.targets : [];
    var rows = targets.map(function (t) {
      if (!t) return null;
      return {
        id: String(t.id || ''),
        name: String(t.name || ''),
        status: String(t.status || ''),
        fanout: typeof t.fanout === 'number' ? t.fanout : (parseInt(t.fanout, 10) || 0),
        value: typeof t.value === 'number' ? t.value : undefined,
      };
    }).filter(function (r) {
      return r && r.id;
    });
    var available = !!p.available;
    var error = p.error ? String(p.error) : '';
    return {
      available: available,
      ledger: p.ledger ? String(p.ledger) : '',
      cwd: p.cwd ? String(p.cwd) : '',
      rows: rows,
      empty: !available || rows.length === 0,
      error: error,
      updatedAt: p.updated_at ? String(p.updated_at) : '',
    };
  }

  // Ogham feather mark (U+169B) — fanout glyph (🎯T173).
  var FANOUT_MARK = '\u169B';

  // Canonical status → short code + display title (🎯T173).
  // Keys are lowercased with underscores/spaces stripped for lookup.
  var STATUS_ABBR = {
    identified: { abbr: 'Id', title: 'Identified' },
    converging: { abbr: 'Cv', title: 'Converging' },
    achieved: { abbr: 'Ac', title: 'Achieved' },
    setaside: { abbr: 'Sa', title: 'Set aside' },
    set_aside: { abbr: 'Sa', title: 'Set aside' },
    postponed: { abbr: 'Pp', title: 'Postponed' },
    assigned: { abbr: 'As', title: 'Assigned' },
  };

  function statusKey(status) {
    return String(status || '').trim().toLowerCase().replace(/[\s-]+/g, '_');
  }

  // formatStatus — abbreviated code for cell text (🎯T173). Empty → em dash.
  function formatStatus(status) {
    var s = String(status || '').trim();
    if (!s) return '—';
    var key = statusKey(s);
    var hit = STATUS_ABBR[key] || STATUS_ABBR[key.replace(/_/g, '')];
    if (hit) return hit.abbr;
    // Fallback: first letters of CamelCase / snake words, max 2–3 chars.
    var parts = s.replace(/_/g, ' ').split(/(?=[A-Z])|[\s-]+/).filter(Boolean);
    if (parts.length >= 2) {
      return parts.map(function (p) { return p.charAt(0).toUpperCase(); }).join('').slice(0, 3);
    }
    return s.slice(0, 2).charAt(0).toUpperCase() + s.slice(1, 2).toLowerCase();
  }

  // statusTitle — full status string for hover/title.
  function statusTitle(status) {
    var s = String(status || '').trim();
    if (!s) return '';
    var key = statusKey(s);
    var hit = STATUS_ABBR[key] || STATUS_ABBR[key.replace(/_/g, '')];
    if (hit) return hit.title;
    // Pretty-print unknown: snake → words, capitalize.
    return s.replace(/_/g, ' ').replace(/\b\w/g, function (c) { return c.toUpperCase(); });
  }

  // formatFanout(n, id) → { text, title, visible }.
  // Hide cell content when fanout is 0 (🎯T173). Nonzero: "N᚛" + hover detail.
  function formatFanout(n, id) {
    var count = typeof n === 'number' ? n : (parseInt(n, 10) || 0);
    if (count <= 0) {
      return { text: '', title: '', visible: false };
    }
    var tid = String(id || '').trim() || '?';
    var text = String(count) + FANOUT_MARK;
    var title = count === 1
      ? ('1 target depends on ' + tid)
      : (String(count) + ' targets depend on ' + tid);
    return { text: text, title: title, visible: true };
  }

  // shortName truncates long target names for the compact table.
  function shortName(name, maxLen) {
    var n = String(name || '');
    var max = maxLen || 48;
    if (n.length <= max) return n;
    return n.slice(0, max - 1) + '…';
  }

  // emptyMessage for calm unavailable / zero-frontier states.
  function emptyMessage(model) {
    if (!model) return 'Frontier unavailable.';
    if (model.error && !model.available) return model.error;
    if (model.available && model.rows.length === 0) return 'Frontier is empty — no ready targets.';
    if (!model.available) return 'No bullseye ledger for this workdir.';
    return '';
  }

  // API path constant — client never hard-codes ledger file paths.
  var API_PATH = '/api/frontier';

  return {
    TAB_TRANSCRIPT: TAB_TRANSCRIPT,
    TAB_FRONTIER: TAB_FRONTIER,
    POLL_MS: POLL_MS,
    API_PATH: API_PATH,
    FANOUT_MARK: FANOUT_MARK,
    nextBottomTab: nextBottomTab,
    tabAfterAgentSelect: tabAfterAgentSelect,
    normalizePayload: normalizePayload,
    formatStatus: formatStatus,
    statusTitle: statusTitle,
    formatFanout: formatFanout,
    shortName: shortName,
    emptyMessage: emptyMessage,
  };
}));
