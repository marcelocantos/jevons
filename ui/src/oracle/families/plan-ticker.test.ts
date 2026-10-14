// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render } from '@testing-library/react';
import { expect, it } from 'vitest';
import { PlanUsageBar } from '../../components/PlanUsageBar';
import { nativeTitleForbidden } from '../../components/InstantTip';
import {
  CLASS_AHEAD,
  CLASS_HOT,
  CLASS_LOCKED,
  CLASS_UNDER,
  formatWindow,
  leftoverHoverName,
  PACE_AHEAD,
  PACE_HOT,
  PACE_LOCKED,
  PACE_OK,
  PACE_UNDER,
} from '../../plan/pace';
import { tickerGroups, tickerTipBody } from '../../plan/tickerGroups';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

function withQuery(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
  qc.setQueryData(['plan-usage'], { windows: [
    { provider: 'claude', name: 'weekly', remaining_percent: 36, resets_at: '2026-09-07T00:00:00Z' },
  ] });
  return createElement(QueryClientProvider, { client: qc }, node);
}

describeOracle(family('plan-ticker'), () => {
  // 🎯T1072: the cockpit classifies nothing. Each of these windows carries
  // numbers the deleted local model would have painted differently; the
  // served band is what paints. The verdicts themselves are pinned in Go
  // (internal/planusage, Claudia's Spend Pressure Map).
  const NOW = Date.parse('2026-09-12T12:00:00Z');
  const weekly = (used: number, timeLeftPct: number, band: string) => ({
    name: 'weekly',
    used_percent: used,
    remaining_percent: 100 - used,
    resets_at: new Date(NOW + (timeLeftPct / 100) * 7 * 24 * 3600 * 1000).toISOString(),
    limit_window_seconds: 7 * 24 * 3600,
    band,
  });

  itOracle('T390.1.1', 'served leftover paints blue (under) and purple (locked), hover in the daemon\'s words', () => {
    expect(formatWindow(weekly(0, 50, 'under'), NOW).className.split(' ')).toContain(CLASS_UNDER);
    expect(formatWindow(weekly(0, 40, 'locked'), NOW).className.split(' ')).toContain(CLASS_LOCKED);
    // Late-month Cursor, 2026-10-14: 8.3% left, 2.2% of the window to go.
    const cursor = { ...weekly(91.66, 2.24, 'under'), name: 'monthly' };
    expect(formatWindow(cursor, NOW).pace).toBe(PACE_UNDER);
    expect(leftoverHoverName(PACE_UNDER)).toBe('under pace');
    expect(leftoverHoverName(PACE_LOCKED)).toBe('surplus locked in');
  });

  itOracle('T390.1.6.2', 'no elapsed cutoff — Codex 26% used at ~5% elapsed paints the served hot', () => {
    const painted = formatWindow(weekly(26, 95.1, 'hot'), NOW);
    expect(painted.pace).toBe(PACE_HOT);
    expect(painted.className.split(' ')).toContain(CLASS_HOT);
  });

  itOracle('T390.1.6.1', 'week-start 9%/5.6% paints the served band, not a local ratio', () => {
    expect(formatWindow(weekly(9, 94.4, 'ok'), NOW).pace).toBe(PACE_OK);
    expect(formatWindow(weekly(9, 94.4, 'ahead'), NOW).className.split(' ')).toContain(CLASS_AHEAD);
    expect(formatWindow(weekly(9, 94.4, 'ahead'), NOW).pace).toBe(PACE_AHEAD);
  });

  itOracle('T390.1.6', 'a window with no served band paints no pace colour', () => {
    const painted = formatWindow({ ...weekly(80, 50, ''), band: undefined }, NOW);
    expect(painted.pace).toBe('');
    expect(painted.className).not.toMatch(/plan-(hot|ahead|under|locked)/);
  });

  itOracle('T390', 'on-pace mid-window is green; hover is InstantTip remaining/rollover, not title=', () => {
    expect(formatWindow(weekly(50, 50, 'ok'), NOW).pace).toBe(PACE_OK);
    const groups = tickerGroups({
      windows: [
        { provider: 'claude', name: 'weekly', remaining_percent: 36, resets_at: '2026-08-27T00:00:00Z' },
      ],
    });
    const body = tickerTipBody(groups);
    expect(body).toMatch(/36% remaining/);
    expect(body).toMatch(/rollover/);

    const { container } = render(withQuery(createElement(PlanUsageBar)));
    const ticker = container.querySelector('#plan-ticker');
    expect(ticker).toBeTruthy();
    expect(nativeTitleForbidden(ticker)).toBe(true);
    const host = container.querySelector('[data-instant-tip-host]');
    expect(host).toBeTruthy();
    fireEvent.pointerEnter(host!);
    const tip = container.querySelector('.instant-tip-show');
    expect(tip).toBeTruthy();
    // 🎯T588.1 turned the hover into a grid, so the measure is a row label
    // rather than a sentence, and 🎯T670 made that measure usage — the
    // complement T390 asked for, in the direction every harness reports.
    // Assert the information — label, an actual percentage, and the
    // rollover row — rather than either version's wording.
    const text = tip?.textContent || '';
    expect(text).toMatch(/usage/i);
    expect(text).toMatch(/\d+%/);
    expect(text).toMatch(/rollover/i);
  });

  itOracle('T175', 'plan-usage hover is InstantTip-class, not a delayed native title=', () => {
    const { container } = render(withQuery(createElement(PlanUsageBar)));
    const ticker = container.querySelector('#plan-ticker');
    expect(nativeTitleForbidden(ticker)).toBe(true);
    fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
    expect(container.querySelector('.instant-tip-show')).toBeTruthy();
  });

  itOracle('T390.1.3', 'exhausted Claude still paints the boxed session+weekly pair', () => {
    // A *published* zero is a reading like any other and keeps its pair of
    // bars. 🎯T681 removed this oracle's other half: a 429 from the usage
    // endpoint used to be synthesised into this same shape, which painted
    // a failed reading as a spent plan.
    const groups = tickerGroups({
      backends: [
        {
          provider: 'claude',
          status: 'available',
          windows: [
            { name: 'session', remaining_percent: 0, used_percent: 100 },
            { name: 'weekly', remaining_percent: 0, used_percent: 100 },
          ],
        },
      ],
    });
    expect(groups[0]?.available).toBe(true);
    expect(groups[0]?.windows.map((w) => w.name)).toEqual(['session', 'weekly']);
    expect(groups[0]?.windows.map((w) => w.remaining_percent)).toEqual([0, 0]);
  });

  it('T550: cursor monthly window in tickerGroups; codex 7d stays weekly', () => {
    const cursor = tickerGroups({
      backends: [
        {
          provider: 'cursor',
          status: 'available',
          windows: [
            {
              name: 'monthly',
              remaining_percent: 84,
              used_percent: 16,
              resets_at: '2026-09-14T00:00:00Z',
              limit_window_seconds: 31 * 24 * 3600,
            },
          ],
        },
      ],
    });
    expect(cursor[0]?.windows.map((w) => w.name)).toEqual(['monthly']);
    expect(tickerTipBody(cursor)).toMatch(/84% remaining/);
    expect(tickerTipBody(cursor)).toMatch(/rollover/);

    const codex = tickerGroups({
      backends: [
        {
          provider: 'codex',
          status: 'available',
          windows: [{ name: 'weekly', remaining_percent: 50, resets_at: '2026-08-29T00:00:00Z' }],
        },
      ],
    });
    expect(codex[0]?.windows[0]?.name).toBe('weekly');
  });

  itOracle('T117', 'cost ticker is honest or absent — no invented zero rates', () => {
    const groups = tickerGroups({
      backends: [{ provider: 'cursor', status: 'unavailable', reason: 'no plan-remaining published' }],
    });
    const cursor = groups.find((g) => g.provider === 'cursor');
    expect(cursor?.windows).toEqual([]);
    expect(tickerTipBody(groups)).not.toMatch(/0% remaining/);
  });

});
