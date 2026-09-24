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
  // Every sample at the same usage is not a burn. A flat stroke across
  // an almost-empty period is what made the Cursor API card look like a
  // stray mark. The current-value mark still sits at the latest sample.
  const y0 = points[0].y;
  if (points.every((p) => p.y === y0)) return null;
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

type BoundaryUnit = 'hour' | 'day' | 'week';

/** Which clock boundary a window's graph marks. Null when the window is not one of these periods. */
export function boundaryUnitFor(name: string | undefined): BoundaryUnit | null {
  const n = String(name || '').toLowerCase();
  if (n.startsWith('session')) return 'hour';
  if (n.startsWith('week')) return 'day';
  if (n.startsWith('month')) return 'week';
  return null;
}

type WeekInfoLocale = Intl.Locale & { getWeekInfo?: () => { firstDay?: number } };

/**
 * Viewer's first day of the week, as Date.getUTCDay (0 Sunday … 6 Saturday).
 * Intl numbers Sunday as 7. Monday when the locale does not say.
 */
export function localeWeekStartsOn(): number {
  try {
    const locale = Intl.DateTimeFormat().resolvedOptions().locale;
    const first = (new Intl.Locale(locale) as WeekInfoLocale).getWeekInfo?.()?.firstDay;
    if (first === 7) return 0;
    if (typeof first === 'number' && first >= 1 && first <= 6) return first;
  } catch {
    /* Monday */
  }
  return 1;
}

type Wall = { y: number; m: number; d: number; h: number; min: number; s: number };

function wallOf(ms: number, timeZone: string): Wall | null {
  let fmt: Intl.DateTimeFormat;
  try {
    fmt = new Intl.DateTimeFormat('en-US', {
      timeZone,
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch {
    return null;
  }
  const parts = fmt.formatToParts(new Date(ms));
  const pick = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((p) => p.type === type)?.value);
  const y = pick('year');
  const m = pick('month');
  const d = pick('day');
  let h = pick('hour');
  const min = pick('minute');
  const s = pick('second');
  if (![y, m, d, h, min, s].every((n) => Number.isFinite(n))) return null;
  if (h === 24) h = 0;
  return { y, m, d, h, min, s };
}

function wallToUtc(wall: Wall, timeZone: string): number | null {
  const want = Date.UTC(wall.y, wall.m - 1, wall.d, wall.h, wall.min, wall.s);
  let utc = want;
  for (let i = 0; i < 4; i++) {
    const got = wallOf(utc, timeZone);
    if (!got) return null;
    const delta = want - Date.UTC(got.y, got.m - 1, got.d, got.h, got.min, got.s);
    if (delta === 0) return utc;
    utc += delta;
  }
  return utc;
}

function addCalendar(wall: Wall, days: number, hours: number): Wall {
  const t = new Date(Date.UTC(wall.y, wall.m - 1, wall.d + days, wall.h + hours, 0, 0));
  return {
    y: t.getUTCFullYear(),
    m: t.getUTCMonth() + 1,
    d: t.getUTCDate(),
    h: t.getUTCHours(),
    min: 0,
    s: 0,
  };
}

function weekdayOf(wall: Wall): number {
  return new Date(Date.UTC(wall.y, wall.m - 1, wall.d)).getUTCDay();
}

/** The next clock boundary strictly after `from`. */
function nextBoundary(from: number, unit: BoundaryUnit, timeZone: string, weekStartsOn: number): number | null {
  const w = wallOf(from, timeZone);
  if (!w) return null;
  if (unit === 'hour') {
    const top = { ...w, min: 0, s: 0 };
    const at = wallToUtc(top, timeZone);
    if (at == null) return null;
    if (at > from) return at;
    return wallToUtc(addCalendar(top, 0, 1), timeZone);
  }
  const mid: Wall = { y: w.y, m: w.m, d: w.d, h: 0, min: 0, s: 0 };
  if (unit === 'day') {
    const at = wallToUtc(mid, timeZone);
    if (at == null) return null;
    if (at > from) return at;
    return wallToUtc(addCalendar(mid, 1, 0), timeZone);
  }
  let delta = (weekStartsOn - weekdayOf(mid) + 7) % 7;
  const at = wallToUtc(mid, timeZone);
  if (at == null) return null;
  if (delta === 0 && at > from) return at;
  if (delta === 0) delta = 7;
  return wallToUtc(addCalendar(mid, delta, 0), timeZone);
}

/**
 * ViewBox x of each period boundary inside the published window.
 * Session marks local hours, weekly marks local midnights, monthly marks
 * the viewer's week start. The frame already owns the two edges, so a
 * boundary that lands on the start or the reset is omitted.
 * timeZone defaults to the viewer's zone. weekStartsOn is 0 Sunday … 6 Saturday.
 */
export function periodBoundaryXs(w: PlanWindow, timeZone?: string, weekStartsOn = 1): number[] {
  const unit = boundaryUnitFor(w.name);
  const period = periodBounds(w);
  if (!unit || !period) return [];
  const span = period.end - period.start;
  if (!(span > 0)) return [];
  const zone = timeZone || Intl.DateTimeFormat().resolvedOptions().timeZone;
  const cap = unit === 'hour' ? 48 : unit === 'day' ? 40 : 8;
  const out: number[] = [];
  let cursor = nextBoundary(period.start, unit, zone, weekStartsOn);
  for (let i = 0; i < cap && cursor != null && cursor < period.end; i++) {
    if (cursor <= period.start) break;
    out.push((BURN_WIDTH * (cursor - period.start)) / span);
    const nxt = nextBoundary(cursor, unit, zone, weekStartsOn);
    if (nxt == null || nxt <= cursor) break;
    cursor = nxt;
  }
  return out;
}

/**
 * 🎯T667: the sparkline's colour at each sample is the band the daemon
 * assigned the window at that moment, so a week that started on track and
 * ended burning hot shifts green → red along the curve instead of painting
 * the whole period in today's colour. Each stop carries the band's pace class
 * (paceClassForBand — the bar's own chain); cockpit.css colours it, so the
 * palette has one home.
 *
 * DO NOT replace that class with a locally computed colour, a pressure
 * blend, or fillColorForWindow. The server already decided the band. A
 * second model paints the line a different colour from the bar, which is
 * the drift these comments exist to stop. The wash colours the line only.
 * The current-value mark is not a stop, so a band change under the dot
 * cannot slice it. The mark takes the cell's pace class, the same class,
 * and no other colour.
 */
export type BurnStop = { offset: number; className: string };

/**
 * Horizontal gradient stops, one per pixel column, at that column's last
 * sample. Empty when no sample carries a band (an older daemon): the chart
 * then keeps its single inherited colour. Samples that share a column take
 * the last band in it; a gradient with no horizontal extent paints that
 * stop, which is the current band (🎯T688).
 */
export function burnStops(w: PlanWindow, pixelWidth = BURN_WIDTH): BurnStop[] {
  const samples = historyPoints(w);
  const points = burnPoints(w);
  if (!points.length || points.length !== samples.length) return [];
  const classes = samples.map((p) => paceClassForBand(p.band));
  if (!classes.some((c) => c !== null)) return [];
  const columns = Math.max(1, Math.round(pixelWidth) || BURN_WIDTH);
  const ordered = points
    .map((p, i) => ({ p, i }))
    .sort((a, b) => a.p.x - b.p.x || a.i - b.i);
  const stops: BurnStop[] = [];
  let last: string | null = null;
  let col = -1;
  let pending: BurnStop | null = null;
  const emit = () => {
    if (!pending) return;
    stops.push(pending);
    pending = null;
  };
  for (const { p, i } of ordered) {
    const c: string | null = classes[i] ?? last;
    if (c === null) continue;
    last = c;
    const next = xColumn(p.x, columns);
    if (next !== col) emit();
    col = next;
    pending = { offset: clamp(p.x / BURN_WIDTH, 0, 1), className: c };
  }
  emit();
  return stops;
}
