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
 * stored sample — never a fabricated 0% at t=0.
 *
 * 🎯T686: there is no sample-count special case, and there should never be
 * one again. A series of one sample, or of twenty sitting on the same
 * minute of a week, used to be synthesised into an upright stem, because a
 * zero-length stroke with butt caps paints nothing and the cell looked
 * empty. That is a painting problem wearing a geometry costume: it bought
 * a stem band, a minimum width, a lone-sample branch and finally a
 * flat-cluster branch, and it drew a bar where the data was a point. Round
 * caps paint a zero-length segment as a dot, which is what one reading is,
 * so every series — one sample or a thousand — is the same polyline.
 */

import { paceClassForBand } from './pace';
import { limitSecondsFor } from './windowGeom';
import type { PlanHistoryPoint, PlanWindow } from './tickerGroups';

export const BURN_WIDTH = 100;
export const BURN_HEIGHT = 32;
/** Keep every drawn value inside the viewBox, off the cell border. */
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
    // Hold both axes inside the box (🎯T637 / 🎯T685): a sample on the
    // period boundary or at an extreme value would otherwise be painted
    // half outside the viewBox, on the cell border, and read as absent.
    const x = clamp(
      (100 * (at - period.start)) / span,
      BURN_EDGE_INSET,
      BURN_WIDTH - BURN_EDGE_INSET,
    );
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

export function burnPaths(w: PlanWindow): BurnPaths | null {
  const points = burnPoints(w);
  if (!points.length) return null;
  // Every sample is a lineto, including the first, so the path always
  // contains at least one segment. A lone reading is then a zero-length
  // segment, which round caps paint as a dot (🎯T686); on a dense series
  // that first segment is invisible under the line. One rule, no branch:
  // a moveto by itself draws nothing at all, which is the trap the old
  // stem was built to avoid.
  const line =
    `M${round(points[0].x)},${round(points[0].y)} ` +
    points.map((p) => `L${round(p.x)},${round(p.y)}`).join(' ');
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
 * the chart then keeps its single inherited colour. Samples piled on one x
 * simply emit stops at that x; a gradient with no horizontal extent paints
 * the last stop, which is the current band (🎯T686).
 */
export function burnStops(w: PlanWindow): BurnStop[] {
  const samples = historyPoints(w);
  const points = burnPoints(w);
  if (!points.length || points.length !== samples.length) return [];
  const classes = samples.map((p) => paceClassForBand(p.band));
  if (!classes.some((c) => c !== null)) return [];
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
