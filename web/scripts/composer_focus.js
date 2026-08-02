// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T127 — dedicated hotkey focuses main chat composer.
//
// Binding: plain `/` when focus is not already in an editable field
// (input / textarea / contenteditable). Same pattern as common chat UX
// (Slack-style): type `/` outside the box to jump back to the composer
// without hunting for the caret. Does not steal `/` while the owner is
// typing in the composer or any other text field (e.g. Mermaid paste).
//
// DOM-free policy so Node hermetic tests can require(); browser wires
// document keydown in index.html. No aggressive auto-refocus.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ComposerFocus = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Documented binding label for UI hints / help.
  const HOTKEY = '/';
  const HOTKEY_HINT = '/ focuses message box (when not typing)';
  const HOTKEY_DOC =
    'Press / anywhere outside an editable field to focus the main chat composer. ' +
    'Does not fire while typing in the composer or other text fields.';

  function modsOf(mods) {
    const m = mods || {};
    return {
      metaKey: !!m.metaKey,
      ctrlKey: !!m.ctrlKey,
      altKey: !!m.altKey,
      shiftKey: !!m.shiftKey,
    };
  }

  // True for the dedicated focus-composer chord: bare `/`, no modifiers.
  // Shift+/ would be `?` on US layouts; we key on event.key === '/'.
  function isFocusComposerHotkey(key, mods) {
    if (key !== HOTKEY && key !== 'Slash') return false;
    const m = modsOf(mods);
    // Allow shift only if the resolved key is still `/` (some layouts).
    if (m.metaKey || m.ctrlKey || m.altKey) return false;
    return true;
  }

  // True when the active element accepts character input (do not steal `/`).
  function isEditableTarget(el) {
    if (!el) return false;
    const tag = (el.tagName || '').toUpperCase();
    if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
    if (tag === 'INPUT') {
      // Non-textual inputs do not type `/`; still treat generic INPUT as
      // editable when type is missing or text-like.
      const t = String(el.type || 'text').toLowerCase();
      if (
        t === 'button' || t === 'submit' || t === 'reset' ||
        t === 'checkbox' || t === 'radio' || t === 'file' ||
        t === 'image' || t === 'range' || t === 'color' ||
        t === 'hidden'
      ) {
        return false;
      }
      return true;
    }
    if (el.isContentEditable === true) return true;
    const ce = el.contentEditable;
    if (ce === true || ce === 'true') return true;
    return false;
  }

  // Policy: should this keydown focus the main composer?
  function shouldFocusComposer(key, mods, activeElement) {
    if (!isFocusComposerHotkey(key, mods)) return false;
    if (isEditableTarget(activeElement)) return false;
    return true;
  }

  /**
   * Apply the hotkey. Mutates only via composerEl.focus() when applicable.
   * Returns { didFocus, reason } for hermetic assertions.
   *
   * @param {object} eventLike  { key, metaKey, ctrlKey, altKey, shiftKey }
   * @param {object} composerEl element with .focus() (main #input)
   * @param {object|null} activeElement document.activeElement (or mock)
   */
  function tryFocusComposer(eventLike, composerEl, activeElement) {
    const ev = eventLike || {};
    if (!composerEl || typeof composerEl.focus !== 'function') {
      return { didFocus: false, reason: 'no-composer' };
    }
    // Already focused: leave key alone so `/` types into the box.
    if (activeElement === composerEl) {
      return { didFocus: false, reason: 'already-focused' };
    }
    if (!shouldFocusComposer(ev.key, ev, activeElement)) {
      return { didFocus: false, reason: 'not-applicable' };
    }
    composerEl.focus();
    return { didFocus: true, reason: 'focused' };
  }

  return {
    HOTKEY: HOTKEY,
    HOTKEY_HINT: HOTKEY_HINT,
    HOTKEY_DOC: HOTKEY_DOC,
    isFocusComposerHotkey: isFocusComposerHotkey,
    isEditableTarget: isEditableTarget,
    shouldFocusComposer: shouldFocusComposer,
    tryFocusComposer: tryFocusComposer,
  };
}));
