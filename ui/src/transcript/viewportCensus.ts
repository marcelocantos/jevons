// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Pure 🎯T494 / 🎯T494.1 census predicates (shared with scripts/chat-ui-test/viewport_census.js). */

export const VOID_BELOW_VISIBLE_PX = 120;
export const WHEEL_AT_END_PX = 16;

export function latestOnHardReloadFail(opts: {
  fabHidden?: boolean;
  followMode?: string;
  atBottom?: boolean;
}): boolean {
  if (opts.fabHidden === false) return true;
  if (opts.followMode === 'track' && opts.atBottom === false) return true;
  return false;
}

export function emptySlotDesertFail(emptySlots: number): boolean {
  return (Number(emptySlots) || 0) > 0;
}

export function packedPaneFail(visibleBubbles: number, min = 2): boolean {
  return (Number(visibleBubbles) || 0) < min;
}

export function maxInkGapPx(
  rects: { top: number; bottom: number }[],
): number {
  const boxes = (rects || [])
    .filter((r) => Number.isFinite(r.top) && Number.isFinite(r.bottom))
    .slice()
    .sort((a, b) => a.top - b.top);
  let max = 0;
  for (let i = 1; i < boxes.length; i++) {
    const gap = boxes[i].top - boxes[i - 1].bottom;
    if (gap > max) max = gap;
  }
  return max;
}

export function desertGapFail(maxGap: number, clientHeight: number): boolean {
  const ch = Number(clientHeight) || 0;
  const cap = Math.max(120, ch * 0.25);
  return (Number(maxGap) || 0) > cap;
}

export function voidBelowLastFail(
  lastContentBottom: number,
  canvasHeight: number,
  epsPx = VOID_BELOW_VISIBLE_PX,
): boolean {
  const h = Number(canvasHeight) || 0;
  const end = Number(lastContentBottom) || 0;
  if (h <= 0 || end <= 0) return false;
  return h - end > epsPx;
}

export function liveEndDisagreeFail(
  pinWant: number,
  canvasEndPin: number,
  epsPx = 16,
): boolean {
  return Math.abs((Number(pinWant) || 0) - (Number(canvasEndPin) || 0)) > epsPx;
}

export function wheelDownFoughtFail(
  dist: number,
  before: number,
  after: number,
  tracking: boolean,
): boolean {
  if (!tracking) return false;
  if ((Number(dist) || 0) <= WHEEL_AT_END_PX) return false;
  return Math.abs((Number(after) || 0) - (Number(before) || 0)) < 2;
}

export function wheelRoundTripSnapFail(opts: {
  pinSt: number;
  afterRound: number;
  dist: number;
  fabHidden?: boolean;
}): boolean {
  if (opts.fabHidden === false) {
    return (
      Math.abs((Number(opts.afterRound) || 0) - (Number(opts.pinSt) || 0)) < 2 &&
      (Number(opts.dist) || 0) > WHEEL_AT_END_PX
    );
  }
  return false;
}
