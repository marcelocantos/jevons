// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { burnPaths, burnPoints, currentMark, periodBounds, BURN_HEIGHT, BURN_WIDTH } from './burnGeom';
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

  it('leaves a lone sample to the current-value mark (🎯T687)', () => {
    const mid = new Date(START + WEEK * 1000 * 0.25).toISOString();
    const w = win({ history: [{ at: mid, remaining_percent: 71 }] });
    const spec = burnPaths(w);
    // The line for one sample is a bare moveto, which paints nothing at
    // all — and that is fine, because the reading is carried by its own
    // mark. Nothing here counts samples or synthesises a shape.
    expect(spec?.line).toBe('M25,22.7');
    expect(currentMark(w)).toBe('M25,22.7 L25,22.7');
  });

  it('plots a just-reset cluster where it falls, at the period start (🎯T687)', () => {
    const a = new Date(START).toISOString();
    const b = new Date(START + 5 * 60_000).toISOString();
    const w = win({
      remaining_percent: 100,
      history: [
        { at: a, remaining_percent: 100 },
        { at: b, remaining_percent: 100 },
      ],
    });
    const spec = burnPaths(w);
    expect(spec?.points).toHaveLength(2);
    // No inset and no stem: the samples sit on the period start because
    // that is when they were taken. The mark is drawn in front of the
    // plot and outside its clip, so sitting on the edge costs nothing.
    expect(spec!.points[0].x).toBe(0);
    // 100% remaining is 0% used, so the mark sits in the bottom-left
    // corner: the true position of an untouched, just-reset week.
    expect(currentMark(w)).toBe('M0,32 L0,32');
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

function lineExtent(d: string): { x0: number; x1: number } {
  const xs = [...d.matchAll(/[ML]([\d.]+),/g)].map((m) => Number(m[1]));
  return { x0: Math.min(...xs), x1: Math.max(...xs) };
}
