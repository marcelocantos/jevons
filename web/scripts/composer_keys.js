// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Composer Home/End caret vs jump-to-bottom policy (🎯T126 / 🎯T149).
// DOM-free so Node hermetic tests can require(); index.html wires the
// caret apply + jump gate.
//
// 🎯T149: operate on *effective* (Wispr seed-stripped) content bounds so
// seed-only EMPTY_SEED does not look like a Home/End no-op, and seed+text
// lands on the visible draft — not inside the invisible seed prefix.
// Meta/Ctrl+ArrowLeft/Right act as field ends (macOS / cross-platform habit).

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
  //   Home / Meta|Ctrl+ArrowLeft  → start of effective content
  //   End  / Meta|Ctrl+ArrowRight → end of effective content
  // Seed-only (effective empty): both collapse to the insert point
  // (after seed / value.length) so caret never sits *before* the seed
  // where typed characters would embed inside EMPTY_SEED.

  function isComposerTextControl(tagName) {
    const t = String(tagName || '').toUpperCase();
    return t === 'TEXTAREA' || t === 'INPUT';
  }

  /**
   * Effective caret bounds for field Home/End.
   * @param {string} value raw composer value (may include EMPTY_SEED prefix)
   * @param {{
   *   seedPrefixLen?: number,
   *   effectiveLength?: number,
   *   isSeedOnly?: boolean
   * }} [opts]
   * @returns {{ home: number, end: number }}
   */
  function contentCaretBounds(value, opts) {
    const s = String(value == null ? '' : value);
    const o = opts || {};
    let seedLen = 0;
    if (typeof o.seedPrefixLen === 'number' && o.seedPrefixLen > 0) {
      seedLen = Math.min(Math.floor(o.seedPrefixLen), s.length);
    }
    let effectiveLen;
    if (typeof o.effectiveLength === 'number' && o.effectiveLength >= 0) {
      effectiveLen = o.effectiveLength;
    } else {
      effectiveLen = Math.max(0, s.length - seedLen);
    }
    const seedOnly = o.isSeedOnly === true
      || (seedLen > 0 && effectiveLen === 0)
      || (s.length > 0 && seedLen > 0 && s.length === seedLen);

    if (!s.length || seedOnly || effectiveLen === 0) {
      // Empty effective content: one resting insert point (after seed).
      return { home: s.length, end: s.length };
    }
    // Real draft: Home at first effective char (skip seed prefix if any).
    return { home: seedLen, end: s.length };
  }

  function caretAfterHome(value, _selectionStart, opts) {
    return contentCaretBounds(value, opts).home;
  }

  function caretAfterEnd(value, _selectionStart, opts) {
    return contentCaretBounds(value, opts).end;
  }

  /**
   * Pure selection result for Home/End and Meta|Ctrl+ArrowLeft/Right
   * (and Shift+ variants). Returns null when the key is not a composer
   * caret key we own (e.g. plain ArrowLeft, Meta+ArrowDown for jump).
   *
   * @param {string} key
   * @param {string} value
   * @param {number} selStart
   * @param {number} selEnd
   * @param {{ metaKey?: boolean, ctrlKey?: boolean, altKey?: boolean, shiftKey?: boolean }} mods
   * @param {{ seedPrefixLen?: number, effectiveLength?: number, isSeedOnly?: boolean }} [opts]
   */
  function selectionAfterHomeEnd(key, value, selStart, selEnd, mods, opts) {
    const m = mods || {};
    // Alt: leave Option+Arrow paragraph / browser defaults alone.
    if (m.altKey) return null;

    const metaChord = !!(m.metaKey || m.ctrlKey);
    let goHome = false;
    let goEnd = false;

    if (key === 'Home') {
      goHome = true;
    } else if (key === 'End') {
      goEnd = true;
    } else if (key === 'ArrowLeft' && metaChord) {
      // macOS Cmd+Left (and Ctrl+Left) → field start of effective content.
      goHome = true;
    } else if (key === 'ArrowRight' && metaChord) {
      goEnd = true;
    } else {
      return null;
    }

    const start = typeof selStart === 'number' ? selStart : 0;
    const end = typeof selEnd === 'number' ? selEnd : start;
    const bounds = contentCaretBounds(value, opts);
    const home = bounds.home;
    const fieldEnd = bounds.end;

    if (goHome) {
      if (m.shiftKey) {
        // Extend toward start of effective content; clamp anchor into bounds.
        const anchor = Math.max(end, home);
        return { start: home, end: anchor };
      }
      return { start: home, end: home };
    }
    if (goEnd) {
      if (m.shiftKey) {
        const anchor = Math.min(start, fieldEnd);
        return { start: anchor, end: fieldEnd };
      }
      return { start: fieldEnd, end: fieldEnd };
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
    contentCaretBounds: contentCaretBounds,
    caretAfterHome: caretAfterHome,
    caretAfterEnd: caretAfterEnd,
    selectionAfterHomeEnd: selectionAfterHomeEnd,
    shouldAllowJumpToBottom: shouldAllowJumpToBottom,
    isComposerFocused: isComposerFocused,
    // Exported for hermetic parity with VirtualList jump keys.
    defaultIsJumpToBottomHotkey: defaultIsJumpToBottomHotkey,
  };
}));
