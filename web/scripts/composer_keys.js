// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Composer Home/End caret vs jump-to-bottom policy (🎯T126).
// DOM-free so Node hermetic tests can require(); index.html wires the
// caret apply + jump gate.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ComposerKeys = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Field-content policy (owner: start/end of the *message box*, not
  // line-local and not transcript scroll):
  //   Home → caret/selection anchor at 0
  //   End  → caret/selection at value.length

  function isComposerTextControl(tagName) {
    const t = String(tagName || '').toUpperCase();
    return t === 'TEXTAREA' || t === 'INPUT';
  }

  function caretAfterHome(_value, _selectionStart) {
    return 0;
  }

  function caretAfterEnd(value, _selectionStart) {
    return String(value == null ? '' : value).length;
  }

  // Pure selection result for Home/End (and Shift+ variants).
  // Returns null when the key is not a composer caret key we own.
  // Ignores Meta/Ctrl chords so Cmd/Ctrl+ArrowDown jump still works
  // via the document-level jump handler; plain Home/End (optional
  // Shift for extend) are ours.
  function selectionAfterHomeEnd(key, value, selStart, selEnd, mods) {
    const m = mods || {};
    if (m.metaKey || m.ctrlKey || m.altKey) return null;
    const start = typeof selStart === 'number' ? selStart : 0;
    const end = typeof selEnd === 'number' ? selEnd : start;
    const len = String(value == null ? '' : value).length;
    if (key === 'Home') {
      if (m.shiftKey) return { start: 0, end: end };
      return { start: 0, end: 0 };
    }
    if (key === 'End') {
      if (m.shiftKey) return { start: start, end: len };
      return { start: len, end: len };
    }
    return null;
  }

  // Document-level jump-to-bottom gate (🎯T119 + 🎯T126).
  // isJumpHotkey(key, mods) is typically VirtualList.isJumpToBottomHotkey.
  // When the main composer (or any text field) is focused, plain End must
  // not jump — only move the caret. Explicit jump chords
  // (Meta/Ctrl+ArrowDown) still jump.
  function shouldAllowJumpToBottom(key, mods, opts) {
    const m = mods || {};
    const o = opts || {};
    const isJump = typeof o.isJumpHotkey === 'function'
      ? o.isJumpHotkey(key, m)
      : defaultIsJumpToBottomHotkey(key, m);
    if (!isJump) return false;
    const textFocused = !!(o.composerFocused || o.inTextField);
    if (textFocused && key === 'End' && !m.metaKey && !m.ctrlKey) {
      return false;
    }
    return true;
  }

  function defaultIsJumpToBottomHotkey(key, mods) {
    const m = mods || {};
    if (key === 'End' && !m.altKey && !m.shiftKey) return true;
    if (key === 'ArrowDown' && (m.metaKey || m.ctrlKey) && !m.altKey && !m.shiftKey) {
      return true;
    }
    return false;
  }

  // True when active element is the main composer control (by id or ref).
  function isComposerFocused(activeEl, composerEl) {
    if (!activeEl || !composerEl) return false;
    return activeEl === composerEl;
  }

  return {
    isComposerTextControl: isComposerTextControl,
    caretAfterHome: caretAfterHome,
    caretAfterEnd: caretAfterEnd,
    selectionAfterHomeEnd: selectionAfterHomeEnd,
    shouldAllowJumpToBottom: shouldAllowJumpToBottom,
    isComposerFocused: isComposerFocused,
    // Exported for hermetic parity with VirtualList jump keys.
    defaultIsJumpToBottomHotkey: defaultIsJumpToBottomHotkey,
  };
}));
