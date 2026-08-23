// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Harnessable wall clock (🎯T537.2). Product code reads current time only
// through JevonsClock.now / .date. Date.now and `new Date()` with no args
// live here and nowhere else in the cockpit. Playwright freezes via
// window.__JEVONS_CLOCK_NOW (init) or JevonsClock.setNow(ms).

(function (root, factory) {
  var clock = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = clock;
  }
  if (root) root.JevonsClock = clock;
}(typeof globalThis !== 'undefined' ? globalThis : this, function () {
  'use strict';

  var frozen = null;

  function readInitFreeze() {
    var g = typeof globalThis !== 'undefined' ? globalThis : null;
    if (!g) return null;
    var v = g.__JEVONS_CLOCK_NOW;
    if (v == null || v === false) return null;
    var n = Number(v);
    return isFinite(n) ? n : null;
  }

  frozen = readInitFreeze();

  function now() {
    return frozen != null ? frozen : Date.now();
  }

  function date() {
    return new Date(now());
  }

  function setNow(ms) {
    if (ms == null || ms === false) {
      frozen = null;
      return;
    }
    var n = Number(ms);
    frozen = isFinite(n) ? n : null;
  }

  function reset() {
    frozen = null;
  }

  function isFrozen() {
    return frozen != null;
  }

  return {
    now: now,
    date: date,
    setNow: setNow,
    reset: reset,
    isFrozen: isFrozen,
  };
}));
