// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { BURN_HEIGHT, burnPaths, burnPoints } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

// 🎯T685: a reading at either extreme is still a reading.
//
// Fable arrived at 100% used and its chart was blank. The samples were
// there and the history was served; the line was simply drawn along y=0,
// the top border, with half the stroke outside the viewBox. An empty
// chart and a fully spent one must not look alike — that is the same
// confusion 🎯T681 removed from the bars.

const START = Date.parse('2026-09-14T02:00:00Z');
const WEEK = 7 * 24 * 3600;
const RESET = new Date(START + WEEK * 1000).toISOString();

function win(remaining: number[], partial: Partial<PlanWindow> = {}): PlanWindow {
  return {
    name: 'weekly_model',
    model: 'Fable',
    resets_at: RESET,
    limit_window_seconds: WEEK,
    remaining_percent: remaining[remaining.length - 1],
    history: remaining.map((r, i) => ({
      at: new Date(START + (i + 1) * 3600_000).toISOString(),
      remaining_percent: r,
    })),
    ...partial,
  } as PlanWindow;
}

describe('a fully spent window still draws (🎯T685)', () => {
  it('keeps the whole stroke inside the box at 100% used', () => {
    const pts = burnPoints(win([0, 0, 0]));
    expect(pts).toHaveLength(3);
    for (const p of pts) {
      expect(p.y).toBeGreaterThan(0);
      expect(p.y).toBeLessThan(BURN_HEIGHT);
    }
    // Still at the top of the chart, where 100% used belongs.
    expect(pts[0].y).toBeLessThan(BURN_HEIGHT * 0.1);
  });

  it('draws a path rather than nothing', () => {
    const spec = burnPaths(win([0, 0, 0]));
    expect(spec).not.toBeNull();
    expect(spec!.line.startsWith('M')).toBe(true);
    // No coordinate sits on the top edge where it would be half-clipped.
    const ys = [...spec!.line.matchAll(/,([\d.]+)/g)].map((m) => Number(m[1]));
    expect(ys.length).toBeGreaterThan(0);
    expect(Math.min(...ys)).toBeGreaterThan(0);
  });

  it('does the same for an untouched window at the bottom edge', () => {
    const pts = burnPoints(win([100, 100]));
    for (const p of pts) {
      expect(p.y).toBeLessThan(BURN_HEIGHT);
      expect(p.y).toBeGreaterThan(0);
    }
    expect(pts[0].y).toBeGreaterThan(BURN_HEIGHT * 0.9);
  });

  it('leaves interior values where they were', () => {
    // The inset must not become a visible distortion of ordinary
    // readings: 30% used still sits at 70% of the height.
    const pts = burnPoints(win([70]));
    expect(pts[0].y).toBeCloseTo(BURN_HEIGHT * 0.7, 5);
  });
});
