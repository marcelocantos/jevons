// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Virtualised chat list helpers (🎯T56). DOM-free core so Node can
// require(); browser integration dematerialises off-screen message
// bodies into height-preserving shells.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.VirtualList = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Default overscan in px beyond the viewport.
  const DEFAULT_BUFFER = 800;

  // itemTops: array of { top, height } relative to container.
  // Returns indices that should be fully materialised.
  function visibleIndices(itemTops, scrollTop, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const viewTop = scrollTop - buf;
    const viewBot = scrollTop + clientHeight + buf;
    const out = [];
    for (let i = 0; i < itemTops.length; i++) {
      const it = itemTops[i];
      const top = it.top;
      const bot = top + (it.height || 0);
      if (bot >= viewTop && top <= viewBot) out.push(i);
    }
    return out;
 }

  function shouldMaterialize(top, height, scrollTop, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const viewTop = scrollTop - buf;
    const viewBot = scrollTop + clientHeight + buf;
    const bot = top + (height || 0);
    return bot >= viewTop && top <= viewBot;
  }

  // Count how many items would stay materialised for a long list
  // scrolled to the bottom (oracle for "bounded DOM work").
  function materialisedCount(n, avgHeight, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const scrollTop = Math.max(0, n * avgHeight - clientHeight);
    const tops = [];
    for (let i = 0; i < n; i++) {
      tops.push({ top: i * avgHeight, height: avgHeight });
    }
    return visibleIndices(tops, scrollTop, clientHeight, buf).length;
  }

  return {
    DEFAULT_BUFFER: DEFAULT_BUFFER,
    visibleIndices: visibleIndices,
    shouldMaterialize: shouldMaterialize,
    materialisedCount: materialisedCount,
  };
}));
