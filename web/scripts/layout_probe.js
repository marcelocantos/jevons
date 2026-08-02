// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Layout probe surface for 🎯T110.1 / T70.1.
// Pure geometry helpers are DOM-free (Node-requireable). Browser bind()
// attaches window.JevonsProbe.snapshot() over live #messages / #input /
// last-reply metrics.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.LayoutProbe = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Product contract: #input max-height 28vh (🎯T70.1).
  const COMPOSER_MAX_VH = 28;
  const DEFAULT_NEAR_BOTTOM_PX = 48;
  const MIN_LAST_REPLY_VISIBLE = 0.15;

  /** Fraction of [elTop, elTop+elH) intersecting the viewport. */
  function visibleRatio(elTop, elH, viewTop, viewH) {
    if (!(elH > 0) || !(viewH > 0)) return 0;
    const elBot = elTop + elH;
    const viewBot = viewTop + viewH;
    let overlap = Math.min(elBot, viewBot) - Math.max(elTop, viewTop);
    if (overlap <= 0) return 0;
    if (overlap > elH) overlap = elH;
    return overlap / elH;
  }

  function nearBottom(scrollTop, scrollHeight, clientHeight, thresholdPx) {
    const thr = typeof thresholdPx === 'number' ? thresholdPx : DEFAULT_NEAR_BOTTOM_PX;
    if (scrollHeight <= clientHeight) return true;
    return (scrollHeight - scrollTop - clientHeight) <= thr;
  }

  /**
   * Growth-without-cover policy (pure).
   * composerMaxVh must stay ≤ COMPOSER_MAX_VH; last-reply visible ratio
   * in the messages viewport must stay ≥ minVisible.
   */
  function growthWithoutCover(viewportH, composerMaxVh, messagesH, lastReplyH, minVisible) {
    const minV = typeof minVisible === 'number' ? minVisible : MIN_LAST_REPLY_VISIBLE;
    if (!(viewportH > 0) || !(messagesH > 0) || !(lastReplyH > 0)) {
      return { ratio: 0, ok: false };
    }
    const composerCap = viewportH * (composerMaxVh / 100);
    if (!(composerCap > 0) || composerMaxVh > 40) {
      return { ratio: 0, ok: false };
    }
    // Reply pinned to bottom of messages viewport.
    const ratio = visibleRatio(Math.max(0, messagesH - lastReplyH), lastReplyH, 0, messagesH);
    const ok = ratio >= minV && composerMaxVh <= COMPOSER_MAX_VH + 0.01;
    return { ratio: ratio, ok: ok };
  }

  /**
   * Build a metrics snapshot from raw box measurements (DOM-free).
   * inputs are plain numbers in CSS pixels.
   */
  function snapshotFromBoxes(boxes) {
    const b = boxes || {};
    const composerH = num(b.composerHeight);
    const messagesH = num(b.messagesViewportHeight);
    const messagesScrollTop = num(b.messagesScrollTop);
    const messagesScrollHeight = num(b.messagesScrollHeight);
    const lastTop = num(b.lastReplyTopInMessages);
    const lastH = num(b.lastReplyHeight);
    const viewportH = num(b.viewportHeight) || (composerH + messagesH);
    const thr = typeof b.nearBottomPx === 'number' ? b.nearBottomPx : DEFAULT_NEAR_BOTTOM_PX;

    const lastVisible = visibleRatio(lastTop, lastH, messagesScrollTop, messagesH);
    const atBottom = nearBottom(messagesScrollTop, messagesScrollHeight, messagesH, thr);
    const policy = growthWithoutCover(
      viewportH,
      typeof b.composerMaxVh === 'number' ? b.composerMaxVh : COMPOSER_MAX_VH,
      messagesH,
      lastH || 1,
      MIN_LAST_REPLY_VISIBLE
    );

    return {
      composerHeight: composerH,
      messagesViewportHeight: messagesH,
      lastReplyVisibleRatio: lastVisible,
      nearBottom: atBottom,
      composerMaxVh: typeof b.composerMaxVh === 'number' ? b.composerMaxVh : COMPOSER_MAX_VH,
      growthWithoutCoverOk: policy.ok,
      viewportHeight: viewportH,
      lastReplyHeight: lastH,
    };
  }

  function num(v) {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }

  /**
   * Browser: read live DOM. opts may override selectors.
   * Returns the same shape as snapshotFromBoxes.
   */
  function snapshotDOM(doc, win, opts) {
    doc = doc || (typeof document !== 'undefined' ? document : null);
    win = win || (typeof window !== 'undefined' ? window : null);
    opts = opts || {};
    if (!doc || !win) {
      return snapshotFromBoxes({});
    }
    const messages = doc.querySelector(opts.messagesSelector || '#messages');
    const input = doc.querySelector(opts.inputSelector || '#input');
    const inputBar = doc.querySelector(opts.inputBarSelector || '#input-bar');
    const chatPane = doc.querySelector(opts.chatPaneSelector || '#chat-pane');

    let last = null;
    if (messages) {
      const nodes = messages.querySelectorAll('.msg.jevons, .msg.user, .msg');
      if (nodes.length) last = nodes[nodes.length - 1];
    }

    const msgRect = messages ? messages.getBoundingClientRect() : { height: 0, top: 0 };
    const composerEl = inputBar || input;
    const composerRect = composerEl ? composerEl.getBoundingClientRect() : { height: 0 };
    let lastTopInMsg = 0;
    let lastH = 0;
    if (last && messages) {
      const lr = last.getBoundingClientRect();
      lastTopInMsg = (lr.top - msgRect.top) + (messages.scrollTop || 0);
      lastH = lr.height;
    }

    return snapshotFromBoxes({
      composerHeight: composerRect.height,
      messagesViewportHeight: msgRect.height,
      messagesScrollTop: messages ? messages.scrollTop : 0,
      messagesScrollHeight: messages ? messages.scrollHeight : 0,
      lastReplyTopInMessages: lastTopInMsg,
      lastReplyHeight: lastH,
      viewportHeight: chatPane
        ? chatPane.getBoundingClientRect().height
        : (win.innerHeight || 0),
      composerMaxVh: COMPOSER_MAX_VH,
      nearBottomPx: opts.nearBottomPx,
    });
  }

  /**
   * Install window.JevonsProbe (or root.JevonsProbe).
   * Idempotent.
   */
  function bind(root, doc, win) {
    root = root || (typeof window !== 'undefined' ? window : null);
    if (!root) return null;
    const probe = {
      COMPOSER_MAX_VH: COMPOSER_MAX_VH,
      MIN_LAST_REPLY_VISIBLE: MIN_LAST_REPLY_VISIBLE,
      snapshot: function () {
        return snapshotDOM(doc || root.document, win || root, {});
      },
      visibleRatio: visibleRatio,
      nearBottom: nearBottom,
      growthWithoutCover: growthWithoutCover,
      snapshotFromBoxes: snapshotFromBoxes,
    };
    root.JevonsProbe = probe;
    return probe;
  }

  return {
    COMPOSER_MAX_VH: COMPOSER_MAX_VH,
    MIN_LAST_REPLY_VISIBLE: MIN_LAST_REPLY_VISIBLE,
    visibleRatio: visibleRatio,
    nearBottom: nearBottom,
    growthWithoutCover: growthWithoutCover,
    snapshotFromBoxes: snapshotFromBoxes,
    snapshotDOM: snapshotDOM,
    bind: bind,
  };
}));
