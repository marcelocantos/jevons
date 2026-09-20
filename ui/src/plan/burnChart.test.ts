// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { burnPaths, burnPoints, periodBounds, BURN_HEIGHT, BURN_WIDTH } from './burnGeom';
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

  it('paints a single sample as a dot, inside the box (🎯T686)', () => {
    const mid = new Date(START + WEEK * 1000 * 0.25).toISOString();
    const spec = burnPaths(
      win({
        history: [{ at: mid, remaining_percent: 71 }],
      }),
    );
    // 🎯T686 removed the synthesised stem. One reading is one point: the
    // path is a zero-length segment, which round caps paint as a dot.
    // The old behaviour drew a bar down to the baseline, which claimed a
    // climb the data never showed.
    expect(spec?.line).toBe('M25,22.7 L25,22.7');
    expect(spec?.line).not.toContain(',' + BURN_HEIGHT);
    const ext = lineExtent(spec!.line);
    expect(ext.x0).toBe(ext.x1);
    expect(ext.x0).toBeGreaterThan(0);
  });

  it('keeps a just-reset cluster off the cell border (🎯T637 / 🎯T686)', () => {
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
    // The samples sit on the period start, but nothing is drawn on the
    // border: the inset holds them inside the box, so the mark is a dot
    // just inside the left edge rather than a sliver on it. No stem is
    // synthesised to make it visible (🎯T686).
    const ext = lineExtent(spec!.line);
    expect(ext.x0).toBeGreaterThan(0);
    expect(ext.x1).toBeLessThan(BURN_WIDTH);
    expect(spec!.line).not.toContain(',' + BURN_HEIGHT);
  });

  it('holds a value at either end of the period inside the box (🎯T686)', () => {
    const atStart = burnPaths(
      win({ history: [{ at: new Date(START).toISOString(), remaining_percent: 100 }] }),
    );
    const atEnd = burnPaths(
      win({
        history: [{ at: new Date(START + WEEK * 1000).toISOString(), remaining_percent: 0 }],
      }),
    );
    for (const spec of [atStart, atEnd]) {
      const ext = lineExtent(spec!.line);
      expect(ext.x0).toBeGreaterThan(0);
      expect(ext.x1).toBeLessThan(BURN_WIDTH);
    }
  });
});

function lineExtent(d: string): { x0: number; x1: number } {
  const xs = [...d.matchAll(/[ML]([\d.]+),/g)].map((m) => Number(m[1]));
  return { x0: Math.min(...xs), x1: Math.max(...xs) };
}
