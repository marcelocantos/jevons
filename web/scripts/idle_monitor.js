// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// In-page idle-storm classifier (🎯T532 self-monitor). DOM-free so Node
// tests can require(). The tab cannot read Isolated Web Content core-%;
// it can see snap/RO rate, longtasks, and rAF hitches.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.IdleMonitor = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // T532 idle Chat was ~118 snaps/s. 15/s over a 2s window is still a loop.
  const STORM_SNAPS_PER_S = 15;
  const STORM_LONGTASKS = 3;
  const WINDOW_MS = 2000;
  const CONSECUTIVE_WINDOWS = 2;

  function windowedRate(delta, windowMs) {
    if (!(windowMs > 0) || !(delta >= 0)) return 0;
    return delta / (windowMs / 1000);
  }

  function classifyIdleStorm(o) {
    o = o || {};
    const streaming = !!o.streaming;
    const replaying = !!o.replaying;
    const snapsPerS = Number(o.snapsPerS) || 0;
    const longtasks = Number(o.longtasks) || 0;
    const rafPerS = Number(o.rafPerS) || 0;
    const idle = !streaming && !replaying;
    if (!idle) {
      return { idle: false, warn: false, reason: '', snapsPerS: snapsPerS, rafPerS: rafPerS };
    }
    if (snapsPerS >= STORM_SNAPS_PER_S) {
      return { idle: true, warn: true, reason: 'snap', snapsPerS: snapsPerS, rafPerS: rafPerS };
    }
    if (rafPerS >= STORM_SNAPS_PER_S) {
      return { idle: true, warn: true, reason: 'raf', snapsPerS: snapsPerS, rafPerS: rafPerS };
    }
    if (longtasks >= STORM_LONGTASKS) {
      return { idle: true, warn: true, reason: 'longtask', snapsPerS: snapsPerS, rafPerS: rafPerS };
    }
    return { idle: true, warn: false, reason: '', snapsPerS: snapsPerS, rafPerS: rafPerS };
  }

  function stormHysteresis(streak, classified) {
    if (!classified || !classified.warn) return { warn: false, streak: 0 };
    const n = (Number(streak) || 0) + 1;
    return { warn: n >= CONSECUTIVE_WINDOWS, streak: n };
  }

  function bannerText(c) {
    if (!c || !c.warn) return '';
    const rate = Number(c.snapsPerS);
    const rateS = Number.isFinite(rate) ? String(Math.round(rate)) : '?';
    if (c.reason === 'snap') {
      return 'Idle chat is snap-looping (' + rateS + '/s). Hard-reload if the tab stays hot.';
    }
    if (c.reason === 'longtask') {
      return 'Idle chat is logging long tasks. Hard-reload if the tab stays hot.';
    }
    if (c.reason === 'raf') {
      return 'Idle chat is hitching on animation frames. Hard-reload if the tab stays hot.';
    }
    return 'Idle chat is using unusual main-thread time. Hard-reload if the tab stays hot.';
  }

  function tickWindow(prev, nowCount, nowMs) {
    const p = prev || { count: 0, ms: 0 };
    const dCount = Math.max(0, (Number(nowCount) || 0) - (Number(p.count) || 0));
    const dMs = Math.max(0, (Number(nowMs) || 0) - (Number(p.ms) || 0));
    return {
      rate: windowedRate(dCount, dMs),
      delta: dCount,
      windowMs: dMs,
      next: { count: Number(nowCount) || 0, ms: Number(nowMs) || 0 },
    };
  }

  return {
    STORM_SNAPS_PER_S: STORM_SNAPS_PER_S,
    STORM_LONGTASKS: STORM_LONGTASKS,
    WINDOW_MS: WINDOW_MS,
    CONSECUTIVE_WINDOWS: CONSECUTIVE_WINDOWS,
    windowedRate: windowedRate,
    classifyIdleStorm: classifyIdleStorm,
    stormHysteresis: stormHysteresis,
    bannerText: bannerText,
    tickWindow: tickWindow,
  };
}));
