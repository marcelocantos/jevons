// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// RHS agent/aside transcript inspect policy (🎯T124 / 🎯T205). DOM-free pure
// helpers for hermetic tests: selection transitions, auto-select on new aside,
// pane model, shared .msg body paint + scroll stickiness. Main chat is never
// the sink for fleet monologue.

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

  // Map inspect turn roles → main chat .msg role classes (🎯T205).
  function inspectToMsgRole(role) {
    if (role === 'user') return 'user';
    if (role === 'assistant') return 'jevons';
    return 'status';
  }

  // 🎯T205: body paint policy for one inspect turn (same product paths as main).
  // Assistant → HTML via parseAssistantMarkdown (sealed main-chat path).
  // User → HTML via renderUserText when provided (quotes/images); else plain text.
  // Other → plain text. msgRole is the .msg class role for shared chrome.
  // deps: { parseAssistantMarkdown?, renderUserText? }
  function paintInspectLineBody(role, text, deps) {
    deps = deps || {};
    const t = text == null ? '' : String(text);
    const msgRole = inspectToMsgRole(role);
    if (role === 'assistant') {
      const parse = deps.parseAssistantMarkdown;
      if (typeof parse === 'function') {
        return { mode: 'html', content: parse(t), msgRole: msgRole };
      }
      return { mode: 'text', content: t, msgRole: msgRole };
    }
    if (role === 'user') {
      const renderUser = deps.renderUserText;
      if (typeof renderUser === 'function') {
        return { mode: 'html', content: renderUser(t), msgRole: msgRole };
      }
      return { mode: 'text', content: t, msgRole: msgRole };
    }
    return { mode: 'text', content: t, msgRole: msgRole };
  }

  // Hermetic HTML fixture for #agent-inspect-body: main .msg bubble chrome (🎯T205).
  // deps.parseAssistantMarkdown / deps.renderUserText mirror index.html paths.
  function paintInspectLinesHTML(lines, deps) {
    deps = deps || {};
    let html = '';
    (lines || []).forEach(function (line) {
      if (!line) return;
      const role = line.role || 'other';
      const body = paintInspectLineBody(role, line.text, deps);
      const msgRole = body.msgRole || inspectToMsgRole(role);
      const bodyInner = body.mode === 'html'
        ? body.content
        : escapeHtml(body.content);
      html += '<div class="msg ' + escapeHtml(msgRole) + '">'
        + '<div class="msg-body">' + bodyInner + '</div>'
        + '</div>';
    });
    return html;
  }

  // Stable fingerprint for poll no-op (skip full replace when content unchanged).
  function linesFingerprint(lines) {
    let s = '';
    (lines || []).forEach(function (l, i) {
      if (!l) return;
      s += i + '\0' + (l.role || '') + '\0' + (l.text == null ? '' : String(l.text)) + '\n';
    });
    return s;
  }

  // 🎯T205: latched stick-to-bottom policy (Track | Free) — pure, reusable for
  // #agent-inspect-body. Mirrors main #messages: free-scroll is never yanked
  // to bottom on content growth; near-bottom / Track keeps following.
  //
  // Mode is latched, not re-derived from distance every frame:
  //   track — pin scrollTop to scrollHeight after updates
  //   free  — preserve scrollTop; growth never pins
  // Enter: boot / explicit enterTrack / arrive at bottom (ε entry only).
  // Leave: intentional scroll up (wheel / scroll metrics).
  function createScrollFollow(opts) {
    opts = opts || {};
    const eps = opts.eps != null ? Number(opts.eps) : 16;
    let mode = 'track'; // 'track' | 'free'
    let mayEnterFromGeometry = true;
    let lastScrollTop = 0;
    let lastScrollHeight = 0;
    let bookkeeping = 0;

    function distFromBottom(el) {
      if (!el) return 0;
      return el.scrollHeight - el.clientHeight - el.scrollTop;
    }
    function atBottom(el) {
      if (!el) return true;
      const room = el.scrollHeight - el.clientHeight;
      if (room <= 0) return true;
      return distFromBottom(el) <= eps;
    }
    function isTracking() { return mode === 'track'; }
    function getMode() { return mode; }
    function setMode(m) { mode = (m === 'free') ? 'free' : 'track'; }
    function enterTrack() {
      mode = 'track';
      mayEnterFromGeometry = true;
    }
    function leaveTrack(el) {
      mode = 'free';
      mayEnterFromGeometry = el ? distFromBottom(el) > eps : false;
    }
    function noteAwayFromBottom(el) {
      if (el && distFromBottom(el) > eps) mayEnterFromGeometry = true;
    }
    function tryEnterFromGeometry(el) {
      noteAwayFromBottom(el);
      if (mayEnterFromGeometry && atBottom(el)) enterTrack();
    }
    function noteMetrics(el) {
      if (!el) return;
      lastScrollTop = el.scrollTop;
      lastScrollHeight = el.scrollHeight;
    }
    function beginBookkeeping() { bookkeeping++; }
    function endBookkeeping() { bookkeeping = Math.max(0, bookkeeping - 1); }
    function onWheel(deltaY, el) {
      if (deltaY < 0) leaveTrack(el);
      else if (deltaY > 0) tryEnterFromGeometry(el);
    }
    function onScroll(el) {
      if (!el) return;
      if (bookkeeping > 0) {
        noteMetrics(el);
        return;
      }
      const top = el.scrollTop;
      const h = el.scrollHeight;
      if (top + 1 < lastScrollTop && h + 1 >= lastScrollHeight) {
        leaveTrack(el);
      } else {
        tryEnterFromGeometry(el);
      }
      noteMetrics(el);
    }
    function shouldPin() { return mode === 'track'; }
    // After content mutation: pin if tracking; else restore prevTop (free read).
    function applyAfterUpdate(el, prevTop) {
      if (!el) return;
      beginBookkeeping();
      try {
        if (mode === 'track') {
          el.scrollTop = el.scrollHeight;
        } else if (typeof prevTop === 'number' && isFinite(prevTop)) {
          el.scrollTop = prevTop;
        }
        noteMetrics(el);
      } finally {
        endBookkeeping();
      }
    }
    // Pure policy (no DOM): next scrollTop after update given metrics.
    function nextScrollTop(args) {
      args = args || {};
      const scrollHeight = Number(args.scrollHeight) || 0;
      const prevTop = args.prevTop;
      if (mode === 'track') return scrollHeight;
      if (typeof prevTop === 'number' && isFinite(prevTop)) return prevTop;
      return 0;
    }

    return {
      eps: eps,
      isTracking: isTracking,
      getMode: getMode,
      setMode: setMode,
      enterTrack: enterTrack,
      leaveTrack: leaveTrack,
      tryEnterFromGeometry: tryEnterFromGeometry,
      atBottom: atBottom,
      distFromBottom: distFromBottom,
      onWheel: onWheel,
      onScroll: onScroll,
      shouldPin: shouldPin,
      applyAfterUpdate: applyAfterUpdate,
      nextScrollTop: nextScrollTop,
      noteMetrics: noteMetrics,
      beginBookkeeping: beginBookkeeping,
      endBookkeeping: endBookkeeping,
    };
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
    inspectToMsgRole: inspectToMsgRole,
    paintInspectLineBody: paintInspectLineBody,
    paintInspectLinesHTML: paintInspectLinesHTML,
    linesFingerprint: linesFingerprint,
    createScrollFollow: createScrollFollow,
    MAIN_CHAT_IS_OWNER_OVERSEER_ONLY: MAIN_CHAT_IS_OWNER_OVERSEER_ONLY,
  };
}));
