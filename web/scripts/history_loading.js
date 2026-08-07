// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T300 — pure "Loading earlier…" chrome policy (DOM-free).
//
// The chrome exists for one case only: the owner asked for older history and
// is waiting on a fetch. Everything else that pages /api/history — connect
// hydrate, the T119 progressive background loop, the top IntersectionObserver
// sentinel, reconnect replays — is machinery the owner never asked for, so it
// must page silently. Before this policy each background page flipped the
// overlay on and off (~one strobe per HISTORY_PAGE_GAP_MS), which is the
// flashing the owner reported.
//
// Rules encoded here:
//   • Only an owner-triggered load may show the chrome (reason OWNER).
//   • Once shown it stays stable until that fetch settles — background pages
//     starting/finishing underneath never toggle it.
//   • 🎯T274: while the large Frontier Graph panel is open the chrome is
//     suppressed, and closing the panel does not resurrect a stale overlay.
//   • No older pages remain (older === 0) → off, and stays off.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.HistoryLoading = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /** Owner explicitly asked for older history (the only visible reason). */
  const OWNER = 'owner';
  /** Machinery: connect hydrate, progressive loop, sentinel nudge, reconnect. */
  const AUTO = 'auto';

  function initialState() {
    return {
      // An owner-triggered fetch is outstanding.
      ownerActive: false,
      // 🎯T274 large graph panel open.
      suppressed: false,
      // Remaining older journal lines; null = not yet known.
      older: null,
    };
  }

  /**
   * reduce applies one event and returns the next state (never mutates).
   *
   * Events:
   *   {type:'begin', reason}          a /api/history page is starting
   *   {type:'settle', older}          that page finished (success or failure)
   *   {type:'graph', open}            🎯T274 large panel opened/closed
   *   {type:'older', older}           remaining-older count changed
   *   {type:'reset'}                  connect / reconnect / hard replay
   */
  function reduce(state, ev) {
    const s = state ? {
      ownerActive: !!state.ownerActive,
      suppressed: !!state.suppressed,
      older: typeof state.older === 'number' ? state.older : null,
    } : initialState();
    if (!ev || !ev.type) return s;
    switch (ev.type) {
      case 'begin':
        // Background pages never raise the chrome, and never lower an
        // owner-triggered one already up — that stability is the whole point.
        // A load begun behind the graph panel stays dark for good, so closing
        // the panel cannot surface a stale overlay (🎯T274).
        if (ev.reason === OWNER && !s.suppressed) s.ownerActive = true;
        break;
      case 'settle':
        s.ownerActive = false;
        if (typeof ev.older === 'number') s.older = ev.older;
        break;
      case 'graph':
        s.suppressed = !!ev.open;
        // Suppression is not a pause: a load that completed behind the panel
        // must not pop back into view when the panel closes.
        if (s.suppressed) s.ownerActive = false;
        break;
      case 'older':
        if (typeof ev.older === 'number') s.older = ev.older;
        if (s.older === 0) s.ownerActive = false;
        break;
      case 'reset':
        s.ownerActive = false;
        break;
      default:
        break;
    }
    if (s.older === 0) s.ownerActive = false;
    return s;
  }

  /** isVisible reports whether the "Loading earlier…" chrome should be shown. */
  function isVisible(state) {
    if (!state) return false;
    if (state.suppressed) return false;
    if (state.older === 0) return false;
    return !!state.ownerActive;
  }

  /** reasonFor maps a loadEarlier call's options to a policy reason. */
  function reasonFor(opts) {
    return opts && opts.owner ? OWNER : AUTO;
  }

  /**
   * run replays a scripted event sequence and reports the visibility trace —
   * the thrash oracle's primitive. `shows` counts off→on transitions, so a
   * silent sequence is shows === 0 and a well-behaved owner load is shows === 1.
   */
  function run(events, state) {
    let s = state || initialState();
    let visible = isVisible(s);
    const trace = [visible];
    let shows = 0;
    (events || []).forEach(function (ev) {
      s = reduce(s, ev);
      const next = isVisible(s);
      if (next && !visible) shows++;
      visible = next;
      trace.push(visible);
    });
    return { state: s, visible: visible, trace: trace, shows: shows };
  }

  return {
    OWNER: OWNER,
    AUTO: AUTO,
    initialState: initialState,
    reduce: reduce,
    isVisible: isVisible,
    reasonFor: reasonFor,
    run: run,
  };
}));
