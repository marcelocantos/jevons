// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T383 — fleet tree selection is sticky: a background refresh never moves
// the owner's selection.
//
// THE FAULT, as the owner met it. "I've got two asides, and when I select the
// second one, between 5 and 15 seconds later it just selects the first one
// spontaneously without me touching anything." The window named the culprit
// before any code was read: too long for a click race, too short for the 30s
// fallback poll — it is the agents_changed burst refresh (coalesced at
// 120–600ms, fired per progress-summary change across a busy fleet).
//
// The mechanism is one line of 🎯T252's attention queue, in its poll branch:
//
//     // poll: stay if current still needs attention; else first in queue
//     if (cur && names.indexOf(cur) >= 0) return cur;
//     return names[0];                       // ← moves a selection the owner made
//
// Aside A raised attention and sits in the queue. The owner clicks aside B,
// which is not in the queue — it does not need the owner's turn, which is
// precisely why they went to read it. The next refresh finds `cur` (B) absent
// from `names` ([A]) and answers with `names[0]`: A. The first aside. Every
// refresh, forever, until the owner's selection happens to coincide with the
// head of a queue they cannot see.
//
// WHY THE FIX IS NOT "DELETE THAT LINE". 🎯T124 (a new aside opens its
// transcript) and 🎯T252 (an aside needing the owner is auto-activated) are
// deliberate, and they are the reason the owner does not have to hunt for the
// aside that just asked them something. Both are *good defaults for a
// selection nobody chose*. Neither is a licence to move a selection somebody
// did. So the distinction this module draws is not poll-vs-push, nor
// aside-vs-worker: it is WHO CHOSE THE CURRENT SELECTION.
//
//   - OWNER-PINNED — a click, the ⌘⇧[ / ⌘⇧] chord (🎯T370), a target-ask
//     focus, an aside the owner created. Immovable by any background
//     repaint. Only owner input moves it, and only owner input un-pins it.
//   - AUTO — the page filled an empty selection on the owner's behalf.
//     Still advanceable by auto-select, exactly as before this target.
//
// That keeps every auto-open path working for the case it was written for
// (nothing selected, or nothing the owner asked for) while making acceptance
// 1's "no jump at all, without owner input" true by construction rather than
// by enumerating which refreshes are allowed to steal.
//
// IDENTITY, NOT POSITION. Resolution is by agent NAME against the incoming
// row list. A row inserted above the selected one, a name-sort that reorders
// siblings (🎯T68), a portfolio weave that re-parents a subtree (🎯T200) — none
// of them can drag the selection, because no index is ever consulted. This is
// also why `agents` is read only through `isPresent`.
//
// A SELECTION THAT VANISHES CLEARS. When the selected agent genuinely leaves
// the tree — reaped on finish (🎯T165/🎯T195), stopped, dismissed — the answer
// is null, never a substitute. Landing the owner silently on a *different*
// agent's transcript is the same bug wearing a plausible excuse: the pane
// would keep painting, the name in the header would change, and a message
// typed into it would go to an agent the owner never chose. Clearing is
// visible; substituting is not.
//
// DOM-free so Node hermetic tests can require() it; index.html owns the
// selectAgent/paint side effects. Same split as 🎯T370's fleet_cycle.js.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.FleetSelection = factory();
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Who chose the current selection. Only OWNER survives a background repaint.
  const OWNER = 'owner';
  const AUTO = 'auto';

  /** Normalize a name-ish value to a comparable string ('' when absent). */
  function nameOf(v) {
    if (v == null) return '';
    if (typeof v === 'string') return v;
    return String(v.name == null ? '' : v.name);
  }

  /**
   * Is `name` a row in this fleet list? Identity by name — never by index,
   * so reordering and re-parenting are invisible here (acceptance 3).
   *
   * @param {Array<object|string>} agents
   * @param {string} name
   * @returns {boolean}
   */
  function isPresent(agents, name) {
    const want = nameOf(name);
    if (!want) return false;
    const list = agents || [];
    for (let i = 0; i < list.length; i++) {
      if (nameOf(list[i]) === want) return true;
    }
    return false;
  }

  /**
   * The one decision a background refresh is allowed to make about selection.
   *
   * @param {{
   *   selected?: string|null,   current selection (may be null/'')
   *   origin?: string,          'owner' (pinned) | 'auto'; default 'auto'
   *   agents?: Array,           incoming fleet rows for this repaint
   *   candidate?: string|null,  what auto-select proposes this pass
   * }} ctx
   * @returns {{selected: string|null, origin: string, changed: boolean, reason: string}}
   *   reason: gone | pinned | sticky | auto-advance | auto-fill | idle
   */
  function resolveSelection(ctx) {
    const c = ctx || {};
    const sel = nameOf(c.selected);
    const origin = c.origin === OWNER ? OWNER : AUTO;
    const agents = c.agents || [];
    const cand = nameOf(c.candidate);

    if (sel) {
      // Acceptance 4: a selection whose agent left the tree clears, and is
      // never quietly replaced by whatever auto-select had in hand.
      if (!isPresent(agents, sel)) {
        return { selected: null, origin: AUTO, changed: true, reason: 'gone' };
      }
      // Acceptance 1 + 2: owner input outranks every background repaint.
      if (origin === OWNER) {
        return { selected: sel, origin: OWNER, changed: false, reason: 'pinned' };
      }
      // The page picked this one, so the page may move it on — but only to a
      // row that is actually in the tree it is about to paint.
      if (cand && cand !== sel && isPresent(agents, cand)) {
        return { selected: cand, origin: AUTO, changed: true, reason: 'auto-advance' };
      }
      return { selected: sel, origin: AUTO, changed: false, reason: 'sticky' };
    }

    // Nothing selected: this is the case 🎯T124 / 🎯T252 were written for.
    if (cand && isPresent(agents, cand)) {
      return { selected: cand, origin: AUTO, changed: true, reason: 'auto-fill' };
    }
    return { selected: null, origin: AUTO, changed: false, reason: 'idle' };
  }

  /**
   * True when a background pass may apply `candidate` at all — the guard for
   * call sites (🎯T252 attention activation) that select directly rather than
   * through the refresh tail.
   *
   * @param {{selected?: string|null, origin?: string}} ctx
   * @returns {boolean}
   */
  function backgroundMayMove(ctx) {
    const c = ctx || {};
    if (!nameOf(c.selected)) return true;   // empty selection is always fillable
    return c.origin !== OWNER;
  }

  return {
    OWNER: OWNER,
    AUTO: AUTO,
    isPresent: isPresent,
    resolveSelection: resolveSelection,
    backgroundMayMove: backgroundMayMove,
  };
}));
