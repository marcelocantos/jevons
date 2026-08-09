// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Fleet-tree paint coalescing and diffing (🎯T289).
//
// Problem: `agents_changed` is pushed per agent whenever a progress summary
// changes — i.e. on every tool call / phase flip across the whole fleet. The
// old RHS handler answered each push with `agentsEl.innerHTML = ''` plus a
// recursive rebuild of every row (fetch → destroy → recreate → reparse →
// rebind). Under normal fleet chat that is a full subtree teardown several
// times a second: constant stutter and sustained CPU.
//
// This module holds the DOM-free half of the fix so Node can test it:
//
//   coalesce      — collapse a burst of pushes into one repaint
//   structureKey  — does the tree *shape* differ, or only row content?
//   diffPlan      — noop / patch-these-rows / rebuild
//   pollPlan      — background tabs must not poll at foreground rates
//
// Descriptors are built by the caller (index.html) from the existing row
// model, so chrome changes in fleet_row.js (🎯T287 model prefix) flow through
// the diff automatically — this module never re-derives row content.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.FleetPaint = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // ── Coalescing ─────────────────────────────────────────────────────
  //
  // Trailing-edge: a burst of N pushes inside the window costs one repaint,
  // and the repaint sees the *latest* state. Leading-edge would paint stale
  // data and then still owe a trailing paint.

  const DEFAULT_COALESCE_MS = 120;

  // makeCoalescer(run, opts) → { schedule, flush, cancel, pending, stats }
  //
  // opts.waitMs   — burst window (default 120ms)
  // opts.maxWaitMs— never starve a repaint past this even under a steady
  //                 stream of pushes (default 600ms)
  // opts.setTimer/clearTimer — injectable for tests
  // opts.now      — injectable clock for tests
  function makeCoalescer(run, opts) {
    opts = opts || {};
    const waitMs = typeof opts.waitMs === 'number' ? opts.waitMs : DEFAULT_COALESCE_MS;
    const maxWaitMs = typeof opts.maxWaitMs === 'number' ? opts.maxWaitMs : waitMs * 5;
    const setTimer = opts.setTimer || function (fn, ms) { return setTimeout(fn, ms); };
    const clearTimer = opts.clearTimer || function (t) { clearTimeout(t); };
    const now = opts.now || function () { return Date.now(); };

    let timer = null;
    let firstRequest = 0;
    const stats = { scheduled: 0, ran: 0, coalesced: 0 };

    function fire() {
      timer = null;
      firstRequest = 0;
      stats.ran++;
      run();
    }

    function schedule() {
      stats.scheduled++;
      const t = now();
      if (timer === null) {
        firstRequest = t;
      } else {
        stats.coalesced++;
        // Steady stream: cap total delay so the tree cannot starve.
        if (t - firstRequest >= maxWaitMs) {
          clearTimer(timer);
          fire();
          return;
        }
        clearTimer(timer);
      }
      timer = setTimer(fire, waitMs);
    }

    function flush() {
      if (timer === null) return false;
      clearTimer(timer);
      fire();
      return true;
    }

    function cancel() {
      if (timer === null) return false;
      clearTimer(timer);
      timer = null;
      firstRequest = 0;
      return true;
    }

    return {
      schedule: schedule,
      flush: flush,
      cancel: cancel,
      pending: function () { return timer !== null; },
      stats: stats,
    };
  }

  // ── Descriptor diffing ─────────────────────────────────────────────
  //
  // A descriptor describes one painted row:
  //   { key, parentKey, className, html, dataset:{}, selectable, dismiss }
  // Order in the array is paint order (depth-first, already sorted).

  function datasetKey(ds) {
    if (!ds) return '';
    return Object.keys(ds).sort().map(function (k) {
      return k + '=' + String(ds[k] == null ? '' : ds[k]);
    }).join('');
  }

  // Shape only: which rows exist, in what order, under which parent.
  // Row *content* (html/class/dataset) is deliberately excluded — content
  // changes patch in place; shape changes need real DOM surgery.
  function structureKey(descriptors) {
    const list = descriptors || [];
    const parts = new Array(list.length);
    for (let i = 0; i < list.length; i++) {
      const d = list[i] || {};
      parts[i] = String(d.key == null ? '' : d.key) + '' +
        String(d.parentKey == null ? '' : d.parentKey);
    }
    return parts.join('');
  }

  // Full identity of a row's painted output.
  function rowKey(d) {
    if (!d) return '';
    return String(d.className == null ? '' : d.className) + '' +
      String(d.html == null ? '' : d.html) + '' +
      datasetKey(d.dataset);
  }

  // diffPlan(prev, next) → { mode, patches, changed, total }
  //
  //   'rebuild' — first paint, or the tree shape changed (spawn/reap/reparent)
  //   'patch'   — same shape; only the listed row indices need repainting
  //   'noop'    — byte-identical paint; touch nothing at all
  //
  // 'noop' is the common case for a redundant push, and 'patch' with one
  // entry is the common case for a working agent's progress line.
  function diffPlan(prev, next) {
    const nextList = next || [];
    if (!prev) {
      return { mode: 'rebuild', patches: [], changed: nextList.length, total: nextList.length };
    }
    if (structureKey(prev) !== structureKey(nextList)) {
      return { mode: 'rebuild', patches: [], changed: nextList.length, total: nextList.length };
    }
    const patches = [];
    for (let i = 0; i < nextList.length; i++) {
      if (rowKey(prev[i]) !== rowKey(nextList[i])) {
        patches.push({ index: i, descriptor: nextList[i] });
      }
    }
    return {
      mode: patches.length ? 'patch' : 'noop',
      patches: patches,
      changed: patches.length,
      total: nextList.length,
    };
  }

  // ── Poll pacing ────────────────────────────────────────────────────
  //
  // Every RHS poll ran at full rate forever, including in a background tab.
  // That is pure sustained CPU (fetch + JSON + paint) for a view nobody is
  // looking at — a large share of the idle Firefox burn.

  // pollPlan(baseMs, {hidden}) → { run, delayMs }
  //
  // Hidden tabs skip the work entirely and re-check on a slow cadence; the
  // visibilitychange handler forces an immediate refresh on return, so
  // nothing is stale by the time the owner looks.
  const HIDDEN_MIN_MS = 60000;

  function pollPlan(baseMs, state) {
    const base = typeof baseMs === 'number' && baseMs > 0 ? baseMs : 1000;
    const hidden = !!(state && state.hidden);
    if (hidden) {
      return { run: false, delayMs: Math.max(base, HIDDEN_MIN_MS) };
    }
    return { run: true, delayMs: base };
  }

  return {
    DEFAULT_COALESCE_MS: DEFAULT_COALESCE_MS,
    HIDDEN_MIN_MS: HIDDEN_MIN_MS,
    makeCoalescer: makeCoalescer,
    structureKey: structureKey,
    rowKey: rowKey,
    diffPlan: diffPlan,
    pollPlan: pollPlan,
  };
}));
