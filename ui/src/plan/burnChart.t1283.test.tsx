// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BurnChart } from './BurnChart';
import { BURN_HEIGHT, BURN_WIDTH, paceLine } from './burnGeom';
import type { PlanWindow } from './tickerGroups';

// 🎯T1283: every burn chart with a known period carries the on-track line,
// 0% used at the window's start (bottom-left) to 100% at its reset (top-right).

const start = Date.parse('2026-10-01T00:00:00Z');
const day = 24 * 3600 * 1000;
const iso = (ms: number) => new Date(ms).toISOString();

function week(extra: Partial<PlanWindow> = {}): PlanWindow {
  return {
    name: 'weekly',
    band: 'ok',
    remaining_percent: 60,
    resets_at: iso(start + 7 * day),
    limit_window_seconds: 7 * 24 * 3600,
    history: [
      { at: iso(start + day), remaining_percent: 90, band: 'ok' },
      { at: iso(start + 3 * day), remaining_percent: 60, band: 'ok' },
    ],
    ...extra,
  } as PlanWindow;
}

describe('the on-track line (🎯T1283)', () => {
  it('runs from the bottom-left corner to the top-right corner of the plot', () => {
    expect(paceLine(week())).toBe(`M0,${BURN_HEIGHT} L${BURN_WIDTH},0`);
  });

  it('is drawn behind the reading, and with no history at all', () => {
    const { container } = render(<BurnChart window={week()} />);
    const paths = [...container.querySelectorAll('path')].map((p) => p.getAttribute('class'));
    expect(paths.indexOf('plan-burn-pace')).toBeGreaterThanOrEqual(0);
    expect(paths.indexOf('plan-burn-pace')).toBeLessThan(paths.indexOf('plan-burn-line'));
    const empty = render(<BurnChart window={week({ history: [] })} />);
    expect(empty.container.querySelector('path.plan-burn-pace')?.getAttribute('d')).toBe(paceLine(week()));
  });

  it('is absent when the window has no known period, rather than guessed', () => {
    const noReset = week({ resets_at: undefined });
    expect(paceLine(noReset)).toBeNull();
    const { container } = render(<BurnChart window={noReset} />);
    expect(container.querySelector('path.plan-burn-pace')).toBeNull();
  });
});
