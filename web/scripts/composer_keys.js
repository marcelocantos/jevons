// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Composer key policy (🎯T126 / 🎯T149 / 🎯T132).
// DOM-free so Node hermetic tests can require(); index.html wires caret
// apply, jump gate, and Enter-chord handling.
//
// 🎯T149: operate on *effective* (Wispr seed-stripped) content bounds so
// seed-only EMPTY_SEED does not look like a Home/End no-op, and seed+text
// lands on the visible draft — not inside the invisible seed prefix.
// Meta/Ctrl+ArrowLeft/Right act as field ends (macOS / cross-platform habit).
//
// 🎯T132 Enter chords (composer focused):
//   Ctrl+Enter  → immediate send / interject (never Alt+Enter — Firefox)
//   Alt+Enter empty (seed-only counts) → pop last owner message as edit seed
//   Alt+Enter non-empty → noop (must not steal Ctrl+Enter immediate-send)
//   plain Enter → send (enqueue while busy per SendQueue)
//   Shift+Enter → newline

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

  // ── 🎯T132 Enter-chord policy ─────────────────────────────────────
  // Pure classification for the composer keydown path.
  //
  // Returns:
  //   'newline'    — Shift+Enter (leave browser default)
  //   'send'       — plain Enter (or Meta+Enter): send / enqueue
  //   'interrupt'  — Ctrl+Enter: immediate send / interject while busy
  //   'pop_last'   — Alt+Enter when composer is effectively empty
  //   'noop'       — Alt+Enter with draft text (do not send)
  //   null         — not Enter (caller ignores)
  //
  // composerEmpty: true when there is no real draft ('' / whitespace /
  // Wispr seed-only). Caller passes WisprContext.isEffectivelyEmpty.

  /**
   * @param {string} key
   * @param {{ metaKey?: boolean, ctrlKey?: boolean, altKey?: boolean, shiftKey?: boolean }} mods
   * @param {{ composerEmpty?: boolean }} [opts]
   * @returns {'newline'|'send'|'interrupt'|'pop_last'|'noop'|null}
   */
  function classifyEnterAction(key, mods, opts) {
    if (key !== 'Enter') return null;
    const m = mods || {};
    const o = opts || {};
    if (m.shiftKey) return 'newline';

    // Ctrl+Enter wins as immediate-send even if other modifiers are held
    // (except Shift, already handled). Alt alone must never map here.
    if (m.ctrlKey) return 'interrupt';

    // Alt+Enter (no Ctrl): empty → pop last owner message; non-empty → noop.
    // Avoids Firefox Alt+Enter collisions on the send/interject path.
    if (m.altKey) {
      return o.composerEmpty ? 'pop_last' : 'noop';
    }

    // Plain Enter or Meta+Enter: normal send path (T113 enqueues when busy).
    return 'send';
  }

  /**
   * Most recent owner message from chronological history.
   * @param {Array<{text?: string, el?: *}>|null|undefined} history
   * @returns {{ text: string, index: number, el: * }|null}
   */
  function lastOwnerHistoryEntry(history) {
    if (!history || !history.length) return null;
    const index = history.length - 1;
    const entry = history[index];
    if (!entry) return null;
    const text = entry.text == null ? '' : String(entry.text);
    return { text: text, index: index, el: entry.el };
  }

  /**
   * 🎯T227: product-path last-owner resolve for Alt+Enter pop.
   *
   * Prefer in-memory msgHistory (WS replay + live user turns). When that is
   * empty or has no text (progressive hydrate paints `.msg.user` into the DOM
   * without pushing msgHistory; soft-reconnect / desync can leave the same
   * hole), fall back to chronological DOM-derived entries so empty Alt+Enter
   * still seeds the last owner bubble. Pure classifier alone never saw this —
   * classifyEnterAction stayed green while pop_last was a silent no-op.
   *
   * @param {Array<{text?: string, el?: *}>|null|undefined} history
   * @param {Array<{text?: string, el?: *}>|null|undefined} domEntries chronological owner bubbles
   * @returns {{ text: string, index: number, el: *, source: 'history'|'dom' }|null}
   */
  function resolveLastOwnerEntry(history, domEntries) {
    const fromHist = lastOwnerHistoryEntry(history);
    if (fromHist && fromHist.text) {
      return {
        text: fromHist.text,
        index: fromHist.index,
        el: fromHist.el,
        source: 'history',
      };
    }
    const fromDom = lastOwnerHistoryEntry(domEntries);
    if (fromDom && fromDom.text) {
      return {
        text: fromDom.text,
        index: fromDom.index,
        el: fromDom.el,
        source: 'dom',
      };
    }
    return null;
  }

  /**
   * Build [{text, el}] from owner bubble nodes (product: `.msg.user` with
   * `_layoutText`). Filters empty text. Order preserved (chronological DOM).
   * @param {Array<{_layoutText?: *, textContent?: *}>|null|undefined} nodes
   * @returns {Array<{text: string, el: *}>}
   */
  function ownerEntriesFromUserNodes(nodes) {
    const list = Array.isArray(nodes) ? nodes : [];
    const out = [];
    for (let i = 0; i < list.length; i++) {
      const el = list[i];
      if (!el) continue;
      let text = '';
      if (el._layoutText != null) text = String(el._layoutText);
      else if (el.textContent != null) text = String(el.textContent);
      if (!text) continue;
      out.push({ text: text, el: el });
    }
    return out;
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
    // 🎯T132
    classifyEnterAction: classifyEnterAction,
    lastOwnerHistoryEntry: lastOwnerHistoryEntry,
    // 🎯T227
    resolveLastOwnerEntry: resolveLastOwnerEntry,
    ownerEntriesFromUserNodes: ownerEntriesFromUserNodes,
  };
}));
