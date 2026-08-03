// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Activity-strip tool-arg summaries (🎯T116). DOM-free for Node tests.
// Prefer real argument values over Object.keys dumps for nested MCP payloads.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ToolSummary = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var MAX_LEN = 60;

  // Content-ish keys first; tool_name is a label (used with nested tool_input).
  var PREFERRED_KEYS = [
    'query', 'path', 'command', 'title', 'name', 'text', 'url', 'id', 'tool_name',
  ];

  function collapse(s) {
    return String(s).replace(/\s+/g, ' ').trim();
  }

  function truncate(s, n) {
    s = collapse(s);
    if (!s) return '';
    n = n == null ? MAX_LEN : n;
    return s.length > n ? s.slice(0, n - 3) + '...' : s;
  }

  // Non-empty string, or short stringifiable scalar (number / boolean).
  function asUsefulString(v) {
    if (typeof v === 'string') {
      var t = collapse(v);
      return t || null;
    }
    if (typeof v === 'number' && isFinite(v)) return String(v);
    if (typeof v === 'boolean') return String(v);
    return null;
  }

  function hasOwn(obj, k) {
    return Object.prototype.hasOwnProperty.call(obj, k);
  }

  function pickFromObject(obj, depth) {
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return null;

    var hasToolInput = hasOwn(obj, 'tool_input')
      && obj.tool_input != null
      && typeof obj.tool_input === 'object'
      && !Array.isArray(obj.tool_input);

    // 1. Preferred keys (skip bare tool_name when tool_input is present —
    //    we combine them after nesting so the query/path wins).
    for (var i = 0; i < PREFERRED_KEYS.length; i++) {
      var k = PREFERRED_KEYS[i];
      if (!hasOwn(obj, k)) continue;
      if (k === 'tool_name' && hasToolInput) continue;
      var pref = asUsefulString(obj[k]);
      if (pref) return truncate(pref, MAX_LEN);
    }

    // 2. Nested tool_input (one level), optionally prefixed with tool_name.
    if (depth < 1 && hasToolInput) {
      var nested = pickFromObject(obj.tool_input, depth + 1);
      if (nested) {
        var tn = asUsefulString(obj.tool_name);
        if (tn) return truncate(tn + ': ' + nested, MAX_LEN);
        return nested;
      }
    }

    // 3. Any other non-empty string / short scalar at this level.
    var keys = Object.keys(obj);
    for (var j = 0; j < keys.length; j++) {
      var s = asUsefulString(obj[keys[j]]);
      if (s) return truncate(s, MAX_LEN);
    }

    // 4. One nesting level into other plain objects (not arrays).
    if (depth < 1) {
      for (var n = 0; n < keys.length; n++) {
        var v = obj[keys[n]];
        if (v && typeof v === 'object' && !Array.isArray(v)) {
          var deeper = pickFromObject(v, depth + 1);
          if (deeper) return deeper;
        }
      }
    }

    return null;
  }

  // summariseInput(input) → short single-line value gist (never bare key lists
  // when any useful value exists at this level or one nest).
  function summariseInput(input) {
    if (input == null) return '';
    if (typeof input === 'string') return truncate(input, MAX_LEN);
    if (typeof input !== 'object') {
      var scalar = asUsefulString(input);
      return scalar ? truncate(scalar, MAX_LEN) : '';
    }
    if (Array.isArray(input)) {
      for (var a = 0; a < input.length; a++) {
        var item = summariseInput(input[a]);
        if (item) return item;
      }
      return '';
    }
    return pickFromObject(input, 0) || '';
  }

  return {
    MAX_LEN: MAX_LEN,
    PREFERRED_KEYS: PREFERRED_KEYS,
    summariseInput: summariseInput,
    truncate: truncate,
  };
}));
