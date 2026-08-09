// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T370 — Cmd-Shift-[ / Cmd-Shift-] cycle the fleet tree selection.
//
// Owner intent: one chord walks *every* visible fleet tree node, root
// overseer included, in the order the tree is painted. Landing on a node
// shows that agent's transcript and puts the caret in that node's message
// box. Landing on root `jevons` is not a gap in the cycle — the overseer's
// stream *is* the main view, so root focuses the main composer rather than
// a sidebar that deliberately does not exist for it (🎯T124 residual).
//
// The order is the painted depth-first row order, so it matches what the
// owner sees. Virtual portfolio folders (🎯T200) are display chrome with no
// transcript and no message box — they are not cycle stops, exactly as they
// are not click targets. Rows the tree has hidden or collapsed are likewise
// absent, which is how the chord inherits the existing tree UI rules
// instead of re-deciding them here.
//
// Root is guaranteed present: the overseer is prepended when the caller's
// row list has not painted it yet (first frame, fetch in flight). That is
// what makes the chord work "empty beyond root" — with no fleet at all the
// cycle is a single stop that still claims the key and focuses the main box,
// rather than falling through to the browser.
//
// DOM-free so Node hermetic tests can require(); index.html collects the
// painted rows and does the selecting/focusing. Same split as 🎯T366.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.FleetCycle = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Root overseer name. Mirrors AgentTranscript.isOverseer's literal so the
  // pure module stays standalone for Node tests; callers may override.
  const OVERSEER = 'jevons';

  const CHORD_HINT = '⌘⇧] / ⌘⇧[ cycle fleet tree selection';
  const CHORD_DOC =
    'Cmd+Shift+] selects the next fleet tree node and Cmd+Shift+[ the previous, ' +
    'wrapping at both ends. The cycle includes the root overseer: landing there ' +
    'shows the main chat and focuses the main message box, and every other node ' +
    'shows its transcript in the sidebar and focuses the sidebar message box.';

  const FORWARD = 1;
  const BACKWARD = -1;

  function overseerName(opts) {
    const o = opts || {};
    const n = o.overseer == null ? '' : String(o.overseer);
    return n || OVERSEER;
  }

  function isOverseerName(name, opts) {
    return String(name || '').toLowerCase() === overseerName(opts).toLowerCase();
  }

  /**
   * Direction for the cycle chord, or 0 when this keydown is not ours.
   *
   * Requires Meta+Shift and rejects Ctrl/Alt, so it cannot collide with the
   * bare-`[`/`]` of ordinary typing. Shift turns the bracket keys into
   * braces on US layouts and other layouts move them entirely, so `code`
   * (BracketLeft/BracketRight — physical key) is checked alongside `key`.
   *
   * @param {string} key
   * @param {{metaKey?:boolean, ctrlKey?:boolean, altKey?:boolean, shiftKey?:boolean, code?:string}} mods
   * @returns {1|-1|0} FORWARD / BACKWARD / not-our-chord
   */
  function fleetCycleDirection(key, mods) {
    const m = mods || {};
    if (!m.metaKey || !m.shiftKey) return 0;
    if (m.ctrlKey || m.altKey) return 0;
    const code = m.code == null ? '' : String(m.code);
    const k = key == null ? '' : String(key);
    if (code === 'BracketRight' || k === ']' || k === '}') return FORWARD;
    if (code === 'BracketLeft' || k === '[' || k === '{') return BACKWARD;
    return 0;
  }

  /**
   * Painted rows → ordered cycle stops.
   *
   * Accepts row-shaped objects (fleet descriptors or element-derived
   * `{name, selectable, hidden, purpose}`) and keeps their given order,
   * which for the product is the depth-first paint order the owner sees.
   *
   * Dropped: unnamed rows, `selectable === false` / `purpose === 'portfolio'`
   * virtual folders, rows flagged hidden, and duplicates (a name can only be
   * one stop). The overseer is prepended when absent — see the header note.
   *
   * @param {Array<{name?:string, key?:string, selectable?:boolean, hidden?:boolean, purpose?:string}>|null|undefined} rows
   * @param {{overseer?:string, ensureOverseer?:boolean}} [opts]
   * @returns {string[]}
   */
  function fleetCycleOrder(rows, opts) {
    const list = Array.isArray(rows) ? rows : [];
    const out = [];
    const seen = {};
    for (let i = 0; i < list.length; i++) {
      const r = list[i];
      if (!r) continue;
      const name = String(r.name != null ? r.name : (r.key != null ? r.key : ''));
      if (!name) continue;
      if (r.selectable === false) continue;
      if (r.hidden === true) continue;
      if (String(r.purpose || '').toLowerCase() === 'portfolio') continue;
      if (Object.prototype.hasOwnProperty.call(seen, name)) continue;
      seen[name] = true;
      out.push(name);
    }
    const o = opts || {};
    if (o.ensureOverseer !== false) {
      const ov = overseerName(o);
      let hasOverseer = false;
      for (let i = 0; i < out.length; i++) {
        if (isOverseerName(out[i], o)) { hasOverseer = true; break; }
      }
      if (!hasOverseer) out.unshift(ov);
    }
    return out;
  }

  /**
   * The cycle stop the current selection sits on.
   *
   * A null/empty selection is the root: `selectedAgent` is cleared precisely
   * when the owner is looking at the overseer's main stream. A selection the
   * order no longer contains (agent reaped mid-session) also resolves to root
   * so the chord always has somewhere to step from.
   *
   * @param {string[]} order
   * @param {string|null|undefined} selected
   * @param {{overseer?:string}} [opts]
   * @returns {string} '' only when the order is empty
   */
  function currentCycleNode(order, selected, opts) {
    const list = Array.isArray(order) ? order : [];
    if (!list.length) return '';
    const sel = selected == null ? '' : String(selected);
    if (sel && list.indexOf(sel) >= 0) return sel;
    for (let i = 0; i < list.length; i++) {
      if (isOverseerName(list[i], opts)) return list[i];
    }
    return list[0];
  }

  /**
   * Step one stop, wrapping at both ends. A one-stop order (root only) always
   * returns that stop — the chord is still ours, it just has nowhere else
   * to go.
   *
   * @param {string[]} order
   * @param {string} current
   * @param {1|-1} direction
   * @returns {string}
   */
  function stepFleetCycle(order, current, direction) {
    const list = Array.isArray(order) ? order : [];
    if (!list.length) return '';
    const n = list.length;
    const dir = direction === BACKWARD ? BACKWARD : FORWARD;
    let i = list.indexOf(String(current == null ? '' : current));
    if (i < 0) i = 0;
    return list[((i + dir) % n + n) % n];
  }

  /**
   * Full plan for one cycle keydown. Pure: selects and focuses nothing.
   *
   * `select` is false only when the step lands back on what is already
   * selected (single-stop order), so the caller never re-enters selectAgent
   * with the current name — that call toggles the selection *off*.
   *
   * @param {{key?:string, code?:string, metaKey?:boolean, ctrlKey?:boolean, altKey?:boolean, shiftKey?:boolean}} eventLike
   * @param {{rows?:Array, order?:string[], selected?:string|null, overseer?:string, ensureOverseer?:boolean}} ctx
   * @returns {{
   *   claim: boolean, direction: 1|-1|0, target: string, current: string,
   *   isRoot: boolean, focus: 'main'|'sidebar'|null, select: boolean, reason: string
   * }}
   */
  function planFleetCycle(eventLike, ctx) {
    const ev = eventLike || {};
    const c = ctx || {};
    const none = function (reason) {
      return {
        claim: false, direction: 0, target: '', current: '',
        isRoot: false, focus: null, select: false, reason: reason,
      };
    };

    const dir = fleetCycleDirection(ev.key, ev);
    if (!dir) return none('not-chord');

    const order = Array.isArray(c.order) ? c.order : fleetCycleOrder(c.rows, c);
    if (!order.length) return none('no-nodes');

    const current = currentCycleNode(order, c.selected, c);
    const target = stepFleetCycle(order, current, dir);
    if (!target) return none('no-nodes');

    const isRoot = isOverseerName(target, c);
    // Compare against the *raw* selection, not the resolved stop: a stale
    // selection resolves to root for stepping but still has to be cleared.
    const selRaw = c.selected == null || c.selected === ''
      ? overseerName(c)
      : String(c.selected);

    return {
      claim: true,
      direction: dir,
      target: target,
      current: current,
      isRoot: isRoot,
      focus: isRoot ? 'main' : 'sidebar',
      select: target !== selRaw,
      reason: 'cycle',
    };
  }

  return {
    OVERSEER: OVERSEER,
    FORWARD: FORWARD,
    BACKWARD: BACKWARD,
    CHORD_HINT: CHORD_HINT,
    CHORD_DOC: CHORD_DOC,
    isOverseerName: isOverseerName,
    fleetCycleDirection: fleetCycleDirection,
    fleetCycleOrder: fleetCycleOrder,
    currentCycleNode: currentCycleNode,
    stepFleetCycle: stepFleetCycle,
    planFleetCycle: planFleetCycle,
  };
}));
