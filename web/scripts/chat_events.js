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

  // ── Segment-edge join (🎯T161; replaces T147 content sniff) ─────
  // Grok/ACP emits separate assistant *segments* at protocol boundaries
  // (tool rounds, multi-block content parts). Bare concat across those
  // edges smashes paragraphs/fences/lists. T145 ensureFenceNewlines is
  // display-only for already-fused fences.
  //
  // Zero content heuristics: join never inspects for `.`, capitals, ```,
  // headings, lists, or word counts. The caller knows it is a segment
  // edge from the event/protocol shape and uses joinAssistantSegments.
  // Intra-segment token deltas use appendAssistantStream (bare concat).

  /**
   * Join two assistant segments at a known segment boundary.
   * If neither side already has a line break at the boundary, insert a
   * blank line. Does not inspect markdown or prose shape.
   *
   * @param {string|null|undefined} prev
   * @param {string|null|undefined} next
   * @returns {string}
   */
  function joinAssistantSegments(prev, next) {
    prev = String(prev == null ? '' : prev);
    next = String(next == null ? '' : next);
    if (!prev) return next;
    if (!next) return prev;
    if (/[\n\r]$/.test(prev) || /^[\n\r]/.test(next)) return prev + next;
    return prev + '\n\n' + next;
  }

  /**
   * Intra-segment stream append (token/delta). Always bare concat.
   *
   * @param {string|null|undefined} prev
   * @param {string|null|undefined} next
   * @returns {string}
   */
  function appendAssistantStream(prev, next) {
    prev = String(prev == null ? '' : prev);
    next = String(next == null ? '' : next);
    return prev + next;
  }

  /**
   * Join an ordered list of known segments (multi-block content).
   * Prefer this over `.join('')` for multi-block assistant content.
   *
   * @param {Array<string|null|undefined>|null|undefined} parts
   * @returns {string}
   */
  function joinAssistantTexts(parts) {
    return (parts || []).reduce((acc, p) => joinAssistantSegments(acc, p), '');
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
  // segmentEdgePending: next text after non-text protocol content
  // (tool_use, tool_result) joins via joinAssistantSegments, not bare concat.
  function createTurnState() {
    return {
      working: false,
      userTexts: [],
      // Each entry is one jevons bubble's accumulated raw text.
      assistantBubbles: [],
      // Index of the open stream bubble, or -1 when sealed.
      openStream: -1,
      segmentEdgePending: false,
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
        state.segmentEdgePending = false;
      }
      return state;
    }

    if (m.type === 'tool_result' || m.type === 'result') {
      if (state.openStream >= 0) state.segmentEdgePending = true;
      return state;
    }

    if (m.type === 'assistant') {
      const content = m.message && m.message.content;
      // Walk content in order: non-text marks a segment edge for the next
      // text part; multiple text blocks in one message are segment edges
      // between them; continuous single-block frames stay bare-concat.
      if (Array.isArray(content)) {
        let textPartsThisEvent = 0;
        for (let i = 0; i < content.length; i++) {
          const c = content[i];
          if (!c) continue;
          if (c.type === 'text' && c.text) {
            if (state.openStream >= 0) {
              const atSegmentEdge =
                state.segmentEdgePending || textPartsThisEvent > 0;
              state.assistantBubbles[state.openStream] = atSegmentEdge
                ? joinAssistantSegments(
                  state.assistantBubbles[state.openStream],
                  c.text,
                )
                : appendAssistantStream(
                  state.assistantBubbles[state.openStream],
                  c.text,
                );
              state.segmentEdgePending = false;
            } else {
              state.assistantBubbles.push(c.text);
              state.openStream = state.assistantBubbles.length - 1;
              state.segmentEdgePending = false;
            }
            textPartsThisEvent += 1;
          } else if (c.type && c.type !== 'text') {
            // tool_use and other non-text content: next text is a new segment
            if (state.openStream >= 0) state.segmentEdgePending = true;
          }
        }
      }
      if (shouldClearWorking(m)) {
        state.working = false;
        state.openStream = -1;
        state.segmentEdgePending = false;
      }
      return state;
    }

    if (m.type === 'system') {
      state.working = false;
      state.openStream = -1;
      state.segmentEdgePending = false;
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
    joinAssistantSegments,
    appendAssistantStream,
    joinAssistantTexts,
    // Alias: only for known segment-edge joins (not token streams).
    coalesceAssistantText: joinAssistantSegments,
    shouldClearWorking,
    workingLifecycle,
    createTurnState,
    applyChatEvent,
    applyChatEvents,
    TERMINAL_STOPS,
  };
}));
