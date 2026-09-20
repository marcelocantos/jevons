// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render } from '@testing-library/react';
import { expect, it } from 'vitest';
import { PlanUsageBar } from '../../components/PlanUsageBar';
import { nativeTitleForbidden } from '../../components/InstantTip';
import {
  classifyPace,
  leftoverHoverName,
  overspendStops,
  paceColor,
  PACE_AHEAD,
  PACE_COLOR_AHEAD,
  PACE_COLOR_HOT,
  PACE_COLOR_OK,
  PACE_COLOR_UNDER,
  PACE_HOT,
  PACE_LOCKED,
  PACE_OK,
  PACE_UNDER,
  PACE_UNDER_WASTE,
} from '../../plan/pace';
import { parseCssColor } from '../../plan/hsv';
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
  itOracle('T390.1.2', 'paceColor HSV-lerps; midpoints are not named class colours', () => {
    const { a, b, c } = overspendStops();
    const midAB = paceColor((a + b) / 2);
    const midBC = paceColor((b + c) / 2);
    const midUnder = paceColor(0.5, { continuation: PACE_UNDER_WASTE / 2, locked: 0 });
    const named = [PACE_COLOR_OK, PACE_COLOR_AHEAD, PACE_COLOR_HOT, PACE_COLOR_UNDER];
    for (const sample of [midAB, midBC, midUnder]) {
      const got = parseCssColor(sample);
      for (const hex of named) {
        const stop = parseCssColor(hex);
        const dist = got && stop
          ? Math.abs(got.r - stop.r) + Math.abs(got.g - stop.g) + Math.abs(got.b - stop.b)
          : 999;
        expect(dist).toBeGreaterThan(30);
      }
    }
    expect(classifyPace(50, 50, 50)).toBe(PACE_OK);
  });

  itOracle('T390.1.1', 'weekly continuation leftover is blue, locked surplus is purple; session is exempt', () => {
    expect(classifyPace(0, 100, 81, 'weekly')).toBe(PACE_UNDER);
    expect(classifyPace(0, 100, 50, 'weekly')).toBe(PACE_LOCKED);
    expect(classifyPace(0, 100, 97, 'weekly')).toBe(PACE_OK);
    expect(classifyPace(0, 100, 50, 'session')).toBe(PACE_OK);
    expect(classifyPace(64, 36, 9.5, 'monthly')).toBe(PACE_LOCKED);
    expect(leftoverHoverName(PACE_UNDER)).toBe('continuation leftover');
    expect(leftoverHoverName(PACE_LOCKED)).toBe('already-unrecoverable at 1.5×');
  });

  itOracle('T390.1.6.2', 'no elapsed cutoff — Codex 26% used at ~5% elapsed is hot', () => {
    expect(classifyPace(26, 74, 95.1, 'weekly')).toBe(PACE_HOT);
  });

  itOracle('T390.1.6.1', 'week-start 9%/5.6% damps to ahead, not hot', () => {
    const weekStart = classifyPace(9, 91, 94.4, 'weekly');
    expect(weekStart === PACE_OK || weekStart === PACE_AHEAD).toBe(true);
  });

  itOracle('T390', 'on-pace mid-window is green; hover is InstantTip remaining/rollover, not title=', () => {
    expect(classifyPace(50, 50, 50, 'weekly')).toBe(PACE_OK);
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

  itOracle.skip('T390.1.6', 'ticker vertices come from the served thresholds document', 'served /api/plan-usage/thresholds — daily path');
});
