// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Typed fleet-message envelopes (🎯T509). DOM-free so Node hermetics can
// require() it. The cockpit paints a compact header instead of dumping the
// ```jevons fence as a code block; the authored form stays in the journal.
//
// Fence info string is `jevons`. Slot lines use the `jevons:` sigil. YAML
// front matter (`---`) is not this format — mainstream markdown renderers
// strip or hide it.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.JevonsEnvelope = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var FENCE_OPEN = /^```jevons\b[^\n]*\n/;
  var SIGIL = /^jevons:\s+(\S+)\s*(.*)$/i;
  var ALREADY = /^<div class="jevons-envelope\b/;

  var AGENT_PREFIX = /^\[Agent [^\]]+ responded\]\r?\n/;
  var BANNER_PREFIX = /^⚠ (?:FALSE-GREEN|ENVELOPE CHECK)[^\n]*(?:\n [^\n]*)*\n*/;
  var IDENTITY_PREFIX = /^\[Who you are[\s\S]*?\n\n/;

  function stripPrefixes(text) {
    var s = String(text == null ? '' : text);
    var prev;
    do {
      prev = s;
      s = s.replace(AGENT_PREFIX, '');
      s = s.replace(BANNER_PREFIX, '');
      s = s.replace(IDENTITY_PREFIX, '');
    } while (s !== prev);
    return s;
  }

  function parse(text) {
    if (text == null || text === '') return null;
    var body = stripPrefixes(text);
    var open = FENCE_OPEN.exec(body);
    if (!open) return null;
    var rest = body.slice(open[0].length);
    var closeAt = rest.indexOf('\n```');
    if (closeAt < 0) {
      if (rest === '```' || rest.slice(-4) === '\n```') {
        rest = rest === '```' ? '' : rest.slice(0, -4);
        closeAt = rest.length;
      } else {
        return { incomplete: true };
      }
    }
    var fence = rest.slice(0, closeAt);
    var payload = rest.slice(closeAt);
    if (payload.indexOf('\n```') === 0) payload = payload.slice(4);
    payload = payload.replace(/^\n/, '');
    var slots = {};
    var lines = fence.split('\n');
    for (var i = 0; i < lines.length; i++) {
      var trim = lines[i].trim();
      if (!trim) continue;
      var m = SIGIL.exec(trim);
      if (!m) continue;
      var key = m[1].toLowerCase();
      var value = (m[2] || '').trim();
      if (key === 'oracle') {
        applyOracle(slots, value);
      } else {
        slots[key] = value;
      }
    }
    return { slots: slots, payload: payload, incomplete: false };
  }

  function applyOracle(slots, value) {
    var toks = value.split(/\s+/);
    for (var i = 0; i < toks.length; i++) {
      var cut = toks[i].indexOf('=');
      if (cut < 0) {
        if (!slots.sha && /^[0-9a-f]{7,40}$/i.test(toks[i])) slots.sha = toks[i];
        continue;
      }
      var k = toks[i].slice(0, cut).toLowerCase();
      var v = toks[i].slice(cut + 1);
      if (k === 'sha') slots.sha = v;
      else if (k === 'gate-id' || k === 'gateid' || k === 'gate') slots['gate-id'] = v;
      else if (k === 'daily') slots.daily = v;
    }
  }

  function headerHTML(parsed) {
    if (!parsed || !parsed.slots) return '';
    var s = parsed.slots;
    var kind = s.kind || 'envelope';
    var parts = ['<div class="jevons-envelope" data-kind="' + escapeAttr(kind) + '">'];
    parts.push('<span class="env-kind">' + escapeText(kind) + '</span>');
    if (s.target) {
      parts.push('<span class="env-target">' + escapeText(s.target) + '</span>');
    }
    if (s.verdict) {
      var vclass = 'env-verdict env-verdict-' + String(s.verdict).replace(/[^A-Za-z]/g, '');
      parts.push('<span class="' + vclass + '">' + escapeText(s.verdict) + '</span>');
    }
    if (s.status) {
      parts.push('<span class="env-status">' + escapeText(s.status) + '</span>');
    }
    if (s.risk && s.risk !== 'none') {
      parts.push('<span class="env-risk">' + escapeText(s.risk) + '</span>');
    }
    parts.push('</div>');
    return parts.join('');
  }

  function rewrite(text) {
    if (text == null || text === '') return text;
    var s = String(text);
    if (ALREADY.test(s.trim())) return s;
    var parsed = parse(s);
    if (!parsed || parsed.incomplete) return s;
    if (!parsed.slots || !parsed.slots.kind) return s;
    var header = headerHTML(parsed);
    var payload = parsed.payload || '';
    return header + (payload ? '\n\n' + payload : '');
  }

  function escapeText(v) {
    return String(v)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  function escapeAttr(v) {
    return escapeText(v).replace(/"/g, '&quot;');
  }

  // Test helper: what a Jekyll-style renderer would hide. Contrast only —
  // not used on the product path.
  function stripYamlFrontMatter(text) {
    var s = String(text == null ? '' : text);
    if (!s.startsWith('---')) return s;
    var end = s.indexOf('\n---', 3);
    if (end < 0) return s;
    return s.slice(end + 4).replace(/^\n/, '');
  }

  return {
    parse: parse,
    headerHTML: headerHTML,
    rewrite: rewrite,
    stripPrefixes: stripPrefixes,
    stripYamlFrontMatter: stripYamlFrontMatter,
  };
}));
