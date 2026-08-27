// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Activity-strip tool-step tooltip layout policy (🎯T122). Port of vanilla
 * web/scripts/tool_tooltip.js. Must match cockpit.css .turn-tip / .turn-item.
 */
export const TOOL_TIP_MIN_WIDTH_PX = 320;
export const TOOL_TIP_MAX_WIDTH_PX = 720;
export const TOOL_TIP_MAX_WIDTH_CSS = 'min(720px, 92vw)';
export const TOOL_TIP_MIN_WIDTH_CSS = '320px';
/** Prefer growing horizontally; wrap only after the tip hits max-width. */
export const TOOL_ITEM_WHITE_SPACE = 'pre-wrap';
export const TOOL_ITEM_WORD_BREAK = 'normal';
export const TOOL_ITEM_FORBIDDEN_WORD_BREAK = 'break-word';

/** Mono ~11px ≈ 7px/char — oracle for "typical summaries stay single-line". */
const APPROX_CH_PX = 7;

export function estimateLineCount(text: string, maxWidthPx = TOOL_TIP_MAX_WIDTH_PX): number {
  const s = String(text || '').replace(/\s+/g, ' ').trim();
  if (!s) return 0;
  const max = maxWidthPx > 0 ? maxWidthPx : TOOL_TIP_MAX_WIDTH_PX;
  const charsPerLine = Math.max(1, Math.floor(max / APPROX_CH_PX));
  return Math.ceil(s.length / charsPerLine);
}

/** A typical T116 summary (≤60 chars) is one line inside tip max-width. */
export function typicalSummaryIsSingleLine(summary: string): boolean {
  return estimateLineCount(summary, TOOL_TIP_MAX_WIDTH_PX) <= 1;
}
