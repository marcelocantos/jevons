// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// RHS agent/aside transcript inspect policy (🎯T124). DOM-free pure helpers
// for hermetic tests: selection transitions, auto-select on new aside, pane
// model from API turns. Main chat is never the sink for fleet monologue.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.AgentTranscript = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Overseer root uses main chat — never open RHS inspect for it (T124 residual).
  function isOverseer(name, purpose) {
    const n = String(name || '').toLowerCase();
    if (n === 'jevons') return true;
    const p = String(purpose || '').toLowerCase();
    return p === 'overseer';
  }

  // True when a fleet row click should open the transcript pane.
  function shouldOpenTranscript(name, purpose) {
    if (!name) return false;
    return !isOverseer(name, purpose);
  }

  // nextSelection(prevName, clickName, opts) — toggle off if same name.
  // opts.purpose optional; overseer clicks never select for inspect.
  function nextSelection(prevName, clickName, opts) {
    if (!clickName) return null;
    const purpose = opts && opts.purpose;
    if (!shouldOpenTranscript(clickName, purpose)) {
      // Clear selection if re-clicking overseer or any overseer click.
      return null;
    }
    if (prevName && prevName === clickName) return null;
    return String(clickName);
  }

  // detectNewAsides(prevList, nextList) → names of new purpose=aside agents
  // (order: stable name-sort of newly appeared asides).
  function detectNewAsides(prevList, nextList) {
    const prev = {};
    (prevList || []).forEach(function (a) {
      if (a && a.name) prev[a.name] = true;
    });
    const out = [];
    (nextList || []).forEach(function (a) {
      if (!a || !a.name) return;
      if (prev[a.name]) return;
      const p = String(a.purpose || '').toLowerCase();
      if (p === 'aside') out.push(a.name);
    });
    out.sort();
    return out;
  }

  // pickAutoSelect(prevList, nextList, currentSelection) → name|null
  // Prefer the last newly appeared aside (name-sorted: last = z-order); if
  // none, keep currentSelection when still present. Never auto-select overseer.
  function pickAutoSelect(prevList, nextList, currentSelection) {
    const news = detectNewAsides(prevList, nextList).filter(function (n) {
      return shouldOpenTranscript(n, 'aside');
    });
    if (news.length) return news[news.length - 1];
    if (currentSelection) {
      const row = (nextList || []).find(function (a) {
        return a && a.name === currentSelection;
      });
      if (row && shouldOpenTranscript(row.name, row.purpose)) return currentSelection;
    }
    return null;
  }

  // turnsToLines(turns) → [{role, text}] for pane render (filters empty).
  function turnsToLines(turns) {
    const out = [];
    (turns || []).forEach(function (t) {
      if (!t) return;
      const role = t.role === 'user' ? 'user' : (t.role === 'assistant' ? 'assistant' : (t.role || 'other'));
      const text = t.text == null ? '' : String(t.text);
      if (!text.trim() && role === 'other') return;
      out.push({ role: role, text: text });
    });
    return out;
  }

  // paneModel(name, payload) → { title, empty, lines, error }
  function paneModel(name, payload, err) {
    if (err) {
      return {
        title: name || '',
        empty: true,
        error: String(err.message || err),
        lines: [],
      };
    }
    const lines = turnsToLines(payload && payload.turns);
    return {
      title: (payload && payload.name) || name || '',
      empty: lines.length === 0,
      error: '',
      lines: lines,
      sessionId: (payload && payload.session_id) || '',
    };
  }

  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // 🎯T157: how one inspect turn body is painted.
  // Assistant → HTML via parseAssistantMarkdown (same seal path as main chat).
  // User/other → plain escaped text (CSS pre-wrap on .ai-turn.user .ai-text).
  // deps.parseAssistantMarkdown(text) → html string; required for assistant.
  function paintInspectLineBody(role, text, deps) {
    deps = deps || {};
    const t = text == null ? '' : String(text);
    if (role === 'assistant') {
      const parse = deps.parseAssistantMarkdown;
      if (typeof parse === 'function') {
        return { mode: 'html', content: parse(t) };
      }
      // Missing parse: still never pretends to be markdown — plain escape.
      return { mode: 'text', content: t };
    }
    return { mode: 'text', content: t };
  }

  // Hermetic HTML fixture for #agent-inspect-body turn list (🎯T157).
  // deps.parseAssistantMarkdown mirrors index.html seal path (marked + T145).
  function paintInspectLinesHTML(lines, deps) {
    deps = deps || {};
    let html = '';
    (lines || []).forEach(function (line) {
      if (!line) return;
      const role = line.role || 'other';
      const roleLabel = role === 'user' ? 'User'
        : (role === 'assistant' ? 'Assistant' : (role || 'msg'));
      const body = paintInspectLineBody(role, line.text, deps);
      const bodyInner = body.mode === 'html'
        ? body.content
        : escapeHtml(body.content);
      html += '<div class="ai-turn ' + escapeHtml(role) + '">'
        + '<div class="ai-role">' + escapeHtml(roleLabel) + '</div>'
        + '<div class="ai-text">' + bodyInner + '</div>'
        + '</div>';
    });
    return html;
  }

  // mainChatMustNotContainFleetTraffic — oracle marker: product rule string.
  const MAIN_CHAT_IS_OWNER_OVERSEER_ONLY = true;

  return {
    isOverseer: isOverseer,
    shouldOpenTranscript: shouldOpenTranscript,
    nextSelection: nextSelection,
    detectNewAsides: detectNewAsides,
    pickAutoSelect: pickAutoSelect,
    turnsToLines: turnsToLines,
    paneModel: paneModel,
    escapeHtml: escapeHtml,
    paintInspectLineBody: paintInspectLineBody,
    paintInspectLinesHTML: paintInspectLinesHTML,
    MAIN_CHAT_IS_OWNER_OVERSEER_ONLY: MAIN_CHAT_IS_OWNER_OVERSEER_ONLY,
  };
}));
