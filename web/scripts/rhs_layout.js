// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T248 — drag-resize RHS sidebar width + fleet/frontier vertical split.
// DOM-free pure helpers (Node-requireable). Browser bind() wires handles
// and persists layout prefs to localStorage (same plane as theme/draft).
//
// Residual: mobile/narrow layouts may keep fixed proportions (no drag
// below MIN_MAIN_FOR_DRAG); min floors prevent unusable zero panes.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.RhsLayout = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const STORAGE_KEY = 'jevons-rhs-layout-v1';

  // Default matches pre-T248 CSS (#activity-pane width: 420px).
  const DEFAULT_SIDEBAR_WIDTH = 420;
  // Pre-T248 agents max-height 45% of pane ≈ fleet share of split area.
  const DEFAULT_FLEET_FRACTION = 0.45;

  // Floors so drag cannot collapse panes to unusable zero.
  const MIN_SIDEBAR_WIDTH = 220;
  const MIN_CHAT_WIDTH = 280;
  const MIN_FLEET_PX = 60;
  const MIN_BOTTOM_PX = 140;
  const SPLIT_HANDLE_PX = 6;
  // Below this main width, skip interactive drag (narrow residual).
  const MIN_MAIN_FOR_DRAG = MIN_SIDEBAR_WIDTH + MIN_CHAT_WIDTH;

  function num(v, fallback) {
    const n = Number(v);
    return Number.isFinite(n) ? n : (fallback != null ? fallback : 0);
  }

  function clamp(n, lo, hi) {
    if (!(hi >= lo)) return lo;
    return Math.min(Math.max(n, lo), hi);
  }

  /**
   * Clamp requested sidebar width so chat and sidebar both stay usable.
   * @param {number} requested
   * @param {number} mainWidth  #main clientWidth
   * @param {object} [opts] optional floor overrides for tests
   */
  function clampSidebarWidth(requested, mainWidth, opts) {
    const o = opts || {};
    const minSide = num(o.minSidebar, MIN_SIDEBAR_WIDTH);
    const minChat = num(o.minChat, MIN_CHAT_WIDTH);
    const main = Math.max(0, num(mainWidth, 0));
    const maxSide = Math.max(minSide, main - minChat);
    return clamp(num(requested, DEFAULT_SIDEBAR_WIDTH), minSide, maxSide);
  }

  /**
   * Clamp fleet (agents) fraction of the resizable split area (agents +
   * handle + rhs-bottom). Floors are pixel mins converted to fractions.
   * @param {number} fraction  0..1 share of split height for fleet
   * @param {number} splitHeight  #rhs-split clientHeight (px)
   * @param {object} [opts]
   */
  function clampFleetFraction(fraction, splitHeight, opts) {
    const o = opts || {};
    const minFleet = num(o.minFleet, MIN_FLEET_PX);
    const minBottom = num(o.minBottom, MIN_BOTTOM_PX);
    const handle = num(o.handlePx, SPLIT_HANDLE_PX);
    const splitH = Math.max(0, num(splitHeight, 0));
    const usable = Math.max(0, splitH - handle);
    if (!(usable > 0)) {
      return clamp(num(fraction, DEFAULT_FLEET_FRACTION), 0, 1);
    }
    // When the pane is shorter than both floors, favour mins proportionally
    // but still return a finite fraction in [0,1].
    if (usable < minFleet + minBottom) {
      const f = minFleet / Math.max(1, minFleet + minBottom);
      return clamp(f, 0, 1);
    }
    const minF = minFleet / usable;
    const maxF = 1 - minBottom / usable;
    return clamp(num(fraction, DEFAULT_FLEET_FRACTION), minF, maxF);
  }

  /**
   * Convert a pointer Y (relative to split top) into a fleet fraction.
   */
  function fleetFractionFromPointer(pointerYInSplit, splitHeight, opts) {
    const handle = num((opts || {}).handlePx, SPLIT_HANDLE_PX);
    const splitH = Math.max(0, num(splitHeight, 0));
    const usable = Math.max(1, splitH - handle);
    const y = Math.max(0, num(pointerYInSplit, 0));
    return clampFleetFraction(y / usable, splitH, opts);
  }

  /**
   * Sidebar width from pointer X relative to #main left.
   */
  function sidebarWidthFromPointer(pointerXInMain, mainWidth, opts) {
    // Handle is at the chat|sidebar border; width = main - x.
    const main = Math.max(0, num(mainWidth, 0));
    const x = num(pointerXInMain, main - DEFAULT_SIDEBAR_WIDTH);
    const w = main - x;
    return clampSidebarWidth(w, main, opts);
  }

  function defaultState() {
    return {
      sidebarWidth: DEFAULT_SIDEBAR_WIDTH,
      fleetFraction: DEFAULT_FLEET_FRACTION,
    };
  }

  function normalizeState(raw) {
    const d = defaultState();
    if (!raw || typeof raw !== 'object') return d;
    const w = num(raw.sidebarWidth, d.sidebarWidth);
    const f = num(raw.fleetFraction, d.fleetFraction);
    return {
      sidebarWidth: w > 0 ? w : d.sidebarWidth,
      fleetFraction: f > 0 && f <= 1 ? f : d.fleetFraction,
    };
  }

  function serialize(state) {
    const s = normalizeState(state);
    return JSON.stringify({
      sidebarWidth: s.sidebarWidth,
      fleetFraction: s.fleetFraction,
    });
  }

  function deserialize(raw) {
    if (raw == null || raw === '') {
      return { ok: true, state: defaultState(), present: false };
    }
    try {
      const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (!parsed || typeof parsed !== 'object') {
        return { ok: false, state: defaultState(), present: true, error: 'not-object' };
      }
      return { ok: true, state: normalizeState(parsed), present: true };
    } catch (e) {
      return {
        ok: false,
        state: defaultState(),
        present: true,
        error: e && e.message ? e.message : 'parse',
      };
    }
  }

  function load(storage) {
    if (!storage || typeof storage.getItem !== 'function') {
      return { ok: true, state: defaultState(), present: false };
    }
    let raw;
    try {
      raw = storage.getItem(STORAGE_KEY);
    } catch (e) {
      return {
        ok: false,
        state: defaultState(),
        present: false,
        error: e && e.message ? e.message : 'getItem',
      };
    }
    return deserialize(raw);
  }

  function save(storage, state) {
    if (!storage || typeof storage.setItem !== 'function') {
      return { ok: false, error: 'no-storage' };
    }
    try {
      storage.setItem(STORAGE_KEY, serialize(state));
      return { ok: true };
    } catch (e) {
      return { ok: false, error: e && e.message ? e.message : 'setItem' };
    }
  }

  /**
   * Styles to apply for a layout state. fleetFraction → flex-basis %.
   * Callers still re-clamp against live main/split sizes before apply.
   */
  function stylesForState(state) {
    const s = normalizeState(state);
    const pct = Math.round(s.fleetFraction * 10000) / 100;
    return {
      sidebarWidthPx: Math.round(s.sidebarWidth),
      fleetFlexBasis: pct + '%',
      fleetFraction: s.fleetFraction,
    };
  }

  /**
   * True when the main area is wide enough for interactive drag.
   * Narrow residual: keep fixed proportions (no handle active).
   */
  function dragEnabled(mainWidth) {
    return num(mainWidth, 0) >= MIN_MAIN_FOR_DRAG;
  }

  /**
   * Re-clamp a stored state against current geometry (window resize).
   */
  function reclamp(state, mainWidth, splitHeight, opts) {
    const s = normalizeState(state);
    return {
      sidebarWidth: clampSidebarWidth(s.sidebarWidth, mainWidth, opts),
      fleetFraction: clampFleetFraction(s.fleetFraction, splitHeight, opts),
    };
  }

  /**
   * Browser wiring. opts:
   *   main, activityPane, agents, rhsSplit, widthHandle, splitHandle
   *   storage (localStorage), onChange(state)
   * Returns { getState, setState, destroy } or null if missing critical els.
   */
  function bind(opts) {
    const o = opts || {};
    const main = o.main;
    const activityPane = o.activityPane;
    const agents = o.agents;
    const rhsSplit = o.rhsSplit;
    const widthHandle = o.widthHandle;
    const splitHandle = o.splitHandle;
    if (!main || !activityPane || !agents) return null;

    let storage = o.storage;
    if (storage == null) {
      try { storage = window.localStorage; } catch (_) { storage = null; }
    }

    const loaded = load(storage);
    let state = loaded.state;

    function measure() {
      const mainW = main.clientWidth || 0;
      const splitH = rhsSplit ? (rhsSplit.clientHeight || 0) : 0;
      return { mainW: mainW, splitH: splitH };
    }

    function apply(next, persist) {
      const m = measure();
      state = reclamp(next, m.mainW, m.splitH, o);
      const styles = stylesForState(state);
      activityPane.style.width = styles.sidebarWidthPx + 'px';
      activityPane.style.flexShrink = '0';
      agents.style.flex = '0 0 ' + styles.fleetFlexBasis;
      agents.style.minHeight = MIN_FLEET_PX + 'px';
      agents.style.maxHeight = 'none';
      agents.style.overflowY = 'auto';
      if (rhsSplit) {
        const bottom = o.rhsBottom || document.getElementById('rhs-bottom');
        if (bottom) {
          bottom.style.flex = '1 1 auto';
          bottom.style.minHeight = MIN_BOTTOM_PX + 'px';
          bottom.style.maxHeight = 'none';
        }
      }
      const enabled = dragEnabled(m.mainW);
      if (widthHandle) {
        widthHandle.classList.toggle('disabled', !enabled);
        widthHandle.setAttribute('aria-disabled', enabled ? 'false' : 'true');
      }
      if (splitHandle) {
        splitHandle.classList.toggle('disabled', !enabled);
        splitHandle.setAttribute('aria-disabled', enabled ? 'false' : 'true');
      }
      if (persist !== false) save(storage, state);
      if (typeof o.onChange === 'function') o.onChange(state);
      return state;
    }

    apply(state, false);

    let drag = null; // { kind: 'width'|'fleet', startX, startY, startState }

    function onPointerDown(kind, e) {
      const m = measure();
      if (!dragEnabled(m.mainW)) return;
      if (e.button != null && e.button !== 0) return;
      e.preventDefault();
      const target = e.currentTarget;
      try {
        if (target && target.setPointerCapture) target.setPointerCapture(e.pointerId);
      } catch (_) { /* ignore */ }
      drag = {
        kind: kind,
        pointerId: e.pointerId,
        startState: { sidebarWidth: state.sidebarWidth, fleetFraction: state.fleetFraction },
      };
      document.body.classList.add('rhs-resizing');
      document.body.classList.add(kind === 'width' ? 'rhs-resizing-col' : 'rhs-resizing-row');
    }

    function onPointerMove(e) {
      if (!drag) return;
      const m = measure();
      if (drag.kind === 'width') {
        const rect = main.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const w = sidebarWidthFromPointer(x, m.mainW, o);
        apply({ sidebarWidth: w, fleetFraction: state.fleetFraction }, false);
      } else if (drag.kind === 'fleet' && rhsSplit) {
        const rect = rhsSplit.getBoundingClientRect();
        const y = e.clientY - rect.top;
        const f = fleetFractionFromPointer(y, m.splitH, o);
        apply({ sidebarWidth: state.sidebarWidth, fleetFraction: f }, false);
      }
    }

    function endDrag() {
      if (!drag) return;
      drag = null;
      document.body.classList.remove('rhs-resizing', 'rhs-resizing-col', 'rhs-resizing-row');
      save(storage, state);
      if (typeof o.onChange === 'function') o.onChange(state);
    }

    function onPointerUp() { endDrag(); }

    function wireHandle(el, kind) {
      if (!el) return function () {};
      const down = function (e) { onPointerDown(kind, e); };
      const move = function (e) { onPointerMove(e); };
      const up = function () { onPointerUp(); };
      el.addEventListener('pointerdown', down);
      el.addEventListener('pointermove', move);
      el.addEventListener('pointerup', up);
      el.addEventListener('pointercancel', up);
      el.addEventListener('lostpointercapture', up);
      return function () {
        el.removeEventListener('pointerdown', down);
        el.removeEventListener('pointermove', move);
        el.removeEventListener('pointerup', up);
        el.removeEventListener('pointercancel', up);
        el.removeEventListener('lostpointercapture', up);
      };
    }

    const unWidth = wireHandle(widthHandle, 'width');
    const unSplit = wireHandle(splitHandle, 'fleet');

    function onResize() {
      apply(state, true);
    }
    window.addEventListener('resize', onResize);

    return {
      getState: function () {
        return { sidebarWidth: state.sidebarWidth, fleetFraction: state.fleetFraction };
      },
      setState: function (next, persist) {
        return apply(next || state, persist !== false);
      },
      destroy: function () {
        unWidth();
        unSplit();
        window.removeEventListener('resize', onResize);
        endDrag();
      },
    };
  }

  return {
    STORAGE_KEY: STORAGE_KEY,
    DEFAULT_SIDEBAR_WIDTH: DEFAULT_SIDEBAR_WIDTH,
    DEFAULT_FLEET_FRACTION: DEFAULT_FLEET_FRACTION,
    MIN_SIDEBAR_WIDTH: MIN_SIDEBAR_WIDTH,
    MIN_CHAT_WIDTH: MIN_CHAT_WIDTH,
    MIN_FLEET_PX: MIN_FLEET_PX,
    MIN_BOTTOM_PX: MIN_BOTTOM_PX,
    SPLIT_HANDLE_PX: SPLIT_HANDLE_PX,
    MIN_MAIN_FOR_DRAG: MIN_MAIN_FOR_DRAG,
    clampSidebarWidth: clampSidebarWidth,
    clampFleetFraction: clampFleetFraction,
    fleetFractionFromPointer: fleetFractionFromPointer,
    sidebarWidthFromPointer: sidebarWidthFromPointer,
    defaultState: defaultState,
    normalizeState: normalizeState,
    serialize: serialize,
    deserialize: deserialize,
    load: load,
    save: save,
    stylesForState: stylesForState,
    dragEnabled: dragEnabled,
    reclamp: reclamp,
    bind: bind,
  };
}));
