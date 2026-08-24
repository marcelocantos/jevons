// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T106 / T537.2 / T540.1.3: one size-clip + pocket policy for AgentTranscript. */

export const DEFAULT_COLLAPSED_PX = 224;
export const COLLAPSE_EPS_PX = 1;

/** CSS --expand-tab-height / --expand-tab-clearance (🎯T166). */
export const EXPAND_TAB_HEIGHT_REM = 1.05;
export const EXPAND_TAB_CLEARANCE_REM = 0.35;

/** 🎯T261: slack matching typical jump-to-bottom hysteresis. */
export const NEAR_END_SLACK_PX = 48;
/** 🎯T341: require real ink in view before auto-expand (enter hysteresis). */
export const MIN_VISIBLE_PX_FOR_AUTO_EXPAND = 8;

export function shouldClip(
  fullH: number,
  collapsedH: number = DEFAULT_COLLAPSED_PX,
): boolean {
  if (!(fullH > 0) || !(collapsedH > 0)) return false;
  return fullH > collapsedH + COLLAPSE_EPS_PX;
}

export function clipClassName(base: string, fullH: number, collapsedH?: number): string {
  if (!shouldClip(fullH, collapsedH)) return base;
  return base ? `${base} msg-clipped` : 'msg-clipped';
}

/** Vanilla pocket-tab glyphs (U+25B4 ▴ / U+25BE ▾). */
export function expandTabChevron(expanded: boolean): string {
  return expanded ? '\u25B4' : '\u25BE';
}

/** Bottom padding (px) reserved so the last line clears the collapse tab. */
export function expandedTabClearancePx(rootFontPx = 16): number {
  const root = rootFontPx > 0 ? rootFontPx : 16;
  return (EXPAND_TAB_HEIGHT_REM + EXPAND_TAB_CLEARANCE_REM) * root;
}

/** Painted row height used for viewport math (clipped box, not full scrollHeight). */
export function paintedClipHeight(
  fullH: number,
  expanded: boolean,
  collapsedH: number = DEFAULT_COLLAPSED_PX,
): number {
  if (!shouldClip(fullH, collapsedH) || expanded) return fullH;
  return collapsedH;
}

export function lastMessageRowIndex(kinds: readonly string[]): number {
  for (let i = kinds.length - 1; i >= 0; i--) {
    if (kinds[i] === 'user' || kinds[i] === 'assistant') return i;
  }
  return -1;
}

/** 🎯T246: strict viewport intersection (no materialize buffer). */
export function anyPartInViewport(
  top: number,
  height: number,
  scrollTop: number,
  clientHeight: number,
): boolean {
  const t = Number(top) || 0;
  const h = Number(height) || 0;
  const st = Number(scrollTop) || 0;
  const ch = Number(clientHeight) || 0;
  const bot = t + h;
  const viewBot = st + ch;
  return bot > st && t < viewBot;
}

export function visibleOverlapPx(
  top: number,
  height: number,
  scrollTop: number,
  clientHeight: number,
): number {
  const t = Number(top) || 0;
  const h = Number(height) || 0;
  const st = Number(scrollTop) || 0;
  const ch = Number(clientHeight) || 0;
  if (h <= 0 || ch <= 0) return 0;
  const bot = t + h;
  const viewBot = st + ch;
  const oTop = Math.max(t, st);
  const oBot = Math.min(bot, viewBot);
  return Math.max(0, oBot - oTop);
}

export function isFullyAboveViewport(top: number, height: number, scrollTop: number): boolean {
  const t = Number(top) || 0;
  const h = Number(height) || 0;
  const st = Number(scrollTop) || 0;
  return t + h <= st;
}

/**
 * Auto-collapse policy for tall bubbles that were auto-expanded (T66/T246).
 * Latest always stays expanded. Manual toggle never auto-collapses.
 * Non-latest auto-expanded may collapse only when fully outside the strict
 * viewport (any remaining pixel on-screen keeps it expanded).
 */
export function shouldAutoCollapseOffScreen(opts: {
  isLatest?: boolean;
  userToggled?: boolean;
  autoExpanded?: boolean;
  top?: number;
  height?: number;
  scrollTop?: number;
  clientHeight?: number;
}): boolean {
  const o = opts || {};
  if (o.isLatest) return false;
  if (o.userToggled) return false;
  if (!o.autoExpanded) return false;
  return !anyPartInViewport(o.top ?? 0, o.height ?? 0, o.scrollTop ?? 0, o.clientHeight ?? 0);
}

export function isNearTranscriptEnd(
  scrollTop: number,
  scrollHeight: number,
  clientHeight: number,
  slackPx?: number,
): boolean {
  const slack = slackPx == null ? NEAR_END_SLACK_PX : Number(slackPx);
  const st = Number(scrollTop) || 0;
  const sh = Number(scrollHeight) || 0;
  const ch = Number(clientHeight) || 0;
  const s = Number.isFinite(slack) ? slack : NEAR_END_SLACK_PX;
  return st + ch >= sh - s;
}

/**
 * 🎯T261: while pinned near end, tall in-view bubbles (not user-toggled)
 * should be auto-expanded — not only the single latest message.
 */
export function shouldAutoExpandInView(opts: {
  tall?: boolean;
  nearEnd?: boolean;
  userToggled?: boolean;
  historyReplayActive?: boolean;
  minVisiblePx?: number;
  top?: number;
  height?: number;
  scrollTop?: number;
  clientHeight?: number;
}): boolean {
  const o = opts || {};
  if (o.userToggled) return false;
  if (!o.tall) return false;
  if (!o.nearEnd) return false;
  if (o.historyReplayActive) return false;
  const minPx = o.minVisiblePx != null ? Number(o.minVisiblePx) : MIN_VISIBLE_PX_FOR_AUTO_EXPAND;
  const need = Number.isFinite(minPx) && minPx > 0 ? minPx : MIN_VISIBLE_PX_FOR_AUTO_EXPAND;
  return visibleOverlapPx(o.top ?? 0, o.height ?? 0, o.scrollTop ?? 0, o.clientHeight ?? 0) >= need;
}

/** Mid history-replay the viewport is not the post-pin end. 🎯T261. */
export function shouldRunOffScreenCollapse(historyReplayActive: unknown): boolean {
  return !historyReplayActive;
}

/** 🎯T66: the latest user/assistant row stays expanded when tall. */
export function shouldStayExpandedLatest(opts: {
  isLatest?: boolean;
  tall?: boolean;
  userToggled?: boolean;
}): boolean {
  const o = opts || {};
  if (o.userToggled) return false;
  return !!o.isLatest && !!o.tall;
}

export type ClipExpandInput = {
  tall: boolean;
  isLatest: boolean;
  userToggled: boolean;
  expanded: boolean;
  autoExpanded: boolean;
  nearEnd: boolean;
  historyReplayActive: boolean;
  top: number;
  height: number;
  scrollTop: number;
  clientHeight: number;
};

/** One auto expand/collapse decision for both panes (🎯T480). */
export function nextAutoExpanded(input: ClipExpandInput): boolean {
  if (input.userToggled) return input.expanded;
  if (!input.tall) return false;
  if (shouldStayExpandedLatest(input)) return true;
  if (
    shouldAutoExpandInView({
      tall: input.tall,
      nearEnd: input.nearEnd,
      userToggled: input.userToggled,
      historyReplayActive: input.historyReplayActive,
      top: input.top,
      height: input.height,
      scrollTop: input.scrollTop,
      clientHeight: input.clientHeight,
    })
  ) {
    return true;
  }
  if (
    shouldRunOffScreenCollapse(input.historyReplayActive) &&
    shouldAutoCollapseOffScreen({
      isLatest: input.isLatest,
      userToggled: input.userToggled,
      autoExpanded: input.autoExpanded,
      top: input.top,
      height: input.height,
      scrollTop: input.scrollTop,
      clientHeight: input.clientHeight,
    })
  ) {
    return false;
  }
  return input.expanded;
}
