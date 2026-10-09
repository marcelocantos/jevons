// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  boundaryUnitFor,
  burnPaths,
  burnPoints,
  currentMark,
  periodBoundaryXs,
  periodBounds,
  pixelColumns,
  stemBand,
  BURN_HEIGHT,
  BURN_STEM_MIN,
  BURN_WIDTH,
} from './burnGeom';
import type { PlanWindow } from './tickerGroups';

const START = Date.parse('2026-09-01T00:00:00Z');
const WEEK = 7 * 24 * 3600;
const RESET = new Date(START + WEEK * 1000).toISOString();

function win(partial: Partial<PlanWindow>): PlanWindow {
  return {
    name: 'weekly',
    resets_at: RESET,
    limit_window_seconds: WEEK,
    remaining_percent: 50,
    ...partial,
  };
}

describe('burn chart geometry (🎯T634 / T637)', () => {
  it('spans the published period on x and remaining on y', () => {
    const bounds = periodBounds(win({}));
    expect(bounds).toEqual({ start: START, end: START + WEEK * 1000 });
  });

  it('starts at the first stored sample, never a fabricated 100% at t=0', () => {
    const mid = new Date(START + WEEK * 1000 * 0.25).toISOString();
    const pts = burnPoints(
      win({
        history: [{ at: mid, remaining_percent: 71 }],
      }),
    );
    expect(pts).toHaveLength(1);
    expect(pts[0].x).toBeCloseTo(25, 5);
    // 71% remaining is 29% used, and y grows downward: a low-usage sample
    // sits near the bottom of the plot (🎯T670).
    expect(pts[0].y).toBeCloseTo(32 * 0.71, 5);
    expect(pts[0].x).not.toBe(0);
  });

  it('returns no path when history is empty or the period is unknown', () => {
    expect(burnPaths(win({ history: [] }))).toBeNull();
    expect(burnPaths(win({ history: undefined }))).toBeNull();
    expect(
      burnPaths(
        win({
          resets_at: null,
          history: [{ at: new Date(START).toISOString(), remaining_percent: 90 }],
        }),
      ),
    ).toBeNull();
  });

  it('plots only the supplied samples', () => {
    const a = new Date(START + 3600_000).toISOString();
    const b = new Date(START + 2 * 3600_000).toISOString();
    const spec = burnPaths(
      win({
        history: [
          { at: a, remaining_percent: 80 },
          { at: b, remaining_percent: 50 },
        ],
      }),
    );
    expect(spec?.points).toHaveLength(2);
    expect(spec?.line.startsWith('M')).toBe(true);
    // Usage climbs as the period runs, so the curve rises: later samples
    // have smaller y (🎯T670).
    expect(spec?.points[0].y).toBeGreaterThan(spec!.points[1].y);
  });

  it('keeps a just-reset cluster as an inward stem, not a 1px left border (🎯T635)', () => {
    const a = new Date(START).toISOString();
    const b = new Date(START + 5 * 60_000).toISOString();
    const spec = burnPaths(
      win({
        remaining_percent: 100,
        history: [
          { at: a, remaining_percent: 100 },
          { at: b, remaining_percent: 100 },
        ],
      }),
    );
    expect(spec?.points).toHaveLength(2);
    expect(spec?.points[0].x).toBe(0);
    expect(spec?.fill).toBeTruthy();
    const ext = fillExtent(spec!.fill!);
    expect(ext.x0).toBeGreaterThan(0);
    expect(ext.x1 - ext.x0).toBeGreaterThanOrEqual(BURN_STEM_MIN);
    const lineX = Number(spec!.line.match(/^M([\d.]+),/)?.[1]);
    expect(lineX).toBeGreaterThan(0);
    // True sample position is still x=0; the stem is what is inset.
    expect(currentMark(win({
      remaining_percent: 100,
      history: [
        { at: a, remaining_percent: 100 },
        { at: b, remaining_percent: 100 },
      ],
    }))).toBe('M0,32 L0,32');
  });

  it('shifts a period-start stem inward instead of sitting on x=0 (🎯T635)', () => {
    const band = stemBand(0);
    expect(band.x0).toBeGreaterThan(0);
    expect(band.x1 - band.x0).toBeGreaterThanOrEqual(BURN_STEM_MIN);
    expect(band.lineX).toBeGreaterThan(0);
    expect(band.lineX).toBeGreaterThanOrEqual(band.x0);
    expect(band.lineX).toBeLessThanOrEqual(band.x1);
  });

  it('plots a mid-window two-hour span as those two points only (🎯T635)', () => {
    const a = new Date(START + WEEK * 1000 * 0.25).toISOString();
    const b = new Date(START + WEEK * 1000 * 0.25 + 2 * 3600_000).toISOString();
    const spec = burnPaths(
      win({
        history: [
          { at: a, remaining_percent: 80 },
          { at: b, remaining_percent: 50 },
        ],
      }),
    );
    expect(spec?.points).toHaveLength(2);
    expect(spec?.points[0].x).toBeCloseTo(25, 5);
    expect(spec?.points[1].x).toBeGreaterThan(spec!.points[0].x);
    expect(spec?.points[0].x).not.toBe(0);
  });

  it('leaves a real curve whose span exceeds the stem minimum unchanged (🎯T635)', () => {
    const a = new Date(START + WEEK * 1000 * 0.2).toISOString();
    const b = new Date(START + WEEK * 1000 * 0.5).toISOString();
    const spec = burnPaths(
      win({
        history: [
          { at: a, remaining_percent: 80 },
          { at: b, remaining_percent: 50 },
        ],
      }),
    );
    expect(spec?.points).toHaveLength(2);
    expect(spec?.fill).toBeUndefined();
    expect(spec?.points[0].x).toBeCloseTo(20, 5);
    expect(spec?.points[1].x).toBeCloseTo(50, 5);
    const lineX0 = Number(spec!.line.match(/^M([\d.]+),/)?.[1]);
    expect(lineX0).toBeCloseTo(20, 5);
  });

  it('keeps a one-sample spike inside a crowded pixel column', () => {
    const crowded: { x: number; y: number }[] = [];
    for (let i = 0; i < 40; i++) crowded.push({ x: i * 0.01, y: 28 });
    crowded.push({ x: 0.2, y: 2 });
    crowded.push({ x: 50, y: 16 });
    const thinned = pixelColumns(crowded);
    const firstColumn = thinned.filter((p) => p.x < 1);
    expect(firstColumn.map((p) => p.y).sort((a, b) => a - b)).toEqual([2, 28]);
    expect(thinned[thinned.length - 1]).toEqual({ x: 50, y: 16 });
    expect(thinned.length).toBeLessThan(crowded.length);
  });

  it('splits a column when the cell is wide enough to show it', () => {
    const pair = [
      { x: 0, y: 20 },
      { x: 0.6, y: 20 },
    ];
    expect(pixelColumns(pair, 100)).toHaveLength(1);
    expect(pixelColumns(pair, 200)).toHaveLength(2);
  });

  it('marks a value at either extreme without moving it (🎯T687)', () => {
    const atStart = win({
      history: [{ at: new Date(START).toISOString(), remaining_percent: 100 }],
    });
    const atEnd = win({
      history: [{ at: new Date(START + WEEK * 1000).toISOString(), remaining_percent: 0 }],
    });
    // Untouched at the very start of the period: bottom-left corner.
    expect(burnPoints(atStart)[0]).toEqual({ x: 0, y: BURN_HEIGHT });
    // Fully spent at the very end: top-right corner. Both are true
    // positions, not nudged inward to survive a clip.
    expect(burnPoints(atEnd)[0]).toEqual({ x: BURN_WIDTH, y: 0 });
    expect(currentMark(atEnd)).toBe('M100,0 L100,0');
  });
});

describe('period boundary marks', () => {
  const sessionStart = Date.parse('2026-09-01T00:00:00Z');
  const fiveHours = 5 * 3600;

  it('marks local hours on a session, not the frame', () => {
    expect(boundaryUnitFor('session')).toBe('hour');
    const onTheHour = periodBoundaryXs(
      win({
        name: 'session',
        resets_at: new Date(sessionStart + fiveHours * 1000).toISOString(),
        limit_window_seconds: fiveHours,
      }),
      'UTC',
    );
    // 00:00–05:00. The edges are the frame. 01, 02, 03, 04 remain.
    expect(onTheHour.map((x) => Math.round(x))).toEqual([20, 40, 60, 80]);

    const halfPast = periodBoundaryXs(
      win({
        name: 'session',
        resets_at: new Date(sessionStart + 30 * 60_000 + fiveHours * 1000).toISOString(),
        limit_window_seconds: fiveHours,
      }),
      'UTC',
    );
    // 00:30–05:30. Hours at 01, 02, 03, 04, 05.
    expect(halfPast.map((x) => Math.round(x))).toEqual([10, 30, 50, 70, 90]);
  });

  it('follows the local hour, including a zone that is not a whole hour from UTC', () => {
    // 00:00Z is 09:30 in Adelaide. A five-hour session ends 14:30 local.
    const xs = periodBoundaryXs(
      win({
        name: 'session',
        resets_at: new Date(sessionStart + fiveHours * 1000).toISOString(),
        limit_window_seconds: fiveHours,
      }),
      'Australia/Adelaide',
    );
    expect(xs.map((x) => Math.round(x))).toEqual([10, 30, 50, 70, 90]);
  });

  it('skips an hour the clock does not have', () => {
    // Melbourne springs forward at 02:00 local on 2026-10-04: 15:00Z is 01:00,
    // 16:00Z is 03:00. A session from 01:00 to 07:00 local has no 02:00 line.
    const start = Date.parse('2026-10-03T15:00:00Z');
    const xs = periodBoundaryXs(
      win({
        name: 'session',
        resets_at: new Date(start + fiveHours * 1000).toISOString(),
        limit_window_seconds: fiveHours,
      }),
      'Australia/Melbourne',
    );
    expect(xs.map((x) => Math.round(x))).toEqual([20, 40, 60, 80]);
  });

  it('marks local midnights on a week', () => {
    expect(boundaryUnitFor('weekly')).toBe('day');
    expect(boundaryUnitFor('weekly_model')).toBe('day');
    // Monday 00:00Z through the next Monday. Six midnights inside.
    const monday = Date.parse('2026-09-07T00:00:00Z');
    const xs = periodBoundaryXs(
      win({
        name: 'weekly',
        resets_at: new Date(monday + WEEK * 1000).toISOString(),
        limit_window_seconds: WEEK,
      }),
      'UTC',
    );
    expect(xs).toHaveLength(6);
    expect(xs[0]).toBeCloseTo(100 / 7, 5);
    expect(xs[5]).toBeCloseTo(600 / 7, 5);
  });

  it('marks week starts on a month, and the week start is the one asked for', () => {
    expect(boundaryUnitFor('monthly')).toBe('week');
    const start = Date.parse('2026-09-02T00:00:00Z'); // Wednesday
    const month = 30 * 24 * 3600;
    const w = win({
      name: 'monthly',
      resets_at: new Date(start + month * 1000).toISOString(),
      limit_window_seconds: month,
    });
    const mondays = periodBoundaryXs(w, 'UTC', 1);
    const sundays = periodBoundaryXs(w, 'UTC', 0);
    expect(mondays.map((x) => Math.round(x * 10) / 10)).toEqual([16.7, 40, 63.3, 86.7]);
    expect(sundays.map((x) => Math.round(x * 10) / 10)).toEqual([13.3, 36.7, 60, 83.3]);
  });

  it('draws nothing when the period is unknown', () => {
    expect(periodBoundaryXs(win({ name: 'weekly', resets_at: null }), 'UTC')).toEqual([]);
    expect(periodBoundaryXs(win({ name: 'other' }), 'UTC')).toEqual([]);
  });
});

function fillExtent(d: string): { x0: number; x1: number } {
  const xs = [...d.matchAll(/[ML]([\d.]+),/g)].map((m) => Number(m[1]));
  return { x0: Math.min(...xs), x1: Math.max(...xs) };
}
