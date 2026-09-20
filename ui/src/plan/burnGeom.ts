// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Plan-usage burn-down sparkline (🎯T634 / T637).
 *
 * X is the published window period; Y is usage 0–100, rising to the right.
 * The curve is a line alone (🎯T671): shading the area under it said
 * nothing the line did not.
 *
 * 🎯T670: every harness reports usage, and usage trending right reads more
 * naturally than remaining trending left. The line starts at the first
 * stored sample — never a fabricated 0% at t=0. A cluster
 * whose x-span is below the stem minimum (just-reset week, second Refresh)
 * keeps a visible inward stem so it is not a 1px line on the column border.
 */

import { paceClassForBand } from './pace';
import { limitSecondsFor } from './windowGeom';
import type { PlanHistoryPoint, PlanWindow } from './tickerGroups';

export const BURN_WIDTH = 100;
export const BURN_HEIGHT = 32;
/** Half-width of a zero-span stem in viewBox units (🎯T637). */
export const BURN_STEM_HALF = 3.5;
export const BURN_STEM_MIN = BURN_STEM_HALF * 2;
/** Keep the stem and stroke inside the viewBox so x=0 is not the cell border. */
const BURN_EDGE_INSET = 1;

export type BurnPoint = { x: number; y: number };

export type BurnPaths = {
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
    const point: PlanHistoryPoint = { at: p.at, remaining_percent: clamp(p.remaining_percent, 0, 100) };
    if (typeof p.band === 'string' && p.band) point.band = p.band;
    out.push(point);
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
    // The API publishes remaining; the chart plots usage (🎯T670). Derived
    // here in full rather than folded into one inverted expression, so the
    // next reader sees the quantity the axis claims to show.
    const used = 100 - clamp(p.remaining_percent, 0, 100);
    // 🎯T685: hold the value inside the box the way x already is. A
    // fully spent window plots at usage 100, which is y=0 — the top
    // border — so the stroke straddles the edge, half of it clipped, and
    // the chart reads as empty. Fable at 100% used showed nothing at all.
    // The same is true of an untouched window at y=BURN_HEIGHT. One unit
    // of inset out of 32 is invisible as distortion and is the
    // difference between a reading and a blank cell.
    const y = clamp(
      BURN_HEIGHT - (BURN_HEIGHT * used) / 100,
      BURN_EDGE_INSET,
      BURN_HEIGHT - BURN_EDGE_INSET,
    );
    return { x, y };
  });
}

/** Place a stem of at least BURN_STEM_MIN fully inside the viewBox. */
export function stemBand(x: number): { x0: number; x1: number; lineX: number } {
  const lo = BURN_EDGE_INSET;
  const hi = BURN_WIDTH - BURN_EDGE_INSET;
  let x0 = x - BURN_STEM_HALF;
  let x1 = x + BURN_STEM_HALF;
  if (x0 < lo) {
    x1 = Math.min(hi, x1 + (lo - x0));
    x0 = lo;
  }
  if (x1 > hi) {
    x0 = Math.max(lo, x0 - (x1 - hi));
    x1 = hi;
  }
  if (x1 - x0 < BURN_STEM_MIN) {
    if (x0 <= lo) x1 = Math.min(hi, x0 + BURN_STEM_MIN);
    else if (x1 >= hi) x0 = Math.max(lo, x1 - BURN_STEM_MIN);
  }
  const lineX = clamp(x, x0, x1);
  return { x0, x1, lineX };
}

function stemPaths(points: BurnPoint[]): BurnPaths {
  const first = points[0];
  const last = points[points.length - 1];
  const midX = (first.x + last.x) / 2;
  const { x0, x1, lineX } = stemBand(midX);
  if (points.length === 1) {
    // A lone sample is drawn as an upright stem from its value to the
    // baseline: a single point has no slope to show, and a bare dot on
    // the border was the 🎯T637 sliver.
    const line = `M${round(lineX)},${round(first.y)} L${round(lineX)},${BURN_HEIGHT}`;
    return { line, points };
  }
  // Keep the real slope; the stroke is held inside the stem band so a
  // week-start cluster is drawn inward rather than on the cell border.
  const drawn = points.map((p) => ({ x: clamp(p.x, x0, x1), y: p.y }));
  const line = drawn
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${round(p.x)},${round(p.y)}`)
    .join(' ');
  return { line, points };
}

export function burnPaths(w: PlanWindow): BurnPaths | null {
  const points = burnPoints(w);
  if (!points.length) return null;
  // A single sample — or several minutes of samples still sitting on the
  // same pixel of a week — is a sliver if we only close the area on itself
  // (🎯T637: the just-reset Codex hover). Give that cluster a visible stem.
  const span = points[points.length - 1].x - points[0].x;
  if (points.length === 1 || span < BURN_STEM_MIN) {
    return stemPaths(points);
  }
  const line = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${round(p.x)},${round(p.y)}`)
    .join(' ');
  return { line, points };
}

function round(n: number): string {
  return (Math.round(n * 10) / 10).toString();
}

/**
 * 🎯T667: the sparkline's colour at each sample is the band the daemon
 * assigned the window at that moment, so a week that started on track and
 * ended burning hot shifts green → red along the curve instead of painting
 * the whole period in today's colour. Each stop carries the band's pace class
 * (paceClassForBand — the bar's own chain); cockpit.css colours it, so the
 * palette has one home.
 */
export type BurnStop = { offset: number; className: string };

/**
 * Horizontal gradient stops, one per sample, at the sample's x as a fraction
 * of the plot width. Empty when no sample carries a band (an older daemon):
 * the chart then keeps its single inherited colour. A stem-width cluster has
 * no horizontal extent to shift across, so it takes the latest band flat.
 */
export function burnStops(w: PlanWindow): BurnStop[] {
  const samples = historyPoints(w);
  const points = burnPoints(w);
  if (!points.length || points.length !== samples.length) return [];
  const classes = samples.map((p) => paceClassForBand(p.band));
  if (!classes.some((c) => c !== null)) return [];
  const span = points[points.length - 1].x - points[0].x;
  if (points.length === 1 || span < BURN_STEM_MIN) {
    let latest: string | null = null;
    for (const c of classes) if (c !== null) latest = c;
    return [
      { offset: 0, className: latest as string },
      { offset: 1, className: latest as string },
    ];
  }
  const stops: BurnStop[] = [];
  let last: string | null = null;
  for (let i = 0; i < points.length; i++) {
    const c: string | null = classes[i] ?? last;
    if (c === null) continue;
    last = c;
    stops.push({ offset: clamp(points[i].x / BURN_WIDTH, 0, 1), className: c });
  }
  return stops;
}
