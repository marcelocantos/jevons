// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { burnPoints } from './burnGeom';
import { gaugeFillPercent, usedPercentOf } from './tickerGroups';
import { remainingSpan, rolloverCell } from './tipTable';
import { PlanTipTable } from './tipTable';
import type { TickerGroup } from './tickerGroups';

// 🎯T670: the cockpit reports usage, the way every harness does, and both
// the bar and the chevron travel rightward as the period is spent.

const NOW = Date.parse('2026-08-31T00:00:00Z');
const hoursOut = (h: number) => new Date(NOW + h * 3600_000).toISOString();

describe('the gauge fills with what was spent (🎯T670)', () => {
  it('prefers the published usage, falls back to the complement of remaining', () => {
    expect(usedPercentOf({ used_percent: 41, remaining_percent: 59 })).toBe(41);
    expect(usedPercentOf({ remaining_percent: 59 })).toBe(41);
    expect(usedPercentOf({})).toBeNull();
    expect(usedPercentOf({ used_percent: 140 })).toBe(100);
  });

  it('keeps a spent window empty, not a solid block', () => {
    // The owner's edge case: 0% left must stay the empty red-bordered bar
    // it has always been. "Nothing left" and "full" must not paint alike.
    expect(gaugeFillPercent({ remaining_percent: 0, used_percent: 100 })).toBe(0);
    expect(gaugeFillPercent({ remaining_percent: -1, used_percent: 100 })).toBe(0);
    // Every other window fills with usage.
    expect(gaugeFillPercent({ remaining_percent: 28, used_percent: 72 })).toBe(72);
    expect(gaugeFillPercent({ remaining_percent: 100 })).toBe(0);
    expect(gaugeFillPercent({})).toBe(0);
  });

  it('plots the sparkline as usage rising to the right', () => {
    const start = NOW;
    const week = 7 * 24 * 3600;
    const pts = burnPoints({
      name: 'weekly',
      resets_at: new Date(start + week * 1000).toISOString(),
      limit_window_seconds: week,
      history: [
        { at: new Date(start + 24 * 3600_000).toISOString(), remaining_percent: 90 },
        { at: new Date(start + 5 * 24 * 3600_000).toISOString(), remaining_percent: 20 },
      ],
    } as never);
    // y grows downward, so a rising usage curve has decreasing y.
    expect(pts[0].y).toBeGreaterThan(pts[1].y);
    // 10% used early sits near the bottom; 80% used later sits near the top.
    expect(pts[0].y).toBeCloseTo(32 * 0.9, 5);
    expect(pts[1].y).toBeCloseTo(32 * 0.2, 5);
  });
});

describe('the rollover cell says when and how long (🎯T670)', () => {
  it('reads in minutes, then hours, then days (🎯T672)', () => {
    expect(remainingSpan(22 * 3600_000)).toBe('22h');
    expect(remainingSpan(89 * 60_000)).toBe('89m');
    expect(remainingSpan(90 * 60_000)).toBe('2h');
    expect(remainingSpan(21.6 * 3600_000)).toBe('22h');
    expect(remainingSpan(30_000)).toBe('1m');
    expect(remainingSpan(0)).toBe('0m');
    expect(remainingSpan(-5000)).toBe('0m');
    // Three days is where hours stop being readable: 622h says nothing.
    expect(remainingSpan(71 * 3600_000)).toBe('71h');
    expect(remainingSpan(72 * 3600_000)).toBe('3d');
    expect(remainingSpan(622 * 3600_000)).toBe('26d');
    expect(remainingSpan(6.3 * 24 * 3600_000)).toBe('6d');
  });

  it('reads as a moment and a span', () => {
    expect(rolloverCell(hoursOut(22), NOW, 'UTC')).toBe('Mon 22:00 22h');
  });
});

describe('the tip leads with usage (🎯T670)', () => {
  const fleet = [
    {
      provider: 'claude',
      available: true,
      windows: [{ name: 'weekly', remaining_percent: 41, used_percent: 59, resets_at: hoursOut(22) }],
    },
  ] as unknown as TickerGroup[];

  it('shows usage, rollover and burn — available and time-left are gone', () => {
    const { container } = render(<PlanTipTable groups={fleet} nowMs={NOW} timeZone="UTC" />);
    const labels = [...container.querySelectorAll('th[scope="row"]')].map((e) => e.textContent);
    expect(labels).toEqual(['usage', 'rollover', 'burn']);
    expect(container.querySelector('td.plan-avail')?.textContent).toBe('59%');
    expect(container.textContent).toContain('Mon 22:00 22h');
    expect(container.textContent).not.toMatch(/available|time left/i);
  });
});
