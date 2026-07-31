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

  function hasToolUse(m) {
    const content = m && m.message && m.message.content;
    if (!Array.isArray(content)) return false;
    return content.some(c => c && c.type === 'tool_use');
  }

  // Empty terminal = cancelled-turn marker (no text, no tools). Used so
  // cancel-and-send can ignore the cancelled turn's end_turn without
  // swallowing the replacement turn's terminal.
  function isEmptyTerminal(m) {
    if (!isTerminalStop(m)) return false;
    return !hasAssistantText(m) && !hasToolUse(m);
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

  // shouldSuppressWorkingClear: when cancel-and-send is armed, only the
  // cancelled turn's empty end_turn is suppressed. A non-empty terminal
  // (text or tools) always clears — so force-idle without a cancel
  // end_turn cannot leave working stuck forever.
  function shouldSuppressWorkingClear(armed, m) {
    if (!armed) return false;
    if (m && m.type === 'status' && m.state === 'cancel_settled') return true;
    return isEmptyTerminal(m);
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
          state.assistantBubbles[state.openStream] += b.text;
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

  // followAfterPin models stick-to-bottom: while following, content growth
  // must keep us at the bottom; a tiny shortfall must not disable follow
  // when the pin flag is set (matches scrollDown _pinningScroll).
  function shouldStayFollowing(args) {
    const slack = args.slackPx == null ? 120 : args.slackPx;
    if (args.pinning) return true;
    return args.scrollTop + args.clientHeight >= args.scrollHeight - slack;
  }

  // Pure scroll pin: after content height grows, scrollTop becomes
  // scrollHeight - clientHeight when following.
  function pinFollow(state) {
    if (!state.following) return state;
    return {
      following: true,
      scrollTop: Math.max(0, state.scrollHeight - state.clientHeight),
      clientHeight: state.clientHeight,
      scrollHeight: state.scrollHeight,
    };
  }

  return {
    stopReason,
    isTerminalStop,
    assistantTextBlocks,
    hasAssistantText,
    hasToolUse,
    isEmptyTerminal,
    shouldClearWorking,
    shouldSuppressWorkingClear,
    workingLifecycle,
    createTurnState,
    applyChatEvent,
    applyChatEvents,
    shouldStayFollowing,
    pinFollow,
    TERMINAL_STOPS,
  };
}));
