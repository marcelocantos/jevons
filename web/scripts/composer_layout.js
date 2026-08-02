// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Composer ↔ transcript layout policy (🎯T70.1). DOM-free so Node can
// require(); browser autoGrow applies the returned scrollTop after the
// textarea grows and the messages viewport shrinks.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ComposerLayout = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // After the composer grows by growPx, the flex transcript viewport
  // shrinks by the same amount. Browsers keep scrollTop fixed, so the
  // bottom of the transcript (latest reply) slides under the taller
  // composer. Shift scrollTop up by growPx (clamped) so the same bottom
  // content stays in view.
  //
  // clientHeightAfter / scrollHeight are post-reflow metrics on #messages.
  function scrollTopAfterComposerGrow(scrollTop, clientHeightAfter, scrollHeight, growPx) {
    const top = Number(scrollTop) || 0;
    const grow = Number(growPx) || 0;
    if (!(grow > 0)) return Math.max(0, top);
    const client = Math.max(0, Number(clientHeightAfter) || 0);
    const sh = Math.max(0, Number(scrollHeight) || 0);
    const maxScroll = Math.max(0, sh - client);
    return Math.min(Math.max(0, top + grow), maxScroll);
  }

  // True when the last message's full box lies inside the viewport
  // [scrollTop, scrollTop+clientHeight], allowing a small bottom margin
  // for sub-pixel/layout noise (default 1px).
  function lastMessageFullyVisible(lastTop, lastHeight, scrollTop, clientHeight, marginPx) {
    const margin = typeof marginPx === 'number' ? marginPx : 1;
    const top = Number(lastTop) || 0;
    const h = Math.max(0, Number(lastHeight) || 0);
    const st = Number(scrollTop) || 0;
    const ch = Math.max(0, Number(clientHeight) || 0);
    const lastBot = top + h;
    const viewTop = st;
    const viewBot = st + ch;
    return lastBot <= viewBot + margin && top >= viewTop - margin;
  }

  // Oracle for growth-without-cover: tall last bubble flush to bottom,
  // then composer grows. Without scroll adjust the bubble is covered;
  // with scrollTopAfterComposerGrow it stays fully visible.
  function growthWithoutCoverHolds(lastHeight, clientHeight, growPx) {
    const lh = Math.max(1, Number(lastHeight) || 0);
    const ch = Math.max(1, Number(clientHeight) || 0);
    const grow = Math.max(0, Number(growPx) || 0);
    // Transcript: filler above + last bubble flush to content bottom.
    const filler = Math.max(ch, 200);
    const scrollHeight = filler + lh;
    const lastTop = filler;
    const scrollTop = Math.max(0, scrollHeight - ch);
    const clientAfter = Math.max(0, ch - grow);
    if (clientAfter <= 0) return false;
    const unfixedTop = scrollTop;
    const fixedTop = scrollTopAfterComposerGrow(scrollTop, clientAfter, scrollHeight, grow);
    const coveredWithout = !lastMessageFullyVisible(lastTop, lh, unfixedTop, clientAfter);
    const visibleWith = lastMessageFullyVisible(lastTop, lh, fixedTop, clientAfter);
    // When grow is 0, both should stay visible (no-op).
    if (grow === 0) return visibleWith;
    return coveredWithout && visibleWith;
  }

  return {
    scrollTopAfterComposerGrow: scrollTopAfterComposerGrow,
    lastMessageFullyVisible: lastMessageFullyVisible,
    growthWithoutCoverHolds: growthWithoutCoverHolds,
  };
}));
