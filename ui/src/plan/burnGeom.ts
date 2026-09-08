// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Plan-usage burn-down sparkline (🎯T634).
 *
 * X is the published window period; Y is remaining 0–100. The line starts
 * at the first stored sample — never a fabricated 100% at t=0.
 */

import { limitSecondsFor } from './windowGeom';
import type { PlanHistoryPoint, PlanWindow } from './tickerGroups';

export const BURN_WIDTH = 100;
export const BURN_HEIGHT = 32;

export type BurnPoint = { x: number; y: number };

export type BurnPaths = {
  fill: string;
  line: string;
  points: BurnPoint[];
};

function clamp(v: number, lo: number, hi: number): number {
  if (v < lo) return lo;
  if (v > hi) return hi;
  return v;
}

/** Period [start, end] from resets_at minus the published/inferred length. */
export function periodBounds(w: PlanWindow): { start: number; end: number } | null {
  const resets = w.resets_at;
  if (!resets) return null;
  const end = Date.parse(resets);
  if (Number.isNaN(end)) return null;
  const limitSec = limitSecondsFor(w);
  if (limitSec == null) return null;
  return { start: end - limitSec * 1000, end };
}

export function historyPoints(w: PlanWindow): PlanHistoryPoint[] {
  const raw = w.history;
  if (!Array.isArray(raw) || raw.length === 0) return [];
  const out: PlanHistoryPoint[] = [];
  for (const p of raw) {
    if (!p || typeof p.at !== 'string') continue;
    if (typeof p.remaining_percent !== 'number' || !Number.isFinite(p.remaining_percent)) continue;
    const at = Date.parse(p.at);
    if (Number.isNaN(at)) continue;
    out.push({ at: p.at, remaining_percent: clamp(p.remaining_percent, 0, 100) });
  }
  return out;
}

/** Map stored samples onto the period. Empty when there is no period or no samples. */
export function burnPoints(w: PlanWindow): BurnPoint[] {
  const period = periodBounds(w);
  const samples = historyPoints(w);
  if (!period || !samples.length) return [];
  const span = period.end - period.start;
  if (!(span > 0)) return [];
  return samples.map((p) => {
    const at = Date.parse(p.at);
    const x = clamp((100 * (at - period.start)) / span, 0, BURN_WIDTH);
    const y = BURN_HEIGHT - (BURN_HEIGHT * clamp(p.remaining_percent, 0, 100)) / 100;
    return { x, y };
  });
}

export function burnPaths(w: PlanWindow): BurnPaths | null {
  const points = burnPoints(w);
  if (!points.length) return null;
  // One sample is a sliver of zero width if we only close the area on
  // itself — the live ticker looked empty after the first Refresh.
  // Give that point a visible stem down to the axis.
  if (points.length === 1) {
    const p = points[0];
    const half = 3.5;
    const x0 = clamp(p.x - half, 0, BURN_WIDTH);
    const x1 = clamp(p.x + half, 0, BURN_WIDTH);
    const line = `M${round(p.x)},${round(p.y)} L${round(p.x)},${BURN_HEIGHT}`;
    const fill = `M${round(x0)},${round(p.y)} L${round(x1)},${round(p.y)} L${round(x1)},${BURN_HEIGHT} L${round(x0)},${BURN_HEIGHT} Z`;
    return { fill, line, points };
  }
  const line = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${round(p.x)},${round(p.y)}`)
    .join(' ');
  const first = points[0];
  const last = points[points.length - 1];
  const fill = `${line} L${round(last.x)},${BURN_HEIGHT} L${round(first.x)},${BURN_HEIGHT} Z`;
  return { fill, line, points };
}

function round(n: number): string {
  return (Math.round(n * 10) / 10).toString();
}
