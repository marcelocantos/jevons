// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BURN_HEIGHT, BURN_WIDTH, burnPoints, currentMark } from './burnGeom';
import { BurnChart } from './BurnChart';
import type { PlanWindow } from './tickerGroups';

// 🎯T685 / 🎯T687: a reading at either extreme is still a reading.
//
// Fable arrived at 100% used and its chart was blank: the line was drawn
// along the top border with half the stroke outside the plot. The first
// answer nudged every value one unit inward, which kept readings visible
// by moving them — a fudge 🎯T687 retired. The current reading now has a
// mark of its own, painted in front of the plot and outside its clip, so
// a value that lands on an edge is drawn whole, exactly where it falls.

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

describe('a value at an extreme is plotted where it falls (🎯T687)', () => {
  it('puts 100% used on the top edge, not one unit below it', () => {
    const pts = burnPoints(win([0, 0, 0]));
    expect(pts).toHaveLength(3);
    for (const p of pts) expect(p.y).toBe(0);
  });

  it('puts an untouched window on the bottom edge', () => {
    for (const p of burnPoints(win([100, 100]))) expect(p.y).toBe(BURN_HEIGHT);
  });

  it('leaves interior values exactly where the arithmetic puts them', () => {
    expect(burnPoints(win([70]))[0].y).toBeCloseTo(BURN_HEIGHT * 0.7, 5);
  });
});

describe('the current reading always carries a mark (🎯T687)', () => {
  it('marks a fully spent window on the top edge', () => {
    expect(currentMark(win([0, 0, 0]))).toMatch(/,0 L[\d.]+,0$/);
  });

  it('marks the latest sample, not the first', () => {
    // Usage climbing 10 → 30 → 60: the mark belongs to 60% used.
    const mark = currentMark(win([90, 70, 40]));
    const y = Number(mark!.match(/L[\d.]+,([\d.]+)$/)![1]);
    expect(y).toBeCloseTo(BURN_HEIGHT * 0.4, 1);
  });

  it('marks a lone sample, where the line itself paints nothing', () => {
    expect(currentMark(win([71]))).not.toBeNull();
  });

  it('has nothing to mark when there are no samples', () => {
    expect(currentMark(win([], { history: [] }))).toBeNull();
  });
});

describe('the mark renders in front of the line (🎯T687)', () => {
  it('paints after the line, so an edge value is not hidden under the frame', () => {
    const { container } = render(<BurnChart window={win([0, 0, 0])} />);
    const svg = container.querySelector('svg.plan-burn-svg')!;
    const kids = [...svg.querySelectorAll('path')].map((p) => p.getAttribute('class'));
    expect(kids).toEqual(['plan-burn-line', 'plan-burn-now']);
  });

  it('still marks a single sample, whose line paints nothing', () => {
    const { container } = render(<BurnChart window={win([55])} />);
    const line = container.querySelector('path.plan-burn-line')!.getAttribute('d');
    const mark = container.querySelector('path.plan-burn-now')!.getAttribute('d');
    // One sample: the line is a bare moveto that paints nothing, and the
    // mark is the whole of what the owner sees. No branch produced this.
    expect(line).not.toContain('L');
    expect(mark).toContain('L');
  });

  it('keeps a full series inside the plot on x', () => {
    const pts = burnPoints(win([100, 80, 60, 40]));
    expect(pts[0].x).toBeGreaterThanOrEqual(0);
    expect(pts[pts.length - 1].x).toBeLessThanOrEqual(BURN_WIDTH);
  });
});
