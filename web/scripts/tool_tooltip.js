// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Activity-strip tool-step tooltip layout policy (🎯T122). DOM-free constants
// so hermetic tests lock chrome choices without Playwright.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ToolTooltip = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Must match web/index.html .turn-tip / .turn-item policy.
  const MIN_WIDTH_PX = 320;
  const MAX_WIDTH_PX = 720;
  const MAX_WIDTH_CSS = 'min(720px, 92vw)';
  const MIN_WIDTH_CSS = '320px';
  // Prefer growing horizontally; wrap only after tip hits max-width.
  const ITEM_WHITE_SPACE = 'pre-wrap';
  const ITEM_WORD_BREAK = 'normal';
  const FORBIDDEN_WORD_BREAK = 'break-word';

  // Estimate whether a mono ~11px line would fit on one row inside max width
  // (approx 7px/char — oracle for "typical summaries stay single-line").
  const APPROX_CH_PX = 7;

  function estimateLineCount(text, maxWidthPx) {
    const s = String(text || '').replace(/\s+/g, ' ').trim();
    if (!s) return 0;
    const max = typeof maxWidthPx === 'number' && maxWidthPx > 0 ? maxWidthPx : MAX_WIDTH_PX;
    const charsPerLine = Math.max(1, Math.floor(max / APPROX_CH_PX));
    return Math.ceil(s.length / charsPerLine);
  }

  // Policy: a typical T116 summary (≤60 chars) is one line inside tip max-width.
  function typicalSummaryIsSingleLine(summary) {
    return estimateLineCount(summary, MAX_WIDTH_PX) <= 1;
  }

  return {
    MIN_WIDTH_PX: MIN_WIDTH_PX,
    MAX_WIDTH_PX: MAX_WIDTH_PX,
    MAX_WIDTH_CSS: MAX_WIDTH_CSS,
    MIN_WIDTH_CSS: MIN_WIDTH_CSS,
    ITEM_WHITE_SPACE: ITEM_WHITE_SPACE,
    ITEM_WORD_BREAK: ITEM_WORD_BREAK,
    FORBIDDEN_WORD_BREAK: FORBIDDEN_WORD_BREAK,
    estimateLineCount: estimateLineCount,
    typicalSummaryIsSingleLine: typicalSummaryIsSingleLine,
  };
}));
