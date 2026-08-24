// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { DEFAULT_COLLAPSED_PX } from '../conversation/clip';

/** Default tanstack overscan once the post-reload band is measured. */
export const DEFAULT_ROW_OVERSCAN = 12;

/**
 * PageUp is 0.8 viewport. Measure this many viewports from the live end
 * before collapsing overscan so the first PageUp only walks measured rows
 * (🎯T494.1.3).
 */
export const HYDRATE_BAND_VIEWPORTS = 2.5;

export const HYDRATE_OVERSCAN_MAX = 96;

/**
 * First paint of a tall bubble is unclipped (fullH still 0). Those sizes
 * are not the PageUp band — treat them as unmeasured until clip lands.
 * Slack covers bubble chrome on top of --collapsed-max-height (14rem / 224).
 */
export const HYDRATE_STEADY_SLACK_PX = 80;

export function isHydrateSteadySize(
  size: number,
  collapsedH: number = DEFAULT_COLLAPSED_PX,
): boolean {
  const s = Number(size) || 0;
  if (!(s > 0)) return false;
  return s <= collapsedH + HYDRATE_STEADY_SLACK_PX;
}

/** Consecutive measured px walking backward from the last row. */
export function measuredPxFromEnd(measured: ReadonlyMap<number, number>, count: number): number {
  return measuredSuffixFromEnd(measured, count).px;
}

/** Suffix walk: stops at a gap or an unclipped-tall first paint. */
export function measuredSuffixFromEnd(
  measured: ReadonlyMap<number, number>,
  count: number,
): { px: number; complete: boolean } {
  let px = 0;
  const n = Math.max(0, count | 0);
  for (let i = n - 1; i >= 0; i--) {
    const s = measured.get(i);
    if (s == null || !isHydrateSteadySize(s)) {
      return { px, complete: false };
    }
    px += s;
  }
  return { px, complete: n === 0 || px > 0 };
}

/**
 * Standing overscan after hydrate: enough extra rows to cover a PageUp
 * even when the tail is 13px turn-markers. Collapsing to 12 lets the
 * first PageUp walk estimates and the canvas grows under the caret.
 */
export function standingOverscan(clientHeight: number, minRowPx = 16): number {
  const ch = Math.max(0, Number(clientHeight) || 0);
  if (ch <= 0) return HYDRATE_OVERSCAN_MAX;
  const extra = Math.ceil((ch * (HYDRATE_BAND_VIEWPORTS - 1)) / Math.max(8, minRowPx));
  return Math.min(HYDRATE_OVERSCAN_MAX, Math.max(DEFAULT_ROW_OVERSCAN, extra));
}

export function nextHydrateOverscan(o: {
  clientHeight: number;
  count: number;
  current: number;
  measuredFromEndPx: number;
  estimate: number;
  /** Entire list has a steady measured suffix (no gap / unclipped-tall). */
  suffixComplete?: boolean;
}): { overscan: number; settled: boolean } {
  const ch = Math.max(0, Number(o.clientHeight) || 0);
  const count = Math.max(0, o.count | 0);
  const want = ch * HYDRATE_BAND_VIEWPORTS;
  const floor = standingOverscan(ch);
  if (count <= 0) {
    return { overscan: floor, settled: true };
  }
  if (ch <= 0) {
    return { overscan: Math.max(o.current, floor), settled: false };
  }
  // A short transcript that is fully steady-measured is done even if it
  // is shorter than the PageUp band. Do not settle on overscan>=count
  // alone — that fires while the last bubbles are still unclipped.
  if (o.measuredFromEndPx >= want || o.suffixComplete) {
    return { overscan: Math.min(count, floor), settled: true };
  }
  const est = Math.max(16, Number(o.estimate) || 16);
  const add = Math.max(8, Math.ceil((want - o.measuredFromEndPx) / est));
  const base = Math.max(o.current, floor);
  const next = Math.min(count, HYDRATE_OVERSCAN_MAX, base + add);
  return { overscan: next, settled: false };
}
