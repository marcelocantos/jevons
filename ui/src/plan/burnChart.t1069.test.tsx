// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BurnChart } from './BurnChart';
import { BURN_MARK_OK_CLASS, currentMarkClass } from './burnGeom';
import { CLASS_HOT } from './pace';
import type { PlanWindow } from './tickerGroups';

// 🎯T1069: the current-value dot wears the band of the sample it sits on,
// the band the line ends in. With an owner override the window's own band
// is the forced one (🎯T948) — Cursor on 2026-10-12 served `exhausted`
// while its samples read `ok`, and the dot came out red on a green line.

const start = Date.parse('2026-10-01T00:00:00Z');
const day = 24 * 3600 * 1000;
const iso = (ms: number) => new Date(ms).toISOString();

function month(band: string, history: PlanWindow['history']): PlanWindow {
  return {
    name: 'monthly',
    band,
    remaining_percent: 8,
    resets_at: iso(start + 30 * day),
    limit_window_seconds: 30 * 24 * 3600,
    history,
  } as PlanWindow;
}

describe('the current-value mark follows its own sample (🎯T1069)', () => {
  const greenEnd = [
    { at: iso(start + 2 * day), remaining_percent: 60, band: 'hot' },
    { at: iso(start + 9 * day), remaining_percent: 10, band: 'ok' },
  ];

  it('an overridden window with an ok newest sample paints the dot ok, not the override', () => {
    const w = month('exhausted', greenEnd);
    expect(currentMarkClass(w)).toBe(BURN_MARK_OK_CLASS);
    const { container } = render(<BurnChart window={w} />);
    const dot = container.querySelector('.plan-burn-now');
    expect(dot?.getAttribute('class')).toContain(BURN_MARK_OK_CLASS);
    expect(dot?.getAttribute('class')).not.toContain(CLASS_HOT);
  });

  it('a hot newest sample paints the dot hot', () => {
    const w = month('ok', [greenEnd[1], { at: iso(start + 10 * day), remaining_percent: 5, band: 'hot' }]);
    expect(currentMarkClass(w)).toBe(CLASS_HOT);
  });

  it('samples without bands leave the dot in the cell colour', () => {
    const w = month('ok', [{ at: iso(start + 9 * day), remaining_percent: 10 }]);
    expect(currentMarkClass(w)).toBeNull();
    const { container } = render(<BurnChart window={w} />);
    expect(container.querySelector('.plan-burn-now')?.getAttribute('class')).toBe('plan-burn-now');
  });
});
