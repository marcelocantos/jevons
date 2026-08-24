// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Composer ↔ transcript layout policy (🎯T70 / T70.1 / T123 / T478).
 * Port of web/scripts/composer_layout.js — DOM-free so vitest can run it.
 */

/** After the composer grows, shift scrollTop so the latest reply stays in view. */
export function scrollTopAfterComposerGrow(
  scrollTop: number,
  clientHeightAfter: number,
  scrollHeight: number,
  growPx: number,
): number {
  const top = Number(scrollTop) || 0;
  const grow = Number(growPx) || 0;
  if (!(grow > 0)) return Math.max(0, top);
  const client = Math.max(0, Number(clientHeightAfter) || 0);
  const sh = Math.max(0, Number(scrollHeight) || 0);
  const maxScroll = Math.max(0, sh - client);
  return Math.min(Math.max(0, top + grow), maxScroll);
}

/** True when the last message's full box lies inside the viewport. */
export function lastMessageFullyVisible(
  lastTop: number,
  lastHeight: number,
  scrollTop: number,
  clientHeight: number,
  marginPx = 1,
): boolean {
  const top = Number(lastTop) || 0;
  const h = Math.max(0, Number(lastHeight) || 0);
  const st = Number(scrollTop) || 0;
  const ch = Math.max(0, Number(clientHeight) || 0);
  const lastBot = top + h;
  const viewTop = st;
  const viewBot = st + ch;
  return lastBot <= viewBot + marginPx && top >= viewTop - marginPx;
}

/**
 * Tall last bubble flush to bottom, then composer grows.
 * Without scroll adjust the bubble is covered; with it, it stays visible.
 */
export function growthWithoutCoverHolds(lastHeight: number, clientHeight: number, growPx: number): boolean {
  const lh = Math.max(1, Number(lastHeight) || 0);
  const ch = Math.max(1, Number(clientHeight) || 0);
  const grow = Math.max(0, Number(growPx) || 0);
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
  if (grow === 0) return visibleWith;
  return coveredWithout && visibleWith;
}

/**
 * 🎯T478: used height for an empty / seed-only composer is the control
 * height, never the wrapping placeholder's scrollHeight.
 */
export function emptyComposerUsedHeight(scrollHeight: number, controlH: number, isEmpty: boolean): number {
  if (isEmpty) return Number(controlH) || 0;
  return Number(scrollHeight) || 0;
}

/** CSS grow cap so the composer cannot eat the latest reply (🎯T70.1). */
export const COMPOSER_MAX_HEIGHT = '28vh';
