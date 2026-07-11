// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure chat-event helpers shared by the browser UI and headless tests.
// Keep this file free of DOM / transport so Node can require() it.

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
    return TERMINAL_STOPS.has(stopReason(m));
  }

  function assistantTextBlocks(m) {
    const content = m && m.message && m.message.content;
    if (!Array.isArray(content)) return [];
    return content.filter(c => c && c.type === 'text' && c.text);
  }

  function hasAssistantText(m) {
    return assistantTextBlocks(m).length > 0;
  }

  // shouldClearWorking mirrors web/index.html handle() for the working
  // indicator. Returns true when the indicator must be removed.
  function shouldClearWorking(m) {
    if (!m || !m.type) return false;
    if (m.type === 'system') return true;
    if (m.type !== 'assistant') return false;
    const content = m.message && m.message.content;
    if (!Array.isArray(content)) return false;
    const hasText = hasAssistantText(m);
    const stop = stopReason(m);
    const terminal = TERMINAL_STOPS.has(stop);
    return terminal || (hasText && !stop);
  }

  // workingLifecycle runs a synthetic turn through the pure rules and
  // returns whether working is still on at the end. Used by hermetic tests.
  function workingLifecycle(events) {
    let working = true; // set true when the user sends
    for (const m of events) {
      if (shouldClearWorking(m)) working = false;
    }
    return working;
  }

  return {
    stopReason,
    isTerminalStop,
    assistantTextBlocks,
    hasAssistantText,
    shouldClearWorking,
    workingLifecycle,
    TERMINAL_STOPS,
  };
}));
