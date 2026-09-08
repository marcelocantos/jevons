// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { burnPaths, burnPoints, periodBounds } from './burnGeom';
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

describe('burn chart geometry (🎯T634)', () => {
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
    expect(pts[0].y).toBeCloseTo(32 * (1 - 0.71), 5);
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
    expect(spec?.fill.endsWith('Z')).toBe(true);
    expect(spec?.points[0].y).toBeLessThan(spec!.points[1].y);
  });
});
