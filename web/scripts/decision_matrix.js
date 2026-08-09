// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Structured design-choice UX (🎯T369). When the overseer presents a bounded
// set of owner decisions as a markdown table ("Option | Approach | … |
// Recommended"), the owner should get something to inspect and click — not a
// cramped markdown table they have to answer in prose.
//
// This module is the DOM-free half: markdown table cells in, a decision model
// out, plus the durable selection store and the canonical reply text. Browser
// glue (card render, click, send) lives in web/index.html.
//
// Detection is deliberately conservative. A false positive turns an ordinary
// comparison table into a radio group the owner never asked for, which is
// worse than leaving a real matrix as a table: only a table whose option keys
// form a clean consecutive sequence (A, B, C… or 1, 2, 3…) is promoted.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.DecisionMatrix = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /** localStorage key for durable owner selections. */
  var STORAGE_KEY = 'jevons-decision-choice-v1';

  /** Newest-first cap on remembered selections. */
  var MAX_CHOICES = 200;

  /** Fewest options that count as a choice matrix. */
  var MIN_OPTIONS = 2;

  var DEFAULT_TITLE = 'Design choice';

  /** Headers that name the option column in row-oriented tables. */
  var OPTION_HEADER_RE = /^(option|options|choice|choices|alt|alternative)s?$/i;

  /** Headers that name a recommendation column. */
  var RECOMMEND_HEADER_RE = /recommend|verdict|pick/i;

  /** Headers whose cell is the option's human label when the key cell is bare. */
  var LABEL_HEADER_RE = /^(approach|name|label|summary|description|design|proposal|option|choice)s?$/i;

  /** Cell values that read as "yes, this one" in a recommendation column. */
  var RECOMMEND_CELL_RE = /^(y|yes|✓|✔|✅|★|⭐|x|recommended|recommend|best|default|pick)$/i;

  /** Inline marker anywhere in a row that flags it as the recommendation. */
  var RECOMMEND_MARK_RE = /✅|⭐|★|\brecommended\b/i;

  // Bare key: "A", "**B**", "(C)", "Option A", "3.".
  var KEY_ONLY_RE = /^(?:\*\*|__)?\s*(?:(?:option|opt|choice)\s+)?\(?([A-Za-z]|\d{1,2})\)?\.?(?:\*\*|__)?$/i;

  // Key + label: "A — CLI-first", "B: allowlist", "3. force-pause",
  // "Option C force-pause". A separator (punctuation or whitespace) is
  // mandatory, which is what stops a bare word like "Approach" from reading
  // as option "A" with label "pproach".
  var KEY_LABEL_RE =
    /^(?:\*\*|__)?\s*(?:(?:option|opt|choice)\s+)?\(?([A-Za-z]|\d{1,2})\)?(?:\*\*|__)?\s*(?:[—–\-:.)]+|\s)\s*(\S.*)$/i;

  function str(v) {
    return v == null ? '' : String(v);
  }

  /** stripMarkup(s) — drop the inline emphasis/code marks a cell may carry. */
  function stripMarkup(s) {
    return str(s)
      .replace(/[*_`]+/g, '')
      .replace(/\s+/g, ' ')
      .trim();
  }

  /**
   * parseOptionCell(cell) → {key, label} | null.
   * `key` is upper-cased for letters so "a)" and "A)" are the same option.
   */
  function parseOptionCell(cell) {
    var raw = stripMarkup(cell);
    if (!raw || raw.length > 120) return null;
    var m = KEY_ONLY_RE.exec(raw);
    if (m) return { key: normalizeKey(m[1]), label: '' };
    m = KEY_LABEL_RE.exec(raw);
    if (!m) return null;
    return { key: normalizeKey(m[1]), label: stripMarkup(m[2]) };
  }

  function normalizeKey(k) {
    return /^[a-z]$/i.test(k) ? k.toUpperCase() : k;
  }

  /**
   * isConsecutiveKeys(keys) — the anti-false-positive gate. Keys must be
   * unique and form A,B,C… (from A) or 1,2,3… (from 1). A table whose first
   * column happens to start with capital letters will not survive this.
   */
  function isConsecutiveKeys(keys) {
    if (!Array.isArray(keys) || keys.length < MIN_OPTIONS) return false;
    var seen = {};
    for (var i = 0; i < keys.length; i++) {
      if (seen[keys[i]]) return false;
      seen[keys[i]] = true;
    }
    var alpha = keys.every(function (k) { return /^[A-Z]$/.test(k); });
    if (alpha) {
      for (var a = 0; a < keys.length; a++) {
        if (keys[a].charCodeAt(0) !== 65 + a) return false;
      }
      return true;
    }
    var numeric = keys.every(function (k) { return /^\d{1,2}$/.test(k); });
    if (numeric) {
      for (var n = 0; n < keys.length; n++) {
        if (parseInt(keys[n], 10) !== n + 1) return false;
      }
      return true;
    }
    return false;
  }

  function isRecommendedCell(v) {
    var s = stripMarkup(v);
    if (!s) return false;
    if (RECOMMEND_CELL_RE.test(s)) return true;
    return RECOMMEND_MARK_RE.test(s);
  }

  /** titleFrom(text, fallback) — heading/lead-in text trimmed to a card title. */
  function titleFrom(text, fallback) {
    var s = stripMarkup(text).replace(/[:：]\s*$/, '').trim();
    if (!s) return str(fallback) || DEFAULT_TITLE;
    if (s.length > 90) s = s.slice(0, 87).replace(/\s+\S*$/, '') + '…';
    return s;
  }

  /** fingerprint(s) — FNV-1a, base36. Stable id for the selection store. */
  function fingerprint(s) {
    var h = 0x811c9dc5;
    var t = str(s);
    for (var i = 0; i < t.length; i++) {
      h ^= t.charCodeAt(i);
      h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0;
    }
    return h.toString(36);
  }

  /**
   * matrixID(model) — identity is the question plus the option keys/labels, so
   * the same matrix re-rendered (reload, virtual-list rematerialize, history
   * replay) keeps the owner's selection, while an edited matrix gets a new id
   * rather than inheriting a stale answer.
   */
  function matrixID(model) {
    var m = model || {};
    var parts = [str(m.title)];
    (m.options || []).forEach(function (o) {
      parts.push(str(o.key) + '=' + str(o.label));
    });
    return fingerprint(parts.join('|'));
  }

  // ── table parsing ──────────────────────────────────────────────────

  function normalizeGrid(headers, rows) {
    var h = (headers || []).map(function (c) { return stripMarkup(c); });
    var r = (rows || []).map(function (row) {
      return (row || []).map(function (c) { return stripMarkup(c); });
    });
    return { headers: h, rows: r };
  }

  function detailCells(headers, cells, skip) {
    var out = [];
    for (var i = 0; i < cells.length; i++) {
      if (skip.indexOf(i) !== -1) continue;
      var v = cells[i];
      if (!v) continue;
      out.push({ name: headers[i] || '', value: v });
    }
    return out;
  }

  /** Row-oriented: one row per option (the common overseer shape). */
  function parseRowOriented(grid, title) {
    var headers = grid.headers;
    var rows = grid.rows;
    if (rows.length < MIN_OPTIONS) return null;

    var candidates = [];
    headers.forEach(function (h, i) { if (OPTION_HEADER_RE.test(h)) candidates.push(i); });
    if (candidates.indexOf(0) === -1) candidates.push(0);

    for (var c = 0; c < candidates.length; c++) {
      var col = candidates[c];
      var parsed = rows.map(function (row) { return parseOptionCell(row[col]); });
      if (parsed.some(function (p) { return !p; })) continue;
      if (!isConsecutiveKeys(parsed.map(function (p) { return p.key; }))) continue;

      var recCol = -1;
      headers.forEach(function (h, i) {
        if (recCol === -1 && i !== col && RECOMMEND_HEADER_RE.test(h)) recCol = i;
      });
      var labelCol = -1;
      headers.forEach(function (h, i) {
        if (labelCol === -1 && i !== col && i !== recCol && LABEL_HEADER_RE.test(h)) labelCol = i;
      });

      var skip = [col];
      if (recCol !== -1) skip.push(recCol);

      var options = rows.map(function (row, ri) {
        var p = parsed[ri];
        var label = p.label;
        if (!label && labelCol !== -1) label = row[labelCol];
        if (!label) {
          for (var i = 0; i < row.length && !label; i++) {
            if (i !== col && i !== recCol) label = row[i];
          }
        }
        var recommended = recCol !== -1
          ? isRecommendedCell(row[recCol])
          : row.some(function (v, i) { return i !== col && RECOMMEND_MARK_RE.test(v); });
        return {
          key: p.key,
          label: label || p.key,
          recommended: recommended,
          cells: detailCells(headers, row, labelCol !== -1 ? skip.concat([labelCol]) : skip),
        };
      });
      return { title: titleFrom(title, headers[col] || DEFAULT_TITLE), orientation: 'rows', options: options };
    }
    return null;
  }

  /** Column-oriented: options are the header cells, criteria are the rows. */
  function parseColumnOriented(grid, title) {
    var headers = grid.headers;
    var rows = grid.rows;
    if (headers.length < MIN_OPTIONS + 1 || !rows.length) return null;
    // First header must be the criterion gutter, not an option.
    if (headers[0] && parseOptionCell(headers[0])) return null;

    var parsed = headers.slice(1).map(parseOptionCell);
    if (parsed.some(function (p) { return !p; })) return null;
    if (!isConsecutiveKeys(parsed.map(function (p) { return p.key; }))) return null;

    var options = parsed.map(function (p, i) {
      var col = i + 1;
      var cells = [];
      var recommended = false;
      var label = p.label;
      rows.forEach(function (row) {
        var name = row[0] || '';
        var v = row[col] || '';
        if (RECOMMEND_HEADER_RE.test(name)) {
          if (isRecommendedCell(v)) recommended = true;
          return;
        }
        if (RECOMMEND_MARK_RE.test(v)) recommended = true;
        if (v) cells.push({ name: name, value: v });
      });
      if (!label && cells.length) label = cells[0].value;
      return { key: p.key, label: label || p.key, recommended: recommended, cells: cells };
    });
    return { title: titleFrom(title, headers[0] || DEFAULT_TITLE), orientation: 'columns', options: options };
  }

  /**
   * parseTable({headers, rows, title}) → model | null.
   *
   * model = {id, title, orientation, options:[{key,label,recommended,cells}]}
   * null means "leave this table alone" — the honest default for every table
   * that is not a bounded owner choice.
   */
  function parseTable(input) {
    var src = input || {};
    var grid = normalizeGrid(src.headers, src.rows);
    if (!grid.headers.length) return null;
    var model = parseRowOriented(grid, src.title) || parseColumnOriented(grid, src.title);
    if (!model) return null;
    if (model.options.length < MIN_OPTIONS) return null;
    model.id = matrixID(model);
    model.recommendedKey = (model.options.filter(function (o) { return o.recommended; })[0] || {}).key || '';
    return model;
  }

  function findOption(model, key) {
    var k = str(key).toUpperCase();
    return ((model && model.options) || []).filter(function (o) {
      return String(o.key).toUpperCase() === k;
    })[0] || null;
  }

  // ── durable selection store ────────────────────────────────────────

  /** parseStore(raw) — malformed/absent storage reads as empty, never throws. */
  function parseStore(raw) {
    var empty = { v: 1, choices: {} };
    var s = str(raw).trim();
    if (!s) return empty;
    var parsed;
    try { parsed = JSON.parse(s); } catch (_) { return empty; }
    if (!parsed || typeof parsed !== 'object' || !parsed.choices ||
        typeof parsed.choices !== 'object') {
      return empty;
    }
    var out = { v: 1, choices: {} };
    Object.keys(parsed.choices).forEach(function (id) {
      var c = parsed.choices[id];
      if (!c || typeof c !== 'object' || !str(c.key)) return;
      out.choices[id] = {
        key: str(c.key).toUpperCase(),
        label: str(c.label),
        title: str(c.title),
        at: typeof c.at === 'number' ? c.at : 0,
        sent: !!c.sent,
      };
    });
    return out;
  }

  function serializeStore(store) {
    return JSON.stringify(store && store.choices ? store : { v: 1, choices: {} });
  }

  /**
   * recordChoice(store, model, key, at, opts) → new store (input untouched).
   * Oldest entries are evicted past MAX_CHOICES so the key cannot grow without
   * bound over a long-lived browser profile.
   */
  function recordChoice(store, model, key, at, opts) {
    var base = parseStore(serializeStore(store));
    var opt = findOption(model, key);
    if (!model || !model.id || !opt) return base;
    base.choices[model.id] = {
      key: opt.key,
      label: opt.label,
      title: str(model.title),
      at: typeof at === 'number' ? at : 0,
      sent: !!(opts && opts.sent),
    };
    var ids = Object.keys(base.choices);
    if (ids.length > MAX_CHOICES) {
      ids.sort(function (a, b) { return (base.choices[a].at || 0) - (base.choices[b].at || 0); });
      ids.slice(0, ids.length - MAX_CHOICES).forEach(function (id) { delete base.choices[id]; });
    }
    return base;
  }

  /** choiceFor(store, id) → stored entry | null. */
  function choiceFor(store, id) {
    var s = store && store.choices ? store.choices : {};
    return s[str(id)] || null;
  }

  // ── owner-facing text ──────────────────────────────────────────────

  /**
   * replyText(model, key) — the agent-actionable reply. Naming the option key
   * AND its label keeps it readable when the agent's table has scrolled far up
   * the transcript; calling out a pick that differs from the recommendation
   * spares the next turn a round of "are you sure".
   */
  function replyText(model, key) {
    var opt = findOption(model, key);
    if (!opt) return '';
    var title = str(model.title) || DEFAULT_TITLE;
    var s = 'Decision — ' + title + ': **' + opt.key + '**';
    if (opt.label && opt.label !== opt.key) s += ' — ' + opt.label;
    var recKey = str(model.recommendedKey);
    if (opt.recommended) {
      s += ' (your recommended option)';
    } else if (recKey) {
      var rec = findOption(model, recKey);
      s += ' (not the recommended ' + recKey + (rec && rec.label !== rec.key ? ' — ' + rec.label : '') + ')';
    }
    return s + '.';
  }

  /** statusText(model, choice) — the footer line under the options. */
  function statusText(model, choice) {
    if (!choice || !choice.key) {
      var n = ((model && model.options) || []).length;
      return 'Pick one of ' + n + ' options';
    }
    var opt = findOption(model, choice.key);
    var label = (opt && opt.label) || choice.label || '';
    var head = 'Selected ' + choice.key + (label && label !== choice.key ? ' — ' + label : '');
    return head + (choice.sent ? ' · sent' : ' · not sent yet');
  }

  return {
    STORAGE_KEY: STORAGE_KEY,
    MAX_CHOICES: MAX_CHOICES,
    MIN_OPTIONS: MIN_OPTIONS,
    DEFAULT_TITLE: DEFAULT_TITLE,
    stripMarkup: stripMarkup,
    parseOptionCell: parseOptionCell,
    isConsecutiveKeys: isConsecutiveKeys,
    isRecommendedCell: isRecommendedCell,
    titleFrom: titleFrom,
    fingerprint: fingerprint,
    matrixID: matrixID,
    parseTable: parseTable,
    findOption: findOption,
    parseStore: parseStore,
    serializeStore: serializeStore,
    recordChoice: recordChoice,
    choiceFor: choiceFor,
    replyText: replyText,
    statusText: statusText,
  };
}));
