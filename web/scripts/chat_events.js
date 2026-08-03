// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure chat-event helpers shared by the browser UI and headless tests.
// Keep this file free of DOM / transport so Node can require() it.
//
// Grok ACP streams many assistant text chunks (often one token each),
// then a terminal empty-content end_turn. The UI must:
//   1. Keep "working" until a terminal stop (not the first text chunk)
//   2. Coalesce all text chunks into a single jevons bubble per turn

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ChatEvents = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const TERMINAL_STOPS = new Set(['end_turn', 'stop_sequence', 'max_tokens']);

  function stopReason(m) {
    const msg = m && m.message;
    if (!msg) return '';
    return msg.stop_reason || msg.stopReason || '';
  }

  function isTerminalStop(m) {
    return m && m.type === 'assistant' && TERMINAL_STOPS.has(stopReason(m));
  }

  function assistantTextBlocks(m) {
    const content = m && m.message && m.message.content;
    if (!Array.isArray(content)) return [];
    return content.filter(c => c && c.type === 'text' && c.text);
  }

  function hasAssistantText(m) {
    return assistantTextBlocks(m).length > 0;
  }

  // ── Join-time segment repair (🎯T147 fence + 🎯T161 paragraph) ──
  // ACP can emit separate assistant segments: prose ending without NL,
  // then (after tool_result) a fence or a new paragraph. Naive
  // `+=` / join('') smushes into `snippet.```cpp` or fused paragraphs
  // with no space/break. T145 ensureFenceNewlines is display-only;
  // this is definitive repair at every segment edge.
  //
  // Do not invent mid-sentence breaks: token glue ("Hello"+".", "a"+"b")
  // and same-paragraph continuations that already carry a leading space
  // ("Done."+" More") stay bare concat.

  /** Sentence end: .!? with optional closing quotes/brackets (no trailing space). */
  const SENTENCE_END_RE = /[.!?]['"»\u201d\u2019\)\]]*$/;
  /** Capital / emphasis-capital paragraph start (no leading whitespace). */
  const PARA_START_RE =
    /^([A-Z\u00C0-\u024F]|(\*\*|__|[_*])[A-Z\u00C0-\u024F])/;

  /**
   * True when `next` looks like a real segment (paragraph/block), not a
   * single stream token. Token streams often emit "Hello" + "." + "What"
   * with no spaces — must stay bare-concat (screenshot regression).
   * ACP segment edges after tool_result are multi-word (or complete short
   * sentences like "Done.").
   *
   * @param {string} next
   * @returns {boolean}
   */
  function looksLikeParagraphSegment(next) {
    if (/\s/.test(next)) return true; // multi-word / multi-line segment
    if (next.length >= 2 && SENTENCE_END_RE.test(next)) return true; // "Yes." "Done!"
    return false;
  }

  /**
   * True when a bare concat would fuse a fence or paragraph boundary.
   * T147 fence rule is a subset; T161 generalizes to paragraph/block edges.
   * Requires no existing line break on either side of the join.
   *
   * @param {string} prev
   * @param {string} next
   * @returns {boolean}
   */
  function needsJoinBreak(prev, next) {
    if (/[\n\r]$/.test(prev) || /^[\n\r]/.test(next)) return false;
    // Fence (T147 subset) and common markdown block openers always need a break.
    if (/^```/.test(next)) return true;
    if (/^(#{1,6}\s|[-*+]\s|\d+\.\s|>\s)/.test(next)) return true;
    // Paragraph (T161): sentence-ending prev + capital-start next that looks
    // like a segment (not a bare token). Leading space on next ⇒ continue.
    if (
      SENTENCE_END_RE.test(prev) &&
      PARA_START_RE.test(next) &&
      looksLikeParagraphSegment(next)
    ) {
      return true;
    }
    return false;
  }

  /**
   * Coalesce two assistant text segments at a join boundary.
   * Inserts a blank line before fence openers (T147) and between
   * paragraph-shaped segments that would otherwise smash (T161).
   *
   * @param {string|null|undefined} prev
   * @param {string|null|undefined} next
   * @returns {string}
   */
  function coalesceAssistantText(prev, next) {
    prev = String(prev == null ? '' : prev);
    next = String(next == null ? '' : next);
    if (!prev) return next;
    if (!next) return prev;
    if (needsJoinBreak(prev, next)) return prev + '\n\n' + next;
    return prev + next;
  }

  /**
   * Join an array of assistant text parts with segment-edge fence/paragraph repair.
   * Prefer this over `.join('')` for multi-block assistant content.
   *
   * @param {Array<string|null|undefined>|null|undefined} parts
   * @returns {string}
   */
  function joinAssistantTexts(parts) {
    return (parts || []).reduce((acc, p) => coalesceAssistantText(acc, p), '');
  }

  // shouldClearWorking: only end-of-turn signals. Mid-stream text chunks
  // must NOT clear — clearing early drops workingEl and used to force a
  // new bubble per token (the "Hello / . / What / do / you / need / ?"
  // regression).
  function shouldClearWorking(m) {
    if (!m || !m.type) return false;
    if (m.type === 'system') return true;
    if (m.type !== 'assistant') return false;
    const content = m.message && m.message.content;
    // Reject non-array content (raw ACP shapes).
    if (!Array.isArray(content)) return false;
    return TERMINAL_STOPS.has(stopReason(m));
  }

  // Pure stream coalescer — models bubble count without DOM.
  // Used as the oracle for multi-chunk turns.
  function createTurnState() {
    return {
      working: false,
      userTexts: [],
      // Each entry is one jevons bubble's accumulated raw text.
      assistantBubbles: [],
      // Index of the open stream bubble, or -1 when sealed.
      openStream: -1,
    };
  }

  function applyChatEvent(state, m) {
    if (!m || !m.type) return state;

    if (m.type === 'user') {
      const content = m.message && m.message.content;
      if (typeof content === 'string' && content) {
        const last = state.userTexts[state.userTexts.length - 1];
        if (last === content) return state; // dedupe echo + ACP user chunk
        state.userTexts.push(content);
        state.working = true;
        state.openStream = -1; // new user turn seals prior stream
      }
      return state;
    }

    if (m.type === 'assistant') {
      const blocks = assistantTextBlocks(m);
      for (const b of blocks) {
        if (state.openStream >= 0) {
          state.assistantBubbles[state.openStream] = coalesceAssistantText(
            state.assistantBubbles[state.openStream],
            b.text,
          );
        } else {
          state.assistantBubbles.push(b.text);
          state.openStream = state.assistantBubbles.length - 1;
        }
      }
      if (shouldClearWorking(m)) {
        state.working = false;
        state.openStream = -1;
      }
      return state;
    }

    if (m.type === 'system') {
      state.working = false;
      state.openStream = -1;
    }
    return state;
  }

  function applyChatEvents(events) {
    const state = createTurnState();
    for (const m of events) applyChatEvent(state, m);
    return state;
  }

  // workingLifecycle: final working flag after events (send already set true).
  function workingLifecycle(events) {
    const state = createTurnState();
    state.working = true;
    for (const m of events) applyChatEvent(state, m);
    return state.working;
  }

  return {
    stopReason,
    isTerminalStop,
    assistantTextBlocks,
    hasAssistantText,
    coalesceAssistantText,
    joinAssistantTexts,
    shouldClearWorking,
    workingLifecycle,
    createTurnState,
    applyChatEvent,
    applyChatEvents,
    TERMINAL_STOPS,
  };
}));
