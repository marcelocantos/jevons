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

  // nextSelection(prevName, clickName) — toggle off if same name.
  function nextSelection(prevName, clickName) {
    if (!clickName) return null;
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
  // none, keep currentSelection when still present.
  function pickAutoSelect(prevList, nextList, currentSelection) {
    const news = detectNewAsides(prevList, nextList);
    if (news.length) return news[news.length - 1];
    if (currentSelection) {
      const still = (nextList || []).some(function (a) {
        return a && a.name === currentSelection;
      });
      if (still) return currentSelection;
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

  // mainChatMustNotContainFleetTraffic — oracle marker: product rule string.
  const MAIN_CHAT_IS_OWNER_OVERSEER_ONLY = true;

  return {
    nextSelection: nextSelection,
    detectNewAsides: detectNewAsides,
    pickAutoSelect: pickAutoSelect,
    turnsToLines: turnsToLines,
    paneModel: paneModel,
    MAIN_CHAT_IS_OWNER_OVERSEER_ONLY: MAIN_CHAT_IS_OWNER_OVERSEER_ONLY,
  };
}));
