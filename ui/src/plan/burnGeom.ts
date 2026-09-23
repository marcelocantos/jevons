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
 * 🎯T688: there is no sample-count special case, and there should never be
 * one again. A series of one sample, or of twenty sitting on the same
 * minute of a week, used to be synthesised into an upright stem, because a
 * zero-length stroke with butt caps paints nothing and the cell looked
 * empty. That is a painting problem wearing a geometry costume: it bought
 * a stem band, a minimum width, a lone-sample branch and finally a
 * flat-cluster branch, and it drew a bar where the data was a point.
 *
 * 🎯T687: the current value is drawn as its own mark, always, in front of
 * the line. That is what makes the line's own degeneracy uninteresting —
 * a one-sample series shows its dot and an empty line, exactly as a
 * thousand-sample series shows its dot at the end of a long line, so
 * nothing anywhere reasons about how many samples there are. The mark is
 * painted outside the clipped plot so a reading at an extreme is whole
 * rather than sliced by the frame, which also retires the inset the
 * earlier fix needed: values plot where they actually fall.
 *
 * The painted line is a different question from where the samples fall.
 * A week of readings is far denser than the cell is wide, and a path
 * vertex the renderer cannot separate from its neighbour is wasted work.
 * pixelColumns keeps, per screen pixel, the lowest and highest reading
 * in time order. The mark stays on the true latest sample.
 */

import { paceClassForBand } from './pace';
import { limitSecondsFor } from './windowGeom';
import type { PlanHistoryPoint, PlanWindow } from './tickerGroups';

export const BURN_WIDTH = 100;
export const BURN_HEIGHT = 32;


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
    const y = BURN_HEIGHT - (BURN_HEIGHT * used) / 100;
    return { x, y };
  });
}

/**
 * Thin plotted points to the chart's laid-out width. pixelWidth is CSS
 * pixels; one column is about one pixel across the viewBox. Each column
 * keeps its lowest and highest reading, in time order, so a one-sample
 * spike still reaches the line. A flat column keeps a single vertex.
 * Width 0 (not laid out yet) uses one column per viewBox unit.
 */
export function pixelColumns<T extends BurnPoint>(points: T[], pixelWidth = BURN_WIDTH): T[] {
  if (points.length <= 1) return points.slice();
  const columns = Math.max(1, Math.round(pixelWidth) || BURN_WIDTH);
  const ordered = points
    .map((p, i) => ({ p, i }))
    .sort((a, b) => a.p.x - b.p.x || a.i - b.i);
  const out: T[] = [];
  let col = -1;
  let bucket: T[] = [];
  const flush = (lastBucket: boolean) => {
    if (!bucket.length) return;
    let lo = bucket[0];
    let hi = bucket[0];
    let later = bucket[0];
    for (const p of bucket) {
      if (p.y < lo.y) lo = p;
      if (p.y > hi.y) hi = p;
      if (p.x >= later.x) later = p;
    }
    const first = lo.x <= hi.x ? lo : hi;
    const second = first === lo ? hi : lo;
    if (lo.y === hi.y) {
      // One vertex. The last column keeps the newest reading so the
      // stroke ends on the current-value mark instead of beside it.
      out.push(lastBucket ? later : first);
    } else {
      out.push(first);
      out.push(second);
      if (lastBucket && later !== first && later !== second) out.push(later);
    }
    bucket = [];
  };
  for (const { p } of ordered) {
    const c = xColumn(p.x, columns);
    if (c !== col) {
      flush(false);
      col = c;
    }
    bucket.push(p);
  }
  flush(true);
  return out;
}

function xColumn(x: number, columns: number): number {
  if (x >= BURN_WIDTH) return columns - 1;
  if (x <= 0) return 0;
  return Math.min(columns - 1, Math.floor((x / BURN_WIDTH) * columns));
}

export function burnPaths(w: PlanWindow, pixelWidth = BURN_WIDTH): BurnPaths | null {
  const points = pixelColumns(burnPoints(w), pixelWidth);
  if (!points.length) return null;
  const line = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${round(p.x)},${round(p.y)}`)
    .join(' ');
  return { line, points };
}

/**
 * The current reading, as a zero-length segment for the marker path
 * (🎯T687). Null when there is nothing to mark.
 *
 * A zero-length segment with a round cap is a circle of the stroke's own
 * width, which is why the mark is a path and not a <circle>: the viewBox
 * is scaled unequally on the two axes, so a circle element would paint as
 * an ellipse, while a non-scaling stroke is in screen pixels and stays
 * round wherever the column lands.
 */
export function currentMark(w: PlanWindow): string | null {
  const points = burnPoints(w);
  if (!points.length) return null;
  const last = points[points.length - 1];
  return `M${round(last.x)},${round(last.y)} L${round(last.x)},${round(last.y)}`;
}

function round(n: number): string {
  return (Math.round(n * 10) / 10).toString();
}

/**
 * One solid-coloured stretch of the sparkline (🎯T667). className null
 * means no sample carried a band: the stretch inherits the cell colour.
 * Otherwise it is the pace class of that stretch, and cockpit.css colours
 * it. A colour belongs to the stretch. It is not a wash laid across the
 * finished shape, which is what sliced the end dot in half.
 */
export type BurnRun = { d: string; className: string | null };

type ClassedPoint = BurnPoint & { className: string | null };

export function burnRuns(w: PlanWindow, pixelWidth = BURN_WIDTH): BurnRun[] {
  const samples = historyPoints(w);
  const raw = burnPoints(w);
  if (!raw.length || raw.length !== samples.length) return [];
  const classed: ClassedPoint[] = raw.map((p, i) => ({
    ...p,
    className: paceClassForBand(samples[i].band),
  }));
  return runsOf(pixelColumns(classed, pixelWidth));
}

function pathD(points: BurnPoint[]): string {
  return points.map((p, i) => `${i === 0 ? 'M' : 'L'}${round(p.x)},${round(p.y)}`).join(' ');
}

function runsOf(points: ClassedPoint[]): BurnRun[] {
  if (!points.length) return [];
  if (!points.some((p) => p.className !== null)) {
    return [{ d: pathD(points), className: null }];
  }
  const runs: { className: string; pts: BurnPoint[] }[] = [];
  let last: string | null = null;
  for (const p of points) {
    const c: string | null = p.className ?? last;
    if (c === null) continue;
    last = c;
    const prev = runs[runs.length - 1];
    if (!prev || prev.className !== c) {
      // Share the boundary vertex so two solid stretches meet, instead of
      // leaving a gap for a colour wash to fill.
      const pts: BurnPoint[] = prev ? [prev.pts[prev.pts.length - 1], p] : [p];
      runs.push({ className: c, pts });
    } else {
      prev.pts.push(p);
    }
  }
  return runs.map((r) => ({ d: pathD(r.pts), className: r.className }));
}
