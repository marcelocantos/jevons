// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** When to request the next older page (T537.2). One decision, used by AgentTranscript. */
export const PAGE_TOP_PX = 48;

export function shouldRequestPage(o: {
  scrollTop: number;
  older?: number;
  truncated?: boolean;
  inFlight: boolean;
  following?: boolean;
  scrollHeight?: number;
  clientHeight?: number;
}): boolean {
  if (o.inFlight) return false;
  if (!o.older && !o.truncated) return false;
  // Live tail: scrollTop is 0 when the window is shorter than the pane.
  // That is the bottom, not history-top — paging here freezes mux follow.
  if (o.following) return false;
  const sh = Number(o.scrollHeight) || 0;
  const ch = Number(o.clientHeight) || 0;
  if (sh > 0 && ch > 0 && sh <= ch) return false;
  return o.scrollTop < PAGE_TOP_PX;
}

/** Explicit control when the live tail is shorter than the pane (🎯T1159). */
export function shouldOfferShowEarlier(o: {
  older?: number;
  truncated?: boolean;
  inFlight?: boolean;
  scrollHeight?: number;
  clientHeight?: number;
}): boolean {
  if (o.inFlight) return false;
  if (!o.older && !o.truncated) return false;
  const sh = Number(o.scrollHeight) || 0;
  const ch = Number(o.clientHeight) || 0;
  if (!(sh > 0 && ch > 0 && sh <= ch)) return false;
  return true;
}

/**
 * Auto-fill (🎯T1248): an opened transcript whose tail is shorter
 * than the pane pages older history by itself until the pane holds more than
 * a screen, instead of leaving an empty pane and a "Show earlier" button.
 * Prepending a `before` page does not leave the live window (🎯T1159 already
 * backfills while following). The page bound stops a run of folded
 * machine-only turns, which add no height, from pulling the whole history.
 */
export const AUTO_FILL_MAX_PAGES = 8;
export const AUTO_FILL_SCREENS = 1.5;

export function shouldAutoFillPane(o: {
  older?: number;
  inFlight?: boolean;
  scrollHeight?: number;
  clientHeight?: number;
  pages: number;
}): boolean {
  if (o.inFlight) return false;
  if (!o.older || o.older <= 0) return false;
  if (o.pages >= AUTO_FILL_MAX_PAGES) return false;
  const sh = Number(o.scrollHeight) || 0;
  const ch = Number(o.clientHeight) || 0;
  if (ch <= 0) return false;
  return sh < ch * AUTO_FILL_SCREENS;
}
