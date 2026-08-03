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

  function normalizeStringList(raw) {
    if (!Array.isArray(raw)) return [];
    var out = [];
    for (var i = 0; i < raw.length; i++) {
      var s = raw[i] == null ? '' : String(raw[i]).trim();
      if (s) out.push(s);
    }
    return out;
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
      var deps = normalizeDependents(t.dependents);
      var fanout = typeof t.fanout === 'number' ? t.fanout : (parseInt(t.fanout, 10) || 0);
      // Prefer dependents length when present (API Fanout == len(Dependents)).
      if (deps.length > 0 || Array.isArray(t.dependents)) {
        fanout = deps.length;
      }
      return {
        id: String(t.id || ''),
        name: String(t.name || ''),
        status: String(t.status || ''),
        fanout: fanout,
        dependents: deps,
        value: typeof t.value === 'number' ? t.value : undefined,
        acceptance: normalizeStringList(t.acceptance),
        context: t.context != null ? String(t.context).trim() : '',
        tags: normalizeStringList(t.tags),
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

  // normalizeDependents — API objects {id,name}, bare strings, or parallel fields.
  function normalizeDependents(raw) {
    if (!Array.isArray(raw)) return [];
    var out = [];
    for (var i = 0; i < raw.length; i++) {
      var d = raw[i];
      if (d == null) continue;
      if (typeof d === 'string') {
        var sid = String(d).trim();
        if (sid) out.push({ id: sid, name: '' });
        continue;
      }
      var id = String(d.id || d.ID || '').trim();
      if (!id) continue;
      out.push({ id: id, name: String(d.name || d.Name || '').trim() });
    }
    return out;
  }

  // formatFanout(n, id, dependents?) → { text, title, visible }.
  // Hide when 0 (🎯T173). Nonzero: "N᚛" + InstantTip lead count + bullets "• TID — name" (🎯T179).
  function formatFanout(n, id, dependents) {
    var deps = normalizeDependents(dependents);
    var count = deps.length > 0
      ? deps.length
      : (typeof n === 'number' ? n : (parseInt(n, 10) || 0));
    if (count <= 0) {
      return { text: '', title: '', visible: false };
    }
    var tid = String(id || '').trim() || '?';
    var text = String(count) + FANOUT_MARK;
    var lead = count === 1
      ? ('1 target depends on ' + tid)
      : (String(count) + ' targets depend on ' + tid);
    var title = lead;
    if (deps.length > 0) {
      var lines = [lead];
      for (var i = 0; i < deps.length; i++) {
        var d = deps[i];
        var bullet = '• ' + d.id;
        if (d.name) bullet += ' — ' + d.name;
        lines.push(bullet);
      }
      title = lines.join('\n');
    }
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

  // formatTargetCardMarkdown(row) — rich markdown for ID/name InstantTip (🎯T181).
  // Sections: title (🎯id + name), status, acceptance (all), context, tags, dependents.
  // Pure string builder; product renders via marked / parseAssistantMarkdown.
  function formatTargetCardMarkdown(row) {
    if (!row || !row.id) return '';
    var id = String(row.id).trim();
    var name = String(row.name || '').trim();
    var lines = [];
    lines.push('**🎯' + id + '**' + (name ? (' — ' + name) : ''));
    var st = statusTitle(row.status) || String(row.status || '').trim();
    if (st) {
      lines.push('');
      lines.push('**Status:** ' + st);
    }
    var acc = normalizeStringList(row.acceptance);
    if (acc.length > 0) {
      lines.push('');
      lines.push('**Acceptance**');
      for (var i = 0; i < acc.length; i++) {
        lines.push('- ' + acc[i]);
      }
    }
    var ctx = row.context != null ? String(row.context).trim() : '';
    if (ctx) {
      // Keep tip compact: first paragraph only if multi-paragraph.
      var firstPara = ctx.split(/\n\s*\n/)[0].trim();
      // Cap very long single paragraphs.
      if (firstPara.length > 480) {
        firstPara = firstPara.slice(0, 479) + '…';
      }
      if (firstPara) {
        lines.push('');
        lines.push('**Context**');
        lines.push(firstPara);
      }
    }
    var tags = normalizeStringList(row.tags);
    if (tags.length > 0) {
      lines.push('');
      lines.push('**Tags:** ' + tags.join(', '));
    }
    var deps = normalizeDependents(row.dependents);
    if (deps.length > 0) {
      lines.push('');
      lines.push('**Dependents** (' + deps.length + ')');
      for (var j = 0; j < deps.length; j++) {
        var d = deps[j];
        var bullet = '- ' + d.id;
        if (d.name) bullet += ' — ' + d.name;
        lines.push(bullet);
      }
    }
    return lines.join('\n');
  }

  // formatTargetCardPlain — aria-label / text fallback (no markdown markers).
  function formatTargetCardPlain(row) {
    if (!row || !row.id) return '';
    var parts = ['🎯' + String(row.id).trim()];
    if (row.name) parts.push(String(row.name).trim());
    var st = statusTitle(row.status) || String(row.status || '').trim();
    if (st) parts.push('Status: ' + st);
    var acc = normalizeStringList(row.acceptance);
    if (acc.length > 0) {
      parts.push('Acceptance: ' + acc.join('; '));
    }
    return parts.join('. ');
  }

  // API path constant — client never hard-codes ledger file paths.
  var API_PATH = '/api/frontier';

  // 🎯T182: play control → message product PO to start work on a frontier row.
  var PLAY_GLYPH = '\u25B6'; // ▶
  var DEFAULT_PLAY_PO = 'jevons-po';

  // resolvePlayPO — residual multi-PO: default jevons-po for jevons ledger.
  function resolvePlayPO(opts) {
    var o = opts || {};
    if (o.po) return String(o.po).trim() || DEFAULT_PLAY_PO;
    if (o.agent) return String(o.agent).trim() || DEFAULT_PLAY_PO;
    return DEFAULT_PLAY_PO;
  }

  // agentSendPath(name) — product HTTP proxy for fleet agent_send (🎯T182).
  function agentSendPath(name) {
    var n = String(name || DEFAULT_PLAY_PO).trim() || DEFAULT_PLAY_PO;
    return '/api/agents/' + encodeURIComponent(n) + '/send';
  }

  // buildPlayKickoffText(row) — body for PO: target id + name + spawn brief.
  // Asks PO to kick off with full brief, parent=jevons-po (not toast-only).
  function buildPlayKickoffText(row, opts) {
    if (!row || !row.id) return '';
    var id = String(row.id).trim();
    var name = row.name != null ? String(row.name).trim() : '';
    var po = resolvePlayPO(opts);
    var lines = [];
    lines.push('Start work on frontier target 🎯' + id +
      (name ? (' — ' + name) : '') + '.');
    lines.push('');
    lines.push(
      'Kick off now: spawn/brief a fleet worker with parent=' + po +
      ' and a full brief to execute this target end-to-end ' +
      '(local commits + oracle evidence; no Ship/PR unless the owner asks). ' +
      'Do not only toast or acknowledge — actually start the worker.'
    );
    var st = statusTitle(row.status) || String(row.status || '').trim();
    if (st) {
      lines.push('');
      lines.push('Status: ' + st);
    }
    var acc = normalizeStringList(row.acceptance);
    if (acc.length > 0) {
      lines.push('');
      lines.push('Acceptance:');
      for (var i = 0; i < acc.length; i++) {
        lines.push('- ' + acc[i]);
      }
    }
    return lines.join('\n');
  }

  // playKickoffRequest(row, opts) — pure request shape for hermetic mocks.
  // { url, method, body: { text }, po }
  function playKickoffRequest(row, opts) {
    var po = resolvePlayPO(opts);
    var text = buildPlayKickoffText(row, opts);
    return {
      url: agentSendPath(po),
      method: 'POST',
      body: { text: text },
      po: po,
    };
  }

  return {
    TAB_TRANSCRIPT: TAB_TRANSCRIPT,
    TAB_FRONTIER: TAB_FRONTIER,
    POLL_MS: POLL_MS,
    API_PATH: API_PATH,
    FANOUT_MARK: FANOUT_MARK,
    PLAY_GLYPH: PLAY_GLYPH,
    DEFAULT_PLAY_PO: DEFAULT_PLAY_PO,
    nextBottomTab: nextBottomTab,
    tabAfterAgentSelect: tabAfterAgentSelect,
    normalizePayload: normalizePayload,
    formatStatus: formatStatus,
    statusTitle: statusTitle,
    formatFanout: formatFanout,
    normalizeDependents: normalizeDependents,
    shortName: shortName,
    emptyMessage: emptyMessage,
    formatTargetCardMarkdown: formatTargetCardMarkdown,
    formatTargetCardPlain: formatTargetCardPlain,
    resolvePlayPO: resolvePlayPO,
    agentSendPath: agentSendPath,
    buildPlayKickoffText: buildPlayKickoffText,
    playKickoffRequest: playKickoffRequest,
  };
}));
