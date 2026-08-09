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
// document keydown + focusComposer() call sites in index.html.
// 🎯T153: aggressive return-to-composer after pointer chrome (send, expand
// tab, route-switch, aside dismiss) — separate from the `/` hotkey.
// 🎯T366: Tab cycles the two message boxes (main #input ↔ sidebar
// #agent-inspect-input) when the sidebar composer is visible; when it is
// hidden the chord is not claimed, so normal focus order is untouched.

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

  /**
   * 🎯T153: force focus onto the main composer after common chrome actions
   * (send, expand/collapse click, route-switch, aside dismiss). Unconditional
   * — not gated on activeElement (unlike the `/` hotkey). DOM-free: caller
   * passes the #input element (or a mock with .focus()).
   *
   * @param {object} composerEl element with .focus() (main #input)
   * @returns {{ didFocus: boolean, reason: string }}
   */
  function focusComposer(composerEl) {
    if (!composerEl || typeof composerEl.focus !== 'function') {
      return { didFocus: false, reason: 'no-composer' };
    }
    composerEl.focus();
    return { didFocus: true, reason: 'focused' };
  }

  // ── 🎯T366 Tab cycle: main composer ↔ sidebar composer ────────────
  //
  // Two stops only. Tab and Shift+Tab both toggle: in a two-element cycle
  // forward and reverse land on the same partner, so the reverse chord is
  // the documented way back rather than a third state.
  //
  // The cycle is claimed only when focus already sits in one of the two
  // message boxes AND the partner can take focus. Anywhere else — and any
  // time the sidebar composer is hidden, collapsed, or disabled — the plan
  // is null and the caller leaves the browser's own focus order alone. That
  // is what keeps a missing sidebar from trapping Tab.

  const TAB_CYCLE_HINT = 'Tab switches between the main and sidebar message boxes';
  const TAB_CYCLE_DOC =
    'With the sidebar Transcript composer visible, Tab moves focus from the main ' +
    'message box to the sidebar message box; Tab (or Shift+Tab) moves it back. ' +
    'When the sidebar composer is hidden the chord is not claimed and Tab keeps ' +
    'its normal document focus order.';

  // Tab with no Meta/Ctrl/Alt. Shift is allowed (documented reverse).
  function isComposerTabChord(key, mods) {
    const m = modsOf(mods);
    const code = mods && mods.code != null ? String(mods.code) : '';
    if (key !== 'Tab' && code !== 'Tab') return false;
    return !(m.metaKey || m.ctrlKey || m.altKey);
  }

  // True when an element can be focused as a cycle stop. Element-shaped but
  // reads only `.focus`, `.disabled`, `.hidden`, and `.classList.contains`,
  // so hermetic tests pass plain mocks instead of a live DOM.
  function isCycleStopFocusable(el, opts) {
    if (!el || typeof el.focus !== 'function') return false;
    if (el.disabled === true || el.hidden === true) return false;
    const o = opts || {};
    // Sidebar composer wrapper: hidden attribute or missing `visible` class
    // means the box is not on screen (collapsed pane, no agent selected).
    const wrap = o.composerEl;
    if (wrap) {
      if (wrap.hidden === true) return false;
      if (wrap.classList && typeof wrap.classList.contains === 'function' &&
          !wrap.classList.contains('visible')) {
        return false;
      }
    }
    // RHS tab pane: only the active pane is displayed.
    const pane = o.paneEl;
    if (pane) {
      if (pane.hidden === true) return false;
      if (pane.classList && typeof pane.classList.contains === 'function' &&
          !pane.classList.contains('active')) {
        return false;
      }
    }
    return true;
  }

  /**
   * Decide where Tab should land. Pure: never focuses anything.
   *
   * @param {object} eventLike { key, code, metaKey, ctrlKey, altKey, shiftKey }
   * @param {object} ctx {
   *   activeElement, mainEl, sidebarEl,
   *   sidebarComposerEl?, sidebarPaneEl?,
   *   sidebarFocusable?  // explicit override for callers that already know
   * }
   * @returns {{ target: 'main'|'sidebar'|null, focusEl: *, preventDefault: boolean, reason: string }}
   */
  function planComposerTabCycle(eventLike, ctx) {
    const ev = eventLike || {};
    const c = ctx || {};
    const none = function (reason) {
      return { target: null, focusEl: null, preventDefault: false, reason: reason };
    };
    if (!isComposerTabChord(ev.key, ev)) return none('not-tab');

    const active = c.activeElement;
    const main = c.mainEl;
    const side = c.sidebarEl;
    if (!active) return none('not-in-cycle');

    const sideOk = typeof c.sidebarFocusable === 'boolean'
      ? c.sidebarFocusable
      : isCycleStopFocusable(side, {
        composerEl: c.sidebarComposerEl,
        paneEl: c.sidebarPaneEl,
      });

    if (main && active === main) {
      if (!sideOk) return none('sidebar-unavailable');
      return { target: 'sidebar', focusEl: side, preventDefault: true, reason: 'to-sidebar' };
    }
    if (side && active === side) {
      // Leaving the sidebar does not depend on the sidebar's own visibility —
      // focus is already there — only on the main box being focusable.
      if (!isCycleStopFocusable(main)) return none('main-unavailable');
      return { target: 'main', focusEl: main, preventDefault: true, reason: 'to-main' };
    }
    return none('not-in-cycle');
  }

  /**
   * Apply the Tab cycle: focus the partner box when the plan claims the
   * chord. Returns the plan (with `didFocus`) for hermetic assertions.
   */
  function applyComposerTabCycle(eventLike, ctx) {
    const plan = planComposerTabCycle(eventLike, ctx);
    if (!plan.target || !plan.focusEl) {
      return { target: plan.target, focusEl: plan.focusEl, preventDefault: plan.preventDefault, reason: plan.reason, didFocus: false };
    }
    plan.focusEl.focus();
    return { target: plan.target, focusEl: plan.focusEl, preventDefault: true, reason: plan.reason, didFocus: true };
  }

  return {
    HOTKEY: HOTKEY,
    HOTKEY_HINT: HOTKEY_HINT,
    HOTKEY_DOC: HOTKEY_DOC,
    isFocusComposerHotkey: isFocusComposerHotkey,
    isEditableTarget: isEditableTarget,
    shouldFocusComposer: shouldFocusComposer,
    tryFocusComposer: tryFocusComposer,
    focusComposer: focusComposer,
    // 🎯T366
    TAB_CYCLE_HINT: TAB_CYCLE_HINT,
    TAB_CYCLE_DOC: TAB_CYCLE_DOC,
    isComposerTabChord: isComposerTabChord,
    isCycleStopFocusable: isCycleStopFocusable,
    planComposerTabCycle: planComposerTabCycle,
    applyComposerTabCycle: applyComposerTabCycle,
  };
}));
