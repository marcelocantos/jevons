// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { windowAbbrev } from './companyMark';
import { tickerGroups } from './tickerGroups';
import { PlanTipTable, tipColumns, windowLabel } from './tipTable';
import type { TickerGroup } from './tickerGroups';

// 🎯T682: Anthropic meters its premium model apart from the plan, so the
// Anthropic group carries a third bar after the week, labelled for the
// model, and the tip gets a column of its own.

const NOW = Date.parse('2026-09-20T12:00:00Z');

const SNAP = {
  backends: [
    {
      provider: 'claude',
      status: 'available',
      windows: [
        { name: 'weekly', remaining_percent: 33, used_percent: 67 },
        {
          name: 'weekly_model',
          model: 'Fable',
          remaining_percent: 0,
          used_percent: 100,
          resets_at: '2026-09-21T01:59:58Z',
        },
        { name: 'session', remaining_percent: 90, used_percent: 10 },
      ],
    },
  ],
};

describe('a per-model window is its own bar (🎯T682)', () => {
  it('sits after the plan week, in the order the owner reads them', () => {
    const groups = tickerGroups(SNAP);
    expect(groups[0]?.windows.map((w) => w.name)).toEqual([
      'session',
      'weekly',
      'weekly_model',
    ]);
  });

  it('is labelled for its model, not for its period', () => {
    // Two weekly bars both labelled W would be indistinguishable.
    expect(windowAbbrev('weekly_model', 'Fable')).toBe('F');
    expect(windowAbbrev('weekly_model', 'Opus')).toBe('O');
    expect(windowAbbrev('weekly')).toBe('W');
    expect(windowAbbrev('session')).toBe('S');
    // No model label falls back to the window's own name.
    expect(windowAbbrev('weekly_model', '')).toBe('W');
  });

  it('carries its own usage, distinct from the plan week', () => {
    const groups = tickerGroups(SNAP);
    const model = groups[0]!.windows.find((w) => w.name === 'weekly_model')!;
    const week = groups[0]!.windows.find((w) => w.name === 'weekly')!;
    expect(model.used_percent).toBe(100);
    expect(week.used_percent).toBe(67);
    expect(model.model).toBe('Fable');
    expect(week.model).toBeUndefined();
  });
});

describe('the tip gains a column for the model (🎯T682)', () => {
  it('heads the column with the model name', () => {
    expect(windowLabel('weekly_model', 'Fable')).toBe('Fable');
    expect(windowLabel('weekly')).toBe('week');
    const cols = tipColumns(tickerGroups(SNAP) as TickerGroup[]);
    expect(cols[0]?.columns.map((c) => c.label)).toEqual(['session', 'week', 'Fable']);
  });

  it('renders the model column with its own usage and rollover', () => {
    const { container } = render(
      <PlanTipTable groups={tickerGroups(SNAP) as TickerGroup[]} nowMs={NOW} timeZone="UTC" />,
    );
    const cell = container.querySelector<HTMLElement>('td[data-window="Fable"].plan-avail');
    expect(cell?.textContent).toBe('100%');
    // The plan's own week is still reported separately and differently.
    const week = container.querySelector<HTMLElement>('td[data-window="week"].plan-avail');
    expect(week?.textContent).toBe('67%');
    expect(container.textContent).toContain('Fable');
  });
});
